package detection

import (
	"strings"

	"go.minekube.com/gate/pkg/edition/java/forge/modinfo"
)

// ─── Action trigger ───────────────────────────────────────────────────────────

// ForgeActionTrigger represents a pending action output from the Forge detector.
// It carries the human-readable display name (used in alert placeholders) and the
// list of action IDs (keys into ActionsConfig.Actions) that should be executed.
//
// Callers queue these as closures via DetectedPlayer.QueuePendingAction rather
// than executing them inline — execution happens at first server join (Task 14).
type ForgeActionTrigger struct {
	// Name is the human-readable identifier for the trigger (e.g. "Forge", "forgewurst").
	Name string
	// ActionIDs is the list of action IDs from forge.toml to execute.
	ActionIDs []string
}

// ─── Built-in channel namespaces ──────────────────────────────────────────────

// builtinNamespaces mirrors ForgeChannelParser.BUILTIN_NAMESPACES.
// These namespaces appear in minecraft:register payloads but are NOT user mods.
var builtinNamespaces = map[string]struct{}{
	"minecraft": {},
	"neoforge":  {},
	"forge":     {},
	"fml":       {},
	"c":         {}, // NeoForge common namespace
	"fabric":    {}, // Fabric loader (detected separately)
}

// ─── Brand parsing (REGISTER parser fallback) ─────────────────────────────────

// ParseClientTypeFromBrand inspects the minecraft:brand message to determine
// whether the client is Forge or NeoForge.
//
// This mirrors ForgeChannelParser.parseClientType in Java and is the REGISTER
// parser fallback path. The primary path is PlayerModInfoEvent (ProcessForgeModInfo).
//
// Returns (ForgeClientForge|ForgeClientNeoForge, true) on a match; (0, false)
// when the brand does not indicate a Forge-based client.
func ParseClientTypeFromBrand(brand string) (ForgeClientType, bool) {
	if brand == "" {
		return 0, false
	}
	lower := strings.ToLower(strings.TrimSpace(brand))
	if strings.Contains(lower, "neoforge") {
		return ForgeClientNeoForge, true
	}
	if strings.Contains(lower, "forge") || strings.Contains(lower, "fml") {
		return ForgeClientForge, true
	}
	return 0, false
}

// ContainsFabricChannels reports whether a REGISTER payload advertises Fabric
// loader channels. This deliberately runs before the Forge mod namespace
// filtering, because Fabric is evidence for brand-spoof detection.
func ContainsFabricChannels(message string) bool {
	if message == "" {
		return false
	}
	parts := strings.Fields(message)
	if strings.ContainsRune(message, 0) {
		parts = strings.Split(message, "\x00")
	}
	for _, part := range parts {
		channel := strings.ToLower(strings.TrimSpace(part))
		if strings.HasPrefix(channel, "fabric-") || strings.HasPrefix(channel, "fabric:") || strings.HasPrefix(channel, "fabricloader") {
			return true
		}
	}
	return false
}

// ─── Channel REGISTER parser (fallback mod list) ──────────────────────────────

// ParseModsFromChannels extracts Forge mod IDs from a list of channel identifiers
// (as returned by PlayerChannelRegisterEvent.Channels() or a pre-parsed string list).
//
// The minecraft:register payload carries a null-separated list of channel IDs in
// "namespace:path" format. The namespace reveals which mod registered the channel.
// Built-in and Fabric namespaces are excluded (see builtinNamespaces).
//
// This mirrors ForgeChannelParser.parseRegisteredChannels in Java.
func ParseModsFromChannels(channels []string) []ForgeModInfo {
	if len(channels) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(channels))
	var mods []ForgeModInfo

	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}

		colonIdx := strings.IndexByte(ch, ':')
		if colonIdx <= 0 {
			continue
		}

		ns := strings.ToLower(ch[:colonIdx])

		// Skip built-in Forge/NeoForge infrastructure namespaces.
		if _, builtin := builtinNamespaces[ns]; builtin {
			continue
		}

		// Skip Fabric API module namespaces (fabric-*, fabricloader*).
		if strings.HasPrefix(ns, "fabric-") || strings.HasPrefix(ns, "fabricloader") {
			continue
		}

		// Each namespace maps to exactly one mod entry.
		if _, dup := seen[ns]; dup {
			continue
		}
		seen[ns] = struct{}{}
		mods = append(mods, ForgeModInfo{ModID: ns})
	}

	return mods
}

