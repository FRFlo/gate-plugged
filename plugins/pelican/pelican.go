package pelican

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/minekube/gate-plugin-template/util/chatfmt"
	"github.com/robinbraemer/event"
	"go.minekube.com/gate/pkg/edition/java/proxy"
)

var wakeSent = make(map[string]time.Time)
var wakeSentMu sync.Mutex

var Plugin = proxy.Plugin{
	Name: "Pelican",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		log := logr.FromContextOrDiscard(ctx)
		log.Info("Pelican plugin loading...")

		cfg, err := LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading pelican config from plugged.yml: %w", err)
		}

		c := NewHttpClient(cfg.Token, cfg.URL)

		event.Subscribe(p.Event(), 0, onKickedFromServerEvent(log, cfg, c))
		if cfg.Autostop {
			event.Subscribe(p.Event(), 0, onDisconnectEvent(log, cfg, c))
			event.Subscribe(p.Event(), 0, onConnectEvent)
		}

		log.Info("servers configured", "count", len(cfg.Servers))
		log.Info("Pelican plugin loaded.")

		return nil
	},
}

func onKickedFromServerEvent(log logr.Logger, cfg *Config, c *HttpClient) func(*proxy.KickedFromServerEvent) {
	return func(e *proxy.KickedFromServerEvent) {
		if e.OriginalReason() != nil {
			return
		}

		if s, ok := cfg.Servers[e.Server().ServerInfo().Name()]; ok {
			wakeSentMu.Lock()
			lastWake, alreadySent := wakeSent[s]
			wakeSentMu.Unlock()
			if alreadySent {
				if time.Since(lastWake) < 30*time.Second {
					log.Info("Already sent wake to Pelican", "server", e.Server().ServerInfo().Name(), "pelican", s)
					result := &proxy.RedirectPlayerKickResult{Message: chatfmt.Render("Pelican", cfg.Prefix, cfg.Messages.ServerStartingWait)}
					e.SetResult(result)
					return
				} else {
					wakeSentMu.Lock()
					delete(wakeSent, s)
					wakeSentMu.Unlock()
				}
			}

			log.Info("Sending wake to Pelican", "server", e.Server().ServerInfo().Name(), "pelican", s)

			err := c.StartServer(s)
			if err != nil {
				log.Error(err, "error starting server", "server", s)
				result := &proxy.RedirectPlayerKickResult{Message: chatfmt.Render("Pelican", cfg.Prefix, cfg.Messages.ErrorStarting)}
				e.SetResult(result)
				return
			}

			result := &proxy.RedirectPlayerKickResult{Message: chatfmt.Render("Pelican", cfg.Prefix, cfg.Messages.StartingServer)}
			e.SetResult(result)
			wakeSentMu.Lock()
			wakeSent[s] = time.Now()
			wakeSentMu.Unlock()
		}
	}
}

func onDisconnectEvent(log logr.Logger, cfg *Config, c *HttpClient) func(*proxy.DisconnectEvent) {
	return func(e *proxy.DisconnectEvent) {
		if conn := e.Player().CurrentServer(); conn != nil {
			srv := conn.Server()
			if s, ok := cfg.Servers[srv.ServerInfo().Name()]; ok {
				if srv.Players().Len() == 0 {
					log.Info("Planning to stop the server", "server", srv.ServerInfo().Name(), "pelican", s)
					go planStop(cfg, c, log, srv)
				}
			}
		}
	}
}
