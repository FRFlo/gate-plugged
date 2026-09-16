package detection

import (
	"strings"
	"sync"
	"time"
)

// tickDuration is the wall-clock duration of one game tick (50ms = 20 ticks/sec).
const tickDuration = 50 * time.Millisecond

// ActionCallbacks holds the side-effecting operations that the actions pipeline
// needs to perform. Callers (event handlers in Task 14/16) inject concrete
// implementations; tests inject no-op or recording stubs.
//
// All callbacks receive already-rendered strings (placeholders substituted).
type ActionCallbacks struct {
	// SendAlert is called for the send_alert field. The rendered MiniMessage
	// string is passed as-is; the caller is responsible for parsing and routing
	// it to players with the hackedserver.alert permission.
	SendAlert func(rendered string)

	// ExecuteConsoleCommand is called once per console command entry.
	ExecuteConsoleCommand func(rendered string)

	// ExecutePlayerCommand is called once per player command entry.
	ExecutePlayerCommand func(rendered string)

	// ExecuteOppedPlayerCommand is called once per opped-player command entry.
	ExecuteOppedPlayerCommand func(rendered string)
}

// ActionContext carries the player-specific values used for placeholder
// substitution in action strings.
//
// Java mapping:
//
//	Placeholder.unparsed("player", player.getUsername())  → PlayerName
//	Placeholder.parsed("name", check.getName())            → CheckName
type ActionContext struct {
	// PlayerName is the in-game name of the player that triggered the action.
	PlayerName string
	// CheckName is the human-readable name of the check that matched
	// (e.g. "LabyMod", "Forge", "Bedrock").
	CheckName string
}

// Clock is an interface over time scheduling so tests can run synchronously
// without real sleeps.
type Clock interface {
	// AfterFunc schedules fn to be called after d in a separate goroutine.
	// Implementations may call fn synchronously (for tests) or via time.AfterFunc.
	AfterFunc(d time.Duration, fn func())
}

// RealClock is the production Clock that delegates to time.AfterFunc.
type RealClock struct{}

// AfterFunc schedules fn after d using the stdlib timer.
func (RealClock) AfterFunc(d time.Duration, fn func()) {
	if d <= 0 {
		go fn()
		return
	}
	time.AfterFunc(d, fn)
}

// ImmediateClock is a test Clock that calls fn synchronously (no goroutine, no sleep).
// This makes action timing deterministic in unit tests.
type ImmediateClock struct{}

// AfterFunc calls fn synchronously, ignoring d.
func (ImmediateClock) AfterFunc(_ time.Duration, fn func()) {
	fn()
}

// ActionExecutor resolves action IDs against a loaded config and executes them
// with placeholder substitution and optional delay.
//
// It is intentionally stateless beyond the config snapshot it holds — callers
// may create one at startup and reuse it for the plugin's lifetime.
type ActionExecutor struct {
	mu       sync.RWMutex
	actions  ActionsConfig
	settings MainSettings
	clock    Clock
}

// NewActionExecutor creates an ActionExecutor backed by the given config and
// global settings, using the real wall-clock for scheduling.
func NewActionExecutor(actions ActionsConfig, settings MainSettings) *ActionExecutor {
	return &ActionExecutor{
		actions:  actions,
		settings: settings,
		clock:    RealClock{},
	}
}

// newActionExecutorWithClock is a package-internal constructor used by tests to
// inject a deterministic clock.
func newActionExecutorWithClock(actions ActionsConfig, settings MainSettings, clk Clock) *ActionExecutor {
	return &ActionExecutor{actions: actions, settings: settings, clock: clk}
}

// Execute looks up each action ID in the loaded config and runs the action
// pipeline (alert → commands) after the appropriate delay.
//
// Unknown action IDs are silently skipped (matches Java behaviour: HackedServer
// simply returns nil from getAction() and the caller checks for null).
//
// Each action runs in its own goroutine-after-delay so multiple actions with
// different delays do not block each other.
func (e *ActionExecutor) Execute(actionIDs []string, ctx ActionContext, cb ActionCallbacks) {
	for _, id := range actionIDs {
		e.mu.RLock()
		def, ok := e.actions.Actions[id]
		e.mu.RUnlock()
		if !ok {
			continue
		}
		// Capture loop variable for closure.
		actionDef := def
		delay := e.resolveDelay(actionDef)

		e.clock.AfterFunc(delay, func() {
			e.perform(actionDef, ctx, cb)
		})
	}
}

// Reload atomically replaces the action configuration used by future actions.
func (e *ActionExecutor) Reload(actions ActionsConfig, settings MainSettings) {
	e.mu.Lock()
	e.actions = actions
	e.settings = settings
	e.mu.Unlock()
}

// resolveDelay returns the effective delay for a single ActionDef.
//
// Java mapping:
//
//	if (delayTicks >= 0) return delayTicks;   // per-action override
//	return Config.ACTION_DELAY_TICKS;          // global fallback
//
// Go mapping:
//
//	if def.DelayTicks != nil → use *def.DelayTicks
//	else → use settings.ActionDelayTicks
func (e *ActionExecutor) resolveDelay(def ActionDef) time.Duration {
	e.mu.RLock()
	settings := e.settings
	e.mu.RUnlock()
	var ticks int64
	if def.DelayTicks != nil {
		ticks = *def.DelayTicks
	} else {
		ticks = settings.ActionDelayTicks
	}
	if ticks <= 0 {
		return 0
	}
	return time.Duration(ticks) * tickDuration
}

// perform applies placeholder substitution and fires all callbacks for one action.
func (e *ActionExecutor) perform(def ActionDef, ctx ActionContext, cb ActionCallbacks) {
	if def.SendAlert != "" && cb.SendAlert != nil {
		cb.SendAlert(RenderPlaceholders(def.SendAlert, ctx.PlayerName, ctx.CheckName))
	}

	if cb.ExecuteConsoleCommand != nil {
		for _, cmd := range def.Commands.Console {
			cb.ExecuteConsoleCommand(RenderPlaceholders(cmd, ctx.PlayerName, ctx.CheckName))
		}
	}

	if cb.ExecutePlayerCommand != nil {
		for _, cmd := range def.Commands.Player {
			cb.ExecutePlayerCommand(RenderPlaceholders(cmd, ctx.PlayerName, ctx.CheckName))
		}
	}

	if cb.ExecuteOppedPlayerCommand != nil {
		for _, cmd := range def.Commands.OppedPlayer {
			cb.ExecuteOppedPlayerCommand(RenderPlaceholders(cmd, ctx.PlayerName, ctx.CheckName))
		}
	}
}

// RenderPlaceholders substitutes <player> and <name> in s with the given
// player name and check name respectively.
//
// Java mapping (from CustomPayloadListener.executeCommands):
//
//	command.replace("<player>", player.getUsername()).replace("<name>", checkName)
//
// This is a plain string replace (not MiniMessage tag parsing); the MiniMessage
// parser in the alert path is the caller's responsibility.
func RenderPlaceholders(s, playerName, checkName string) string {
	s = strings.ReplaceAll(s, "<player>", playerName)
	s = strings.ReplaceAll(s, "<name>", checkName)
	return s
}
