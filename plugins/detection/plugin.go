package detection

import (
	"context"
	"encoding/hex"
	"os"
	"strings"

	"github.com/go-logr/logr"
	sharedcfg "github.com/minekube/gate-plugin-template/plugins/sharedconfig"
	"github.com/robinbraemer/event"
	"go.minekube.com/gate/pkg/edition/java/proxy"
	"go.minekube.com/gate/pkg/edition/java/proxy/message"
)

// Plugin is the Detection Gate plugin.
// It detects client mods, Forge/NeoForge, Lunar Client, and Bedrock clients
// by subscribing to proxy events and wiring all detection helpers together.
var Plugin = proxy.Plugin{
	Name: "Detection",
	Init: initDetection,
}

// initDetection is the full plugin initialisation flow.
//
// Startup sequence (mirrors Java HackedServer.onEnable):
//  1. Resolve resources dir (submodule guard — hard failure if absent)
//  2. Load all six TOML files into a DetectionConfig
//  3. Construct runtime collaborators: store, history, configHolder, executor
//  4. Register lunar:apollo channel with the Gate channel registrar
//  5. Register /detection command (alias /hs)
//  6. Subscribe all event handlers
func initDetection(ctx context.Context, p *proxy.Proxy) error {
	log := logr.FromContextOrDiscard(ctx)
	log.Info("Detection plugin loading...")
	if rootCfg, err := sharedcfg.Load(); err == nil {
		detectionPrefix = rootCfg.Plugins.Detection.Prefix
		detectionMessages = rootCfg.Plugins.Detection.Messages
	} else {
		log.Error(err, "failed to load plugged.yml, using default detection prefix")
	}

	// ── 1. Resolve submodule resources directory ──────────────────────────────
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}
	resourcesDir, err := resourcesDirFromBaseDir(baseDir)
	if err != nil {
		// Submodule missing: return actionable error, Gate will not start.
		return err
	}

	// ── 2. Load TOML config ───────────────────────────────────────────────────
	cfg, err := LoadDetectionConfig(resourcesDir)
	if err != nil {
		return err
	}

	// ── 3. Construct runtime collaborators ────────────────────────────────────
	store := NewPlayerStore()
	history := NewMessageHistory()
	cfgHolder := newConfigHolder(cfg, resourcesDir)
	executor := NewActionExecutor(cfg.Actions, cfg.Main.Settings)

	// ── 4. Register lunar:apollo channel with the Gate channel registrar ──────
	lunarID, err := message.ChannelIdentifierFrom(lunarApolloChannel)
	if err != nil {
		return err
	}
	p.ChannelRegistrar().Register(lunarID)

	// ── 5. Register /detection command (alias /hs) ────────────────────────────
	p.Command().RegisterWithAliases(
		newDetectionCommand(p, store, cfgHolder),
		"hs",
	)

	// ── 6. Subscribe event handlers ───────────────────────────────────────────

	// PlayerClientBrandEvent — brand string detection (generic + bedrock + forge brand).
	event.Subscribe(p.Event(), 0, onBrandEvent(log, store, history, cfgHolder, executor))

	// PlayerChannelRegisterEvent — channel-register detection (generic + forge mods from channels).
	event.Subscribe(p.Event(), 0, onChannelRegisterEvent(log, store, history, cfgHolder, executor))

	// PlayerModInfoEvent — Forge/NeoForge native handshake (primary forge path).
	event.Subscribe(p.Event(), 0, onModInfoEvent(log, store, cfgHolder, executor))

	// PluginMessageEvent — lunar:apollo protobuf payload.
	event.Subscribe(p.Event(), 0, onPluginMessageEvent(log, store, cfgHolder, executor))

	// ServerPostConnectEvent — player has fully joined a server; execute pending actions.
	event.Subscribe(p.Event(), 0, onServerPostConnectEvent(log, store, cfgHolder, executor))

	// DisconnectEvent — clean up store + history for the disconnecting player.
	event.Subscribe(p.Event(), 0, onDisconnectEvent(log, store, history, cfgHolder))

	log.Info("Detection plugin loaded.",
		"generic_checks", len(cfg.Generic.Checks),
		"forge_enabled", cfg.Forge.Enabled,
		"lunar_enabled", cfg.Lunar.Enabled,
		"bedrock_enabled", cfg.Bedrock.Enabled,
	)
	return nil
}

