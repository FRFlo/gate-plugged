package sharedconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const defaultConfigFile = "plugged.yml"

type Config struct {
	Global  GlobalConfig  `yaml:"global"`
	Plugins PluginsConfig `yaml:"plugins"`
}

type GlobalConfig struct {
	Chat ChatConfig `yaml:"chat"`
}

type ChatConfig struct {
	Format ChatFormatConfig `yaml:"format"`
}

type ChatFormatConfig struct {
	Prefix  string `yaml:"prefix"`
	Message string `yaml:"message"`
}

type PluginsConfig struct {
	Pelican   PelicanConfig   `yaml:"pelican"`
	Detection DetectionConfig `yaml:"detection"`
	Vanish    VanishConfig    `yaml:"vanish"`
	Kick      KickConfig      `yaml:"kick"`
	F3        F3Config        `yaml:"customF3Brand"`
	Auth      AuthConfig      `yaml:"loginPassword"`
}

type KickConfig struct {
	Messages KickMessages `yaml:"messages"`
}
type KickMessages struct {
	ConsoleOnly    string `yaml:"consoleOnly"`
	Usage          string `yaml:"usage"`
	PlayerNotFound string `yaml:"playerNotFound"`
	Kicked         string `yaml:"kicked"`
}

type F3Config struct {
	Enabled bool   `yaml:"enabled"`
	Brand   string `yaml:"brand"`
}
type AuthConfig struct {
	Enabled        bool         `yaml:"enabled"`
	LoginServer    string       `yaml:"loginServer"`
	HubServer      string       `yaml:"hubServer"`
	Passwords      []string     `yaml:"passwords"`
	Command        string       `yaml:"command"`
	TimeoutSeconds int          `yaml:"timeoutSeconds"`
	Messages       AuthMessages `yaml:"messages"`
}
type AuthMessages struct {
	ConsoleOnly string `yaml:"consoleOnly"`
	Invalid     string `yaml:"invalid"`
	Timeout     string `yaml:"timeout"`
	Success     string `yaml:"success"`
}

type PelicanConfig struct {
	Token    string            `yaml:"token"`
	URL      string            `yaml:"url"`
	Prefix   string            `yaml:"prefix"`
	AutoStop bool              `yaml:"autoStop"`
	Delay    int               `yaml:"delay"`
	Servers  map[string]string `yaml:"servers"`
	Messages PelicanMessages   `yaml:"messages"`
}

type DetectionConfig struct {
	Prefix   string            `yaml:"prefix"`
	Messages DetectionMessages `yaml:"messages"`
}

type VanishConfig struct {
	Prefix   string         `yaml:"prefix"`
	Messages VanishMessages `yaml:"messages"`
}

type PelicanMessages struct {
	ServerStartingWait string `yaml:"serverStartingWait"`
	ErrorStarting      string `yaml:"errorStarting"`
	StartingServer     string `yaml:"startingServer"`
}

type DetectionMessages struct {
	AvailableCommands  string `yaml:"availableCommands"`
	HelpReload         string `yaml:"helpReload"`
	HelpCheck          string `yaml:"helpCheck"`
	HelpList           string `yaml:"helpList"`
	ReloadFailed       string `yaml:"reloadFailed"`
	ReloadSuccess      string `yaml:"reloadSuccess"`
	PlayerNotFound     string `yaml:"playerNotFound"`
	Checking           string `yaml:"checking"`
	DetectedMods       string `yaml:"detectedMods"`
	NoModsDetected     string `yaml:"noModsDetected"`
	ForgeMods          string `yaml:"forgeMods"`
	NoForgeMods        string `yaml:"noForgeMods"`
	LunarMods          string `yaml:"lunarMods"`
	NoLunarMods        string `yaml:"noLunarMods"`
	BedrockDetected    string `yaml:"bedrockDetected"`
	NoPlayersSpotted   string `yaml:"noPlayersSpotted"`
	SpottedPlayers     string `yaml:"spottedPlayers"`
	ListBullet         string `yaml:"listBullet"`
	ModBullet          string `yaml:"modBullet"`
	ModVersionBullet   string `yaml:"modVersionBullet"`
	LunarTypeSuffix    string `yaml:"lunarTypeSuffix"`
	LunarVersionSuffix string `yaml:"lunarVersionSuffix"`
}

