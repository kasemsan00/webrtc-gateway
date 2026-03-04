const { getDefaultConfig } = require("expo/metro-config");

/** @type {import('expo/metro-config').MetroConfig} */
const config = getDefaultConfig(__dirname);

// Add platform-specific extensions for WebRTC
config.resolver.sourceExts = [...config.resolver.sourceExts];
config.resolver.platforms = ["ios", "android", "web", "native"];

// Ensure proper resolution order for platform-specific files
// Metro will check these extensions in order: .web.ts, .native.ts, .ts
config.resolver.resolverMainFields = ["react-native", "browser", "main"];

module.exports = config;
