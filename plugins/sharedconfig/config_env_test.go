package sharedconfig

import "testing"

func TestApplyEnvOverridesContainerSettings(t *testing.T) {
	t.Setenv("PLUGGED_PLUGINS_PELICAN_TOKEN", "container-token")
	t.Setenv("PLUGGED_PLUGINS_PELICAN_URL", "https://pelican.internal")
	t.Setenv("PLUGGED_PLUGINS_PELICAN_AUTO_STOP", "false")
	t.Setenv("PLUGGED_PLUGINS_PELICAN_DELAY", "15")
	t.Setenv("PLUGGED_PLUGINS_PELICAN_SERVERS_SERVER1", "server-uuid")
	t.Setenv("PLUGGED_PLUGINS_LOGIN_PASSWORD_PASSWORDS", "first,two")

	cfg := defaultConfig()
	normalize(&cfg)
	applyEnv(&cfg)

	if cfg.Plugins.Pelican.Token != "container-token" {
		t.Errorf("token = %q, want container-token", cfg.Plugins.Pelican.Token)
	}
	if cfg.Plugins.Pelican.URL != "https://pelican.internal" {
		t.Errorf("url = %q, want https://pelican.internal", cfg.Plugins.Pelican.URL)
	}
	if cfg.Plugins.Pelican.AutoStop {
		t.Error("auto stop should be disabled by environment")
	}
	if cfg.Plugins.Pelican.Delay != 15 {
		t.Errorf("delay = %d, want 15", cfg.Plugins.Pelican.Delay)
	}
	if cfg.Plugins.Pelican.Servers["server1"] != "server-uuid" {
		t.Errorf("server UUID = %q, want server-uuid", cfg.Plugins.Pelican.Servers["server1"])
	}
	if len(cfg.Plugins.Auth.Passwords) != 2 || cfg.Plugins.Auth.Passwords[1] != "two" {
		t.Errorf("passwords = %#v, want two trimmed values", cfg.Plugins.Auth.Passwords)
	}
}

func TestApplyGenericEnvOverridesAnyYamlKey(t *testing.T) {
	t.Setenv("PLUGGED_PLUGINS_PELICAN_MESSAGES_ERROR_STARTING", "custom error")
	t.Setenv("PLUGGED_PLUGINS_DETECTION_PREFIX", "[Detection] ")
	t.Setenv("PLUGGED_PLUGINS_LOGIN_PASSWORD_PASSWORDS", "one,two")

	cfg := defaultConfig()
	applyGenericEnv(&cfg)

	if cfg.Plugins.Pelican.Messages.ErrorStarting != "custom error" {
		t.Errorf("message = %q, want custom error", cfg.Plugins.Pelican.Messages.ErrorStarting)
	}
	if cfg.Plugins.Detection.Prefix != "[Detection] " {
		t.Errorf("prefix = %q, want [Detection]", cfg.Plugins.Detection.Prefix)
	}
	if len(cfg.Plugins.Auth.Passwords) != 2 || cfg.Plugins.Auth.Passwords[0] != "one" {
		t.Errorf("passwords = %#v, want two values", cfg.Plugins.Auth.Passwords)
	}
}

func TestApplyEnvIgnoresInvalidNumbersAndBooleans(t *testing.T) {
	t.Setenv("PLUGGED_PLUGINS_PELICAN_AUTO_STOP", "not-a-bool")
	t.Setenv("PLUGGED_PLUGINS_PELICAN_DELAY", "not-a-number")

	cfg := defaultConfig()
	applyEnv(&cfg)

	if !cfg.Plugins.Pelican.AutoStop {
		t.Error("invalid auto stop value should keep the current value")
	}
	if cfg.Plugins.Pelican.Delay != 60 {
		t.Errorf("delay = %d, want unchanged default 60", cfg.Plugins.Pelican.Delay)
	}
}
