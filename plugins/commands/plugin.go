// Package commands contains small proxy-level commands that are not part of
// Gate's built-in command set.
package commands

import (
	"context"
	"strings"
	"sync"
	"time"

	sharedcfg "github.com/minekube/gate-plugin-template/plugins/sharedconfig"
	"github.com/minekube/gate-plugin-template/util/mini"
	"github.com/robinbraemer/event"
	"go.minekube.com/brigodier"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/edition/java/proxy"
	"go.minekube.com/gate/pkg/edition/java/proxy/message"
	gateuuid "go.minekube.com/gate/pkg/util/uuid"
)

// Plugin registers commands adapted from GateProxy.
var Plugin = proxy.Plugin{Name: "Commands", Init: initPlugin}

func initPlugin(_ context.Context, p *proxy.Proxy) error {
	cfg, _ := sharedcfg.Load()
	p.Command().Register(newKickCommand(p, cfg.Plugins.Kick.Messages))
	state := &authState{pending: make(map[gateuuid.UUID]time.Time), timers: make(map[gateuuid.UUID]*time.Timer)}
	if cfg.Plugins.F3.Enabled || cfg.Plugins.Auth.Enabled {
		event.Subscribe(p.Event(), 0, onServerPostConnect(cfg.Plugins.F3))
	}
	if cfg.Plugins.Auth.Enabled {
		p.Command().Register(newLoginCommand(p, cfg.Plugins.Auth, state))
		event.Subscribe(p.Event(), 0, onInitialServer(p, cfg.Plugins.Auth, state))
		event.Subscribe(p.Event(), 0, onChat(p, cfg.Plugins.Auth, state))
		event.Subscribe(p.Event(), 0, onDisconnect(state))
	}
	return nil
}

func newLoginCommand(p *proxy.Proxy, cfg sharedcfg.AuthConfig, state *authState) brigodier.LiteralNodeBuilder {
	name := cfg.Command
	if name == "" {
		name = "login"
	}
	return brigodier.Literal(name).Then(
		brigodier.Argument("password", brigodier.String).Executes(command.Command(func(c *command.Context) error {
			player, ok := c.Source.(proxy.Player)
			if !ok {
				return c.Source.SendMessage(render(cfg.Messages.ConsoleOnly))
			}
			id := player.ID()
			state.mu.Lock()
			_, waiting := state.pending[id]
			state.mu.Unlock()
			if !waiting || !contains(cfg.Passwords, c.String("password")) {
				return c.Source.SendMessage(render(cfg.Messages.Invalid))
			}
			state.mu.Lock()
			delete(state.pending, id)
			if timer := state.timers[id]; timer != nil {
				timer.Stop()
				delete(state.timers, id)
			}
			state.mu.Unlock()
			if cfg.HubServer != "" {
				if s := p.Server(cfg.HubServer); s != nil {
					_, _ = player.CreateConnectionRequest(s).Connect(context.Background())
				}
			}
			return c.Source.SendMessage(render(cfg.Messages.Success))
		})),
	)
}

func newKickCommand(p *proxy.Proxy, messages sharedcfg.KickMessages) brigodier.LiteralNodeBuilder {
	return brigodier.Literal("kick").
		Executes(command.Command(func(c *command.Context) error {
			if _, ok := c.Source.(proxy.Player); ok {
				return c.Source.SendMessage(render(messages.ConsoleOnly))
			}
			return c.Source.SendMessage(render(messages.Usage))
		})).
		Then(
			brigodier.Argument("player", brigodier.String).Then(
				brigodier.Argument("reason", brigodier.StringPhrase).
					Executes(command.Command(func(c *command.Context) error {
						if _, ok := c.Source.(proxy.Player); ok {
							return c.Source.SendMessage(render(messages.ConsoleOnly))
						}
						name := c.String("player")
						target := p.PlayerByName(name)
						if target == nil {
							return c.Source.SendMessage(render(replace(messages.PlayerNotFound, "player", name)))
						}
						reason := strings.TrimSpace(c.String("reason"))
						if reason == "" {
							reason = "Expulsion par un administrateur."
						}
						target.Disconnect(render(replace(messages.Kicked, "reason", reason)))
						return nil
					})),
			),
		)
}

