/**
 * Settings Store - Zustand Store with MMKV persistence
 *
 * Global state management for app settings using Zustand + MMKV
 * Simplified for K2 Gateway approach (removed Janus/Asterisk Native dual backend)
 */

import { createMMKV } from "react-native-mmkv";
import { create } from "zustand";
import { createJSONStorage, persist, StateStorage } from "zustand/middleware";

// Initialize MMKV storage
export const storage = createMMKV({
  id: "softphone-settings",
});

// Create a StateStorage adapter for zustand persist middleware
const mmkvStorage: StateStorage = {
  setItem: (name, value) => {
    storage.set(name, value);
  },
  getItem: (name) => {
    const value = storage.getString(name);
    return value ?? null;
  },
  removeItem: (name) => {
    storage.remove(name);
  },
};

// Video resolution types
export type VideoResolution = "1080" | "720" | "480" | "360";

// Video frame rate types
export type VideoFrameRate = 15 | 30 | 60;

export interface VideoResolutionConfig {
  width: number;
  height: number;
}

export const RESOLUTION_MAP: Record<VideoResolution, VideoResolutionConfig> = {
  "1080": { width: 1920, height: 1080 },
  "720": { width: 1280, height: 720 },
  "480": { width: 854, height: 480 },
  "360": { width: 640, height: 360 },
};

// Video bitrate mapping (in kbps) - automatically matched to resolution profiles
export const BITRATE_MAP: Record<VideoResolution, number> = {
  "1080": 5000, // 5 Mbps for 1080p
  "720": 3000, // 3 Mbps for 720p
  "480": 1500, // 1.5 Mbps for 480p
  "360": 1000, // 1 Mbps for 360p
};

// Gateway server from env (read-only, not user-configurable)
const ENV_GATEWAY_SERVER = process.env.EXPO_PUBLIC_GATEWAY_SERVER;

// Call mode type
export type CallMode = "public" | "siptrunk";

export type ThemePreference = "light" | "dark" | "system";

// Settings state interface
interface SettingsState {
  // Gateway Server config
  gatewayServer: string; // WebSocket URL for K2 Gateway

  // Call mode (Public or SIP Trunk)
  callMode: CallMode;
  trunkId: number | null; // Last resolved SIP Trunk ID (valid after successful resolve in current session)
  trunkPublicId: string | null;
  trunkIdManualOverride: boolean;

  // Theme Preference
  themePreference: ThemePreference;

  // SIP credentials (simplified - single set)
  // NOTE: These are legacy/fallback - Public mode uses VRS API credentials
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipDisplayName: string;
  sipPort: number;

  // TURN server config
  turnEnabled: boolean;
  turnUrl: string;
  turnUsername: string;
  turnPassword: string;

  // Video settings
  videoResolution: VideoResolution;
  videoFrameRate: VideoFrameRate; // Video frame rate in fps
}

// Settings actions interface
interface SettingsActions {
  // Gateway server is read-only (from env)

  // Call mode setters
  setCallMode: (value: CallMode) => void;
  setTrunkId: (value: number | null) => void;
  setTrunkPublicId: (value: string | null) => void;
  setResolvedTrunkId: (value: number | null) => void;

  setThemePreference: (value: ThemePreference) => void;

  // SIP credential setters removed - credentials are now hardcoded and read-only

  // TURN settings setters
  setTurnEnabled: (value: boolean) => void;
  setTurnUrl: (value: string) => void;
  setTurnUsername: (value: string) => void;
  setTurnPassword: (value: string) => void;

  // Video settings setters
  setVideoResolution: (value: VideoResolution) => void;
  setVideoFrameRate: (value: VideoFrameRate) => void;

  // Bulk update
  updateSettings: (settings: Partial<SettingsState>) => void;

  // Get video settings helper
  getVideoSettings: () => {
    resolution: VideoResolution;
    resolutionConfig: VideoResolutionConfig;
  };

  // Check if settings are complete
  hasRequiredSettings: () => boolean;

  // Get SIP credentials
  getSipCredentials: () => {
    sipDomain: string;
    sipUsername: string;
    sipPassword: string;
    sipDisplayName: string;
    sipPort: number;
  };

  // Reset to defaults
  resetToDefaults: () => void;
}

type SettingsStore = SettingsState & SettingsActions;

// SIP credentials (hardcoded - fallback/legacy)
// sipDomain: "203.151.21.121",
// sipUsername: "0900200001",
// sipPassword: "njrQGEa7GLcI3HQvGxgb",
// sipDisplayName: "0900200001",
// sipPort: 5060,

