package detection

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.minekube.com/gate/pkg/edition/java/forge/modinfo"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// minimalForgeConfig builds a ForgeConfig with enabled=true and the standard
// blacklist/whitelist from forge.toml (hardcoded to avoid submodule dep here).
func minimalForgeConfig() ForgeConfig {
	return ForgeConfig{
		Enabled: true,
		Settings: ForgeSettings{
			MarkForge:    true,
			MarkNeoForge: true,
		},
		Actions: ForgeActions{
			Forge:    []string{"alert"},
			NeoForge: []string{"alert"},
		},
		ModActions: map[string][]string{
			"forgewurst": {"alert", "kick"},
			"forgehax":   {"alert"},
			"wurst":      {"alert", "kick"},
		},
		Category: ForgeCategory{
			Blacklisted: ForgeModCategory{
				Color: "<red>",
				Mods:  []string{"forgewurst", "forgehax", "wurst", "baritone", "meteor", "aristois", "impact"},
			},
			Whitelisted: ForgeModCategory{
				Color: "<green>",
				Mods:  []string{"betterfoliage", "sodium", "iris", "optifine", "rubidium", "oculus", "embeddium"},
			},
		},
	}
}

func newTestPlayer() *DetectedPlayer {
	return newDetectedPlayer(uuid.New())
}

// ---------------------------------------------------------------------------
// TestForgeBlacklist — QA target
// ---------------------------------------------------------------------------

// TestForgeBlacklist verifies that when a player has a blacklisted mod, the
// configured mod_actions are returned as ForgeActionTrigger entries.
func TestForgeBlacklist(t *testing.T) {
	t.Run("BlacklistedModFromModInfo", testBlacklistedModFromModInfo)
	t.Run("BlacklistedModFromProcessMods", testBlacklistedModFromProcessMods)
	t.Run("BlacklistedModTriggersActionIDs", testBlacklistedModTriggersActionIDs)
	t.Run("WhitelistedModNoTrigger", testWhitelistedModNoTrigger)
	t.Run("IsForgeBlacklistedHelper", testIsForgeBlacklistedHelper)
	t.Run("IsForgeWhitelistedHelper", testIsForgeWhitelistedHelper)
	t.Run("BlacklistCaseInsensitive", testBlacklistCaseInsensitive)
	t.Run("FormatModBlacklistedColor", testFormatModBlacklistedColor)
	t.Run("FormatModWhitelistedColor", testFormatModWhitelistedColor)
	t.Run("FormatModVersionIncluded", testFormatModVersionIncluded)
	t.Run("FormatModVersionExcluded", testFormatModVersionExcluded)
}

func testBlacklistedModFromModInfo(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	info := modinfo.ModInfo{
		Type: "FML2",
		Mods: []modinfo.Mod{
			{ID: "forgewurst", Version: "1.0"},
			{ID: "sodium", Version: "2.0"},
		},
	}

	triggers := ProcessForgeModInfo(player, info, cfg)

	// Should have: Forge client trigger + forgewurst mod_actions trigger (sodium has none).
	if !hasTriggerForName(triggers, "Forge") {
		t.Error("expected Forge client-level trigger")
	}

	// forgewurst is blacklisted AND has mod_actions.
	forgeWurstTrigger := findTriggerForActionIDs(triggers, "kick")
	if forgeWurstTrigger == nil {
		t.Error("expected forgewurst mod trigger with 'kick' action")
	}

	// sodium is whitelisted but has NO mod_actions — no trigger expected.
	sodiumTrigger := findTriggerContaining(triggers, "sodium")
	if sodiumTrigger != nil {
		t.Errorf("unexpected trigger for sodium (no mod_actions): %+v", sodiumTrigger)
	}

	// Verify player state.
	if !player.HasForgeMod("forgewurst") {
		t.Error("forgewurst should be recorded in player state")
	}
	if !player.HasForgeMod("sodium") {
		t.Error("sodium should be recorded in player state")
	}
}

func testBlacklistedModFromProcessMods(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	mods := []ForgeModInfo{
		{ModID: "wurst", Version: "2.0"},
	}

	triggers := ProcessMods(player, mods, cfg)

	if len(triggers) == 0 {
		t.Fatal("expected at least one trigger for blacklisted mod 'wurst'")
	}

	// wurst has "alert" and "kick" in mod_actions.
	if !findTriggerHasAction(triggers, "kick") {
		t.Error("expected 'kick' action in triggers for wurst")
	}

	if !player.HasForgeMod("wurst") {
		t.Error("wurst should be recorded in player state")
	}
}

