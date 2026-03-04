/**
 * Auto-connect service
 * Automatically connects to K2 Gateway and resolves trunk (if needed) on app startup
 */

import { getSettingsSync } from "@/store/settings-store";
import { useSipStore } from "@/store/sip-store";

interface SavedSettings {
  gatewayServer?: string;
  sipDomain?: string;
  sipUsername?: string;
  sipPassword?: string;
  sipDisplayName?: string;
  sipPort?: number;
  turnEnabled?: boolean;
  turnUrl?: string;
  turnUsername?: string;
  turnPassword?: string;
}

// Load all saved settings from MMKV via settings store
function loadSettings(): SavedSettings {
  const state = getSettingsSync();
  return {
    gatewayServer: state.gatewayServer,
    sipDomain: state.sipDomain,
    sipUsername: state.sipUsername,
    sipPassword: state.sipPassword,
    sipDisplayName: state.sipDisplayName,
    sipPort: state.sipPort,
    turnEnabled: state.turnEnabled,
    turnUrl: state.turnUrl,
    turnUsername: state.turnUsername,
    turnPassword: state.turnPassword,
  };
}

// Auto-connect to K2 Gateway
// This should be called on app startup
async function autoConnect(): Promise<{
  success: boolean;
  gatewayConnected: boolean;
  error?: string;
}> {
  const settings = loadSettings();

  // Check if we have required Gateway settings
  if (!settings.gatewayServer) {
    console.log("[AutoConnect] No Gateway server configured, skipping");
    return {
      success: false,
      gatewayConnected: false,
      error: "Gateway server not configured",
    };
  }

  console.log("[AutoConnect] Starting auto-connect...");
  console.log("[AutoConnect] Gateway server:", settings.gatewayServer);

  let gatewayConnected = false;

  try {
    // Step 1: Connect to Gateway
    console.log("[AutoConnect] Connecting to Gateway...");

    const sipStore = useSipStore.getState();

    // Check if already connected
    if (sipStore.isConnected) {
      console.log("[AutoConnect] Already connected to Gateway");
      gatewayConnected = true;
    } else {
      await sipStore.connect();
      gatewayConnected = true;
      console.log("[AutoConnect] ✅ Gateway connected");
    }

    // Step 2: SIP trunk mode requires resolving trunkId from credentials.
    const refreshedSettings = getSettingsSync();
    if (refreshedSettings.callMode === "siptrunk") {
      console.log("[AutoConnect] SIP Trunk mode detected, resolving trunk...");
      await sipStore.resolveTrunk();
      console.log("[AutoConnect] ✅ Trunk resolved");
    } else {
      console.log("[AutoConnect] Public mode: ready for per-call authentication");
    }

    console.log("[AutoConnect] Auto-connect complete:", { gatewayConnected });

    return {
      success: true,
      gatewayConnected,
    };
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : "Unknown error";
    console.error("[AutoConnect] Auto-connect failed:", errorMessage);

    return {
      success: false,
      gatewayConnected,
      error: errorMessage,
    };
  }
}

// Initialize auto-connect
// Call this from the root layout
function initializeAutoConnect(): void {
  console.log("[AutoConnect] Initializing...");

  // Small delay to let the app fully initialize
  setTimeout(async () => {
    try {
      const result = await autoConnect();
      console.log("[AutoConnect] Result:", result);
    } catch (error) {
      console.error("[AutoConnect] Failed:", error);
    }
  }, 1000);
}

export { autoConnect, initializeAutoConnect, loadSettings };
