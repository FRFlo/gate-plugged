package detection

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

// ForgeClientType represents the type of Forge-based client detected.
type ForgeClientType int

const (
	// ForgeClientForge is legacy Forge (before 1.20.2).
	ForgeClientForge ForgeClientType = iota + 1
	// ForgeClientNeoForge is NeoForge (1.20.2+).
	ForgeClientNeoForge
)

// String returns the human-readable name of the client type.
func (t ForgeClientType) String() string {
	switch t {
	case ForgeClientForge:
		return "Forge"
	case ForgeClientNeoForge:
		return "NeoForge"
	default:
		return "Unknown"
	}
}

// LunarModInfo holds information about a Lunar Client mod.
type LunarModInfo struct {
	ID          string
	DisplayName string
	Version     string
	Type        string
}

// IsFabric reports whether the mod is of the Fabric loader type.
func (m LunarModInfo) IsFabric() bool {
	return strings.Contains(strings.ToUpper(m.Type), "FABRIC")
}

// IsForge reports whether the mod is of the Forge loader type.
func (m LunarModInfo) IsForge() bool {
	return strings.Contains(strings.ToUpper(m.Type), "FORGE")
}

// ForgeModInfo holds information about a Forge/NeoForge mod detected via channel registration.
type ForgeModInfo struct {
	ModID   string
	Version string // may be empty
}

// String returns a human-readable representation of the mod info.
func (m ForgeModInfo) String() string {
	if m.Version != "" {
		return m.ModID + " (" + m.Version + ")"
	}
	return m.ModID
}

// DetectedPlayer holds thread-safe per-player state collected during detection.
//
// Java mapping:
//
//	HackedPlayer.genericChecks   → genericChecks  (set of triggered check IDs)
//	HackedPlayer.lunarMods       → lunarMods      (lowercase id → LunarModInfo)
//	HackedPlayer.lunarModsKnown  → lunarModsKnown (bool)
//	HackedPlayer.forgeMods       → forgeMods      (lowercase id → ForgeModInfo)
//	HackedPlayer.forgeModsKnown  → forgeModsKnown (bool)
//	HackedPlayer.forgeClientType → forgeClientType (nil/FORGE/NEOFORGE)
//	HackedPlayer.bedrockDetected → bedrockDetected (bool)
//	HackedPlayer.pendingActions  → pendingActions  ([]func() run at first server join)
type DetectedPlayer struct {
	UUID uuid.UUID

	mu sync.RWMutex

	genericChecks   map[string]struct{}
	lunarMods       map[string]LunarModInfo
	lunarModsKnown  bool
	forgeMods       map[string]ForgeModInfo
	forgeModsKnown  bool
	forgeClientType *ForgeClientType
	brand           string
	fabricChannels  bool
	bedrockDetected bool
	pendingActions  []func()
}

// newDetectedPlayer creates a new DetectedPlayer for the given UUID.
func newDetectedPlayer(id uuid.UUID) *DetectedPlayer {
	return &DetectedPlayer{
		UUID:          id,
		genericChecks: make(map[string]struct{}),
		lunarMods:     make(map[string]LunarModInfo),
		forgeMods:     make(map[string]ForgeModInfo),
	}
}

// --- Generic checks ---

// AddGenericCheck records that the check with the given ID has triggered for this player.
func (p *DetectedPlayer) AddGenericCheck(id string) {
	p.mu.Lock()
	p.genericChecks[id] = struct{}{}
	p.mu.Unlock()
}

// HasGenericCheck reports whether the check with the given ID has triggered.
func (p *DetectedPlayer) HasGenericCheck(id string) bool {
	p.mu.RLock()
	_, ok := p.genericChecks[id]
	p.mu.RUnlock()
	return ok
}

// GenericChecks returns a snapshot copy of all triggered check IDs.
func (p *DetectedPlayer) GenericChecks() []string {
	p.mu.RLock()
	out := make([]string, 0, len(p.genericChecks))
	for id := range p.genericChecks {
		out = append(out, id)
	}
	p.mu.RUnlock()
	return out
}

// --- Lunar mods ---

