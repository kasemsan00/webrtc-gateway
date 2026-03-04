/**
 * WebRTC Configuration Constants
 *
 * These values can be configured via environment variables.
 * All EXPO_PUBLIC_* variables are available at build time and runtime.
 */

/**
 * Video bitrate limit in kilobits per second (kbps)
 *
 * Controls both incoming and outgoing video bandwidth:
 * - Incoming: Sets `b=AS` parameter in SDP to request remote peer limit their sending bitrate
 * - Outgoing: Sets `maxBitrate` via RTCRtpSender.setParameters to limit our sending bitrate
 *
 * @default 1500 (kbps)
 * @env EXPO_PUBLIC_VIDEO_BITRATE_KBPS
 * @range 50-10000 (recommended: 500-3000)
 */
export const VIDEO_BITRATE_KBPS = (() => {
  const DEFAULT_BITRATE = 1500;
  const MIN_BITRATE = 50;
  const MAX_BITRATE = 10000;

  try {
    const envValue = process.env.EXPO_PUBLIC_VIDEO_BITRATE_KBPS;

    if (!envValue) {
      return DEFAULT_BITRATE;
    }

    const parsed = parseInt(envValue, 10);

    if (!Number.isFinite(parsed) || parsed <= 0) {
      console.warn(`[WebRTC Config] Invalid EXPO_PUBLIC_VIDEO_BITRATE_KBPS="${envValue}". Using default: ${DEFAULT_BITRATE}`);
      return DEFAULT_BITRATE;
    }

    // Clamp to safe range
    if (parsed < MIN_BITRATE || parsed > MAX_BITRATE) {
      console.warn(`[WebRTC Config] EXPO_PUBLIC_VIDEO_BITRATE_KBPS=${parsed} is out of range [${MIN_BITRATE}-${MAX_BITRATE}]. Clamping.`);
      return Math.max(MIN_BITRATE, Math.min(MAX_BITRATE, parsed));
    }

    console.log(`[WebRTC Config] Using video bitrate: ${parsed} kbps (from env)`);
    return parsed;
  } catch (error) {
    console.error("[WebRTC Config] Error reading EXPO_PUBLIC_VIDEO_BITRATE_KBPS:", error);
    return DEFAULT_BITRATE;
  }
})();

/**
 * Picture-in-Picture (PiP) enabled flag for iOS video calls
 *
 * Controls whether PiP mode is enabled when the app goes to background during a video call.
 * When enabled, remote video continues in a floating window. When disabled, call continues
 * in background with audio only.
 *
 * @default true
 * @env EXPO_PUBLIC_PIP_ENABLED
 * @returns {boolean} true if PiP should be enabled, false otherwise
 */
export function getPipEnabled(): boolean {
  const DEFAULT_ENABLED = false;

  try {
    const envValue = process.env.EXPO_PUBLIC_PIP_ENABLED;

    if (!envValue) {
      return DEFAULT_ENABLED;
    }

    const normalized = envValue.trim().toLowerCase();

    // Accept: "true", "1", "yes", "on"
    if (normalized === "true" || normalized === "1" || normalized === "yes" || normalized === "on") {
      console.log("[WebRTC Config] PiP enabled: true (from env)");
      return true;
    }

    // Accept: "false", "0", "no", "off"
    if (normalized === "false" || normalized === "0" || normalized === "no" || normalized === "off") {
      console.log("[WebRTC Config] PiP enabled: false (from env)");
      return false;
    }

    // Invalid value - use default
    console.warn(`[WebRTC Config] Invalid EXPO_PUBLIC_PIP_ENABLED="${envValue}". Using default: ${DEFAULT_ENABLED}`);
    return DEFAULT_ENABLED;
  } catch (error) {
    console.error("[WebRTC Config] Error reading EXPO_PUBLIC_PIP_ENABLED:", error);
    return DEFAULT_ENABLED;
  }
}