// ─── Event handlers ───────────────────────────────────────────────────────────

// onBrandEvent handles PlayerClientBrandEvent.
// Runs generic checks and Bedrock/Forge brand detection, queues any resulting actions.
func onBrandEvent(
	log logr.Logger,
	store *PlayerStore,
	history *MessageHistory,
	cfgHolder *configHolder,
	executor *ActionExecutor,
) func(*proxy.PlayerClientBrandEvent) {
	return func(e *proxy.PlayerClientBrandEvent) {
		cfg := cfgHolder.get()
		player := store.Get(gateUUIDToGoogle(e.Player().ID()))
		bypass := e.Player().HasPermission("hackedserver.bypass")
		if bypass {
			log.Info("detection bypass active", "player", e.Player().Username(), "event", "brand")
		}
		logDebug(log, cfg.Main.Settings.Debug, "brand payload", "player", e.Player().Username(), "channel", brandChannel, "payload", e.Brand())

		result := HandleBrandPayload(player, history, e.Brand(), *cfg, bypass)
		if result.BedrockDetected {
			log.Info("bedrock detection", "player", e.Player().Username(), "label", result.BedrockLabel, "action_count", len(result.BedrockActionIDs))
		}

		actCtx := ActionContext{PlayerName: e.Player().Username()}

		for _, t := range result.GenericTriggers {
			log.Info("generic detection", "player", e.Player().Username(), "check_id", t.CheckID, "name", t.Name, "action_count", len(t.ActionIDs))
			if len(t.ActionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			actionIDs := t.ActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}

		for _, t := range result.ForgeTriggers {
			log.Info("forge detection", "player", e.Player().Username(), "name", t.Name, "action_count", len(t.ActionIDs))
			if len(t.ActionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			actionIDs := t.ActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}

		if result.SpoofedBrandDetected {
			log.Info("spoofed brand detection", "player", e.Player().Username(), "action_count", len(result.SpoofedBrandActionIDs))
		}
		if len(result.SpoofedBrandActionIDs) > 0 {
			actionIDs := result.SpoofedBrandActionIDs
			player.QueuePendingAction(func() {
				ctx := actCtx
				ctx.CheckName = "Spoofed Brand (Fabric)"
				executor.Execute(actionIDs, ctx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}

		if result.BedrockDetected && len(result.BedrockActionIDs) > 0 {
			label := result.BedrockLabel
			actionIDs := result.BedrockActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = label
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}
	}
}

// onChannelRegisterEvent handles PlayerChannelRegisterEvent.
// Runs generic checks and Forge mod detection from channel namespaces.
func onChannelRegisterEvent(
	log logr.Logger,
	store *PlayerStore,
	history *MessageHistory,
	cfgHolder *configHolder,
	executor *ActionExecutor,
) func(*proxy.PlayerChannelRegisterEvent) {
	return func(e *proxy.PlayerChannelRegisterEvent) {
		cfg := cfgHolder.get()
		player := store.Get(gateUUIDToGoogle(e.Player().ID()))
		bypass := e.Player().HasPermission("hackedserver.bypass")
		if bypass {
			log.Info("detection bypass active", "player", e.Player().Username(), "event", "channel_register")
		}

		// Convert Gate channel identifiers to plain strings for the handler.
		rawChannels := make([]string, 0, len(e.Channels()))
		for _, ch := range e.Channels() {
			rawChannels = append(rawChannels, ch.ID())
		}
		rawPayload := strings.Join(rawChannels, "\x00")
		logDebug(log, cfg.Main.Settings.Debug, "register payload", "player", e.Player().Username(), "channel", registerChannel, "payload", rawPayload)

		result := HandleChannelRegister(player, history, rawPayload, rawChannels, *cfg, bypass)

		actCtx := ActionContext{PlayerName: e.Player().Username()}

		for _, t := range result.GenericTriggers {
			log.Info("generic detection", "player", e.Player().Username(), "check_id", t.CheckID, "name", t.Name, "action_count", len(t.ActionIDs))
			if len(t.ActionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			actionIDs := t.ActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}

		for _, t := range result.ForgeTriggers {
			log.Info("forge detection", "player", e.Player().Username(), "name", t.Name, "action_count", len(t.ActionIDs))
			if len(t.ActionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			actionIDs := t.ActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}

		if result.SpoofedBrandDetected {
			log.Info("spoofed brand detection", "player", e.Player().Username(), "action_count", len(result.SpoofedBrandActionIDs))
		}
		if len(result.SpoofedBrandActionIDs) > 0 {
			actionIDs := result.SpoofedBrandActionIDs
			player.QueuePendingAction(func() {
				ctx := actCtx
				ctx.CheckName = "Spoofed Brand (Fabric)"
				executor.Execute(actionIDs, ctx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}
	}
}

// onModInfoEvent handles PlayerModInfoEvent (Forge native FML handshake).
// This is the primary Forge/NeoForge detection path.
func onModInfoEvent(
	log logr.Logger,
	store *PlayerStore,
	cfgHolder *configHolder,
	executor *ActionExecutor,
) func(*proxy.PlayerModInfoEvent) {
	return func(e *proxy.PlayerModInfoEvent) {
		cfg := cfgHolder.get()
		player := store.Get(gateUUIDToGoogle(e.Player().ID()))
		bypass := e.Player().HasPermission("hackedserver.bypass")
		if bypass {
			log.Info("detection bypass active", "player", e.Player().Username(), "event", "mod_info")
		}

		if len(e.ModInfo().Mods) > 0 {
			mods := make([]string, 0, len(e.ModInfo().Mods))
			for _, m := range e.ModInfo().Mods {
				if m.ID == "" {
					continue
				}
				mods = append(mods, strings.ToLower(m.ID))
			}
			if len(mods) > 0 {
				log.Info("forge mods received", "player", e.Player().Username(), "client_type", e.ModInfo().Type, "count", len(mods))
				if cfg.Main.Settings.Debug {
					logDebug(log, true, "forge mods detail", "player", e.Player().Username(), "mods", strings.Join(mods, ","))
				}
			}
		}

		triggers := ProcessForgeModInfo(player, e.ModInfo(), cfg.Forge)
		if len(triggers) == 0 {
			return
		}
		for _, t := range triggers {
			log.Info("forge trigger", "player", e.Player().Username(), "name", t.Name, "action_count", len(t.ActionIDs))
		}

		actCtx := ActionContext{PlayerName: e.Player().Username()}

		for _, t := range triggers {
			actionIDs := t.ActionIDs
			if bypass {
				actionIDs = nil
			}
			if len(actionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, e.Player(), cfgHolder))
			})
		}
	}
}

// onPluginMessageEvent handles PluginMessageEvent for the lunar:apollo channel.
func onPluginMessageEvent(
	log logr.Logger,
	store *PlayerStore,
	cfgHolder *configHolder,
	executor *ActionExecutor,
) func(*proxy.PluginMessageEvent) {
	return func(e *proxy.PluginMessageEvent) {
		// Only interested in messages from players (not backend servers).
		gatePlayer, ok := e.Source().(proxy.Player)
		if !ok {
			return
		}

		cfg := cfgHolder.get()
		player := store.Get(gateUUIDToGoogle(gatePlayer.ID()))
		bypass := gatePlayer.HasPermission("hackedserver.bypass")
		if bypass {
			log.Info("detection bypass active", "player", gatePlayer.Username(), "event", "plugin_message")
		}

		channelID := e.Identifier().ID()
		if equalFold(channelID, lunarApolloChannel) {
			log.Info("plugin message received", "player", gatePlayer.Username(), "channel", channelID, "size", len(e.Data()))
		} else {
			logDebug(log, cfg.Main.Settings.Debug, "plugin message ignored", "player", gatePlayer.Username(), "channel", channelID, "size", len(e.Data()))
		}
		if cfg.Main.Settings.Debug {
			logDebug(log, true, "plugin message payload", "player", gatePlayer.Username(), "channel", channelID, "payload_hex", hex.EncodeToString(e.Data()))
		}

		if equalFold(channelID, lunarApolloChannel) {
			if mods, ok := ParseLunarHandshake(e.Data()); ok {
				modIDs := make([]string, 0, len(mods))
				for _, mod := range mods {
					if mod.ID == "" {
						continue
					}
					modIDs = append(modIDs, strings.ToLower(mod.ID))
				}
				log.Info("lunar mods decoded", "player", gatePlayer.Username(), "count", len(modIDs), "mods", strings.Join(modIDs, ","))
			}
		}

		result := HandleLunarPluginMessage(player, channelID, e.Data(), cfg.Lunar, bypass)

		if !result.HasTriggers() {
			return
		}

		actCtx := ActionContext{PlayerName: gatePlayer.Username()}

		for _, t := range result.Triggers {
			log.Info("lunar detection", "player", gatePlayer.Username(), "name", t.Name, "action_count", len(t.ActionIDs))
			if len(t.ActionIDs) == 0 {
				continue
			}
			triggerName := t.Name
			actionIDs := t.ActionIDs
			player.QueuePendingAction(func() {
				actCtx := actCtx
				actCtx.CheckName = triggerName
				executor.Execute(actionIDs, actCtx, buildCallbacks(log, gatePlayer, cfgHolder))
			})
		}
	}
}

// onServerPostConnectEvent handles ServerPostConnectEvent.
// Executes any pending actions queued for a player on their first server join.
func onServerPostConnectEvent(
	log logr.Logger,
	store *PlayerStore,
	cfgHolder *configHolder,
	executor *ActionExecutor,
) func(*proxy.ServerPostConnectEvent) {
	return func(e *proxy.ServerPostConnectEvent) {
		cfg := cfgHolder.get()
		// Only fire pending actions on the FIRST server connection (PreviousServer == nil).
		if e.PreviousServer() != nil {
			return
		}
		player := store.Get(gateUUIDToGoogle(e.Player().ID()))
		if !player.HasPendingActions() {
			return
		}
		logDebug(log, cfg.Main.Settings.Debug, "executing pending detection actions",
			"player", e.Player().Username(),
		)
		player.ExecutePendingActions()
		_ = executor // executor is used inside the queued closures, not here directly.
	}
}

// onDisconnectEvent handles DisconnectEvent.
// Removes player state from store and history to prevent memory leaks.
func onDisconnectEvent(
	log logr.Logger,
	store *PlayerStore,
	history *MessageHistory,
	cfgHolder *configHolder,
) func(*proxy.DisconnectEvent) {
	return func(e *proxy.DisconnectEvent) {
		cfg := cfgHolder.get()
		id := gateUUIDToGoogle(e.Player().ID())
		store.Remove(id)
		history.Remove(id)
		logDebug(log, cfg.Main.Settings.Debug, "detection: player cleaned up on disconnect",
			"player", e.Player().Username(),
		)
	}
}

func logDebug(log logr.Logger, enabled bool, msg string, keysAndValues ...any) {
	if !enabled {
		return
	}
	kv := make([]any, 0, len(keysAndValues)+2)
	kv = append(kv, "verbosity", "debug")
	kv = append(kv, keysAndValues...)
	log.Info(msg, kv...)
}

// ─── Alert routing helper ─────────────────────────────────────────────────────

// buildCallbacks constructs the ActionCallbacks for a given player event.
// Alert messages are broadcast to all online players with the hackedserver.alert permission.
// Console commands are executed via the proxy command manager.
func buildCallbacks(log logr.Logger, gatePlayer proxy.Player, cfgHolder *configHolder) ActionCallbacks {
	return ActionCallbacks{
		SendAlert: func(rendered string) {
			log.Info("detection action alert", "player", gatePlayer.Username(), "message", rendered)
		},
		ExecuteConsoleCommand: func(rendered string) {
			log.Info("detection action console command", "player", gatePlayer.Username(), "cmd", rendered)
		},
		ExecutePlayerCommand: func(rendered string) {
			log.Info("detection action player command", "player", gatePlayer.Username(), "cmd", rendered)
		},
		ExecuteOppedPlayerCommand: func(rendered string) {
			log.Info("detection action opped player command", "player", gatePlayer.Username(), "cmd", rendered)
		},
	}
}
