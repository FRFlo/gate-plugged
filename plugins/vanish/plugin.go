package vanish

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	guuid "github.com/google/uuid"
	sharedcfg "github.com/minekube/gate-plugin-template/plugins/sharedconfig"
	"github.com/minekube/gate-plugin-template/util/chatfmt"
	"github.com/robinbraemer/event"
	"go.minekube.com/brigodier"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/command/suggest"
	"go.minekube.com/gate/pkg/edition/java/proxy"
	gateuuid "go.minekube.com/gate/pkg/util/uuid"
)

const (
	permCommand     = "vanish.command"
	permOthers      = "vanish.command.others"
	permSeeVanished = "vanish.see"
)

var vanishPrefix = "<gray>[<aqua>Vanish</aqua>]</gray> "

var vanishMessages = sharedcfg.VanishMessages{
	NoPermission:       "<red>You do not have permission.</red>",
	OnlyPlayers:        "<red>Only players can use /vanish without a target.</red>",
	NoPermissionOthers: "<red>You do not have permission to target others.</red>",
	ProxyUnavailable:   "<red>Proxy is unavailable for player lookup.</red>",
	PlayerNotFound:     "<red>Player not found: {player}</red>",
	TargetUnavailable:  "<red>Target unavailable.</red>",
	StateEnabled:       "<green>enabled</green>",
	StateDisabled:      "<red>disabled</red>",
	ToggleSelf:         "<gray>Vanish {state}<gray>.</gray></gray>",
	ToggleOther:        "<gray>Vanish {state}<gray> for <gold>{player}</gold>.</gray></gray>",
	ToggleTarget:       "<gray>Your vanish is now {state}<gray>.</gray></gray>",
}

var Plugin = proxy.Plugin{
	Name: "Vanish",
	Init: initVanish,
}

func initVanish(ctx context.Context, p *proxy.Proxy) error {
	log := logr.FromContextOrDiscard(ctx)
	if rootCfg, err := sharedcfg.Load(); err == nil {
		vanishPrefix = rootCfg.Plugins.Vanish.Prefix
		vanishMessages = rootCfg.Plugins.Vanish.Messages
	} else {
		log.Error(err, "failed to load plugged.yml, using default vanish prefix")
	}
	store := newStore()

	p.Command().RegisterWithAliases(newVanishCommand(p, store, log), "v")

	event.Subscribe(p.Event(), 0, onServerPostConnectEvent(p, store, log))
	event.Subscribe(p.Event(), 0, onDisconnectEvent(store))

	log.Info("Vanish plugin loaded.")
	return nil
}

type store struct {
	mu       sync.RWMutex
	vanished map[guuid.UUID]struct{}
}

func newStore() *store {
	return &store{vanished: make(map[guuid.UUID]struct{})}
}

func (s *store) Toggle(id guuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.vanished[id]; ok {
		delete(s.vanished, id)
		return false
	}
	s.vanished[id] = struct{}{}
	return true
}

func (s *store) IsVanished(id guuid.UUID) bool {
	s.mu.RLock()
	_, ok := s.vanished[id]
	s.mu.RUnlock()
	return ok
}

func (s *store) Remove(id guuid.UUID) {
	s.mu.Lock()
	delete(s.vanished, id)
	s.mu.Unlock()
}

func (s *store) Snapshot() []guuid.UUID {
	s.mu.RLock()
	out := make([]guuid.UUID, 0, len(s.vanished))
	for id := range s.vanished {
		out = append(out, id)
	}
	s.mu.RUnlock()
	return out
}

func gateUUIDToGoogle(id [16]byte) guuid.UUID {
	return guuid.UUID(id)
}

func newVanishCommand(p *proxy.Proxy, s *store, log logr.Logger) brigodier.LiteralNodeBuilder {
	const playerArg = "player"

	return brigodier.Literal("vanish").
		Executes(command.Command(func(c *command.Context) error {
			if !c.HasPermission(permCommand) {
				return c.SendMessage(vanishMessage(vanishMessages.NoPermission))
			}

			self, ok := c.Source.(proxy.Player)
			if !ok {
				return c.Source.SendMessage(vanishMessage(vanishMessages.OnlyPlayers))
			}

			return toggleTargetVanish(c, p, s, log, self)
		})).
		Then(
			brigodier.Argument(playerArg, brigodier.String).
				Suggests(playerSuggestionProvider(p, s)).
				Executes(command.Command(func(c *command.Context) error {
					if !c.HasPermission(permCommand) {
						return c.SendMessage(vanishMessage(vanishMessages.NoPermission))
					}
					if !c.HasPermission(permOthers) {
						return c.SendMessage(vanishMessage(vanishMessages.NoPermissionOthers))
					}

					if p == nil {
						return c.SendMessage(vanishMessage(vanishMessages.ProxyUnavailable))
					}

					targetName := c.String(playerArg)
					target := p.PlayerByName(targetName)
					if target == nil {
						return c.SendMessage(vanishMessage(chatfmt.ApplyPlaceholders(vanishMessages.PlayerNotFound, map[string]string{"player": targetName})))
					}

					return toggleTargetVanish(c, p, s, log, target)
				})),
		)
}