// Default values
const DEFAULT_SETTINGS: SettingsState = {
  // Gateway Server - from env (read-only)
  gatewayServer: ENV_GATEWAY_SERVER ?? "",

  // Call mode - SIP trunk by default
  callMode: "siptrunk",
  trunkId: null,
  trunkPublicId: null,
  trunkIdManualOverride: false,

  themePreference: "system",

  // Set 2
  sipDomain: "203.151.21.121",
  sipUsername: "0900200001",
  sipPassword: "njrQGEa7GLcI3HQvGxgb",
  sipDisplayName: "0900200001",
  sipPort: 5060,

  // TURN defaults - enabled by default
  turnEnabled: true,
  turnUrl: "turn:turn.ttrs.or.th:3478",
  turnUsername: "turn01",
  turnPassword: "Test1234",

  // Video defaults - 720p at 30fps
  videoResolution: "720", // Use 720p (3 Mbps) as default
  videoFrameRate: 30, // 30 fps default
  // Note: H264 Baseline profile (42e01f) is always forced for Linphone/mobile compatibility
};

export const useSettingsStore = create<SettingsStore>()(
  persist(
    (set, get) => ({
      ...DEFAULT_SETTINGS,

      // Call mode setters
      setCallMode: (value) => {
        set({ callMode: value });
        console.log("[SettingsStore] 📞 Call mode set:", value);
      },
      setTrunkId: (value) => {
        set({
          trunkId: value,
          trunkIdManualOverride: value !== null,
        });
        console.log("[SettingsStore] 🏢 Trunk ID set:", value);
      },
      setTrunkPublicId: (value) => {
        const normalized = value?.trim() || null;
        set({ trunkPublicId: normalized });
        console.log("[SettingsStore] 🆔 Trunk Public ID set:", normalized);
      },
      setResolvedTrunkId: (value) => {
        const { trunkIdManualOverride } = get();
        if (trunkIdManualOverride) {
          console.log("[SettingsStore] 🏢 Ignoring resolved trunk ID because manual override is active:", value);
          return;
        }
        set({ trunkId: value });
        console.log("[SettingsStore] 🏢 Resolved trunk ID set:", value);
      },

      setThemePreference: (value) => {
        set({ themePreference: value });
        console.log("[SettingsStore] 🎨 Theme preference set:", value);
      },

      // SIP credential setters removed - credentials are now hardcoded and read-only

      // TURN settings setters
      setTurnEnabled: (value) => set({ turnEnabled: value }),
      setTurnUrl: (value) => set({ turnUrl: value }),
      setTurnUsername: (value) => set({ turnUsername: value }),
      setTurnPassword: (value) => set({ turnPassword: value }),

      // Video settings setters
      setVideoResolution: (value) => {
        set({ videoResolution: value });
        console.log("[SettingsStore] 📹 Video resolution set:", value);
      },
      setVideoFrameRate: (value) => {
        set({ videoFrameRate: value });
        console.log("[SettingsStore] 🎞️ Video frame rate set:", value, "fps");
      },

      // Bulk update - filters out SIP credentials and gatewayServer to prevent modification
      updateSettings: (settings) => {
        // Remove SIP credentials and gateway server from the update (read-only from env)
        const { sipDomain, sipUsername, sipPassword, sipDisplayName, sipPort, gatewayServer: _gatewayServer, ...allowedSettings } = settings;
        set(allowedSettings);
      },

      // Get video settings helper
      getVideoSettings: () => {
        const { videoResolution } = get();
        const settings = {
          resolution: videoResolution,
          resolutionConfig: RESOLUTION_MAP[videoResolution],
        };

        console.log("[SettingsStore] 📹 Video settings:", {
          resolution: settings.resolution + "p",
          width: settings.resolutionConfig.width,
          height: settings.resolutionConfig.height,
        });

        return settings;
      },

      // Check if settings are complete
      hasRequiredSettings: () => {
        const { gatewayServer, sipDomain, sipUsername, sipPassword } = get();
        return !!(gatewayServer && sipDomain && sipUsername && sipPassword);
      },

      // Get SIP credentials
      getSipCredentials: () => {
        const { sipDomain, sipUsername, sipPassword, sipDisplayName, sipPort } = get();
        return { sipDomain, sipUsername, sipPassword, sipDisplayName, sipPort };
      },

      // Reset to defaults
      resetToDefaults: () => set(DEFAULT_SETTINGS),
    }),
    {
      name: "softphone-settings-storage",
      storage: createJSONStorage(() => mmkvStorage),
      // Handle rehydration
      onRehydrateStorage: () => (state) => {
        if (state) {
          console.log("[SettingsStore] Settings hydrated from MMKV");

          // Migration: move old credentials to new fields if needed
          // @ts-ignore - check for legacy fields
          if (state.janusSipServer && !state.sipDomain) {
            // @ts-ignore
            state.sipDomain = state.janusSipServer;
            // @ts-ignore
            state.sipUsername = state.janusSipUsername || state.sipUsername;
            // @ts-ignore
            state.sipPassword = state.janusSipPassword || state.sipPassword;
            // @ts-ignore
            state.sipDisplayName = state.janusSipDisplayName || state.sipDisplayName;
            console.log("[SettingsStore] Migrated legacy Janus credentials");
          }
          // @ts-ignore - check for legacy asterisk fields
          if (state.asteriskSipServer && !state.sipDomain) {
            // @ts-ignore
            state.sipDomain = state.asteriskSipServer;
            // @ts-ignore
            state.sipUsername = state.asteriskSipUsername || state.sipUsername;
            // @ts-ignore
            state.sipPassword = state.asteriskSipPassword || state.sipPassword;
            // @ts-ignore
            state.sipDisplayName = state.asteriskSipDisplayName || state.sipDisplayName;
            console.log("[SettingsStore] Migrated legacy Asterisk credentials");
          }

          // Force remove videoEnabled field (audio-only mode removed)
          // @ts-ignore - remove legacy videoEnabled field
          if (state.videoEnabled !== undefined) {
            console.log("[SettingsStore] Removing legacy videoEnabled field (audio-only mode removed)");
            // @ts-ignore
            delete state.videoEnabled;
          }

          // Force hardcoded SIP credentials (ignore any persisted values)
          state.sipDomain = DEFAULT_SETTINGS.sipDomain;
          state.sipUsername = DEFAULT_SETTINGS.sipUsername;
          state.sipPassword = DEFAULT_SETTINGS.sipPassword;
          state.sipDisplayName = DEFAULT_SETTINGS.sipDisplayName;
          state.sipPort = DEFAULT_SETTINGS.sipPort;

          // Gateway server always from env (read-only)
          state.gatewayServer = ENV_GATEWAY_SERVER ?? "";

          if (typeof state.trunkPublicId === "string" && !state.trunkPublicId.trim()) {
            state.trunkPublicId = null;
          }
        }
      },
    },
  ),
);

