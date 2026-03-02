import { Card, CardContent } from "@/components/ui/card";
import { Text } from "@/components/ui/text";
import { BITRATE_MAP, VideoFrameRate, VideoResolution, useSettingsStore } from "@/store/settings-store";
import { useSipStore } from "@/store/sip-store";
import { styles } from "@/styles/screens/settings.styles";
import { useEffect, useMemo, useState } from "react";
import { Alert, Platform, Pressable, ScrollView, TextInput, View, useWindowDimensions } from "react-native";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";

// Video resolution options
const RESOLUTION_OPTIONS: { label: string; value: VideoResolution; height: number; width: number }[] = [
  { label: "1080p", value: "1080", height: 1080, width: 1920 },
  { label: "720p", value: "720", height: 720, width: 1280 },
  { label: "480p", value: "480", height: 480, width: 854 },
  { label: "360p", value: "360", height: 360, width: 640 },
];

// Helper to format bitrate
const formatBitrate = (kbps: number): string => {
  if (kbps >= 1000) {
    return `${kbps / 1000} Mbps`;
  }
  return `${kbps} kbps`;
};

// Video frame rate options
const FRAMERATE_OPTIONS: { label: string; value: VideoFrameRate }[] = [
  { label: "15 fps", value: 15 },
  { label: "30 fps", value: 30 },
  { label: "60 fps", value: 60 },
];

