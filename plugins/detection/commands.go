package detection

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	guuid "github.com/google/uuid"
	sharedcfg "github.com/minekube/gate-plugin-template/plugins/sharedconfig"
	"github.com/minekube/gate-plugin-template/util/chatfmt"
	"go.minekube.com/brigodier"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/command/suggest"
	"go.minekube.com/gate/pkg/edition/java/proxy"
)

var detectionPrefix = "<gray>[<aqua>HackedServer</aqua>]</gray> "

var detectionMessages = sharedcfg.DetectionMessages{
	AvailableCommands:  "<gray>Available commands</gray>",
	HelpReload:         "<dark_gray>/hs <gray>reload <dark_gray>» <gray>reload the plugin</gray>",
	HelpCheck:          "<dark_gray>/hs <gray>check <aqua>target</aqua> <dark_gray>» <gray>check player detected mods</gray>",
	HelpList:           "<dark_gray>/hs <gray>list <dark_gray>» <gray>list all spotted players</gray>",
	ReloadFailed:       "<red>Reload failed: {error}</red>",
	ReloadSuccess:      "<green>Successfully reloaded</green>",
	PlayerNotFound:     "<red>Player not found: {player}</red>",
	Checking:           "<aqua>Checking <gold>{player}</gold></aqua>",
	DetectedMods:       "<green>Detected mods:</green>",
	NoModsDetected:     "<green>No mods detected</green>",
	ForgeMods:          "<green>Forge/NeoForge mods:</green>",
	NoForgeMods:        "<green>No Forge mods detected</green>",
	LunarMods:          "<green>Lunar Client mods:</green>",
	NoLunarMods:        "<green>No Lunar Client mods detected</green>",
	BedrockDetected:    "<green>Bedrock: yes</green>",
	NoPlayersSpotted:   "<green>No chocolate players spotted</green>",
	SpottedPlayers:     "<green>Spotted players:</green>",
	ListBullet:         "<dark_gray>- <gold>{value}</gold></dark_gray>",
	ModBullet:          "<dark_gray>- {value}</dark_gray>",
	ModVersionBullet:   "<dark_gray>- {value} ({version})</dark_gray>",
	LunarTypeSuffix:    " [{type}]",
	LunarVersionSuffix: " ({version})",
}

// configHolder is a thread-safe wrapper that holds the current DetectionConfig
// and the resources directory path needed to reload it.
type configHolder struct {
	mu           sync.RWMutex
	cfg          *DetectionConfig
	resourcesDir string
}

// newConfigHolder creates a configHolder with the given initial config and
// resources directory path (as returned by filepath.Dir(paths.Config)).
func newConfigHolder(cfg *DetectionConfig, resourcesDir string) *configHolder {
	return &configHolder{cfg: cfg, resourcesDir: resourcesDir}
}

// get returns a snapshot of the current config. Safe to call from multiple goroutines.
func (h *configHolder) get() *DetectionConfig {
	h.mu.RLock()
	c := h.cfg
	h.mu.RUnlock()
	return c
}

// reload reloads the TOML config files from the stored resources directory and
// atomically replaces the current config on success.
func (h *configHolder) reload() error {
	newCfg, err := LoadDetectionConfig(h.resourcesDir)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.cfg = newCfg
	h.mu.Unlock()
	return nil
}

// resourcesDirFromBaseDir resolves the resources directory from a repo base dir.
// Convenience wrapper used by Init (T14).
func resourcesDirFromBaseDir(baseDir string) (string, error) {
	paths, err := ResolveSubmodulePaths(baseDir)
	if err != nil {
		return "", err
	}
	return filepath.Dir(paths.Config), nil
}

// gateUUIDToGoogle converts a Gate-internal UUID ([16]byte alias) to github.com/google/uuid.UUID.
// Gate's uuid.UUID is defined as `type UUID guuid.UUID` so a simple type-cast works.
func gateUUIDToGoogle(id [16]byte) guuid.UUID {
	return guuid.UUID(id)
}