// SetLunarMods replaces the lunar mod list and marks lunar data as known.
// Nil entries or entries with empty IDs are skipped (mirrors Java behaviour).
func (p *DetectedPlayer) SetLunarMods(mods []LunarModInfo) {
	p.mu.Lock()
	clear(p.lunarMods)
	for _, m := range mods {
		if m.ID == "" {
			continue
		}
		p.lunarMods[strings.ToLower(m.ID)] = m
	}
	p.lunarModsKnown = true
	p.mu.Unlock()
}

// LunarMods returns a snapshot copy of all known lunar mods.
func (p *DetectedPlayer) LunarMods() []LunarModInfo {
	p.mu.RLock()
	out := make([]LunarModInfo, 0, len(p.lunarMods))
	for _, m := range p.lunarMods {
		out = append(out, m)
	}
	p.mu.RUnlock()
	return out
}

// HasLunarModsData reports whether lunar mod data has been received.
func (p *DetectedPlayer) HasLunarModsData() bool {
	p.mu.RLock()
	v := p.lunarModsKnown
	p.mu.RUnlock()
	return v
}

// HasLunarMod reports whether the player has the lunar mod with the given ID (case-insensitive).
func (p *DetectedPlayer) HasLunarMod(modID string) bool {
	if modID == "" {
		return false
	}
	p.mu.RLock()
	_, ok := p.lunarMods[strings.ToLower(modID)]
	p.mu.RUnlock()
	return ok
}

// --- Forge mods ---

// AddForgeMods appends forge mods, marking forge data as known.
// Nil entries or entries with empty ModID are skipped (mirrors Java behaviour).
func (p *DetectedPlayer) AddForgeMods(mods []ForgeModInfo) {
	p.mu.Lock()
	for _, m := range mods {
		if m.ModID == "" {
			continue
		}
		p.forgeMods[strings.ToLower(m.ModID)] = m
	}
	p.forgeModsKnown = true
	p.mu.Unlock()
}

// ForgeMods returns a snapshot copy of all known forge mods.
func (p *DetectedPlayer) ForgeMods() []ForgeModInfo {
	p.mu.RLock()
	out := make([]ForgeModInfo, 0, len(p.forgeMods))
	for _, m := range p.forgeMods {
		out = append(out, m)
	}
	p.mu.RUnlock()
	return out
}

// HasForgeModsData reports whether forge mod data has been received.
func (p *DetectedPlayer) HasForgeModsData() bool {
	p.mu.RLock()
	v := p.forgeModsKnown
	p.mu.RUnlock()
	return v
}

// HasForgeMod reports whether the player has the forge mod with the given ID (case-insensitive).
func (p *DetectedPlayer) HasForgeMod(modID string) bool {
	if modID == "" {
		return false
	}
	p.mu.RLock()
	_, ok := p.forgeMods[strings.ToLower(modID)]
	p.mu.RUnlock()
	return ok
}

// --- Forge client type ---

// SetForgeClientType sets the detected Forge client type.
func (p *DetectedPlayer) SetForgeClientType(t ForgeClientType) {
	p.mu.Lock()
	p.forgeClientType = &t
	p.mu.Unlock()
}

// ForgeClientType returns the detected forge client type and whether it is set.
func (p *DetectedPlayer) ForgeClientType() (ForgeClientType, bool) {
	p.mu.RLock()
	if p.forgeClientType == nil {
		p.mu.RUnlock()
		return 0, false
	}
	t := *p.forgeClientType
	p.mu.RUnlock()
	return t, true
}

// --- Bedrock ---

// SetBedrockDetected marks whether this player has been identified as a Bedrock client.
func (p *DetectedPlayer) SetBedrockDetected(v bool) {
	p.mu.Lock()
	p.bedrockDetected = v
	p.mu.Unlock()
}

// IsBedrockDetected reports whether the player is a Bedrock client.
func (p *DetectedPlayer) IsBedrockDetected() bool {
	p.mu.RLock()
	v := p.bedrockDetected
	p.mu.RUnlock()
	return v
}

// SetBrand records the latest client brand reported by Gate.
func (p *DetectedPlayer) SetBrand(brand string) {
	p.mu.Lock()
	p.brand = brand
	p.mu.Unlock()
}

// MarkFabricChannels records that Fabric loader channels were advertised.
func (p *DetectedPlayer) MarkFabricChannels() {
	p.mu.Lock()
	p.fabricChannels = true
	p.mu.Unlock()
}