// ParseModsFromRegisterPayload parses a raw minecraft:register payload (null-byte
// separated channel list) and returns the inferred Forge mod list.
//
// This is the lowest-level fallback: Gate may expose the raw bytes when the
// ChannelIdentifier string representation is unavailable. Mirrors
// ForgeChannelParser.parseRegisteredChannels(byte[]) in Java.
func ParseModsFromRegisterPayload(payload []byte) []ForgeModInfo {
	if len(payload) == 0 {
		return nil
	}
	// Split on null bytes; fall through to ParseModsFromChannels for filtering.
	raw := string(payload)
	var parts []string
	if strings.ContainsRune(raw, 0) {
		parts = strings.Split(raw, "\x00")
	} else {
		parts = strings.Fields(raw)
	}
	return ParseModsFromChannels(parts)
}

// ─── Primary path: PlayerModInfoEvent ─────────────────────────────────────────

// ProcessForgeModInfo is the primary Forge detection path.
// It is called when Gate fires a PlayerModInfoEvent (native Forge handshake).
//
// It determines the client type from the ModInfo.Type field, records mods and
// client type on the player state, and returns the action triggers to enqueue.
// The caller (event handler, Task 14) should queue each trigger's ActionIDs as
// pending actions on the player.
//
// Returns nil (no triggers) when forge detection is disabled in cfg.
func ProcessForgeModInfo(player *DetectedPlayer, info modinfo.ModInfo, cfg ForgeConfig) []ForgeActionTrigger {
	if player == nil || !cfg.Enabled {
		return nil
	}

	// Determine client type from the FML type string.
	// "FML2" and "FML" → Forge; "NEOFORGE" → NeoForge.
	clientType := forgeClientTypeFromModInfoType(info.Type)

	// Convert modinfo.Mod slice to our ForgeModInfo slice.
	mods := make([]ForgeModInfo, 0, len(info.Mods))
	for _, m := range info.Mods {
		if m.ID == "" {
			continue
		}
		mods = append(mods, ForgeModInfo{ModID: strings.ToLower(m.ID), Version: m.Version})
	}

	// Store mods in player state (additive — matches Java AddForgeMods semantics).
	if len(mods) > 0 {
		player.AddForgeMods(mods)
	}

	var triggers []ForgeActionTrigger

	// Record client type + collect client-level action triggers.
	if ct, detected := clientTypeTrigger(player, clientType, cfg); detected {
		if len(ct.ActionIDs) > 0 {
			triggers = append(triggers, ct)
		}
	}

	// Apply per-mod blacklist / mod_actions triggers.
	modTriggers := applyModActions(mods, cfg)
	triggers = append(triggers, modTriggers...)

	return triggers
}

// ─── Handshake processor helpers ─────────────────────────────────────────────

// ProcessClientType handles a ForgeClientType detected via any signal
// (brand string, mod info type). It records the client type on the player
// and returns the appropriate action trigger if conditions are met.
//
// Java mapping: ForgeHandshakeProcessor.processClientType
func ProcessClientType(player *DetectedPlayer, clientType ForgeClientType, cfg ForgeConfig) []ForgeActionTrigger {
	if player == nil || !cfg.Enabled {
		return nil
	}

	var triggers []ForgeActionTrigger
	if ct, ok := clientTypeTrigger(player, clientType, cfg); ok {
		if len(ct.ActionIDs) > 0 {
			triggers = append(triggers, ct)
		}
	}
	return triggers
}