func playerSuggestionProvider(p *proxy.Proxy, s *store) brigodier.SuggestionProvider {
	return command.SuggestFunc(func(c *command.Context, b *brigodier.SuggestionsBuilder) *brigodier.Suggestions {
		if !canManageOthers(c.Source) {
			return b.Build()
		}

		viewerCanSeeVanished := c.HasPermission(permSeeVanished)
		names := make([]string, 0)
		if p != nil {
			names = make([]string, 0, len(p.Players()))
			for _, player := range p.Players() {
				if s != nil && s.IsVanished(gateUUIDToGoogle(player.ID())) && !viewerCanSeeVanished {
					continue
				}
				names = append(names, player.Username())
			}
			sort.Strings(names)
		}
		return suggest.Similar(b, names).Build()
	})
}

func canManageOthers(src command.Source) bool {
	if src == nil {
		return false
	}
	return src.HasPermission(permCommand) && src.HasPermission(permOthers)
}

func toggleTargetVanish(c *command.Context, p *proxy.Proxy, s *store, log logr.Logger, target proxy.Player) error {
	if target == nil {
		return c.SendMessage(vanishMessage(vanishMessages.TargetUnavailable))
	}

	vanished := s.Toggle(gateUUIDToGoogle(target.ID()))
	applyTargetVisibility(p, s, target, vanished, log)

	state := vanishMessages.StateEnabled
	if !vanished {
		state = vanishMessages.StateDisabled
	}

	if target.Username() == sourceName(c.Source) {
		return c.SendMessage(vanishMessage(chatfmt.ApplyPlaceholders(vanishMessages.ToggleSelf, map[string]string{"state": state})))
	}

	if err := c.SendMessage(vanishMessage(chatfmt.ApplyPlaceholders(vanishMessages.ToggleOther, map[string]string{"state": state, "player": target.Username()}))); err != nil {
		return err
	}

	_ = target.SendMessage(vanishMessage(chatfmt.ApplyPlaceholders(vanishMessages.ToggleTarget, map[string]string{"state": state})))
	return nil
}

func vanishMessage(message string) component.Component {
	return chatfmt.Render("Vanish", vanishPrefix, message)
}

func sourceName(src command.Source) string {
	p, ok := src.(proxy.Player)
	if !ok {
		return ""
	}
	return p.Username()
}

func onServerPostConnectEvent(p *proxy.Proxy, s *store, log logr.Logger) func(*proxy.ServerPostConnectEvent) {
	return func(e *proxy.ServerPostConnectEvent) {
		viewer := e.Player()
		applyAllToViewer(p, s, viewer, log)

		viewerID := gateUUIDToGoogle(viewer.ID())
		if s.IsVanished(viewerID) {
			applyTargetVisibility(p, s, viewer, true, log)
		}

		go func(viewer proxy.Player) {
			time.Sleep(300 * time.Millisecond)
			applyAllToViewer(p, s, viewer, log)
			if s.IsVanished(gateUUIDToGoogle(viewer.ID())) {
				applyTargetVisibility(p, s, viewer, true, log)
			}
		}(viewer)
	}
}

func onDisconnectEvent(s *store) func(*proxy.DisconnectEvent) {
	return func(e *proxy.DisconnectEvent) {
		s.Remove(gateUUIDToGoogle(e.Player().ID()))
	}
}

func applyAllToViewer(p *proxy.Proxy, s *store, viewer proxy.Player, log logr.Logger) {
	if p == nil || viewer == nil {
		return
	}

	entries := viewer.TabList().Entries()
	viewerCanSee := viewer.HasPermission(permSeeVanished)
	for _, vanishedID := range s.Snapshot() {
		entry, ok := entries[gateuuid.UUID(vanishedID)]
		if !ok {
			continue
		}
		listed := listedForViewer(true, viewerCanSee)
		if err := entry.SetListed(listed); err != nil {
			log.Error(err, "failed to set tab list visibility", "viewer", viewer.Username(), "vanished_uuid", vanishedID.String(), "listed", listed)
		}
	}
}

func applyTargetVisibility(p *proxy.Proxy, s *store, target proxy.Player, vanished bool, log logr.Logger) {
	if p == nil || target == nil {
		return
	}

	targetID := gateUUIDToGoogle(target.ID())
	targetGateID := target.ID()
	for _, viewer := range p.Players() {
		if viewer == nil {
			continue
		}
		if gateUUIDToGoogle(viewer.ID()) == targetID {
			continue
		}

		entry, ok := viewer.TabList().Entries()[targetGateID]
		if !ok {
			continue
		}

		listed := listedForViewer(vanished, viewer.HasPermission(permSeeVanished))

		if err := entry.SetListed(listed); err != nil {
			log.Error(err, "failed to set target tab list visibility", "viewer", viewer.Username(), "target", target.Username(), "listed", listed)
		}
	}

	status := "unvanished"
	if vanished {
		status = "vanished"
	}
	log.Info("vanish toggled", "player", target.Username(), "status", status)
}

func listedForViewer(vanished bool, viewerCanSeeVanished bool) bool {
	if !vanished {
		return true
	}
	return viewerCanSeeVanished
}