type VanishMessages struct {
	NoPermission       string `yaml:"noPermission"`
	OnlyPlayers        string `yaml:"onlyPlayers"`
	NoPermissionOthers string `yaml:"noPermissionOthers"`
	ProxyUnavailable   string `yaml:"proxyUnavailable"`
	PlayerNotFound     string `yaml:"playerNotFound"`
	TargetUnavailable  string `yaml:"targetUnavailable"`
	StateEnabled       string `yaml:"stateEnabled"`
	StateDisabled      string `yaml:"stateDisabled"`
	ToggleSelf         string `yaml:"toggleSelf"`
	ToggleOther        string `yaml:"toggleOther"`
	ToggleTarget       string `yaml:"toggleTarget"`
}

var (
	once    sync.Once
	mu      sync.RWMutex
	loaded  *Config
	loadErr error
)

func Load() (*Config, error) {
	once.Do(func() {
		cfg := defaultConfig()

		b, err := os.ReadFile(defaultConfigFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				loaded = &cfg
				return
			}
			loadErr = fmt.Errorf("read %s: %w", defaultConfigFile, err)
			loaded = &cfg
			return
		}

		if err = yaml.Unmarshal(b, &cfg); err != nil {
			loadErr = fmt.Errorf("unmarshal %s: %w", defaultConfigFile, err)
			loaded = &cfg
			return
		}

		normalize(&cfg)
		loaded = &cfg
	})

	if loaded == nil {
		cfg := defaultConfig()
		normalize(&cfg)
		loaded = &cfg
	}
	mu.RLock()
	cfg, err := loaded, loadErr
	mu.RUnlock()
	return cfg, err
}

// Reload rereads plugged.yml and atomically replaces the cached configuration.
// Missing files restore defaults; malformed files leave the previous snapshot
// untouched and return the parse error.
func Reload() (*Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(defaultConfigFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", defaultConfigFile, err)
		}
	} else if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", defaultConfigFile, err)
	}
	normalize(&cfg)
	mu.Lock()
	loaded = &cfg
	loadErr = nil
	mu.Unlock()
	return &cfg, nil
}

func (c *Config) FormatMessage(pluginName string, message string) string {
	if c == nil {
		tmp := defaultConfig()
		c = &tmp
	}
	prefix := strings.ReplaceAll(c.Global.Chat.Format.Prefix, "{plugin}", pluginName)
	body := strings.ReplaceAll(c.Global.Chat.Format.Message, "{message}", message)
	return prefix + body
}