export default function SettingsScreen() {
  // Settings store (MMKV + Zustand)
  const settingsStore = useSettingsStore();

  // SIP credentials
  const [sipDomain, setSipDomain] = useState(settingsStore.sipDomain);
  const [sipUsername, setSipUsername] = useState(settingsStore.sipUsername);
  const [sipPassword, setSipPassword] = useState(settingsStore.sipPassword);
  const [sipDisplayName, setSipDisplayName] = useState(settingsStore.sipDisplayName);
  const [sipPort, setSipPort] = useState(String(settingsStore.sipPort));

  // TURN server config - synced from store
  const [turnEnabled, setTurnEnabled] = useState(settingsStore.turnEnabled);
  const [turnUrl, setTurnUrl] = useState(settingsStore.turnUrl);
  const [turnUsername, setTurnUsername] = useState(settingsStore.turnUsername);
  const [turnPassword, setTurnPassword] = useState(settingsStore.turnPassword);

  // Video settings - synced from store
  const [videoResolution, setVideoResolution] = useState<VideoResolution>(settingsStore.videoResolution);
  const [videoFrameRate, setVideoFrameRate] = useState<VideoFrameRate>(settingsStore.videoFrameRate);

  // Handle video resolution change - auto-save to MMKV via store
  const handleVideoResolutionChange = (resolution: VideoResolution) => {
    setVideoResolution(resolution);
    settingsStore.setVideoResolution(resolution);
    console.log("[Settings] 📹 Video resolution saved:", resolution);
  };

  // Handle video frame rate change - auto-save to MMKV via store
  const handleVideoFrameRateChange = (frameRate: VideoFrameRate) => {
    setVideoFrameRate(frameRate);
    settingsStore.setVideoFrameRate(frameRate);
    console.log("[Settings] 🎞️ Video frame rate saved:", frameRate, "fps");
  };

  // SIP store values
  const isConnected = useSipStore((s) => s.isConnected);
  const isConnecting = useSipStore((s) => s.isConnecting);
  const connectionError = useSipStore((s) => s.connectionError);
  const isRegistered = useSipStore((s) => s.isRegistered);
  const isRegistering = useSipStore((s) => s.isRegistering);
  const registrationError = useSipStore((s) => s.registrationError);
  const isReconnecting = useSipStore((s) => s.isReconnecting);
  const reconnectAttempt = useSipStore((s) => s.reconnectAttempt);

  // SIP store actions
  const register = useSipStore((s) => s.register);
  const unregister = useSipStore((s) => s.unregister);
  const setSipConfig = useSipStore((s) => s.setSipConfig);

  // Debug: Log connection state
  useEffect(() => {
    console.log("[Settings] Connection state:", { isConnected, isRegistered });
  }, [isConnected, isRegistered]);

  // Sync local state from store when it hydrates
  useEffect(() => {
    setSipDomain(settingsStore.sipDomain);
    setSipUsername(settingsStore.sipUsername);
    setSipPassword(settingsStore.sipPassword);
    setSipDisplayName(settingsStore.sipDisplayName);
    setSipPort(String(settingsStore.sipPort));
    setTurnEnabled(settingsStore.turnEnabled);
    setTurnUrl(settingsStore.turnUrl);
    setTurnUsername(settingsStore.turnUsername);
    setTurnPassword(settingsStore.turnPassword);
    setVideoResolution(settingsStore.videoResolution);
    setVideoFrameRate(settingsStore.videoFrameRate);
  }, [
    settingsStore.sipDomain,
    settingsStore.sipUsername,
    settingsStore.sipPassword,
    settingsStore.sipDisplayName,
    settingsStore.sipPort,
    settingsStore.turnEnabled,
    settingsStore.turnUrl,
    settingsStore.turnUsername,
    settingsStore.turnPassword,
    settingsStore.videoResolution,
    settingsStore.videoFrameRate,
  ]);

  const saveSettings = () => {
    // Save all settings to MMKV via store
    settingsStore.updateSettings({
      sipDomain,
      sipUsername,
      sipPassword,
      sipDisplayName,
      sipPort: parseInt(sipPort, 10) || 5060,
      turnEnabled,
      turnUrl,
      turnUsername,
      turnPassword,
      videoResolution,
      videoFrameRate,
    });

    Alert.alert("Success", "Settings saved");
  };

  const handleRegister = async () => {
    if (isRegistered) {
      await unregister();
      return;
    }

    if (!sipDomain.trim() || !sipUsername.trim() || !sipPassword.trim()) {
      Alert.alert("Error", "Please fill in all SIP fields");
      return;
    }

    try {
      const config = {
        sipDomain,
        sipUsername,
        sipPassword,
        sipDisplayName: sipDisplayName || sipUsername,
        sipPort: parseInt(sipPort, 10) || 5060,
      };
      setSipConfig(config);
      await register(config);
    } catch (error) {
      Alert.alert("Registration Failed", String(error));
    }
  };

  // Status display
  const getConnectionStatus = () => {
    if (isReconnecting) return { text: `Reconnecting... (${reconnectAttempt})`, color: "#F59E0B" };
    if (isConnecting) return { text: "Connecting...", color: "#F59E0B" };
    if (isConnected) return { text: "Connected", color: "#10B981" };
    if (connectionError) return { text: "Error", color: "#EF4444" };
    return { text: "Disconnected", color: "#64748B" };
  };

  const getRegistrationStatus = () => {
    if (!isConnected) return { text: "Not Connected", color: "#64748B" };
    if (isRegistering) return { text: "Registering...", color: "#F59E0B" };
    if (isRegistered) return { text: "Registered", color: "#10B981" };
    if (registrationError) return { text: "Failed", color: "#EF4444" };
    return { text: "Not Registered", color: "#64748B" };
  };

  const connStatus = getConnectionStatus();
  const regStatus = getRegistrationStatus();

  // Calculate tab bar height/padding for proper scrolling
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const isTablet = Math.min(width, height) >= 600;

  const bottomPadding = useMemo(() => {
    const LAST_SECTION_MARGIN_BOTTOM = 24; // keep in sync with styles.section.marginBottom

    // If tablet, we use sidebar, so no bottom tab bar to pad against.
    // Just use safe area bottom + some spacing.
    if (isTablet) {
      // We already have a bottom margin on the last section; avoid double-spacing.
      return Math.max(0, insets.bottom);
    }

    // For mobile (bottom tabs), align with the actual tab bar height from `(tabs)/_layout.tsx`
    // so we don't over-pad and create a large blank area at the bottom.
    const baseHeight = 0;
    const bottomInset = Platform.OS === "android" ? Math.max(insets.bottom, 12) : insets.bottom;
    const tabBarHeight = baseHeight + bottomInset;

    // The last section already has a marginBottom. Subtract it to avoid double spacing.
    return Math.max(0, tabBarHeight - LAST_SECTION_MARGIN_BOTTOM);
  }, [insets.bottom, isTablet]);

  return (
    <SafeAreaView style={styles.container} edges={["top"]}>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[styles.scrollContent, { paddingBottom: bottomPadding }]}
        showsVerticalScrollIndicator={false}
      >
        <View style={[styles.contentWrapper, isTablet && styles.contentWrapperTablet]}>
          {/* Status Overview */}
          <View style={styles.statusOverview}>
            <View style={styles.statusItem}>
              <View style={[styles.statusIndicator, { backgroundColor: connStatus.color }]} />
              <Text style={styles.statusLabel}>Gateway</Text>
              <Text style={[styles.statusValue, { color: connStatus.color }]}>{connStatus.text}</Text>
            </View>
            <View style={styles.statusDivider} />
            <View style={styles.statusItem}>
              <View style={[styles.statusIndicator, { backgroundColor: regStatus.color }]} />
              <Text style={styles.statusLabel}>SIP</Text>
              <Text style={[styles.statusValue, { color: regStatus.color }]}>{regStatus.text}</Text>
            </View>
          </View>

          {/* TURN Server Section - Hidden from UI */}

          {/* SIP Account Section */}
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>SIP Account</Text>
            <Card style={styles.card}>
              <CardContent style={styles.cardContent}>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>SIP Domain</Text>
                  <TextInput
                    style={styles.input}
                    value={sipDomain}
                    onChangeText={setSipDomain}
                    placeholder="203.150.245.39"
                    placeholderTextColor="#64748B"
                    autoCapitalize="none"
                    autoCorrect={false}
                  />
                </View>

                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>Username</Text>
                  <TextInput
                    style={styles.input}
                    value={sipUsername}
                    onChangeText={setSipUsername}
                    placeholder="0000168180044"
                    placeholderTextColor="#64748B"
                    autoCapitalize="none"
                    autoCorrect={false}
                  />
                </View>

                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>Password</Text>
                  <TextInput
                    style={styles.input}
                    value={sipPassword}
                    onChangeText={setSipPassword}
                    placeholder="Enter password"
                    placeholderTextColor="#64748B"
                    autoCapitalize="none"
                    autoCorrect={false}
                  />
                </View>

                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>Port (optional)</Text>
                  <TextInput
                    style={styles.input}
                    value={sipPort}
                    onChangeText={setSipPort}
                    placeholder="5060"
                    placeholderTextColor="#64748B"
                    keyboardType="numeric"
                  />
                </View>

                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>Display Name (optional)</Text>
                  <TextInput
                    style={styles.input}
                    value={sipDisplayName}
                    onChangeText={setSipDisplayName}
                    placeholder="Your Name"
                    placeholderTextColor="#64748B"
                  />
                </View>

                {registrationError && <Text style={styles.errorText}>{registrationError}</Text>}

                <View style={styles.buttonRow}>
                  <Pressable
                    style={[
                      styles.button,
                      styles.flex1,
                      isRegistered ? styles.buttonDanger : styles.buttonPrimary,
                      (!isConnected || isRegistering) && styles.buttonDisabled,
                    ]}
                    onPress={handleRegister}
                    disabled={!isConnected || isRegistering}
                  >
                    <Text style={styles.buttonText}>{isRegistering ? "Registering..." : isRegistered ? "Unregister" : "Register"}</Text>
                  </Pressable>

                  <Pressable style={[styles.button, styles.buttonOutline, styles.flex1]} onPress={saveSettings}>
                    <Text style={styles.buttonOutlineText}>Save</Text>
                  </Pressable>
                </View>
              </CardContent>
            </Card>
          </View>

          {/* Video Settings */}
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>Video Settings</Text>
            <Card style={styles.card}>
              <CardContent style={styles.cardContent}>
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>Local Stream Resolution</Text>
                  <View style={styles.resolutionPicker}>
                    {RESOLUTION_OPTIONS.map((option) => (
                      <Pressable
                        key={option.value}
                        style={[styles.resolutionOption, videoResolution === option.value && styles.resolutionOptionSelected]}
                        onPress={() => handleVideoResolutionChange(option.value)}
                      >
                        <Text style={[styles.resolutionOptionText, videoResolution === option.value && styles.resolutionOptionTextSelected]}>
                          {option.label}
                        </Text>
                        <Text style={[styles.resolutionBitrateText, videoResolution === option.value && styles.resolutionBitrateTextSelected]}>
                          {formatBitrate(BITRATE_MAP[option.value])}
                        </Text>
                      </Pressable>
                    ))}
                  </View>
                  <Text style={styles.inputHint}>Higher resolution = better quality but more bandwidth</Text>
                </View>
                <View style={styles.settingDivider} />

                {/* Frame Rate Settings */}
                <View style={styles.inputGroup}>
                  <Text style={styles.inputLabel}>🎞️ Video Frame Rate</Text>
                  <View style={styles.resolutionPicker}>
                    {FRAMERATE_OPTIONS.map((option) => (
                      <Pressable
                        key={option.value}
                        style={[styles.resolutionOption, videoFrameRate === option.value && styles.resolutionOptionSelected]}
                        onPress={() => handleVideoFrameRateChange(option.value)}
                      >
                        <Text style={[styles.resolutionOptionText, videoFrameRate === option.value && styles.resolutionOptionTextSelected]}>
                          {option.label}
                        </Text>
                      </Pressable>
                    ))}
                  </View>
                  <Text style={styles.inputHint}>Lower frame rate uses less bandwidth</Text>
                </View>
              </CardContent>
            </Card>
          </View>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