// ProcessMods handles a list of detected Forge mods (from REGISTER parser
// fallback or mod info). It records mods on the player state, applies
// per-mod actions from mod_actions, and returns triggers for blacklisted mods.
//
// Java mapping: ForgeHandshakeProcessor.processMods
func ProcessMods(player *DetectedPlayer, mods []ForgeModInfo, cfg ForgeConfig) []ForgeActionTrigger {
	if player == nil || len(mods) == 0 || !cfg.Enabled {
		return nil
	}

	// Build a set of previously-known mod IDs to skip duplicates.
	existing := make(map[string]struct{})
	for _, m := range player.ForgeMods() {
		existing[strings.ToLower(m.ModID)] = struct{}{}
	}

	// Collect only the new (not previously seen) mods.
	var newMods []ForgeModInfo
	for _, m := range mods {
		norm := strings.ToLower(m.ModID)
		if norm == "" {
			continue
		}
		if _, seen := existing[norm]; seen {
			continue
		}
		newMods = append(newMods, ForgeModInfo{ModID: norm, Version: m.Version})
	}

	if len(newMods) == 0 {
		return nil
	}

	player.AddForgeMods(newMods)
	return applyModActions(newMods, cfg)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// clientTypeTrigger sets the client type on the player (if not already set) and
// returns (trigger, true) when:
//   - the config marks that type (mark_forge / mark_neoforge), AND
//   - the corresponding generic check has not been triggered before.
//
// Returns (zero, false) when no trigger is warranted.
func clientTypeTrigger(player *DetectedPlayer, clientType ForgeClientType, cfg ForgeConfig) (ForgeActionTrigger, bool) {
	switch clientType {
	case ForgeClientForge:
		player.SetForgeClientType(ForgeClientForge)
		if cfg.Settings.MarkForge && !player.HasGenericCheck("forge") {
			player.AddGenericCheck("forge")
			return ForgeActionTrigger{Name: "Forge", ActionIDs: cfg.Actions.Forge}, true
		}
	case ForgeClientNeoForge:
		player.SetForgeClientType(ForgeClientNeoForge)
		if cfg.Settings.MarkNeoForge && !player.HasGenericCheck("neoforge") {
			player.AddGenericCheck("neoforge")
			return ForgeActionTrigger{Name: "NeoForge", ActionIDs: cfg.Actions.NeoForge}, true
		}
	}
	return ForgeActionTrigger{}, false
}

// applyModActions returns ForgeActionTrigger entries for mods that have
// mod_actions configured in forge.toml.
//
// Blacklisted mods are always included in the display name context; only mods
// with explicitly configured mod_actions fire triggers here.
func applyModActions(mods []ForgeModInfo, cfg ForgeConfig) []ForgeActionTrigger {
	var triggers []ForgeActionTrigger
	for _, mod := range mods {
		norm := strings.ToLower(mod.ModID)
		if norm == "" {
			continue
		}
		actions, ok := cfg.ModActions[norm]
		if !ok || len(actions) == 0 {
			continue
		}
		triggers = append(triggers, ForgeActionTrigger{
			Name:      formatMod(mod, cfg),
			ActionIDs: actions,
		})
	}
	return triggers
}

// formatMod returns the display-ready mod ID with an optional MiniMessage color
// prefix from the whitelisted/blacklisted category config.
func formatMod(mod ForgeModInfo, cfg ForgeConfig) string {
	norm := strings.ToLower(mod.ModID)
	prefix := ""

	blacklisted := isBlacklisted(norm, cfg)
	whitelisted := isWhitelisted(norm, cfg)

	switch {
	case blacklisted:
		prefix = cfg.Category.Blacklisted.Color
	case whitelisted:
		prefix = cfg.Category.Whitelisted.Color
	}

	name := norm
	if cfg.Settings.ShowModVersions && mod.Version != "" {
		name = name + " " + mod.Version
	}
	return prefix + name
}

// isBlacklisted reports whether the (already-normalized) mod ID appears in
// forge.toml [category.blacklisted.mods].
func isBlacklisted(normID string, cfg ForgeConfig) bool {
	for _, id := range cfg.Category.Blacklisted.Mods {
		if strings.ToLower(id) == normID {
			return true
		}
	}
	return false
}

// isWhitelisted reports whether the (already-normalized) mod ID appears in
// forge.toml [category.whitelisted.mods].
func isWhitelisted(normID string, cfg ForgeConfig) bool {
	for _, id := range cfg.Category.Whitelisted.Mods {
		if strings.ToLower(id) == normID {
			return true
		}
	}
	return false
}

// IsForgeBlacklisted reports whether the given mod ID (case-insensitive) is in
// the configured blacklist. Exported for use by display/alert code (Task 14).
func IsForgeBlacklisted(modID string, cfg ForgeConfig) bool {
	return isBlacklisted(strings.ToLower(modID), cfg)
}

// IsForgeWhitelisted reports whether the given mod ID (case-insensitive) is in
// the configured whitelist.
func IsForgeWhitelisted(modID string, cfg ForgeConfig) bool {
	return isWhitelisted(strings.ToLower(modID), cfg)
}

// forgeClientTypeFromModInfoType maps the FML type string from PlayerModInfoEvent
// to our ForgeClientType constant.
//
// Known FML type strings:
//
//	"FML"   → legacy Forge (1.12 and earlier)
//	"FML2"  → modern Forge (1.13+)
//	"FML3"  → Forge 1.20.1
//	"NEOFORGE" → NeoForge
func forgeClientTypeFromModInfoType(fmlType string) ForgeClientType {
	upper := strings.ToUpper(strings.TrimSpace(fmlType))
	if upper == "NEOFORGE" {
		return ForgeClientNeoForge
	}
	// FML, FML2, FML3 → all Forge
	if strings.HasPrefix(upper, "FML") {
		return ForgeClientForge
	}
	// Default: treat unknown types as Forge (safe fallback).
	return ForgeClientForge
}
