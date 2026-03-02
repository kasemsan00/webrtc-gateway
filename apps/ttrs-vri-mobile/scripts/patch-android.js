#!/usr/bin/env node
/* eslint-env node */

const fs = require("fs");
const path = require("path");

function fail(message) {
  console.error(`[patch-android] ${message}`);
  process.exit(1);
}

function readEnvFile(envPath) {
  if (!fs.existsSync(envPath)) {
    return {};
  }

  const env = {};
  const lines = fs.readFileSync(envPath, "utf8").split(/\r?\n/);

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;

    const separatorIndex = trimmed.indexOf("=");
    if (separatorIndex < 0) continue;

    const key = trimmed.slice(0, separatorIndex).trim();
    let value = trimmed.slice(separatorIndex + 1).trim();

    if (
      (value.startsWith('"') && value.endsWith('"') && value.length >= 2) ||
      (value.startsWith("'") && value.endsWith("'") && value.length >= 2)
    ) {
      value = value.slice(1, -1);
    }

    env[key] = value;
  }

  return env;
}

function resolveGoogleMapsApiKey(projectRoot) {
  const fromProcess = (process.env.GOOGLE_MAPS_API_KEY || "").trim();
  if (fromProcess) {
    return fromProcess;
  }

  const envPath = path.join(projectRoot, ".env");
  const env = readEnvFile(envPath);
  return (env.GOOGLE_MAPS_API_KEY || "").trim();
}

function patchAndroidManifest(manifestPath, googleMapsApiKey) {
  if (!fs.existsSync(manifestPath)) {
    fail(`AndroidManifest.xml not found: ${manifestPath}`);
  }

  const serviceClass = "io.wazo.callkeep.VoiceConnectionService";
  const headlessServiceClass = "io.wazo.callkeep.RNCallKeepBackgroundMessagingService";
  const mapsMetaDataLine = `    <meta-data android:name="com.google.android.geo.API_KEY" android:value="${googleMapsApiKey}"/>`;
  const original = fs.readFileSync(manifestPath, "utf8");
  let patched = original;
  let callKeepPatched = false;
  let mapsKeyPatched = false;

  const marker = '    <activity android:name=".MainActivity"';
  if (!patched.includes(marker)) {
    fail("Unable to find MainActivity marker in AndroidManifest.xml");
  }

  const mapsMetaPattern = /<meta-data[\s\S]*?android:name="com\.google\.android\.geo\.API_KEY"[\s\S]*?\/>/m;
  if (mapsMetaPattern.test(patched)) {
    const replaced = patched.replace(mapsMetaPattern, mapsMetaDataLine);
    if (replaced !== patched) {
      mapsKeyPatched = true;
      patched = replaced;
    }
  } else {
    patched = patched.replace(marker, `${mapsMetaDataLine}\n${marker}`);
    mapsKeyPatched = true;
  }

  if (!(patched.includes(serviceClass) && patched.includes(headlessServiceClass))) {
    const serviceBlock = [
      "    <service",
      `      android:name="${serviceClass}"`,
      '      android:permission="android.permission.BIND_TELECOM_CONNECTION_SERVICE"',
      '      android:exported="true">',
      "      <intent-filter>",
      '        <action android:name="android.telecom.ConnectionService"/>',
      "      </intent-filter>",
      "    </service>",
      "    <service",
      `      android:name="${headlessServiceClass}"`,
      '      android:enabled="true"',
      '      android:exported="false"/>',
      "",
    ].join("\n");

    patched = patched.replace(marker, `${serviceBlock}${marker}`);
    callKeepPatched = true;
  }

  if (patched !== original) {
    fs.writeFileSync(manifestPath, patched, "utf8");
  }

  return {
    callKeepPatched,
    mapsKeyPatched,
  };
}