func testBlacklistedModTriggersActionIDs(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	mods := []ForgeModInfo{{ModID: "forgehax", Version: "1.0"}}
	triggers := ProcessMods(player, mods, cfg)

	if len(triggers) == 0 {
		t.Fatal("expected trigger for forgehax")
	}
	if !findTriggerHasAction(triggers, "alert") {
		t.Error("expected 'alert' action in forgehax trigger")
	}
	// forgehax has only "alert" (no "kick").
	if findTriggerHasAction(triggers, "kick") {
		t.Error("unexpected 'kick' action — forgehax mod_actions has only 'alert'")
	}
}

func testWhitelistedModNoTrigger(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	// sodium is whitelisted but has no mod_actions configured.
	mods := []ForgeModInfo{{ModID: "sodium", Version: "0.5"}}
	triggers := ProcessMods(player, mods, cfg)

	if len(triggers) != 0 {
		t.Errorf("expected no triggers for whitelisted mod with no mod_actions, got %d", len(triggers))
	}
}

func testIsForgeBlacklistedHelper(t *testing.T) {
	cfg := minimalForgeConfig()

	cases := []struct {
		modID       string
		wantBlisted bool
	}{
		{"forgewurst", true},
		{"FORGEWURST", true},
		{"ForgeWurst", true},
		{"wurst", true},
		{"baritone", true},
		{"sodium", false},
		{"optifine", false},
		{"unknownmod", false},
	}

	for _, tc := range cases {
		got := IsForgeBlacklisted(tc.modID, cfg)
		if got != tc.wantBlisted {
			t.Errorf("IsForgeBlacklisted(%q) = %v, want %v", tc.modID, got, tc.wantBlisted)
		}
	}
}

func testIsForgeWhitelistedHelper(t *testing.T) {
	cfg := minimalForgeConfig()

	cases := []struct {
		modID       string
		wantWlisted bool
	}{
		{"sodium", true},
		{"SODIUM", true},
		{"iris", true},
		{"optifine", true},
		{"forgewurst", false},
		{"unknownmod", false},
	}

	for _, tc := range cases {
		got := IsForgeWhitelisted(tc.modID, cfg)
		if got != tc.wantWlisted {
			t.Errorf("IsForgeWhitelisted(%q) = %v, want %v", tc.modID, got, tc.wantWlisted)
		}
	}
}

func testBlacklistCaseInsensitive(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	// Upper-case mod IDs should match blacklist (normalization is required).
	mods := []ForgeModInfo{{ModID: "FORGEWURST", Version: "1.0"}}
	triggers := ProcessMods(player, mods, cfg)

	if len(triggers) == 0 {
		t.Error("expected trigger for FORGEWURST (case-insensitive match)")
	}
}

func testFormatModBlacklistedColor(t *testing.T) {
	cfg := minimalForgeConfig()
	mod := ForgeModInfo{ModID: "forgewurst"}
	result := formatMod(mod, cfg)

	if !strings.HasPrefix(result, "<red>") {
		t.Errorf("expected blacklisted mod to have <red> prefix, got %q", result)
	}
}

func testFormatModWhitelistedColor(t *testing.T) {
	cfg := minimalForgeConfig()
	mod := ForgeModInfo{ModID: "sodium"}
	result := formatMod(mod, cfg)

	if !strings.HasPrefix(result, "<green>") {
		t.Errorf("expected whitelisted mod to have <green> prefix, got %q", result)
	}
}

func testFormatModVersionIncluded(t *testing.T) {
	cfg := minimalForgeConfig()
	cfg.Settings.ShowModVersions = true
	mod := ForgeModInfo{ModID: "sodium", Version: "0.5.1"}
	result := formatMod(mod, cfg)

	if !strings.Contains(result, "0.5.1") {
		t.Errorf("expected version in formatted mod string, got %q", result)
	}
}

