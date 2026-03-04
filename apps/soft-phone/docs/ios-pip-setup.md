# iOS Picture-in-Picture (PiP) Setup

## Capability Verification

PiP requires the "Audio, AirPlay, and Picture in Picture" background mode on iOS. This is satisfied by:

- **app.json** `ios.infoPlist.UIBackgroundModes`: `["audio", "voip"]`
  - `audio` - Enables background audio and PiP (covers "Audio, AirPlay, and Picture in Picture")
  - `voip` - Enables VoIP call handling

After `expo prebuild`, `ios/SoftPhone/Info.plist` should contain:

```xml
<key>UIBackgroundModes</key>
<array>
  <string>audio</string>
  <string>voip</string>
</array>
```

## Requirements

- **iOS 15.0+** (AVPictureInPictureVideoCallViewController)
- **Physical device** - PiP does not work on iPhone Simulator
- **Video call** - PiP shows remote video when app goes to background

## Architecture

1. **Primary path (declarative)**: `RTCView.iosPIP` with `startAutomatically: true`, `stopAutomatically: true` - native AVKit handles PiP on app background/foreground.
2. **Fallback path (best-effort)**: Manual `startPip()` from app state handlers - uses `findNodeHandle` which may be unreliable under New Architecture/Fabric.

## Testing

1. Start a video call on a real iOS device.
2. Wait for remote video to appear.
3. Press Home or swipe up to background the app.
4. PiP window should appear; if not, check Metro/Xcode logs for `[PiP]` and `[SipStore]` messages.
