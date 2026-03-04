/**
 * CallKeep Service - Bridge between react-native-callkeep and sip-store
 *
 * Provides centralized CallKeep integration for managing call states
 * through Android ConnectionService (and iOS CallKit).
 * Uses self-managed mode on Android so the app retains its own call UI.
 */

import { Platform } from "react-native";
import RNCallKeep from "react-native-callkeep";

// ---------- UUID helpers ----------

let currentCallUUID: string | null = null;

function generateUUID(): string {
  // Simple UUID v4 generator (no external dependency)
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export function getCurrentCallUUID(): string | null {
  return currentCallUUID;
}

function ensureCallUUID(): string {
  if (!currentCallUUID) {
    currentCallUUID = generateUUID();
  }
  return currentCallUUID;
}

function clearCallUUID(): void {
  currentCallUUID = null;
}

// ---------- Setup ----------

let isSetupDone = false;

export async function setupCallKeep(): Promise<void> {
  if (isSetupDone) return;

  const options = {
    ios: {
      appName: "SoftPhone",
      supportsVideo: true,
      maximumCallGroups: "1",
      maximumCallsPerCallGroup: "1",
      audioSession: {
        categoryOptions: 0x4 | 0x20, // allowBluetooth | allowBluetoothA2DP
        mode: "AVAudioSessionModeVideoChat",
      },
    },
    android: {
      alertTitle: "Permissions Required",
      alertDescription: "SoftPhone needs access to your phone accounts to manage calls",
      cancelButton: "Cancel",
      okButton: "OK",
      additionalPermissions: [],
      selfManaged: true,
    },
  };

  try {
    await RNCallKeep.setup(options);
    RNCallKeep.setAvailable(true);
    isSetupDone = true;
    console.log("[CallKeep] Setup complete");
  } catch (error) {
    console.error("[CallKeep] Setup failed:", error);
  }
}

// ---------- Report call state changes TO the OS ----------

/**
 * Report an incoming call to the OS.
 * Called from sip-store's onIncomingCall callback.
 */
export function reportIncomingCall(handle: string, displayName?: string): void {
  const uuid = ensureCallUUID();
  console.log("[CallKeep] reportIncomingCall:", { uuid, handle, displayName });

  try {
    RNCallKeep.displayIncomingCall(
      uuid,
      handle,
      displayName || handle,
      "generic", // handleType
      true, // hasVideo
    );
  } catch (error) {
    console.warn("[CallKeep] displayIncomingCall failed:", error);
  }
}

/**
 * Report an outgoing call to the OS.
 * Called from sip-store when a call transitions to CALLING state.
 */
export function reportOutgoingCall(handle: string, displayName?: string): void {
  const uuid = ensureCallUUID();
  console.log("[CallKeep] reportOutgoingCall:", { uuid, handle, displayName });

  try {
    RNCallKeep.startCall(uuid, handle, displayName || handle, "generic", true);
  } catch (error) {
    console.warn("[CallKeep] startCall failed:", error);
  }
}

/**
 * Report that an outgoing call is now connecting/ringing.
 * Called from sip-store when call transitions to RINGING state.
 * iOS only - informs CallKit that the outgoing call is connecting.
 */
export function reportOutgoingCallConnecting(): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  console.log("[CallKeep] reportOutgoingCallConnecting:", uuid);
  try {
    if (Platform.OS === "ios") {
      RNCallKeep.reportConnectingOutgoingCallWithUUID(uuid);
    }
  } catch (error) {
    console.warn("[CallKeep] reportConnectingOutgoingCallWithUUID failed:", error);
  }
}

/**
 * Report that a call has been answered / connected.
 * Called from sip-store's onAnswered callback.
 *
 * @param isOutgoing - true for outgoing calls, false for incoming calls.
 *   Outgoing calls use reportConnectedOutgoingCallWithUUID (iOS) + setCurrentCallActive (Android).
 *   Incoming calls use answerIncomingCall + setCurrentCallActive (Android).
 */
export function reportCallAnswered(isOutgoing: boolean = false): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  console.log("[CallKeep] reportCallAnswered:", { uuid, isOutgoing });
  try {
    if (isOutgoing) {
      // Outgoing call connected
      if (Platform.OS === "ios") {
        RNCallKeep.reportConnectedOutgoingCallWithUUID(uuid);
      }
    } else {
      // Incoming call answered
      RNCallKeep.answerIncomingCall(uuid);
    }

    // Mark call as active for audio routing (Android ConnectionService)
    if (Platform.OS === "android") {
      RNCallKeep.setCurrentCallActive(uuid);
    }
  } catch (error) {
    console.warn("[CallKeep] reportCallAnswered failed:", error);
  }
}