func testFormatModVersionExcluded(t *testing.T) {
	cfg := minimalForgeConfig()
	cfg.Settings.ShowModVersions = false
	mod := ForgeModInfo{ModID: "sodium", Version: "0.5.1"}
	result := formatMod(mod, cfg)

	if strings.Contains(result, "0.5.1") {
		t.Errorf("expected version NOT in formatted mod string (show_mod_versions=false), got %q", result)
	}
}

// ---------------------------------------------------------------------------
// TestForgeRegisterFallback — QA target
// ---------------------------------------------------------------------------

// TestForgeRegisterFallback verifies the REGISTER channel parser path that
// infers Forge mod IDs from channel namespace prefixes.
func TestForgeRegisterFallback(t *testing.T) {
	t.Run("ParseBrandForge", testParseBrandForge)
	t.Run("ParseBrandNeoForge", testParseBrandNeoForge)
	t.Run("ParseBrandFML", testParseBrandFML)
	t.Run("ParseBrandVanilla", testParseBrandVanilla)
	t.Run("ParseBrandEmpty", testParseBrandEmpty)
	t.Run("ParseChannelsBasic", testParseChannelsBasic)
	t.Run("ParseChannelsBuiltinExcluded", testParseChannelsBuiltinExcluded)
	t.Run("ParseChannelsFabricExcluded", testParseChannelsFabricExcluded)
	t.Run("ParseChannelsDeduplicated", testParseChannelsDeduplicated)
	t.Run("ParseChannelsEmpty", testParseChannelsEmpty)
	t.Run("ParseChannelsMissingColon", testParseChannelsMissingColon)
	t.Run("ParseRawPayloadNullSeparated", testParseRawPayloadNullSeparated)
	t.Run("ParseRawPayloadEmpty", testParseRawPayloadEmpty)
	t.Run("RegisterFallbackIntegration", testRegisterFallbackIntegration)
	t.Run("ProcessModsDeduplication", testProcessModsDeduplication)
	t.Run("ProcessModsDisabledConfig", testProcessModsDisabledConfig)
	t.Run("ProcessClientTypeForge", testProcessClientTypeForge)
	t.Run("ProcessClientTypeNeoForge", testProcessClientTypeNeoForge)
	t.Run("ProcessClientTypeAlreadyMarked", testProcessClientTypeAlreadyMarked)
	t.Run("ProcessModInfoType_FML", testProcessModInfoType_FML)
	t.Run("ProcessModInfoType_FML2", testProcessModInfoType_FML2)
	t.Run("ProcessModInfoType_NeoForge", testProcessModInfoType_NeoForge)
	t.Run("ProcessModInfoDisabled", testProcessModInfoDisabled)
}

func testParseBrandForge(t *testing.T) {
	ct, ok := ParseClientTypeFromBrand("Forge")
	if !ok {
		t.Fatal("expected Forge to be detected")
	}
	if ct != ForgeClientForge {
		t.Errorf("expected ForgeClientForge, got %v", ct)
	}
}

func testParseBrandNeoForge(t *testing.T) {
	ct, ok := ParseClientTypeFromBrand("neoforge 1.20.2")
	if !ok {
		t.Fatal("expected NeoForge to be detected")
	}
	if ct != ForgeClientNeoForge {
		t.Errorf("expected ForgeClientNeoForge, got %v", ct)
	}
}

func testParseBrandFML(t *testing.T) {
	ct, ok := ParseClientTypeFromBrand("FML/1.20.1")
	if !ok {
		t.Fatal("expected FML brand to be detected as Forge")
	}
	if ct != ForgeClientForge {
		t.Errorf("expected ForgeClientForge for FML brand, got %v", ct)
	}
}

func testParseBrandVanilla(t *testing.T) {
	_, ok := ParseClientTypeFromBrand("vanilla")
	if ok {
		t.Error("vanilla brand should NOT be detected as Forge")
	}
}

func testParseBrandEmpty(t *testing.T) {
	_, ok := ParseClientTypeFromBrand("")
	if ok {
		t.Error("empty brand should return false")
	}
}

func TestContainsFabricChannels(t *testing.T) {
	for _, payload := range []string{"fabric:registry", "fabric-loader:main", "fabricloader\x00minecraft:brand"} {
		if !ContainsFabricChannels(payload) {
			t.Errorf("ContainsFabricChannels(%q) = false, want true", payload)
		}
	}
	if ContainsFabricChannels("minecraft:brand\x00forge:hand") {
		t.Error("non-Fabric channels incorrectly identified")
	}
}