func defaultConfig() Config {
	return Config{
		Global: GlobalConfig{
			Chat: ChatConfig{
				Format: ChatFormatConfig{
					Prefix:  "<gray>[<aqua>{plugin}</aqua>]</gray> ",
					Message: "<gray>{message}</gray>",
				},
			},
		},
		Plugins: PluginsConfig{
			Kick: KickConfig{Messages: KickMessages{
				ConsoleOnly:    "<red>Cette commande est réservée à la console.</red>",
				Usage:          "<gold>Usage : /kick [joueur] [raison]</gold>",
				PlayerNotFound: "<red>Joueur introuvable : {player}</red>",
				Kicked:         "<red>Vous avez été expulsé.</red>\n<gray>Raison : {reason}</gray>",
			}},
			F3: F3Config{Brand: "Gate"},
			Auth: AuthConfig{LoginServer: "limbo", HubServer: "", Command: "login", TimeoutSeconds: 60, Messages: AuthMessages{
				ConsoleOnly: "<red>Cette commande est réservée aux joueurs.</red>", Invalid: "<red>Mot de passe invalide.</red>", Timeout: "<red>Délai d'authentification dépassé.</red>", Success: "<green>Authentification réussie.</green>",
			}},
			Pelican: PelicanConfig{
				Token:    "Votre token Pelican",
				URL:      "https://demo.pelican.dev",
				Prefix:   "<gray>[<aqua>Pelican</aqua>]</gray> ",
				AutoStop: true,
				Delay:    60,
				Servers: map[string]string{
					"server1": "UUID du serveur auquel se connecter",
				},
				Messages: PelicanMessages{
					ServerStartingWait: "<yellow>Le serveur démarre, veuillez patienter...</yellow>",
					ErrorStarting:      "<red>Erreur lors du démarrage du serveur</red>",
					StartingServer:     "<yellow>Démarrage du serveur...</yellow>",
				},
			},
			Detection: DetectionConfig{
				Prefix: "<gray>[<aqua>HackedServer</aqua>]</gray> ",
				Messages: DetectionMessages{
					AvailableCommands:  "<gray>Commandes disponibles</gray>",
					HelpReload:         "<dark_gray>/hs <gray>reload <dark_gray>» <gray>recharger le plugin</gray>",
					HelpCheck:          "<dark_gray>/hs <gray>check <aqua>cible</aqua> <dark_gray>» <gray>vérifier les mods détectés</gray>",
					HelpList:           "<dark_gray>/hs <gray>list <dark_gray>» <gray>lister les joueurs repérés</gray>",
					ReloadFailed:       "<red>Échec du rechargement : {error}</red>",
					ReloadSuccess:      "<green>Rechargement réussi</green>",
					PlayerNotFound:     "<red>Joueur introuvable : {player}</red>",
					Checking:           "<aqua>Vérification de <gold>{player}</gold></aqua>",
					DetectedMods:       "<green>Mods détectés :</green>",
					NoModsDetected:     "<green>Aucun mod détecté</green>",
					ForgeMods:          "<green>Mods Forge/NeoForge :</green>",
					NoForgeMods:        "<green>Aucun mod Forge détecté</green>",
					LunarMods:          "<green>Mods Lunar Client :</green>",
					NoLunarMods:        "<green>Aucun mod Lunar Client détecté</green>",
					BedrockDetected:    "<green>Bedrock : oui</green>",
					NoPlayersSpotted:   "<green>Aucun joueur repéré</green>",
					SpottedPlayers:     "<green>Joueurs repérés :</green>",
					ListBullet:         "<dark_gray>- <gold>{value}</gold></dark_gray>",
					ModBullet:          "<dark_gray>- {value}</dark_gray>",
					ModVersionBullet:   "<dark_gray>- {value} ({version})</dark_gray>",
					LunarTypeSuffix:    " [{type}]",
					LunarVersionSuffix: " ({version})",
				},
			},
			Vanish: VanishConfig{
				Prefix: "<gray>[<aqua>Vanish</aqua>]</gray> ",
				Messages: VanishMessages{
					NoPermission:       "<red>Vous n'avez pas la permission.</red>",
					OnlyPlayers:        "<red>Seuls les joueurs peuvent utiliser /vanish sans cible.</red>",
					NoPermissionOthers: "<red>Vous n'avez pas la permission de viser d'autres joueurs.</red>",
					ProxyUnavailable:   "<red>Le proxy est indisponible pour rechercher le joueur.</red>",
					PlayerNotFound:     "<red>Joueur introuvable : {player}</red>",
					TargetUnavailable:  "<red>Cible indisponible.</red>",
					StateEnabled:       "<green>activé</green>",
					StateDisabled:      "<red>désactivé</red>",
					ToggleSelf:         "<gray>Vanish {state}<gray>.</gray></gray>",
					ToggleOther:        "<gray>Vanish {state}<gray> pour <gold>{player}</gold>.</gray></gray>",
					ToggleTarget:       "<gray>Votre vanish est maintenant {state}<gray>.</gray></gray>",
				},
			},
		},
	}
}

func normalize(cfg *Config) {
	if cfg.Plugins.Pelican.Servers == nil {
		cfg.Plugins.Pelican.Servers = map[string]string{
			"server1": "The UUID of the server you want to connect to",
		}
	}

	if cfg.Global.Chat.Format.Prefix == "" {
		cfg.Global.Chat.Format.Prefix = "<gray>[<aqua>{plugin}</aqua>]</gray> "
	}
	if cfg.Global.Chat.Format.Message == "" {
		cfg.Global.Chat.Format.Message = "<gray>{message}</gray>"
	}
	if cfg.Plugins.Pelican.Prefix == "" {
		cfg.Plugins.Pelican.Prefix = "<gray>[<aqua>Pelican</aqua>]</gray> "
	}
	if cfg.Plugins.Pelican.Messages.ServerStartingWait == "" {
		cfg.Plugins.Pelican.Messages.ServerStartingWait = "<yellow>Server is starting, please wait...</yellow>"
	}
	if cfg.Plugins.Pelican.Messages.ErrorStarting == "" {
		cfg.Plugins.Pelican.Messages.ErrorStarting = "<red>Error starting server</red>"
	}
	if cfg.Plugins.Pelican.Messages.StartingServer == "" {
		cfg.Plugins.Pelican.Messages.StartingServer = "<yellow>Starting server...</yellow>"
	}
	if cfg.Plugins.Detection.Prefix == "" {
		cfg.Plugins.Detection.Prefix = "<gray>[<aqua>HackedServer</aqua>]</gray> "
	}
	normalizeDetectionMessages(&cfg.Plugins.Detection.Messages)
	if cfg.Plugins.Vanish.Prefix == "" {
		cfg.Plugins.Vanish.Prefix = "<gray>[<aqua>Vanish</aqua>]</gray> "
	}
	normalizeVanishMessages(&cfg.Plugins.Vanish.Messages)
}