// newDetectionCommand builds the /detection (alias: /hs) command tree.
//
// Subcommands:
//
//	/detection reload           – reloads TOML config from the submodule
//	/detection check <player>   – shows what is known about a player
//	/detection list             – lists all players with generic checks
func newDetectionCommand(
	p *proxy.Proxy,
	store *PlayerStore,
	cfgHolder *configHolder,
	executors ...*ActionExecutor,
) brigodier.LiteralNodeBuilder {
	const playerArg = "player"
	var executor *ActionExecutor
	if len(executors) > 0 {
		executor = executors[0]
	}

	return brigodier.Literal("detection").
		Executes(command.Command(func(c *command.Context) error {
			return c.Source.SendMessage(detectionMessage(strings.Join([]string{
				detectionMessages.AvailableCommands,
				detectionMessages.HelpReload,
				detectionMessages.HelpCheck,
				detectionMessages.HelpList,
			}, "\n")))
		})).
		Then(
			brigodier.Literal("reload").
				Executes(command.Command(func(c *command.Context) error {
					return handleReload(c, cfgHolder, executor)
				})),
		).
		Then(
			brigodier.Literal("check").
				Then(
					brigodier.Argument(playerArg, brigodier.String).
						Suggests(playerSuggestionProvider(p)).
						Executes(command.Command(func(c *command.Context) error {
							return handleCheck(c, p, store, cfgHolder, c.String(playerArg))
						})),
				),
		).
		Then(
			brigodier.Literal("list").
				Executes(command.Command(func(c *command.Context) error {
					return handleList(c, p, store)
				})),
		)
}

func playerSuggestionProvider(proxy *proxy.Proxy, additionalPlayers ...string) brigodier.SuggestionProvider {
	return command.SuggestFunc(func(
		_ *command.Context,
		b *brigodier.SuggestionsBuilder,
	) *brigodier.Suggestions {
		candidates := append(playerNames(proxy), additionalPlayers...)
		return suggest.Similar(b, candidates).Build()
	})
}

func suggestPlayerNames(b *brigodier.SuggestionsBuilder, names []string) *brigodier.Suggestions {
	if len(names) == 0 {
		return b.Build()
	}
	return suggest.Similar(b, names).Build()
}

func playerNames(proxy *proxy.Proxy) []string {
	if proxy == nil {
		return nil
	}
	names := make([]string, 0, len(proxy.Players()))
	for _, player := range proxy.Players() {
		names = append(names, player.Username())
	}
	return names
}

// handleReload reloads the TOML configuration from the submodule.
func handleReload(c *command.Context, cfgHolder *configHolder, executor *ActionExecutor) error {
	if err := cfgHolder.reload(); err != nil {
		return c.Source.SendMessage(detectionMessage(chatfmt.ApplyPlaceholders(detectionMessages.ReloadFailed, map[string]string{"error": fmt.Sprintf("%v", err)})))
	}
	if cfg, err := sharedcfg.Reload(); err == nil {
		detectionPrefix = cfg.Plugins.Detection.Prefix
		detectionMessages = cfg.Plugins.Detection.Messages
		if executor != nil {
			current := cfgHolder.get()
			executor.Reload(current.Actions, current.Main.Settings)
		}
	}
	return c.Source.SendMessage(detectionMessage(detectionMessages.ReloadSuccess))
}

// handleCheck shows detected mod information for the named player.
func handleCheck(
	c *command.Context,
	p *proxy.Proxy,
	store *PlayerStore,
	cfgHolder *configHolder,
	username string,
) error {
	// Look up online player by name.
	target := p.PlayerByName(username)
	if target == nil {
		return c.Source.SendMessage(detectionMessage(chatfmt.ApplyPlaceholders(detectionMessages.PlayerNotFound, map[string]string{"player": username})))
	}

	return formatCheckOutput(c, target.Username(), store.Get(gateUUIDToGoogle(target.ID())), cfgHolder.get())
}