type authState struct {
	mu      sync.Mutex
	pending map[gateuuid.UUID]time.Time
	timers  map[gateuuid.UUID]*time.Timer
}

func onServerPostConnect(cfg sharedcfg.F3Config) func(*proxy.ServerPostConnectEvent) {
	return func(e *proxy.ServerPostConnectEvent) {
		if !cfg.Enabled || cfg.Brand == "" {
			return
		}
		// Minecraft brand payloads are encoded as a VarInt-prefixed string.
		data := encodeBrand(cfg.Brand)
		id, err := message.ChannelIdentifierFrom("minecraft:brand")
		if err == nil {
			_ = e.Player().SendPluginMessage(id, data)
		}
	}
}

func onInitialServer(p *proxy.Proxy, cfg sharedcfg.AuthConfig, state *authState) func(*proxy.PlayerChooseInitialServerEvent) {
	return func(e *proxy.PlayerChooseInitialServerEvent) {
		if !cfg.Enabled || cfg.LoginServer == "" {
			return
		}
		if s := p.Server(cfg.LoginServer); s != nil {
			timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
			if timeout <= 0 {
				timeout = time.Minute
			}
			id := e.Player().ID()
			state.mu.Lock()
			state.pending[id] = time.Now().Add(timeout)
			if old := state.timers[id]; old != nil {
				old.Stop()
			}
			state.timers[id] = time.AfterFunc(timeout, func() {
				state.mu.Lock()
				if _, ok := state.pending[id]; ok {
					delete(state.pending, id)
					state.mu.Unlock()
					e.Player().Disconnect(render(cfg.Messages.Timeout))
					return
				}
				state.mu.Unlock()
			})
			state.mu.Unlock()
			e.SetInitialServer(s)
		}
	}
}

func onChat(p *proxy.Proxy, cfg sharedcfg.AuthConfig, state *authState) func(*proxy.PlayerChatEvent) {
	return func(e *proxy.PlayerChatEvent) {
		if !cfg.Enabled {
			return
		}
		id := e.Player().ID()
		state.mu.Lock()
		expiry, waiting := state.pending[id]
		state.mu.Unlock()
		if !waiting || time.Now().After(expiry) {
			return
		}
		msg := strings.TrimSpace(e.Message())
		prefix := "/" + cfg.Command
		if cfg.Command == "" {
			prefix = "/login"
		}
		if strings.HasPrefix(msg, prefix+" ") || msg == prefix {
			parts := strings.Fields(msg)
			if len(parts) == 2 && contains(cfg.Passwords, parts[1]) {
				state.mu.Lock()
				delete(state.pending, id)
				if timer := state.timers[id]; timer != nil {
					timer.Stop()
					delete(state.timers, id)
				}
				state.mu.Unlock()
				e.SetAllowed(false)
				if cfg.HubServer != "" {
					if s := p.Server(cfg.HubServer); s != nil {
						_, _ = e.Player().CreateConnectionRequest(s).Connect(context.Background())
					}
				}
			} else {
				e.SetAllowed(false)
				_ = e.Player().SendMessage(render(cfg.Messages.Invalid))
			}
			e.SetMessage("")
			return
		}
		e.SetAllowed(false)
	}
}

func onDisconnect(state *authState) func(*proxy.DisconnectEvent) {
	return func(e *proxy.DisconnectEvent) {
		state.mu.Lock()
		id := e.Player().ID()
		delete(state.pending, id)
		if timer := state.timers[id]; timer != nil {
			timer.Stop()
			delete(state.timers, id)
		}
		state.mu.Unlock()
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func replace(template, key, value string) string {
	return strings.ReplaceAll(template, "{"+key+"}", value)
}

func render(template string) component.Component { return mini.Parse(template) }

func encodeBrand(brand string) []byte {
	data := make([]byte, 0, len(brand)+5)
	v := len(brand)
	for v >= 0x80 {
		data = append(data, byte(v)|0x80)
		v >>= 7
	}
	data = append(data, byte(v))
	return append(data, []byte(brand)...)
}