func TestSpoofedBrandDetection(t *testing.T) {
	player := newDetectedPlayer(uuid.New())
	cfg := DetectionConfig{Forge: ForgeConfig{Enabled: true, Spoofing: ForgeSpoofing{Enabled: true, Actions: []string{"alert"}}}}
	history := NewMessageHistory()

	brand := HandleBrandPayload(player, history, "vanilla", cfg, false)
	if brand.SpoofedBrandDetected {
		t.Error("spoofing detected before Fabric channels")
	}
	register := HandleChannelRegister(player, history, "fabric:registry", nil, cfg, false)
	if !register.SpoofedBrandDetected || len(register.SpoofedBrandActionIDs) != 1 {
		t.Fatalf("spoofing result = %#v, want detection with one action", register)
	}
	if !player.HasGenericCheck("spoofed_brand") {
		t.Error("spoofed_brand check was not recorded")
	}
	if again := HandleChannelRegister(player, history, "fabric:registry", nil, cfg, false); again.SpoofedBrandDetected {
		t.Error("spoofing detection retriggered")
	}
}

func testParseChannelsBasic(t *testing.T) {
	channels := []string{
		"forgewurst:network",
		"forgehax:channel",
		"minecraft:brand", // builtin — excluded
		"forge:handshake", // builtin — excluded
	}
	mods := ParseModsFromChannels(channels)

	if len(mods) != 2 {
		t.Fatalf("expected 2 mods (forgewurst, forgehax), got %d: %v", len(mods), mods)
	}
	modIDs := modIDSet(mods)
	if _, ok := modIDs["forgewurst"]; !ok {
		t.Error("expected forgewurst in parsed mods")
	}
	if _, ok := modIDs["forgehax"]; !ok {
		t.Error("expected forgehax in parsed mods")
	}
}

func testParseChannelsBuiltinExcluded(t *testing.T) {
	channels := []string{
		"minecraft:register",
		"minecraft:brand",
		"neoforge:channel",
		"forge:handshake",
		"fml:handshake",
		"c:general",
		"fabric:registry_sync",
	}
	mods := ParseModsFromChannels(channels)
	if len(mods) != 0 {
		t.Errorf("expected 0 mods from builtin channels, got %d: %v", len(mods), mods)
	}
}

func testParseChannelsFabricExcluded(t *testing.T) {
	channels := []string{
		"fabric-api:events",
		"fabric-rendering:shader",
		"fabricloader-core:test",
		// "fabrication" — NOT a Fabric API module, should NOT be excluded
		"fabrication:channel",
	}
	mods := ParseModsFromChannels(channels)

	// Only "fabrication" should remain (it doesn't match fabric-* or fabricloader*).
	if len(mods) != 1 {
		t.Fatalf("expected 1 mod (fabrication), got %d: %v", len(mods), mods)
	}
	if mods[0].ModID != "fabrication" {
		t.Errorf("expected fabrication, got %q", mods[0].ModID)
	}
}

func testParseChannelsDeduplicated(t *testing.T) {
	// Same mod registering multiple channels — should appear once.
	channels := []string{
		"wurst:main",
		"wurst:hack",
		"wurst:gui",
	}
	mods := ParseModsFromChannels(channels)
	if len(mods) != 1 {
		t.Fatalf("expected 1 deduped mod, got %d: %v", len(mods), mods)
	}
	if mods[0].ModID != "wurst" {
		t.Errorf("expected wurst, got %q", mods[0].ModID)
	}
}

func testParseChannelsEmpty(t *testing.T) {
	mods := ParseModsFromChannels(nil)
	if mods != nil {
		t.Errorf("expected nil for nil input, got %v", mods)
	}

	mods = ParseModsFromChannels([]string{})
	if mods != nil {
		t.Errorf("expected nil for empty slice, got %v", mods)
	}
}

func testParseChannelsMissingColon(t *testing.T) {
	channels := []string{"invalidchannel", "also_invalid", ":emptyns"}
	mods := ParseModsFromChannels(channels)
	if len(mods) != 0 {
		t.Errorf("expected 0 mods from channels without valid namespace:path, got %d", len(mods))
	}
}

