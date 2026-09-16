package detection

// DetectionConfig holds all six TOML config files loaded from the HackedServer submodule.
type DetectionConfig struct {
	Main    MainConfig
	Generic GenericConfig
	Actions ActionsConfig
	Forge   ForgeConfig
	Lunar   LunarConfig
	Bedrock BedrockConfig
}

// ─── config.toml ─────────────────────────────────────────────────────────────

// MainConfig maps config.toml [settings].
type MainConfig struct {
	Settings MainSettings `toml:"settings"`
}

// MainSettings are the global plugin settings.
type MainSettings struct {
	// Language is the language file name without extension (default: "english").
	Language string `toml:"language"`
	// Debug enables logging of all CustomPayload packets.
	Debug bool `toml:"debug"`
	// SkipDuplicates suppresses repeated triggers for the same payload.
	SkipDuplicates bool `toml:"skip_duplicates"`
	// AutoDownloadDependencies is unused by the Go port (Spigot-specific).
	AutoDownloadDependencies bool `toml:"auto_download_dependencies"`
	// ActionDelayTicks is the global delay in ticks before executing actions.
	// 0 = no delay. Overridable per action in actions.toml.
	ActionDelayTicks int64 `toml:"action_delay_ticks"`
}

// ─── generic.toml ────────────────────────────────────────────────────────────

// GenericConfig maps generic.toml.
// The top-level enabled flag plus a map of check ID → GenericCheck.
// TOML table keys are free-form check IDs (e.g. "labymod_v1", "fabric", etc.).
type GenericConfig struct {
	// Enabled controls whether generic channel checks are active.
	Enabled bool `toml:"enabled"`
	// Checks is the set of individual generic check definitions, keyed by check ID.
	// Populated during TOML decode via custom loader (generic.toml has top-level tables).
	Checks map[string]GenericCheck
}

// GenericCheck is one entry in generic.toml (e.g. [labymod_v1]).
type GenericCheck struct {
	// Actions is the list of action IDs to trigger when this check matches.
	Actions []string `toml:"actions"`
	// Channels is the list of channel identifiers that trigger this check.
	Channels []string `toml:"channels"`
	// Name is the human-readable display name for alerts.
	Name string `toml:"name"`
	// Category is the classification tag (e.g. "client", "mod", "loader").
	Category string `toml:"category"`
	// MessageHas, if non-empty, requires the payload to contain this substring (case-insensitive).
	MessageHas string `toml:"message_has"`
	// MessageNotHas, if non-empty, requires the payload NOT to contain this substring (case-insensitive).
	MessageNotHas string `toml:"message_not_has"`
}

// ─── actions.toml ────────────────────────────────────────────────────────────

// ActionsConfig maps actions.toml.
// Each top-level table key is an action ID → ActionDef.
type ActionsConfig struct {
	// Actions keyed by action ID (e.g. "alert", "kick").
	// Populated during TOML decode via custom loader.
	Actions map[string]ActionDef
}

// ActionDef is one entry in actions.toml (e.g. [alert]).
type ActionDef struct {
	// SendAlert is an optional MiniMessage-formatted alert string.
	// Placeholders: <player>, <name>.
	SendAlert string `toml:"send_alert"`
	// DelayTicks overrides the global action_delay_ticks for this action.
	// -1 (zero value when absent) means use global.
	DelayTicks *int64 `toml:"delay_ticks"`
	// Commands holds the three command lists for this action.
	Commands ActionCommands `toml:"commands"`
}

// ActionCommands holds the three command lists within an ActionDef.
type ActionCommands struct {
	// Console commands are executed by the server console.
	Console []string `toml:"console"`
	// Player commands are executed as the player.
	Player []string `toml:"player"`
	// OppedPlayer commands are executed as the player with OP.
	OppedPlayer []string `toml:"opped_player"`
}

// ─── forge.toml ──────────────────────────────────────────────────────────────

// ForgeConfig maps forge.toml.
type ForgeConfig struct {
	// Enabled controls Forge/NeoForge mod detection.
	Enabled  bool          `toml:"enabled"`
	Settings ForgeSettings `toml:"settings"`
	Actions  ForgeActions  `toml:"actions"`
	// ModActions maps mod ID → list of action IDs triggered for that mod.
	ModActions map[string][]string `toml:"mod_actions"`
	Category   ForgeCategory       `toml:"category"`
	Spoofing   ForgeSpoofing       `toml:"spoofing"`
}