func normalizeDetectionMessages(m *DetectionMessages) {
	def := defaultConfig().Plugins.Detection.Messages
	if m.AvailableCommands == "" {
		m.AvailableCommands = def.AvailableCommands
	}
	if m.HelpReload == "" {
		m.HelpReload = def.HelpReload
	}
	if m.HelpCheck == "" {
		m.HelpCheck = def.HelpCheck
	}
	if m.HelpList == "" {
		m.HelpList = def.HelpList
	}
	if m.ReloadFailed == "" {
		m.ReloadFailed = def.ReloadFailed
	}
	if m.ReloadSuccess == "" {
		m.ReloadSuccess = def.ReloadSuccess
	}
	if m.PlayerNotFound == "" {
		m.PlayerNotFound = def.PlayerNotFound
	}
	if m.Checking == "" {
		m.Checking = def.Checking
	}
	if m.DetectedMods == "" {
		m.DetectedMods = def.DetectedMods
	}
	if m.NoModsDetected == "" {
		m.NoModsDetected = def.NoModsDetected
	}
	if m.ForgeMods == "" {
		m.ForgeMods = def.ForgeMods
	}
	if m.NoForgeMods == "" {
		m.NoForgeMods = def.NoForgeMods
	}
	if m.LunarMods == "" {
		m.LunarMods = def.LunarMods
	}
	if m.NoLunarMods == "" {
		m.NoLunarMods = def.NoLunarMods
	}
	if m.BedrockDetected == "" {
		m.BedrockDetected = def.BedrockDetected
	}
	if m.NoPlayersSpotted == "" {
		m.NoPlayersSpotted = def.NoPlayersSpotted
	}
	if m.SpottedPlayers == "" {
		m.SpottedPlayers = def.SpottedPlayers
	}
	if m.ListBullet == "" {
		m.ListBullet = def.ListBullet
	}
	if m.ModBullet == "" {
		m.ModBullet = def.ModBullet
	}
	if m.ModVersionBullet == "" {
		m.ModVersionBullet = def.ModVersionBullet
	}
	if m.LunarTypeSuffix == "" {
		m.LunarTypeSuffix = def.LunarTypeSuffix
	}
	if m.LunarVersionSuffix == "" {
		m.LunarVersionSuffix = def.LunarVersionSuffix
	}
}

func normalizeVanishMessages(m *VanishMessages) {
	def := defaultConfig().Plugins.Vanish.Messages
	if m.NoPermission == "" {
		m.NoPermission = def.NoPermission
	}
	if m.OnlyPlayers == "" {
		m.OnlyPlayers = def.OnlyPlayers
	}
	if m.NoPermissionOthers == "" {
		m.NoPermissionOthers = def.NoPermissionOthers
	}
	if m.ProxyUnavailable == "" {
		m.ProxyUnavailable = def.ProxyUnavailable
	}
	if m.PlayerNotFound == "" {
		m.PlayerNotFound = def.PlayerNotFound
	}
	if m.TargetUnavailable == "" {
		m.TargetUnavailable = def.TargetUnavailable
	}
	if m.StateEnabled == "" {
		m.StateEnabled = def.StateEnabled
	}
	if m.StateDisabled == "" {
		m.StateDisabled = def.StateDisabled
	}
	if m.ToggleSelf == "" {
		m.ToggleSelf = def.ToggleSelf
	}
	if m.ToggleOther == "" {
		m.ToggleOther = def.ToggleOther
	}
	if m.ToggleTarget == "" {
		m.ToggleTarget = def.ToggleTarget
	}
}