func testParseRawPayloadNullSeparated(t *testing.T) {
	// Simulate a real minecraft:register packet: null-byte separated channel list.
	payload := []byte("forgewurst:network\x00forgehax:channel\x00minecraft:brand")
	mods := ParseModsFromRegisterPayload(payload)

	if len(mods) != 2 {
		t.Fatalf("expected 2 mods from null-separated payload, got %d: %v", len(mods), mods)
	}
	modIDs := modIDSet(mods)
	if _, ok := modIDs["forgewurst"]; !ok {
		t.Error("expected forgewurst in parsed mods")
	}
	if _, ok := modIDs["forgehax"]; !ok {
		t.Error("expected forgehax in parsed mods")
	}
}

func testParseRawPayloadEmpty(t *testing.T) {
	mods := ParseModsFromRegisterPayload(nil)
	if mods != nil {
		t.Errorf("expected nil for nil payload, got %v", mods)
	}

	mods = ParseModsFromRegisterPayload([]byte{})
	if mods != nil {
		t.Errorf("expected nil for empty payload, got %v", mods)
	}
}

// testRegisterFallbackIntegration simulates the full REGISTER fallback path:
// parse channels → ProcessMods → check player state + triggers.
func testRegisterFallbackIntegration(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	// Simulate a minecraft:register payload arriving via PlayerChannelRegisterEvent.
	channels := []string{
		"wurst:main",
		"sodium:shaders",
		"minecraft:brand",
		"forge:handshake",
	}
	mods := ParseModsFromChannels(channels)

	// wurst and sodium should be detected; minecraft and forge are builtin.
	if len(mods) != 2 {
		t.Fatalf("expected 2 mods from channels, got %d", len(mods))
	}

	triggers := ProcessMods(player, mods, cfg)

	// wurst has mod_actions → trigger expected.
	if !findTriggerHasAction(triggers, "kick") {
		t.Error("expected 'kick' action from wurst mod_actions")
	}

	// sodium has no mod_actions → no trigger.
	if findTriggerContaining(triggers, "sodium") != nil {
		t.Error("unexpected trigger for sodium (no mod_actions)")
	}

	// Player state should reflect both mods.
	if !player.HasForgeMod("wurst") {
		t.Error("wurst should be in player state")
	}
	if !player.HasForgeMod("sodium") {
		t.Error("sodium should be in player state")
	}
}

func testProcessModsDeduplication(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	// First call records wurst.
	ProcessMods(player, []ForgeModInfo{{ModID: "wurst", Version: "1.0"}}, cfg)

	// Second call with same mod should produce no new triggers.
	triggers := ProcessMods(player, []ForgeModInfo{{ModID: "wurst", Version: "1.0"}}, cfg)
	if len(triggers) != 0 {
		t.Errorf("expected no triggers for duplicate mod, got %d", len(triggers))
	}
}

func testProcessModsDisabledConfig(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()
	cfg.Enabled = false

	triggers := ProcessMods(player, []ForgeModInfo{{ModID: "wurst"}}, cfg)
	if triggers != nil {
		t.Errorf("expected nil when detection is disabled, got %v", triggers)
	}
}

func testProcessClientTypeForge(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	triggers := ProcessClientType(player, ForgeClientForge, cfg)

	ct, ok := player.ForgeClientType()
	if !ok || ct != ForgeClientForge {
		t.Errorf("expected ForgeClientForge set on player, got %v ok=%v", ct, ok)
	}
	if !player.HasGenericCheck("forge") {
		t.Error("expected 'forge' generic check after ProcessClientType(Forge)")
	}
	if !hasTriggerForName(triggers, "Forge") {
		t.Error("expected Forge trigger from ProcessClientType")
	}
}

func testProcessClientTypeNeoForge(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	triggers := ProcessClientType(player, ForgeClientNeoForge, cfg)

	ct, ok := player.ForgeClientType()
	if !ok || ct != ForgeClientNeoForge {
		t.Errorf("expected ForgeClientNeoForge set on player, got %v ok=%v", ct, ok)
	}
	if !player.HasGenericCheck("neoforge") {
		t.Error("expected 'neoforge' generic check after ProcessClientType(NeoForge)")
	}
	if !hasTriggerForName(triggers, "NeoForge") {
		t.Error("expected NeoForge trigger from ProcessClientType")
	}
}

