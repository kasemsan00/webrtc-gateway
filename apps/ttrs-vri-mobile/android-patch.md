## Android Software Encoder Patch

### Objective

Force the Android build to use the custom software-encoder build of WebRTC (`webrtc-release.aar`) instead of the hardware-accelerated `org.jitsi:webrtc` artifact that ships with `react-native-webrtc`.
Also ensure Google Maps API key is always synced into `AndroidManifest.xml` from `.env` so `react-native-maps` works reliably on Android.

### Artifacts

- `scripts/webrtc-release.aar` – patched WebRTC binary built with software encoder settings.
- `scripts/patch-android.ps1` – PowerShell utility script that patches the Android project after every `expo prebuild`.
- `scripts/patch-android.js` – Node.js patch script used by the npm shortcut.

### When to Run

Run the patch immediately **after** `expo prebuild` (or any command that regenerates the `android/` directory). The generated files are overwritten by prebuild, so rerun the script each time before building.
Before running, make sure `.env` contains `GOOGLE_MAPS_API_KEY=<your_key>`.

### Steps

1. Generate Android native project (if not already present):

   ```powershell
   npx expo prebuild --platform android
   ```

2. Apply the WebRTC patch:

   ```powershell
   powershell -ExecutionPolicy Bypass -File .\scripts\patch-android.ps1
   ```

   The script will:
   - Copy `webrtc-release.aar` into `android/libs`.
   - Ensure `android/app/build.gradle` references `implementation files('../libs/webrtc-release.aar')`.
   - Add `configurations.all { exclude group: "org.jitsi", module: "webrtc" }` so Gradle doesn’t pull the default WebRTC dependency.
   - Ensure `android/app/src/main/AndroidManifest.xml` contains:
     - `<meta-data android:name="com.google.android.geo.API_KEY" android:value="..."/>`
   - Fail fast if `GOOGLE_MAPS_API_KEY` is missing.

   Alternative (recommended for day-to-day use):
   ```powershell
   npm run android:prebuild
   ```
   This runs `expo prebuild` and then applies the patch via `scripts/patch-android.js`.

3. Rebuild the app (example):
   ```powershell
   cd android
   ./gradlew clean assembleDebug
   ```

### Verification

- Confirm `android/libs/webrtc-release.aar` exists after running the patch.
- Check `android/app/build.gradle` for the extra `implementation files('../libs/webrtc-release.aar')` line and the `configurations.all` exclusion block.
- Check `android/app/src/main/AndroidManifest.xml` for:
  - `<meta-data android:name="com.google.android.geo.API_KEY" ... />`
- During Gradle sync, there should be **no** `Duplicate class org.webrtc.*` errors.

### Troubleshooting

- If the script reports missing `android/`, rerun `expo prebuild` first.
- If the script fails with missing `GOOGLE_MAPS_API_KEY`, add it to `.env` and rerun patch.
- If Gradle still downloads `org.jitsi:webrtc`, delete `android/.gradle` and `android/app/build`, run the patch again, then rebuild.