// Export a function to get settings synchronously (for non-React contexts)
// Falls back to defaults if stored values are empty strings
export function getSettingsSync(): SettingsState {
  const state = useSettingsStore.getState();

  // Return state with fallback to defaults for empty strings
  return {
    ...state,
    // Gateway - from env, fallback if state not yet rehydrated
    gatewayServer: state.gatewayServer || (ENV_GATEWAY_SERVER ?? ""),
    themePreference: state.themePreference || "system",
    // SIP credentials - fallback if empty
    sipDomain: state.sipDomain || DEFAULT_SETTINGS.sipDomain,
    sipUsername: state.sipUsername || DEFAULT_SETTINGS.sipUsername,
    sipPassword: state.sipPassword || DEFAULT_SETTINGS.sipPassword,
    sipDisplayName: state.sipDisplayName || DEFAULT_SETTINGS.sipDisplayName,
    sipPort: state.sipPort || DEFAULT_SETTINGS.sipPort,
    // TURN - fallback if empty
    turnUrl: state.turnUrl || DEFAULT_SETTINGS.turnUrl,
    turnUsername: state.turnUsername || DEFAULT_SETTINGS.turnUsername,
    turnPassword: state.turnPassword || DEFAULT_SETTINGS.turnPassword,
  };
}

// Export video settings getter
export function getVideoSettingsSync() {
  const { videoResolution } = useSettingsStore.getState();
  const settings = {
    resolution: videoResolution,
    resolutionConfig: RESOLUTION_MAP[videoResolution],
  };

  console.log("[SettingsStore] 📹 Video settings (sync):", {
    resolution: settings.resolution + "p",
    width: settings.resolutionConfig.width,
    height: settings.resolutionConfig.height,
  });

  return settings;
}

// Helper: Get bitrate for current resolution (in kbps)
export function getBitrateForResolution(resolution: VideoResolution): number {
  return BITRATE_MAP[resolution];
}

// Helper: Get getUserMedia constraints based on current settings
export function getVideoConstraints() {
  const { videoResolution, videoFrameRate } = useSettingsStore.getState();

  const resolutionConfig = RESOLUTION_MAP[videoResolution];

  return {
    width: { ideal: resolutionConfig.width },
    height: { ideal: resolutionConfig.height },
    frameRate: { ideal: videoFrameRate, max: videoFrameRate },
    facingMode: "user" as const,
  };
}
