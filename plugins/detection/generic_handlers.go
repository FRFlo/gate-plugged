package detection

// ─── Handler result types ─────────────────────────────────────────────────────

// GenericCheckTrigger represents a generic check that fired.
// Callers (Task 14) use Name and ActionIDs to enqueue pending actions.
type GenericCheckTrigger struct {
	// CheckID is the key from GenericConfig.Checks (e.g. "labymod_v1").
	CheckID string
	// Name is the human-readable display name from GenericCheck.Name.
	Name string
	// ActionIDs are the action IDs from GenericCheck.Actions.
	ActionIDs []string
}

// BrandHandlerResult is the aggregate output of HandleBrandPayload.
// Callers (Task 14) queue actions from each trigger on the player.
type BrandHandlerResult struct {
	// GenericTriggers are generic checks that matched the brand channel.
	GenericTriggers []GenericCheckTrigger
	// ForgeTriggers are forge/neoforge triggers from brand-string Forge detection.
	ForgeTriggers []ForgeActionTrigger
	// BedrockDetected is true if the brand identified a Bedrock/geyser client.
	BedrockDetected bool
	// BedrockLabel is the human-readable label from BedrockConfig.Label.
	BedrockLabel string
	// BedrockActionIDs are the action IDs from BedrockConfig.Actions.
	BedrockActionIDs      []string
	SpoofedBrandDetected  bool
	SpoofedBrandActionIDs []string
}

// ChannelHandlerResult is the aggregate output of HandleChannelRegister.
type ChannelHandlerResult struct {
	// GenericTriggers are generic checks that matched the register channel.
	GenericTriggers []GenericCheckTrigger
	// ForgeTriggers are forge mod triggers derived from channel namespaces.
	ForgeTriggers         []ForgeActionTrigger
	SpoofedBrandDetected  bool
	SpoofedBrandActionIDs []string
}

// ─── Brand payload handler ────────────────────────────────────────────────────

// HandleBrandPayload processes a minecraft:brand plugin-message payload and
// returns detection results without mutating any global state outside player.
//
// It evaluates every GenericCheck against the brand channel, then performs
// Bedrock brand detection (if enabled), then performs Forge client-type
// detection via brand string.
//
// The bypass flag mirrors Java's hackedserver.bypass permission:
//   - When bypass is true, player state is still mutated (so /detection check
//     can still display data) but ActionIDs in all returned triggers are cleared,
//     preventing action execution. GenericChecks are still recorded.
//
// Call this for the "minecraft:brand" (or "brand" bare) channel only.
//
// Java mapping: CustomPayloadListener.onPacketReceive (brand path) +
//
//	processForgePacket (BRAND_CHANNEL branch)
func HandleBrandPayload(
	player *DetectedPlayer,
	history *MessageHistory,
	brand string,
	cfg DetectionConfig,
	bypass bool,
) BrandHandlerResult {
	var result BrandHandlerResult
	player.SetBrand(brand)

	// ── 1. Generic checks ────────────────────────────────────────────────────
	if cfg.Generic.Enabled {
		for checkID, check := range cfg.Generic.Checks {
			// Skip checks already triggered for this player (dedup at check level).
			if player.HasGenericCheck(checkID) {
				continue
			}
			matched := GenericMatch(GenericMatchInput{
				Player:         player,
				History:        history,
				Check:          check,
				Channel:        brandChannel,
				Message:        brand,
				SkipDuplicates: cfg.Main.Settings.SkipDuplicates,
			})
			if !matched {
				continue
			}
			// Record on player state regardless of bypass.
			player.AddGenericCheck(checkID)

			actionIDs := check.Actions
			if bypass {
				actionIDs = nil
			}
			result.GenericTriggers = append(result.GenericTriggers, GenericCheckTrigger{
				CheckID:   checkID,
				Name:      check.Name,
				ActionIDs: actionIDs,
			})
		}
	}

	// ── 2. Bedrock brand detection ────────────────────────────────────────────
	detected, label, bedrockActions := ApplyBedrockBrand(player, brand, cfg.Bedrock)
	if detected {
		result.BedrockDetected = true
		result.BedrockLabel = label
		result.BedrockActionIDs = bedrockActions
		if bypass {
			result.BedrockActionIDs = nil
		}
	}

	// ── 3. Forge client-type detection via brand string ───────────────────────
	if cfg.Forge.Enabled {
		clientType, ok := ParseClientTypeFromBrand(brand)
		if ok {
			// Only process if forge client type hasn't been set yet (mirrors Java
			// if (clientType != null && hackedPlayer.getForgeClientType() == null)).
			if _, alreadySet := player.ForgeClientType(); !alreadySet {
				triggers := ProcessClientType(player, clientType, cfg.Forge)
				if bypass {
					// Clear action IDs but keep the triggers so the caller knows detection fired.
					for i := range triggers {
						triggers[i].ActionIDs = nil
					}
				}
				result.ForgeTriggers = append(result.ForgeTriggers, triggers...)
			}
		}
	}
	if cfg.Forge.Enabled && player.TryMarkSpoofedBrand(cfg.Forge.Spoofing.Enabled) {
		result.SpoofedBrandDetected = true
		if !bypass {
			result.SpoofedBrandActionIDs = append([]string(nil), cfg.Forge.Spoofing.Actions...)
		}
	}

	return result
}