// ForgeSpoofing controls detection of clients claiming a vanilla brand while
// advertising Fabric channels. Active packet probing is not part of Gate's
// public plugin API and is therefore intentionally not represented here.
type ForgeSpoofing struct {
	Enabled bool     `toml:"enabled"`
	Actions []string `toml:"actions"`
}

// ForgeSettings holds display/mark settings for forge.toml [settings].
type ForgeSettings struct {
	// MarkForge adds a "Forge" generic check mark when a Forge client is confirmed.
	MarkForge bool `toml:"mark_forge"`
	// MarkNeoForge adds a "NeoForge" generic check mark when a NeoForge client is confirmed.
	MarkNeoForge bool `toml:"mark_neoforge"`
	// ShowModsInCheck includes the Forge mod list in /detection check output.
	ShowModsInCheck bool `toml:"show_mods_in_check"`
	// ShowModVersions includes mod versions alongside mod names.
	ShowModVersions bool `toml:"show_mod_versions"`
}

// ForgeActions holds the action lists for Forge and NeoForge detections.
type ForgeActions struct {
	// Forge is the list of action IDs triggered when a Forge client is confirmed.
	Forge []string `toml:"forge"`
	// NeoForge is the list of action IDs triggered when a NeoForge client is confirmed.
	NeoForge []string `toml:"neoforge"`
}

// ForgeCategory holds the whitelisted and blacklisted mod categories.
type ForgeCategory struct {
	Whitelisted ForgeModCategory `toml:"whitelisted"`
	Blacklisted ForgeModCategory `toml:"blacklisted"`
}

// ForgeModCategory is a named list of mod IDs with an optional color tag.
type ForgeModCategory struct {
	// Color is a MiniMessage color tag (e.g. "<green>", "<red>").
	Color string `toml:"color"`
	// Mods is the list of mod IDs in this category.
	Mods []string `toml:"mods"`
}

// ─── lunar.toml ──────────────────────────────────────────────────────────────

// LunarConfig maps lunar.toml.
type LunarConfig struct {
	// Enabled controls Lunar Client Apollo integration.
	Enabled  bool          `toml:"enabled"`
	Settings LunarSettings `toml:"settings"`
	Actions  LunarActions  `toml:"actions"`
	// ModActions maps Lunar mod ID → list of action IDs.
	ModActions map[string][]string `toml:"mod_actions"`
}

// LunarSettings holds display/mark settings for lunar.toml [settings].
type LunarSettings struct {
	// MarkLunarClient adds a "Lunar Client" generic check mark on Apollo confirmation.
	MarkLunarClient bool `toml:"mark_lunar_client"`
	// MarkFabric adds a "Fabric" mark when Lunar reports a Fabric client.
	MarkFabric bool `toml:"mark_fabric"`
	// MarkForge adds a "Forge" mark when Lunar reports a Forge client.
	MarkForge bool `toml:"mark_forge"`
	// ShowModsInCheck includes the Lunar mod list in /detection check output.
	ShowModsInCheck bool `toml:"show_mods_in_check"`
	// ShowModVersions includes mod versions alongside mod names.
	ShowModVersions bool `toml:"show_mod_versions"`
	// ShowModTypes includes mod type information in /detection check output.
	ShowModTypes bool `toml:"show_mod_types"`
}

// LunarActions holds per-client-type action lists.
type LunarActions struct {
	// LunarClient is the list of action IDs triggered on Lunar Client Apollo confirmation.
	LunarClient []string `toml:"lunar_client"`
	// Fabric is the list of action IDs triggered when Apollo reports Fabric.
	Fabric []string `toml:"fabric"`
	// Forge is the list of action IDs triggered when Apollo reports Forge.
	Forge []string `toml:"forge"`
}

// ─── bedrock.toml ────────────────────────────────────────────────────────────

// BedrockConfig maps bedrock.toml.
type BedrockConfig struct {
	// Enabled controls Bedrock detection via brand string fallback.
	// Note: disabled by default (false) in the submodule; Go port uses brand fallback only.
	Enabled bool `toml:"enabled"`
	// Label is the display name used for the <name> placeholder in actions.
	Label string `toml:"label"`
	// Actions is the list of action IDs triggered on Bedrock detection.
	Actions []string `toml:"actions"`
}
