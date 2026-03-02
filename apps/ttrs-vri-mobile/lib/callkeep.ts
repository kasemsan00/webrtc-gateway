/**
 * CallKeep Service - Bridge between react-native-callkeep and sip-store
 *
 * Provides centralized CallKeep integration for managing outgoing call states
 * through Android ConnectionService (and iOS CallKit).
 */

import { Platform } from "react-native";
import RNCallKeep from "react-native-callkeep";

let currentCallUUID: string | null = null;
let isSetupDone = false;
let listenersRegistered = false;

function generateUUID(): string {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
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

export async function setupCallKeep(): Promise<void> {
  if (isSetupDone) return;

  const options = {
    ios: {
      appName: "TTRS VRI",
      supportsVideo: true,
      maximumCallGroups: "1",
      maximumCallsPerCallGroup: "1",
      audioSession: {
        categoryOptions: 0x4 | 0x20,
        mode: "AVAudioSessionModeVideoChat",
      },
    },
    android: {
      alertTitle: "Permissions Required",
      alertDescription: "TTRS VRI needs access to your phone accounts to manage calls",
      cancelButton: "Cancel",
      okButton: "OK",
      additionalPermissions: [],
      // Keep call UX inside app on Android (avoid system in-call UI taking foreground).
      selfManaged: true,
    },
  };

  try {
    await RNCallKeep.setup(options);
    if (Platform.OS === "android") {
      await RNCallKeep.setAvailable(true);
    }
    isSetupDone = true;
    console.log("[CallKeep] Setup complete");
  } catch (error) {
    console.error("[CallKeep] Setup failed:", error);
  }
}

export function reportOutgoingCall(handle: string, displayName?: string): void {
  const uuid = ensureCallUUID();
  console.log("[CallKeep] reportOutgoingCall:", { uuid, handle, displayName });

  try {
    if (Platform.OS === "android") {
      RNCallKeep.startCall(uuid, handle, displayName || handle);
      return;
    }

    RNCallKeep.startCall(uuid, handle, displayName || handle, "generic", true);
  } catch (error) {
    console.warn("[CallKeep] startCall failed:", error);
  }
}

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

export function reportCallAnswered(isOutgoing: boolean = false): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  console.log("[CallKeep] reportCallAnswered:", { uuid, isOutgoing });
  try {
    if (isOutgoing && Platform.OS === "ios") {
      RNCallKeep.reportConnectedOutgoingCallWithUUID(uuid);
    }

    if (Platform.OS === "android") {
      void (RNCallKeep.setCurrentCallActive(uuid) as unknown as Promise<void>).catch(
        (error: unknown) => {
          console.warn("[CallKeep] setCurrentCallActive failed:", error);
        }
      );
    }
  } catch (error) {
    console.warn("[CallKeep] reportCallAnswered failed:", error);
  }
}

export function reportCallEnded(reason?: number): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  console.log("[CallKeep] reportCallEnded:", { uuid, reason });
  try {
    void (RNCallKeep.reportEndCallWithUUID(uuid, reason ?? 2) as unknown as Promise<void>).catch(
      (error: unknown) => {
        console.warn("[CallKeep] reportEndCallWithUUID failed:", error);
      }
    );
  } catch (error) {
    console.warn("[CallKeep] reportEndCallWithUUID failed:", error);
  }
  clearCallUUID();
}

export function reportMuteState(muted: boolean): void {
  const uuid = currentCallUUID;
  if (!uuid) return;

  try {
    void (RNCallKeep.setMutedCall(uuid, muted) as unknown as Promise<void>).catch(
      (error: unknown) => {
        console.warn("[CallKeep] setMutedCall failed:", error);
      }
    );
  } catch (error) {
    console.warn("[CallKeep] setMutedCall failed:", error);
  }
}

interface CallKeepActions {
  hangup: () => void;
  toggleMute: (muted: boolean) => void;
  sendDtmf: (digits: string) => void;
  onAudioSessionActivated?: () => void;
}

export function registerCallKeepListeners(actions: CallKeepActions): void {
  if (listenersRegistered) return;

  RNCallKeep.addEventListener("answerCall", ({ callUUID }) => {
    console.log("[CallKeep] answerCall event (ignored):", callUUID);
  });

  RNCallKeep.addEventListener("endCall", ({ callUUID }) => {
    console.log("[CallKeep] endCall event:", callUUID);
    actions.hangup();
    clearCallUUID();
  });

  RNCallKeep.addEventListener("didPerformSetMutedCallAction", ({ muted }) => {
    console.log("[CallKeep] didPerformSetMutedCallAction:", muted);
    actions.toggleMute(muted);
  });

  RNCallKeep.addEventListener("didPerformDTMFAction", ({ digits }) => {
    console.log("[CallKeep] didPerformDTMFAction:", digits);
    actions.sendDtmf(digits);
  });

  RNCallKeep.addEventListener("didActivateAudioSession", () => {
    console.log("[CallKeep] Audio session activated");
    actions.onAudioSessionActivated?.();
  });

  RNCallKeep.addEventListener("didReceiveStartCallAction", ({ handle, callUUID, name }) => {
    console.log("[CallKeep] didReceiveStartCallAction:", { handle, callUUID, name });
  });

  listenersRegistered = true;
  console.log("[CallKeep] Event listeners registered");
}

export function removeCallKeepListeners(): void {
  if (!listenersRegistered) return;

  RNCallKeep.removeEventListener("answerCall");
  RNCallKeep.removeEventListener("endCall");
  RNCallKeep.removeEventListener("didPerformSetMutedCallAction");
  RNCallKeep.removeEventListener("didPerformDTMFAction");
  RNCallKeep.removeEventListener("didActivateAudioSession");
  RNCallKeep.removeEventListener("didReceiveStartCallAction");

  listenersRegistered = false;
  console.log("[CallKeep] Event listeners removed");
}