// ─── Channel register handler ─────────────────────────────────────────────────

// HandleChannelRegister processes a minecraft:register plugin-message payload
// (a null-byte or whitespace-separated list of channel identifiers) and returns
// detection results.
//
// It evaluates every GenericCheck against the register channel (using the raw
// payload string as the message), then feeds the parsed channel namespaces into
// the Forge mod detector.
//
// The bypass flag mirrors Java's hackedserver.bypass permission:
//   - When bypass is true, player state is still mutated but ActionIDs in all
//     returned triggers are cleared.
//
// rawPayload is the UTF-8 decoded bytes of the minecraft:register packet data.
// channels is the pre-parsed list from Gate's ChannelRegisterEvent (may be empty
// if the caller only has raw bytes; in that case pass nil and non-nil rawPayload).
//
// Java mapping: CustomPayloadListener.onPacketReceive (REGISTER_CHANNEL path) +
//
//	processForgePacket (REGISTER_CHANNEL branch)
func HandleChannelRegister(
	player *DetectedPlayer,
	history *MessageHistory,
	rawPayload string,
	channels []string,
	cfg DetectionConfig,
	bypass bool,
) ChannelHandlerResult {
	var result ChannelHandlerResult
	fabricChannels := ContainsFabricChannels(rawPayload)
	if !fabricChannels {
		for _, channel := range channels {
			if ContainsFabricChannels(channel) {
				fabricChannels = true
				break
			}
		}
	}
	if fabricChannels {
		player.MarkFabricChannels()
	}

	// ── 1. Generic checks (using raw payload as message) ─────────────────────
	if cfg.Generic.Enabled {
		for checkID, check := range cfg.Generic.Checks {
			if player.HasGenericCheck(checkID) {
				continue
			}
			matched := GenericMatch(GenericMatchInput{
				Player:         player,
				History:        history,
				Check:          check,
				Channel:        registerChannel,
				Message:        rawPayload,
				SkipDuplicates: cfg.Main.Settings.SkipDuplicates,
			})
			if !matched {
				continue
			}
			player.AddGenericCheck(checkID)

			actionIDs := check.Actions
			if bypass {
				actionIDs = nil
			}
			result.GenericTriggers = append(result.GenericTriggers, GenericCheckTrigger{
				CheckID:   checkID,
				Name:      check.Name,
				ActionIDs: actionIDs,
			})
		}
	}

	// ── 2. Forge mod detection from channel namespaces ────────────────────────
	if cfg.Forge.Enabled {
		// Prefer pre-parsed channel list from Gate; fall back to raw payload parsing.
		var mods []ForgeModInfo
		if len(channels) > 0 {
			mods = ParseModsFromChannels(channels)
		} else if rawPayload != "" {
			mods = ParseModsFromRegisterPayload([]byte(rawPayload))
		}

		if len(mods) > 0 {
			triggers := ProcessMods(player, mods, cfg.Forge)
			if bypass {
				for i := range triggers {
					triggers[i].ActionIDs = nil
				}
			}
			result.ForgeTriggers = append(result.ForgeTriggers, triggers...)
		}
	}
	if cfg.Forge.Enabled && player.TryMarkSpoofedBrand(cfg.Forge.Spoofing.Enabled) {
		result.SpoofedBrandDetected = true
		if !bypass {
			result.SpoofedBrandActionIDs = append([]string(nil), cfg.Forge.Spoofing.Actions...)
		}
	}

	return result
}

// ─── Channel constants ────────────────────────────────────────────────────────

// brandChannel is the Minecraft plugin-message channel that carries the client
// brand string. Matches ForgeChannelParser.BRAND_CHANNEL in Java.
const brandChannel = "minecraft:brand"

// registerChannel is the channel used by clients to announce custom channels they
// listen on. Matches ForgeChannelParser.REGISTER_CHANNEL in Java.
const registerChannel = "minecraft:register"