// TryMarkSpoofedBrand atomically records the Fabric/vanilla brand spoof check
// once both public Gate events have supplied the required evidence.
func (p *DetectedPlayer) TryMarkSpoofedBrand(enabled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !enabled || p.fabricChannels == false || p.brand == "" {
		return false
	}
	brand := strings.ToLower(strings.TrimSpace(p.brand))
	if brand != "vanilla" && brand != "minecraft" {
		return false
	}
	if _, exists := p.genericChecks["spoofed_brand"]; exists {
		return false
	}
	p.genericChecks["spoofed_brand"] = struct{}{}
	return true
}

// --- Pending actions ---

// QueuePendingAction enqueues an action to be run when the player fully joins the server.
func (p *DetectedPlayer) QueuePendingAction(action func()) {
	if action == nil {
		return
	}
	p.mu.Lock()
	p.pendingActions = append(p.pendingActions, action)
	p.mu.Unlock()
}

// HasPendingActions reports whether there are any pending actions waiting.
func (p *DetectedPlayer) HasPendingActions() bool {
	p.mu.RLock()
	v := len(p.pendingActions) > 0
	p.mu.RUnlock()
	return v
}

// ExecutePendingActions drains and executes all queued pending actions.
// Actions run outside the lock so they may safely call back into this player.
func (p *DetectedPlayer) ExecutePendingActions() {
	p.mu.Lock()
	actions := p.pendingActions
	p.pendingActions = nil
	p.mu.Unlock()

	for _, action := range actions {
		action()
	}
}

// PlayerStore is a thread-safe registry that maps UUIDs to DetectedPlayer instances.
//
// It mirrors the semantics of HackedServer's ConcurrentHashMap<UUID, HackedPlayer>:
//   - Register: add-if-absent (preserves any state already accumulated by concurrent handlers)
//   - Get: get-or-create (ensures handlers can always get a player to mutate)
//   - Remove: clean removal
//   - Cleanup: full reset
type PlayerStore struct {
	mu      sync.RWMutex
	players map[uuid.UUID]*DetectedPlayer
}

// NewPlayerStore creates and returns an empty PlayerStore.
func NewPlayerStore() *PlayerStore {
	return &PlayerStore{
		players: make(map[uuid.UUID]*DetectedPlayer),
	}
}

// Register adds a player with the given UUID if none exists yet.
// If a player was already created by a concurrent handler (e.g. Get called
// before Register), the existing entry is kept so pending actions are preserved.
func (s *PlayerStore) Register(id uuid.UUID) {
	s.mu.Lock()
	if _, exists := s.players[id]; !exists {
		s.players[id] = newDetectedPlayer(id)
	}
	s.mu.Unlock()
}

// Get returns the DetectedPlayer for the given UUID, creating a new entry if
// one does not exist. This mirrors HackedServer.getPlayer() computeIfAbsent.
func (s *PlayerStore) Get(id uuid.UUID) *DetectedPlayer {
	// Fast path: player already exists.
	s.mu.RLock()
	p, ok := s.players[id]
	s.mu.RUnlock()
	if ok {
		return p
	}

	// Slow path: create under write lock, but check again to avoid a race
	// between two concurrent Gets for the same UUID.
	s.mu.Lock()
	if p, ok = s.players[id]; !ok {
		p = newDetectedPlayer(id)
		s.players[id] = p
	}
	s.mu.Unlock()
	return p
}

// Remove deletes the player with the given UUID from the store.
func (s *PlayerStore) Remove(id uuid.UUID) {
	s.mu.Lock()
	delete(s.players, id)
	s.mu.Unlock()
}

// Players returns a snapshot slice of all currently tracked DetectedPlayer pointers.
func (s *PlayerStore) Players() []*DetectedPlayer {
	s.mu.RLock()
	out := make([]*DetectedPlayer, 0, len(s.players))
	for _, p := range s.players {
		out = append(out, p)
	}
	s.mu.RUnlock()
	return out
}

// Len returns the current number of tracked players.
func (s *PlayerStore) Len() int {
	s.mu.RLock()
	n := len(s.players)
	s.mu.RUnlock()
	return n
}

// Cleanup removes all players from the store.
func (s *PlayerStore) Cleanup() {
	s.mu.Lock()
	s.players = make(map[uuid.UUID]*DetectedPlayer)
	s.mu.Unlock()
}