/**
 * Report that a call has ended.
 * Called from sip-store's onCallEnded, hangup, and decline actions.
 */
export function reportCallEnded(reason?: number): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  console.log("[CallKeep] reportCallEnded:", { uuid, reason });
  try {
    // reason: CK_CONSTANTS.END_CALL_REASONS
    // 1 = failed, 2 = remote ended, 3 = unanswered, 4 = answered elsewhere,
    // 5 = declined elsewhere, 6 = missed
    RNCallKeep.reportEndCallWithUUID(uuid, reason ?? 2);
  } catch (error) {
    console.warn("[CallKeep] reportEndCallWithUUID failed:", error);
  }
  clearCallUUID();
}

/**
 * Report mute state change to the OS.
 */
export function reportMuteState(muted: boolean): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  try {
    RNCallKeep.setMutedCall(uuid, muted);
  } catch (error) {
    console.warn("[CallKeep] setMutedCall failed:", error);
  }
}

// ---------- Event listeners FROM the OS ----------

interface CallKeepActions {
  answer: () => void;
  hangup: () => void;
  toggleMute: (muted: boolean) => void;
  sendDtmf: (digits: string) => void;
  onAudioSessionActivated?: () => void;
}

let listenersRegistered = false;

/**
 * Register CallKeep event listeners that bridge OS call actions
 * back to the app's sip-store actions.
 */
export function registerCallKeepListeners(actions: CallKeepActions): void {
  if (listenersRegistered) return;

  // User answered an incoming call from the OS UI (e.g. notification)
  RNCallKeep.addEventListener("answerCall", (event?: { callUUID?: string }) => {
    const callUUID = event?.callUUID;
    console.log("[CallKeep] answerCall event:", callUUID);
    try {
      actions.answer();
    } catch (error) {
      console.warn("[CallKeep] answer action failed:", error);
    }
  });

  // User ended a call from the OS UI
  RNCallKeep.addEventListener("endCall", (event?: { callUUID?: string }) => {
    const callUUID = event?.callUUID;
    console.log("[CallKeep] endCall event:", callUUID);
    try {
      actions.hangup();
    } catch (error) {
      console.warn("[CallKeep] hangup action failed:", error);
    } finally {
      clearCallUUID();
    }
  });

  // Mute toggled from OS
  RNCallKeep.addEventListener("didPerformSetMutedCallAction", (event?: { muted?: boolean }) => {
    const muted = !!event?.muted;
    console.log("[CallKeep] didPerformSetMutedCallAction:", muted);
    try {
      actions.toggleMute(muted);
    } catch (error) {
      console.warn("[CallKeep] toggleMute action failed:", error);
    }
  });

  // DTMF from OS
  RNCallKeep.addEventListener("didPerformDTMFAction", (event?: { digits?: string }) => {
    const digits = event?.digits ?? "";
    console.log("[CallKeep] didPerformDTMFAction:", digits);
    if (!digits) return;
    try {
      actions.sendDtmf(digits);
    } catch (error) {
      console.warn("[CallKeep] sendDtmf action failed:", error);
    }
  });

  if (Platform.OS === "ios") {
    // Audio session activated (iOS)
    RNCallKeep.addEventListener("didActivateAudioSession", () => {
      console.log("[CallKeep] Audio session activated");
      try {
        actions.onAudioSessionActivated?.();
      } catch (error) {
        console.warn("[CallKeep] onAudioSessionActivated failed:", error);
      }
    });

    // Start call action (from Recents/Contacts on iOS)
    RNCallKeep.addEventListener("didReceiveStartCallAction", (event?: { handle?: string; callUUID?: string; name?: string }) => {
      const handle = event?.handle;
      const callUUID = event?.callUUID;
      const name = event?.name;
      console.log("[CallKeep] didReceiveStartCallAction:", { handle, callUUID, name });
    });
  }

  listenersRegistered = true;
  console.log("[CallKeep] Event listeners registered");
}

/**
 * Remove all CallKeep event listeners.
 * Call on app unmount if needed.
 */
export function removeCallKeepListeners(): void {
  if (!listenersRegistered) return;

  RNCallKeep.removeEventListener("answerCall");
  RNCallKeep.removeEventListener("endCall");
  RNCallKeep.removeEventListener("didPerformSetMutedCallAction");
  RNCallKeep.removeEventListener("didPerformDTMFAction");
  if (Platform.OS === "ios") {
    RNCallKeep.removeEventListener("didActivateAudioSession");
    RNCallKeep.removeEventListener("didReceiveStartCallAction");
  }

  listenersRegistered = false;
  console.log("[CallKeep] Event listeners removed");
}
