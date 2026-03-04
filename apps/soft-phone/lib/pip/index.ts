/**
 * PiP (Picture-in-Picture) Helper
 * Platform-guarded wrapper for iOS PiP using react-native-webrtc's built-in support.
 *
 * Primary path: RTCView `iosPIP.startAutomatically` / `stopAutomatically` (native AVKit).
 * This module provides best-effort manual fallback when auto path may be delayed.
 *
 * Usage:
 *   1. Pass a ref to the remote <RTCView> and call setPipRef(ref)
 *   2. Call startPip() from background handler (manual fallback)
 *   3. RTCView iosPIP handles lifecycle; manual start may fail under Fabric.
 */

import { type MutableRefObject } from "react";
import { findNodeHandle, Platform } from "react-native";
// @ts-ignore - exported from react-native-webrtc v124 but TS resolution may not find it
import { startIOSPIP, stopIOSPIP } from "react-native-webrtc";

import { getPipEnabled } from "@/constants/webrtc";

// Store the remote RTCView ref so sip-store can trigger PiP without prop-drilling
let _pipRef: MutableRefObject<unknown> | null = null;

/**
 * Register the remote RTCView ref for PiP control.
 * Called from InCallScreen when the remote video ref is created.
 */
export function setPipRef(ref: MutableRefObject<unknown> | null): void {
  _pipRef = ref;
}

/**
 * Get the currently registered PiP ref.
 */
export function getPipRef(): MutableRefObject<unknown> | null {
  return _pipRef;
}

/**
 * Start PiP programmatically (best-effort fallback).
 * Primary PiP is RTCView iosPIP.startAutomatically. This may fail under Fabric.
 */
export function startPip(): void {
  if (Platform.OS !== "ios") {
    return;
  }

  if (!getPipEnabled()) {
    console.log("[PiP] PiP disabled by env variable - skipping startPip");
    return;
  }

  const ref = _pipRef;
  const refCurrent = ref?.current;

  if (!refCurrent) {
    console.warn("[PiP] startPip skipped: ref or ref.current is null");
    return;
  }

  try {
    const nodeHandle = findNodeHandle(refCurrent as Parameters<typeof findNodeHandle>[0]);
    console.log("[PiP] startPip attempting, ref.current:", !!refCurrent, "nodeHandle:", nodeHandle);

    startIOSPIP(_pipRef!);
    console.log("[PiP] startPip called successfully");
  } catch (e) {
    console.warn("[PiP] startPip failed:", e);
  }
}

/**
 * Stop PiP programmatically (best-effort fallback).
 * Primary stop is RTCView iosPIP.stopAutomatically.
 */
export function stopPip(): void {
  if (Platform.OS !== "ios" || !_pipRef?.current) {
    return;
  }

  if (!getPipEnabled()) {
    return;
  }

  try {
    stopIOSPIP(_pipRef);
    console.log("[PiP] stopPip called");
  } catch (e) {
    console.warn("[PiP] stopPip failed:", e);
  }
}

/**
 * Clear the PiP ref. Called when the call ends or InCallScreen unmounts.
 */
export function clearPipRef(): void {
  _pipRef = null;
}
