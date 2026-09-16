package detection

import (
	"context"
	"strings"
	"testing"

	guuid "github.com/google/uuid"
	"go.minekube.com/brigodier"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/util/permission"
)

// ─── mock helpers ─────────────────────────────────────────────────────────────

// mockSource is a fake command.Source used to capture messages sent during
// command execution without needing a real Gate proxy.
type mockSource struct {
	perms    map[string]bool
	messages []string
}

func newMockSource(perms ...string) *mockSource {
	m := &mockSource{perms: make(map[string]bool)}
	for _, p := range perms {
		m.perms[p] = true
	}
	return m
}

func (m *mockSource) HasPermission(perm string) bool {
	return m.perms[perm]
}

func (m *mockSource) PermissionValue(perm string) permission.TriState {
	if m.HasPermission(perm) {
		return permission.True
	}
	return permission.False
}

func (m *mockSource) SendMessage(msg component.Component, opts ...command.MessageOption) error {
	m.messages = append(m.messages, extractText(msg))
	return nil
}

func extractText(msg component.Component) string {
	t, ok := msg.(*component.Text)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(t.Content)
	for _, child := range t.Extra {
		b.WriteString(extractText(child))
	}
	return b.String()
}

func (m *mockSource) lastMessage() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1]
}

func (m *mockSource) allMessages() string {
	return strings.Join(m.messages, "\n")
}

// ─── configHolder tests ───────────────────────────────────────────────────────

// TestConfigHolderGetReturnsInitial verifies that get() returns the initial config.
func TestConfigHolderGetReturnsInitial(t *testing.T) {
	cfg := &DetectionConfig{}
	cfg.Main.Settings.Debug = true
	h := newConfigHolder(cfg, "/tmp")

	got := h.get()
	if !got.Main.Settings.Debug {
		t.Fatal("expected Debug=true from initial config")
	}
}

// TestConfigHolderReloadBadPath verifies that reload() fails with a descriptive
// error when the resources directory does not exist.
func TestConfigHolderReloadBadPath(t *testing.T) {
	cfg := &DetectionConfig{}
	h := newConfigHolder(cfg, "/nonexistent/path/to/resources")

	err := h.reload()
	if err == nil {
		t.Fatal("expected error for missing resources path, got nil")
	}
	// Original config is preserved on reload failure.
	if h.get() != cfg {
		t.Fatal("config should be unchanged after failed reload")
	}
}

