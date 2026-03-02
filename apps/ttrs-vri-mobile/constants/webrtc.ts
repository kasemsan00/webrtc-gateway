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
 * This value acts as a HARD CAP - even if resolution profile settings specify higher bitrates
 * (e.g., 720p = 3000kbps, 1080p = 5000kbps), this env value will override them.
 * 
 * Example: EXPO_PUBLIC_VIDEO_BITRATE_KBPS=500 will cap all video calls to 500kbps regardless
 * of whether the user selected 1080p (normally 5000kbps) or 720p (normally 3000kbps).
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
      console.warn(
        `[WebRTC Config] Invalid EXPO_PUBLIC_VIDEO_BITRATE_KBPS="${envValue}". Using default: ${DEFAULT_BITRATE}`
      );
      return DEFAULT_BITRATE;
    }

    // Clamp to safe range
    if (parsed < MIN_BITRATE || parsed > MAX_BITRATE) {
      console.warn(
        `[WebRTC Config] EXPO_PUBLIC_VIDEO_BITRATE_KBPS=${parsed} is out of range [${MIN_BITRATE}-${MAX_BITRATE}]. Clamping.`
      );
      return Math.max(MIN_BITRATE, Math.min(MAX_BITRATE, parsed));
    }

    console.log(`[WebRTC Config] Using video bitrate: ${parsed} kbps (from env)`);
    return parsed;
  } catch (error) {
    console.error("[WebRTC Config] Error reading EXPO_PUBLIC_VIDEO_BITRATE_KBPS:", error);
    return DEFAULT_BITRATE;
  }
})();