// formatCheckOutput renders the /detection check output for a known player.
// It is extracted from handleCheck for testability without a real proxy.
func formatCheckOutput(
	c *command.Context,
	username string,
	dp *DetectedPlayer,
	cfg *DetectionConfig,
) error {
	var b strings.Builder
	b.WriteString(chatfmt.ApplyPlaceholders(detectionMessages.Checking, map[string]string{"player": username}) + "\n")

	// ── Generic checks ────────────────────────────────────────────────────────
	checks := dp.GenericChecks()
	sort.Strings(checks)
	if len(checks) > 0 {
		b.WriteString(detectionMessages.DetectedMods + "\n")
		for _, check := range checks {
			b.WriteString(chatfmt.ApplyPlaceholders(detectionMessages.ListBullet, map[string]string{"value": check}) + "\n")
		}
	} else {
		b.WriteString(detectionMessages.NoModsDetected + "\n")
	}

	// ── Forge mods (if ShowModsInCheck is enabled) ─────────────────────────
	if cfg.Forge.Settings.ShowModsInCheck && dp.HasForgeModsData() {
		mods := dp.ForgeMods()
		if len(mods) > 0 {
			sort.Slice(mods, func(i, j int) bool {
				return mods[i].ModID < mods[j].ModID
			})
			b.WriteString(detectionMessages.ForgeMods + "\n")
			for _, m := range mods {
				if cfg.Forge.Settings.ShowModVersions && m.Version != "" {
					b.WriteString(chatfmt.ApplyPlaceholders(detectionMessages.ModVersionBullet, map[string]string{"value": m.ModID, "version": m.Version}) + "\n")
				} else {
					b.WriteString(chatfmt.ApplyPlaceholders(detectionMessages.ModBullet, map[string]string{"value": m.ModID}) + "\n")
				}
			}
		} else {
			b.WriteString(detectionMessages.NoForgeMods + "\n")
		}
	}

	// ── Lunar mods (if enabled and ShowModsInCheck is enabled) ────────────
	if cfg.Lunar.Enabled && cfg.Lunar.Settings.ShowModsInCheck && dp.HasLunarModsData() {
		mods := dp.LunarMods()
		if len(mods) > 0 {
			sort.Slice(mods, func(i, j int) bool {
				return mods[i].ID < mods[j].ID
			})
			b.WriteString(detectionMessages.LunarMods + "\n")
			for _, m := range mods {
				line := chatfmt.ApplyPlaceholders(detectionMessages.ModBullet, map[string]string{"value": m.ID})
				if cfg.Lunar.Settings.ShowModVersions && m.Version != "" {
					line += chatfmt.ApplyPlaceholders(detectionMessages.LunarVersionSuffix, map[string]string{"version": m.Version})
				}
				if cfg.Lunar.Settings.ShowModTypes && m.Type != "" {
					line += chatfmt.ApplyPlaceholders(detectionMessages.LunarTypeSuffix, map[string]string{"type": m.Type})
				}
				b.WriteString(line + "\n")
			}
		} else {
			b.WriteString(detectionMessages.NoLunarMods + "\n")
		}
	}

	// ── Bedrock ───────────────────────────────────────────────────────────
	if dp.IsBedrockDetected() {
		b.WriteString(detectionMessages.BedrockDetected + "\n")
	}

	return c.Source.SendMessage(detectionMessage(strings.TrimRight(b.String(), "\n")))
}

// handleList lists all online players that have at least one generic check triggered.
func handleList(
	c *command.Context,
	p *proxy.Proxy,
	store *PlayerStore,
) error {
	var entries []namedCheckEntry
	for _, player := range p.Players() {
		dp := store.Get(gateUUIDToGoogle(player.ID()))
		checks := dp.GenericChecks()
		if len(checks) > 0 {
			sort.Strings(checks)
			entries = append(entries, namedCheckEntry{name: player.Username(), checks: checks})
		}
	}
	return handleListFromEntries(c, entries)
}

// namedCheckEntry is a player name paired with its triggered generic check IDs.
type namedCheckEntry struct {
	name   string
	checks []string
}

// handleListFromEntries formats and sends the /detection list output.
// entries may be nil or empty to produce the "no players" message.
// Extracted for testability without a real *proxy.Proxy.
func handleListFromEntries(c *command.Context, entries []namedCheckEntry) error {
	if len(entries) == 0 {
		return c.Source.SendMessage(detectionMessage(detectionMessages.NoPlayersSpotted))
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	var b strings.Builder
	b.WriteString(detectionMessages.SpottedPlayers + "\n")
	for _, e := range entries {
		b.WriteString(chatfmt.ApplyPlaceholders(detectionMessages.ListBullet, map[string]string{"value": e.name}) + "\n")
	}
	return c.Source.SendMessage(detectionMessage(strings.TrimRight(b.String(), "\n")))
}

func detectionMessage(message string) component.Component {
	return chatfmt.Render("HackedServer", detectionPrefix, message)
}