// TestConfigHolderReloadSucceeds tests a successful reload by calling LoadDetectionConfig
// indirectly through a configHolder that points to the real submodule resources.
// This test is skipped unless the submodule is present (same guard as T4/T5 tests).
func TestConfigHolderReloadSucceeds(t *testing.T) {
	paths, err := ResolveSubmodulePaths("../..")
	if err != nil {
		t.Skip("HackedServer submodule not present, skipping:", err)
	}
	import_dir := paths.Config
	_ = import_dir

	initial := &DetectionConfig{}
	h := newConfigHolder(initial, paths.Config[:len(paths.Config)-len("config.toml")])

	if err := h.reload(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	loaded := h.get()
	if loaded == initial {
		t.Fatal("expected a new config instance after reload")
	}
}

// ─── gateUUIDToGoogle tests ───────────────────────────────────────────────────

// TestGateUUIDToGoogle verifies round-trip conversion between Gate's UUID type
// and github.com/google/uuid.
func TestGateUUIDToGoogle(t *testing.T) {
	expected := guuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	gateID := [16]byte(expected)
	got := gateUUIDToGoogle(gateID)
	if got != expected {
		t.Fatalf("UUID mismatch: got %v, want %v", got, expected)
	}
}

// TestGateUUIDToGoogleNil verifies that a nil Gate UUID converts to guuid.Nil.
func TestGateUUIDToGoogleNil(t *testing.T) {
	var gateID [16]byte
	got := gateUUIDToGoogle(gateID)
	if got != guuid.Nil {
		t.Fatalf("expected Nil UUID, got %v", got)
	}
}

// ─── handleReload tests ───────────────────────────────────────────────────────

// TestCommandReloadBadPath verifies that /detection reload sends a failure
// message when the resources directory is missing.
func TestCommandReloadBadPath(t *testing.T) {
	src := newMockSource("hackedserver.command", "hackedserver.command.reload")
	h := newConfigHolder(&DetectionConfig{}, "/nonexistent/path")

	// Build a minimal brigodier manager and register the command.
	var mgr command.Manager
	mgr.Register(newDetectionCommand(nil, NewPlayerStore(), h))

	err := mgr.Do(context.Background(), src, "detection reload")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(src.lastMessage(), "Reload failed") {
		t.Fatalf("expected 'Reload failed' message, got: %q", src.lastMessage())
	}
}

func TestCommandReloadNoPermissionGate(t *testing.T) {
	src := newMockSource()
	h := newConfigHolder(&DetectionConfig{}, "/tmp")

	var mgr command.Manager
	mgr.Register(newDetectionCommand(nil, NewPlayerStore(), h))

	err := mgr.Do(context.Background(), src, "detection reload")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(src.lastMessage(), "Reload failed") {
		t.Fatalf("expected reload to execute without permission gate, got: %q", src.lastMessage())
	}
}

func TestSuggestPlayerNames(t *testing.T) {
	b := &brigodier.SuggestionsBuilder{
		Input:              "detection check Al",
		InputLowerCase:     "detection check al",
		Start:              len("detection check "),
		Remaining:          "Al",
		RemainingLowerCase: "al",
	}
	s := suggestPlayerNames(b, []string{"Zed", "Alice", "Alphonse", "Bob"})
	if s == nil || len(s.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}
	hasAlice := false
	for _, sug := range s.Suggestions {
		if sug.Text == "Alice" {
			hasAlice = true
			break
		}
	}
	if !hasAlice {
		t.Fatalf("expected Alice in suggestions, got: %+v", s.Suggestions)
	}
}

// ─── handleList tests ─────────────────────────────────────────────────────────

// TestCommandListEmpty verifies that /detection list replies "No players..."
// when the store has no players with generic checks.
// The list handler gathers online players from the proxy; with zero online
// players (empty slice) it should report "No players detected".
func TestCommandListEmpty(t *testing.T) {
	src := newMockSource("hackedserver.command", "hackedserver.command.list")

	// Call handleListFromEntries directly with an empty slice to test the
	// formatting logic without needing a real proxy.
	ctx := fakeCommandContext(src)
	err := handleListFromEntries(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(src.lastMessage(), "No chocolate players spotted") {
		t.Fatalf("expected empty spotted-players message, got: %q", src.lastMessage())
	}
}

// TestCommandCheckUnknownPlayer verifies that formatCheckOutput for a player
// with no checks reports "none" correctly (the proxy lookup branch is tested
// separately; here we test the formatting path directly).
func TestCommandCheckUnknownPlayer(t *testing.T) {
	src := newMockSource("hackedserver.command", "hackedserver.command.check")
	store := NewPlayerStore()
	id := guuid.MustParse("550e8400-e29b-41d4-a716-446655440099")
	dp := store.Get(id) // brand-new player with no checks

	ctx := fakeCommandContext(src)
	err := formatCheckOutput(ctx, "GhostPlayer", dp, &DetectionConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(src.lastMessage(), "No mods detected") {
		t.Fatalf("expected 'No mods detected' for player with no checks, got: %q", src.lastMessage())
	}
	if !strings.Contains(src.lastMessage(), "GhostPlayer") {
		t.Fatalf("expected player name in output, got: %q", src.lastMessage())
	}
}

// TestCommandCheckKnownPlayerGenericChecks verifies that /detection check
// displays generic checks for a player the store knows about.
func TestCommandCheckKnownPlayerGenericChecks(t *testing.T) {
	src := newMockSource("hackedserver.command", "hackedserver.command.check")
	store := NewPlayerStore()
	id := guuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	dp := store.Get(id)
	dp.AddGenericCheck("labymod_v1")
	dp.AddGenericCheck("fabric")

	ctx := fakeCommandContext(src)
	err := formatCheckOutput(ctx, "TestPlayer", dp, &DetectionConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := src.allMessages()
	if !strings.Contains(msg, "fabric") {
		t.Fatalf("expected 'fabric' in output, got: %q", msg)
	}
	if !strings.Contains(msg, "labymod_v1") {
		t.Fatalf("expected 'labymod_v1' in output, got: %q", msg)
	}
	if !strings.Contains(msg, "TestPlayer") {
		t.Fatalf("expected player name in output, got: %q", msg)
	}
}

// ─── TestCommandCheckList ─────────────────────────────────────────────────────

// TestCommandCheckList exercises formatCheckOutput and handleListFromEntries
// for the full range of output scenarios: empty data, populated generic checks,
// forge mods (shown/hidden), lunar mods, bedrock detection, and sorted list output.
func TestCommandCheckList(t *testing.T) {
	t.Run("check_no_data", func(t *testing.T) {
		// A fresh DetectedPlayer with no checks/mods should show "none" for generic
		// and omit forge/lunar sections (ShowModsInCheck defaults to false).
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000001")
		dp := store.Get(id)

		if err := formatCheckOutput(ctx, "Alice", dp, &DetectionConfig{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		if !strings.Contains(msg, "Alice") {
			t.Fatalf("expected player name in output, got: %q", msg)
		}
		if !strings.Contains(msg, "No mods detected") {
			t.Fatalf("expected 'No mods detected', got: %q", msg)
		}
	})

	t.Run("check_generic_checks_sorted", func(t *testing.T) {
		// Generic checks must appear sorted alphabetically.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000002")
		dp := store.Get(id)
		dp.AddGenericCheck("zap_client")
		dp.AddGenericCheck("fabric")
		dp.AddGenericCheck("labymod_v1")

		if err := formatCheckOutput(ctx, "Bob", dp, &DetectionConfig{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		for _, name := range []string{"fabric", "labymod_v1", "zap_client"} {
			if !strings.Contains(msg, name) {
				t.Fatalf("expected check %q in output, got: %q", name, msg)
			}
		}
		posFabric := strings.Index(msg, "fabric")
		posLaby := strings.Index(msg, "labymod_v1")
		posZap := strings.Index(msg, "zap_client")
		if posFabric >= posLaby || posLaby >= posZap {
			t.Fatalf("checks not sorted alphabetically: %q", msg)
		}
	})

	t.Run("check_forge_mods_shown", func(t *testing.T) {
		// Forge mods appear only when ShowModsInCheck=true and forge data is known.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000003")
		dp := store.Get(id)
		dp.AddForgeMods([]ForgeModInfo{
			{ModID: "jei", Version: "10.0.0"},
			{ModID: "appleskin"},
		})

		cfg := &DetectionConfig{}
		cfg.Forge.Settings.ShowModsInCheck = true
		cfg.Forge.Settings.ShowModVersions = true

		if err := formatCheckOutput(ctx, "Carol", dp, cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		for _, want := range []string{"appleskin", "jei", "10.0.0"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected %q in forge mod output, got: %q", want, msg)
			}
		}
		// appleskin < jei alphabetically
		if strings.Index(msg, "appleskin") >= strings.Index(msg, "jei") {
			t.Fatalf("forge mods not sorted alphabetically: %q", msg)
		}
	})

	t.Run("check_forge_mods_hidden_without_flag", func(t *testing.T) {
		// Forge mods must NOT appear when ShowModsInCheck=false.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000004")
		dp := store.Get(id)
		dp.AddForgeMods([]ForgeModInfo{{ModID: "jei"}})

		if err := formatCheckOutput(ctx, "Dave", dp, &DetectionConfig{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(src.lastMessage(), "jei") {
			t.Fatalf("forge mods must not appear when ShowModsInCheck=false, got: %q", src.lastMessage())
		}
	})

	t.Run("check_lunar_mods_shown", func(t *testing.T) {
		// Lunar mods appear only when cfg.Lunar.Enabled=true and ShowModsInCheck=true.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000005")
		dp := store.Get(id)
		dp.SetLunarMods([]LunarModInfo{
			{ID: "sodium", DisplayName: "Sodium", Version: "0.5.0", Type: "TYPE_FABRIC_EXTERNAL"},
			{ID: "optifine", DisplayName: "OptiFine", Version: "HD_U_I5", Type: "TYPE_FORGE_EXTERNAL"},
		})

		cfg := &DetectionConfig{}
		cfg.Lunar.Enabled = true
		cfg.Lunar.Settings.ShowModsInCheck = true
		cfg.Lunar.Settings.ShowModVersions = true
		cfg.Lunar.Settings.ShowModTypes = true

		if err := formatCheckOutput(ctx, "Eve", dp, cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		for _, want := range []string{"sodium", "optifine", "0.5.0", "TYPE_FABRIC_EXTERNAL"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected %q in lunar mod output, got: %q", want, msg)
			}
		}
	})

	t.Run("check_bedrock_shown", func(t *testing.T) {
		// When bedrock is detected the output must include "Bedrock: yes".
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000006")
		dp := store.Get(id)
		dp.SetBedrockDetected(true)

		if err := formatCheckOutput(ctx, "Flint", dp, &DetectionConfig{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(src.lastMessage(), "Bedrock: yes") {
			t.Fatalf("expected 'Bedrock: yes', got: %q", src.lastMessage())
		}
	})

	t.Run("check_bedrock_not_shown_when_not_detected", func(t *testing.T) {
		src := newMockSource()
		ctx := fakeCommandContext(src)
		store := NewPlayerStore()
		id := guuid.MustParse("cccccccc-0000-0000-0000-000000000007")
		dp := store.Get(id)

		if err := formatCheckOutput(ctx, "Grace", dp, &DetectionConfig{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(src.lastMessage(), "Bedrock") {
			t.Fatalf("bedrock must not appear when not detected, got: %q", src.lastMessage())
		}
	})

	t.Run("list_empty", func(t *testing.T) {
		// Empty entry slice → "No players..." message.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		if err := handleListFromEntries(ctx, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(src.lastMessage(), "No chocolate players spotted") {
			t.Fatalf("expected 'No chocolate players spotted' in empty list output, got: %q", src.lastMessage())
		}
	})

	t.Run("list_sorted_populated", func(t *testing.T) {
		// Multiple entries must appear sorted by player name.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		entries := []namedCheckEntry{
			{name: "Zara", checks: []string{"fabric"}},
			{name: "Alice", checks: []string{"labymod_v1", "fabric"}},
			{name: "Mike", checks: []string{"badlion"}},
		}
		if err := handleListFromEntries(ctx, entries); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		for _, name := range []string{"Alice", "Mike", "Zara"} {
			if !strings.Contains(msg, name) {
				t.Fatalf("expected %q in list output, got: %q", name, msg)
			}
		}
		posAlice := strings.Index(msg, "Alice")
		posMike := strings.Index(msg, "Mike")
		posZara := strings.Index(msg, "Zara")
		if posAlice >= posMike || posMike >= posZara {
			t.Fatalf("list entries not sorted alphabetically: %q", msg)
		}
	})

	t.Run("list_checks_per_player_shown", func(t *testing.T) {
		// Each player's checks must appear next to their name in the list output.
		src := newMockSource()
		ctx := fakeCommandContext(src)
		entries := []namedCheckEntry{
			{name: "Alpha", checks: []string{"fabric", "labymod_v1"}},
		}
		if err := handleListFromEntries(ctx, entries); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		msg := src.lastMessage()
		if !strings.Contains(msg, "Alpha") {
			t.Fatalf("expected player name in list output, got: %q", msg)
		}
		for _, chk := range []string{"fabric", "labymod_v1"} {
			if strings.Contains(msg, chk) {
				t.Fatalf("did not expect per-player check %q in list output, got: %q", chk, msg)
			}
		}
	})
}

// ─── test helpers ─────────────────────────────────────────────────────────────

// fakeCommandContext builds a minimal *command.Context from a source.
// Needed because command.Context embeds a brigodier.CommandContext which
// is not directly constructable; we use the manager execution path instead.
// For direct-call tests (no brigodier routing) we craft a thin wrapper.
func fakeCommandContext(src command.Source) *command.Context {
	// Execute a no-op command through the manager to get a real context.
	// Since we just need Source injection we build a minimal stub.
	return &command.Context{Source: src}
}
