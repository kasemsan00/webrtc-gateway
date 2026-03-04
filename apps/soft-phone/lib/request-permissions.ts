import { Platform, PermissionsAndroid } from "react-native";

/**
 * Ensure media permissions are granted before accessing camera/microphone.
 * On Android, checks and requests permissions if needed.
 * On iOS, returns true (iOS handles permissions through getUserMedia dialog).
 *
 * @param options - Optional configuration
 * @param options.requestIfNeeded - If true, request permissions if not granted (default: true)
 * @returns Promise resolving to permission status
 */
export async function ensureMediaPermissions(options?: { requestIfNeeded?: boolean }): Promise<{
  camera: boolean;
  microphone: boolean;
}> {
  const requestIfNeeded = options?.requestIfNeeded ?? true;

  // Skip on web platform
  if (Platform.OS === "web") {
    return { camera: true, microphone: true };
  }

  // On iOS, permissions are handled by getUserMedia dialog
  // Just return true to allow getUserMedia to handle it
  if (Platform.OS === "ios") {
    return { camera: true, microphone: true };
  }

  // On Android, check and request permissions
  if (Platform.OS === "android") {
    try {
      // Check current permission status
      const cameraStatus = await PermissionsAndroid.check(PermissionsAndroid.PERMISSIONS.CAMERA);
      const microphoneStatus = await PermissionsAndroid.check(PermissionsAndroid.PERMISSIONS.RECORD_AUDIO);

      console.log("[Permissions] Current Android permissions - Camera:", cameraStatus, "Microphone:", microphoneStatus);

      // If both are granted, return immediately
      if (cameraStatus && microphoneStatus) {
        return { camera: true, microphone: true };
      }

      // If permissions are not granted and we should request them
      if (requestIfNeeded) {
        console.log("[Permissions] Requesting Android runtime permissions...");
        const granted = await PermissionsAndroid.requestMultiple([
          PermissionsAndroid.PERMISSIONS.CAMERA,
          PermissionsAndroid.PERMISSIONS.RECORD_AUDIO,
        ]);

        const cameraGranted = granted[PermissionsAndroid.PERMISSIONS.CAMERA] === PermissionsAndroid.RESULTS.GRANTED;
        const microphoneGranted = granted[PermissionsAndroid.PERMISSIONS.RECORD_AUDIO] === PermissionsAndroid.RESULTS.GRANTED;

        console.log(`[Permissions] Android permissions after request - Camera: ${cameraGranted}, Microphone: ${microphoneGranted}`);

        return {
          camera: cameraGranted,
          microphone: microphoneGranted,
        };
      } else {
        // Just return current status without requesting
        return {
          camera: cameraStatus,
          microphone: microphoneStatus,
        };
      }
    } catch (error: any) {
      console.error("❌ Failed to check/request Android permissions:", error);
      return { camera: false, microphone: false };
    }
  }

  // Fallback (should not reach here)
  return { camera: false, microphone: false };
}

/**
 * Request camera and microphone permissions at app startup.
 * This prevents permission prompts from interrupting the call flow.
 */
export async function requestMediaPermissions(): Promise<{
  camera: boolean;
  microphone: boolean;
}> {
  console.log("📸🎤 Requesting camera and microphone permissions...");

  // Default result
  const result = {
    camera: false,
    microphone: false,
  };

  // Skip on web platform
  if (Platform.OS === "web") {
    console.log("[Permissions] Skipping permission request on web");
    return result;
  }

  // On Android, request runtime permissions first using PermissionsAndroid
  // This ensures the permission dialog appears before calling getUserMedia
  if (Platform.OS === "android") {
    try {
      console.log("[Permissions] Requesting Android runtime permissions...");
      const granted = await PermissionsAndroid.requestMultiple([
        PermissionsAndroid.PERMISSIONS.CAMERA,
        PermissionsAndroid.PERMISSIONS.RECORD_AUDIO,
      ]);

      const cameraGranted = granted[PermissionsAndroid.PERMISSIONS.CAMERA] === PermissionsAndroid.RESULTS.GRANTED;
      const microphoneGranted = granted[PermissionsAndroid.PERMISSIONS.RECORD_AUDIO] === PermissionsAndroid.RESULTS.GRANTED;

      console.log(`[Permissions] Android permissions - Camera: ${cameraGranted}, Microphone: ${microphoneGranted}`);

      if (!cameraGranted || !microphoneGranted) {
        console.warn("⚠️ Android permissions not granted:", {
          camera: cameraGranted ? "granted" : granted[PermissionsAndroid.PERMISSIONS.CAMERA],
          microphone: microphoneGranted ? "granted" : granted[PermissionsAndroid.PERMISSIONS.RECORD_AUDIO],
        });
        return {
          camera: cameraGranted,
          microphone: microphoneGranted,
        };
      }

      // Permissions granted, now proceed to getUserMedia
      result.camera = cameraGranted;
      result.microphone = microphoneGranted;
    } catch (error: any) {
      console.error("❌ Failed to request Android permissions:", error);
      return result;
    }
  }

  try {
    // Import react-native-webrtc dynamically to handle cases where it's not available
    const { mediaDevices } = await import("react-native-webrtc");

    // Request both camera and microphone permissions by getting user media
    // This will trigger the system permission dialogs (on iOS) or use already-granted permissions (on Android)
    const stream = await mediaDevices.getUserMedia({
      audio: true,
      video: true,
    });

    // Check which tracks we got
    const audioTracks = stream.getAudioTracks();
    const videoTracks = stream.getVideoTracks();

    // Update result based on actual tracks (more reliable than just permission status)
    result.microphone = audioTracks.length > 0;
    result.camera = videoTracks.length > 0;

    console.log(`✅ Permissions granted - Camera: ${result.camera}, Microphone: ${result.microphone}`);

    // Stop all tracks immediately after getting permissions
    // We don't need the actual stream, just the permission
    stream.getTracks().forEach((track) => {
      track.stop();
    });

    console.log("🛑 Stopped temporary media stream after permission check");

    return result;
  } catch (error: any) {
    // Handle specific permission errors
    if (error.name === "NotAllowedError" || error.name === "PermissionDeniedError") {
      console.warn("⚠️ User denied camera/microphone permissions:", error.message);
    } else if (error.name === "NotFoundError" || error.name === "DevicesNotFoundError") {
      console.warn("⚠️ No camera/microphone found:", error.message);
    } else {
      console.error("❌ Failed to request permissions:", error);
    }

    // On Android, if we already got permissions from PermissionsAndroid, return those
    // On iOS, return false since getUserMedia failed
    return result;
  }
}