// testProcessClientTypeAlreadyMarked verifies idempotency: a second call for the
// same client type must not re-trigger (mirrors Java hadForgeCheck guard).
func testProcessClientTypeAlreadyMarked(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	// First detection.
	triggers1 := ProcessClientType(player, ForgeClientForge, cfg)
	if !hasTriggerForName(triggers1, "Forge") {
		t.Error("first call should produce Forge trigger")
	}

	// Second detection — should be idempotent.
	triggers2 := ProcessClientType(player, ForgeClientForge, cfg)
	if len(triggers2) != 0 {
		t.Errorf("second call should produce no triggers (already marked), got %d", len(triggers2))
	}
}

// testProcessModInfoType_FML verifies that ModInfo.Type = "FML" maps to Forge.
func testProcessModInfoType_FML(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	info := modinfo.ModInfo{Type: "FML", Mods: []modinfo.Mod{{ID: "examplemod", Version: "1.0"}}}
	triggers := ProcessForgeModInfo(player, info, cfg)

	if !hasTriggerForName(triggers, "Forge") {
		t.Error("expected Forge trigger for FML type")
	}
	ct, ok := player.ForgeClientType()
	if !ok || ct != ForgeClientForge {
		t.Errorf("expected ForgeClientForge for FML type, got %v ok=%v", ct, ok)
	}
}

// testProcessModInfoType_FML2 verifies that ModInfo.Type = "FML2" maps to Forge.
func testProcessModInfoType_FML2(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	info := modinfo.ModInfo{Type: "FML2", Mods: []modinfo.Mod{{ID: "examplemod", Version: "1.0"}}}
	ProcessForgeModInfo(player, info, cfg)

	ct, ok := player.ForgeClientType()
	if !ok || ct != ForgeClientForge {
		t.Errorf("expected ForgeClientForge for FML2 type, got %v ok=%v", ct, ok)
	}
}

// testProcessModInfoType_NeoForge verifies that ModInfo.Type = "NEOFORGE" maps to NeoForge.
func testProcessModInfoType_NeoForge(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()

	info := modinfo.ModInfo{Type: "NEOFORGE", Mods: []modinfo.Mod{{ID: "examplemod", Version: "1.0"}}}
	triggers := ProcessForgeModInfo(player, info, cfg)

	if !hasTriggerForName(triggers, "NeoForge") {
		t.Error("expected NeoForge trigger for NEOFORGE type")
	}
	ct, ok := player.ForgeClientType()
	if !ok || ct != ForgeClientNeoForge {
		t.Errorf("expected ForgeClientNeoForge for NEOFORGE type, got %v ok=%v", ct, ok)
	}
}

func testProcessModInfoDisabled(t *testing.T) {
	player := newTestPlayer()
	cfg := minimalForgeConfig()
	cfg.Enabled = false

	info := modinfo.ModInfo{Type: "FML2", Mods: []modinfo.Mod{{ID: "wurst", Version: "1.0"}}}
	triggers := ProcessForgeModInfo(player, info, cfg)

	if triggers != nil {
		t.Errorf("expected nil when detection disabled, got %v", triggers)
	}
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

func modIDSet(mods []ForgeModInfo) map[string]struct{} {
	s := make(map[string]struct{}, len(mods))
	for _, m := range mods {
		s[m.ModID] = struct{}{}
	}
	return s
}

func hasTriggerForName(triggers []ForgeActionTrigger, name string) bool {
	for _, t := range triggers {
		if t.Name == name {
			return true
		}
	}
	return false
}

func findTriggerForActionIDs(triggers []ForgeActionTrigger, actionID string) *ForgeActionTrigger {
	for i := range triggers {
		for _, id := range triggers[i].ActionIDs {
			if id == actionID {
				return &triggers[i]
			}
		}
	}
	return nil
}

func findTriggerHasAction(triggers []ForgeActionTrigger, actionID string) bool {
	return findTriggerForActionIDs(triggers, actionID) != nil
}

func findTriggerContaining(triggers []ForgeActionTrigger, namePart string) *ForgeActionTrigger {
	for i := range triggers {
		if strings.Contains(triggers[i].Name, namePart) {
			return &triggers[i]
		}
	}
	return nil
}