function main() {
  const scriptPath = path.resolve(process.argv[1]);
  const scriptDir = path.dirname(scriptPath);
  const projectRoot = path.resolve(scriptDir, "..");
  const androidDir = path.join(projectRoot, "android");
  const libsDir = path.join(androidDir, "libs");
  const sourceAar = path.join(scriptDir, "webrtc-release.aar");
  const targetAar = path.join(libsDir, "webrtc-release.aar");
  const splashSource = path.join(projectRoot, "assets", "images", "splash-icon.png");
  const splashDir = path.join(androidDir, "app", "src", "main", "res", "drawable");
  const splashTarget = path.join(splashDir, "splashscreen_logo.png");

  const cliBuildFile = process.argv[2];
  const buildFile = cliBuildFile || path.join(androidDir, "app", "build.gradle");
  const manifestFile = path.join(androidDir, "app", "src", "main", "AndroidManifest.xml");
  const googleMapsApiKey = resolveGoogleMapsApiKey(projectRoot);

  if (!googleMapsApiKey) {
    fail("Missing GOOGLE_MAPS_API_KEY. Set it in environment or .env before running patch.");
  }

  if (!fs.existsSync(androidDir)) {
    fail("Android directory not found. Run 'expo prebuild' first.");
  }

  if (!fs.existsSync(sourceAar)) {
    fail("Missing webrtc-release.aar next to this script.");
  }

  fs.mkdirSync(libsDir, { recursive: true });

  try {
    fs.copyFileSync(sourceAar, targetAar);
  } catch (error) {
    fail(`Failed to copy webrtc-release.aar into android/libs. ${error.message}`);
  }

  if (fs.existsSync(splashSource)) {
    fs.mkdirSync(splashDir, { recursive: true });
    fs.copyFileSync(splashSource, splashTarget);
  }

  if (!fs.existsSync(buildFile)) {
    fail(`build.gradle not found: ${buildFile}`);
  }

  const lines = fs.readFileSync(buildFile, "utf8").split(/\r?\n/);

  const implementationPattern = /implementation files\(['"]\.\.\/libs\/webrtc-release\.aar['"]\)/;
  if (!lines.some((line) => implementationPattern.test(line))) {
    const reactAndroidIndex = lines.findIndex((line) => /implementation\("com\.facebook\.react:react-android"\)/.test(line));
    const aarLine = "    implementation files('../libs/webrtc-release.aar')";
    if (reactAndroidIndex >= 0) {
      lines.splice(reactAndroidIndex + 1, 0, aarLine);
    } else {
      const dependenciesIndex = lines.findIndex((line) => /\bdependencies\s*\{/.test(line));
      const insertionPoint = dependenciesIndex >= 0 ? dependenciesIndex + 1 : lines.length;
      lines.splice(insertionPoint, 0, aarLine);
    }
  }

  const excludePattern = /exclude group:\s*"org\.jitsi"/;
  if (!lines.some((line) => excludePattern.test(line))) {
    const block = ["", "configurations.all {", '    exclude group: "org.jitsi", module: "webrtc"', "}", ""];

    const dependenciesIndex = lines.findIndex((line) => /^\s*dependencies\s*\{/.test(line));
    const insertionPoint = dependenciesIndex >= 0 ? dependenciesIndex : lines.length;
    lines.splice(insertionPoint, 0, ...block);
  }

  fs.writeFileSync(buildFile, lines.join("\n"), "utf8");
  const manifestResult = patchAndroidManifest(manifestFile, googleMapsApiKey);

  if (manifestResult.callKeepPatched) {
    console.log("[patch-android] WebRTC patch applied and CallKeep services added to AndroidManifest.");
  } else {
    console.log("[patch-android] WebRTC patch applied and CallKeep services already present in AndroidManifest.");
  }

  if (manifestResult.mapsKeyPatched) {
    console.log("[patch-android] Google Maps API key synced into AndroidManifest.");
  } else {
    console.log("[patch-android] Google Maps API key already synced in AndroidManifest.");
  }
}

main();
