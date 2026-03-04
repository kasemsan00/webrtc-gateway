import { Card, CardContent } from "@/components/ui/card";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/hooks/use-theme-color";
import { BITRATE_MAP, CallMode, ThemePreference, VideoFrameRate, VideoResolution, useSettingsStore } from "@/store/settings-store";
import { useSipStore } from "@/store/sip-store";
import { createSettingsStyles } from "@/styles/screens/Settings.styles";
import * as Clipboard from "expo-clipboard";
import { AlertCircle, CheckCircle2, Copy, Info, RefreshCcw, XCircle } from "lucide-react-native";
import React, { useEffect, useMemo, useState } from "react";
import { Alert, Pressable, ScrollView, TextInput, View, useWindowDimensions } from "react-native";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";

import { AnimatedTabScreen } from "@/components/ui/animated-tab-screen";

// Video resolution options
const RESOLUTION_OPTIONS: { label: string; value: VideoResolution; height: number; width: number }[] = [
  { label: "1080p", value: "1080", height: 1080, width: 1920 },
  { label: "720p", value: "720", height: 720, width: 1280 },
  { label: "480p", value: "480", height: 480, width: 854 },
  { label: "360p", value: "360", height: 360, width: 640 },
];

// Theme options
const THEME_OPTIONS: { label: string; value: ThemePreference; description: string }[] = [
  { label: "System", value: "system", description: "Match your device appearance automatically" },
  { label: "Light", value: "light", description: "Bright background with dark text" },
  { label: "Dark", value: "dark", description: "Dim background with light text" },
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
  const colors = useThemeColors();
  const styles = useMemo(() => createSettingsStyles(colors), [colors]);
  const settingsStore = useSettingsStore();

  // Local state synced from store
  const [callMode, setCallMode] = useState<CallMode>(settingsStore.callMode);
  const [videoResolution, setVideoResolution] = useState<VideoResolution>(settingsStore.videoResolution);
  const [videoFrameRate, setVideoFrameRate] = useState<VideoFrameRate>(settingsStore.videoFrameRate);
  const [themePreference, setThemePreference] = useState<ThemePreference>(settingsStore.themePreference);
  const [trunkIdInput, setTrunkIdInput] = useState(settingsStore.trunkId ? String(settingsStore.trunkId) : "");

  // SIP store values
  const isConnected = useSipStore((s) => s.isConnected);
  const isConnecting = useSipStore((s) => s.isConnecting);
  const connectionError = useSipStore((s) => s.connectionError);
  const isReconnecting = useSipStore((s) => s.isReconnecting);
  const reconnectAttempt = useSipStore((s) => s.reconnectAttempt);
  const trunkResolveStatus = useSipStore((s) => s.trunkResolveStatus);
  const trunkResolveError = useSipStore((s) => s.trunkResolveError);
  const resolveTrunk = useSipStore((s) => s.resolveTrunk);
  const clearTrunkResolveState = useSipStore((s) => s.clearTrunkResolveState);

  // Sync local state from store
  useEffect(() => {
    setCallMode(settingsStore.callMode);
    setVideoResolution(settingsStore.videoResolution);
    setVideoFrameRate(settingsStore.videoFrameRate);
    setThemePreference(settingsStore.themePreference);
  }, [settingsStore.callMode, settingsStore.videoResolution, settingsStore.videoFrameRate, settingsStore.themePreference]);

  useEffect(() => {
    setTrunkIdInput(settingsStore.trunkId ? String(settingsStore.trunkId) : "");
  }, [settingsStore.trunkId]);

  useEffect(() => {
    if (callMode === "siptrunk" && isConnected && trunkResolveStatus === "idle") {
      void resolveTrunk().catch((error) => {
        console.warn("[SettingsScreen] Initial trunk resolve failed:", error);
      });
    }
  }, [callMode, isConnected, trunkResolveStatus, resolveTrunk]);

  const handleVideoResolutionChange = (resolution: VideoResolution) => {
    setVideoResolution(resolution);
    settingsStore.setVideoResolution(resolution);
  };

  const handleVideoFrameRateChange = (frameRate: VideoFrameRate) => {
    setVideoFrameRate(frameRate);
    settingsStore.setVideoFrameRate(frameRate);
  };

  const handleCallModeChange = (mode: CallMode) => {
    const previousMode = callMode;
    setCallMode(mode);
    settingsStore.setCallMode(mode);

    if (previousMode === "siptrunk" && mode === "public") {
      clearTrunkResolveState();
    }

    if (mode === "siptrunk" && isConnected) {
      void resolveTrunk().catch((error) => {
        console.warn("[SettingsScreen] Trunk resolve failed after mode change:", error);
      });
    }
  };

  const handleThemePreferenceChange = (value: ThemePreference) => {
    setThemePreference(value);
    settingsStore.setThemePreference(value);
  };

  const handleTrunkIdChange = (text: string) => {
    const digitsOnly = text.replace(/\D/g, "");
    setTrunkIdInput(digitsOnly);

    if (!digitsOnly) {
      settingsStore.setTrunkId(null);
      return;
    }

    const parsed = Number(digitsOnly);
    settingsStore.setTrunkId(Number.isFinite(parsed) && parsed > 0 ? parsed : null);
  };

  const copyToClipboard = async (text: string) => {
    await Clipboard.setStringAsync(text);
    Alert.alert("Copied", "Server URL copied to clipboard");
  };

  // Status display helpers
  const getConnectionStatus = () => {
    if (isReconnecting) return { text: `Reconnecting (${reconnectAttempt})`, color: "#F59E0B", icon: RefreshCcw };
    if (isConnecting) return { text: "Connecting...", color: "#F59E0B", icon: AlertCircle };
    if (isConnected) return { text: "Connected", color: "#10B981", icon: CheckCircle2 };
    if (connectionError) return { text: "Error", color: "#EF4444", icon: XCircle };
    return { text: "Disconnected", color: "#636366", icon: AlertCircle };
  };

  const getRegistrationStatus = () => {
    if (callMode === "public") {
      return { text: "Per-call auth", color: "#10B981", icon: CheckCircle2 };
    }
    if (!isConnected) return { text: "Not Connected", color: "#636366", icon: AlertCircle };
    if (trunkResolveStatus === "resolving" || trunkResolveStatus === "redirecting") {
      return { text: "Resolving...", color: "#F59E0B", icon: RefreshCcw };
    }
    if (trunkResolveStatus === "resolved") return { text: "Resolved", color: "#10B981", icon: CheckCircle2 };
    if (trunkResolveStatus === "not_ready") return { text: "Not Ready", color: "#F59E0B", icon: AlertCircle };
    if (trunkResolveStatus === "not_found" || trunkResolveStatus === "failed") return { text: "Failed", color: "#EF4444", icon: XCircle };
    return { text: "Waiting", color: "#636366", icon: AlertCircle };
  };

  const connStatus = getConnectionStatus();
  const regStatus = getRegistrationStatus();

  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const isTablet = Math.min(width, height) >= 600;
  const horizontalPadding = isTablet ? 24 : 16;
  const maxContentWidth = isTablet ? 680 : undefined;

  const currentVideoSummary = useMemo(() => {
    return `${videoResolution}p · ${formatBitrate(BITRATE_MAP[videoResolution])} · ${videoFrameRate} fps`;
  }, [videoResolution, videoFrameRate]);

  return (
    <AnimatedTabScreen>
      <SafeAreaView style={styles.container} edges={["top"]}>
        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={[styles.scrollContent, { paddingBottom: insets.bottom + 24 }]}
          showsVerticalScrollIndicator={false}
        >
          <View
            style={[
              styles.contentWrapper,
              { paddingHorizontal: horizontalPadding },
              maxContentWidth ? { maxWidth: maxContentWidth, alignSelf: "center" } : null,
            ]}
          >
            {/* Header */}
            <View style={styles.header}>
              <Text variant="h2" style={styles.headerTitle}>
                Settings
              </Text>
              <Text variant="muted" style={styles.headerSubtitle}>
                Configure your connection and video preferences
              </Text>
            </View>

            {/* Status Section */}
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>System Status</Text>
              <Card style={styles.card}>
                <CardContent style={styles.statusCardContent}>
                  <View style={styles.statusRow}>
                    <View style={styles.statusInfo}>
                      <connStatus.icon size={20} color={connStatus.color} />
                      <View style={styles.statusTextContainer}>
                        <Text style={styles.statusLabel}>Gateway Connection</Text>
                        <Text style={[styles.statusValue, { color: connStatus.color }]}>{connStatus.text}</Text>
                      </View>
                    </View>
                  </View>
                  <View style={styles.statusDivider} />
                  <View style={styles.statusRow}>
                    <View style={styles.statusInfo}>
                      <regStatus.icon size={20} color={regStatus.color} />
                      <View style={styles.statusTextContainer}>
                        <Text style={styles.statusLabel}>{callMode === "public" ? "Public Auth" : "Trunk Resolve"}</Text>
                        <Text style={[styles.statusValue, { color: regStatus.color }]}>{regStatus.text}</Text>
                      </View>
                    </View>
                  </View>
                </CardContent>
              </Card>
            </View>

            {/* Connection Section */}
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Connection</Text>
              <Card style={styles.card}>
                <CardContent style={styles.cardContent}>
                  {/* Gateway Server (Read-only) */}
                  <View style={styles.settingRow}>
                    <View style={styles.settingInfo}>
                      <Text style={styles.settingLabel}>Gateway Server</Text>
                      <Pressable onPress={() => copyToClipboard(settingsStore.gatewayServer)} style={styles.readOnlyContainer}>
                        <Text style={styles.readOnlyText} numberOfLines={1} ellipsizeMode="middle">
                          {settingsStore.gatewayServer}
                        </Text>
                        <Copy size={14} color={colors.mutedForeground} />
                      </Pressable>
                    </View>
                  </View>

                  <View style={styles.settingDivider} />
                </CardContent>
              </Card>
            </View>

            {/* Appearance Section */}
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Appearance</Text>
              <Card style={styles.card}>
                <CardContent style={styles.cardContent}>
                  <View style={styles.settingRow}>
                    <View style={styles.settingInfo}>
                      <Text style={styles.settingLabel}>Theme</Text>
                      <Text style={styles.settingDescription}>Choose how the app looks</Text>
                    </View>
                  </View>

                  <View style={[styles.segmentedControl, styles.themeSegmentedControl]}>
                    {THEME_OPTIONS.map((option) => (
                      <Pressable
                        key={option.value}
                        onPress={() => handleThemePreferenceChange(option.value)}
                        style={[styles.segment, themePreference === option.value && styles.segmentActive]}
                      >
                        <Text style={[styles.segmentText, themePreference === option.value && styles.segmentTextActive]}>{option.label}</Text>
                      </Pressable>
                    ))}
                  </View>

                  <View style={styles.themeDescriptionList}>
                    {THEME_OPTIONS.map((option) => (
                      <View key={`${option.value}-description`} style={styles.themeDescriptionRow}>
                        <View style={styles.themeDescriptionBullet} />
                        <Text style={styles.themeDescriptionText}>{option.description}</Text>
                      </View>
                    ))}
                  </View>
                </CardContent>
              </Card>
            </View>

            {/* Call Mode Section */}
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Call Mode</Text>
              <Card style={styles.card}>
                <CardContent style={styles.cardContent}>
                  <View style={styles.segmentedControl}>
                    <Pressable onPress={() => handleCallModeChange("public")} style={[styles.segment, callMode === "public" && styles.segmentActive]}>
                      <Text style={[styles.segmentText, callMode === "public" && styles.segmentTextActive]}>Public</Text>
                    </Pressable>
                    <Pressable
                      onPress={() => handleCallModeChange("siptrunk")}
                      style={[styles.segment, callMode === "siptrunk" && styles.segmentActive]}
                    >
                      <Text style={[styles.segmentText, callMode === "siptrunk" && styles.segmentTextActive]}>SIP Trunk</Text>
                    </Pressable>
                  </View>

                  <View style={styles.modeDescriptionContainer}>
                    <Info size={14} color={colors.muted} />
                    <Text style={styles.modeDescription}>
                      {callMode === "public"
                        ? "Credentials fetched automatically from VRS API for each call"
                        : "Trunk ID is resolved from SIP credentials before each trunk call"}
                    </Text>
                  </View>

                  {callMode === "siptrunk" && (
                    <View style={styles.trunkInputContainer}>
                      <View style={styles.trunkHeaderRow}>
                        <Text style={styles.inputLabel}>Resolved Trunk ID</Text>
                        <View style={styles.trunkStatusBadge}>
                          <Text style={[styles.trunkStatusText, { color: regStatus.color }]}>{regStatus.text}</Text>
                        </View>
                      </View>

                      <TextInput
                        style={styles.input}
                        value={trunkIdInput}
                        placeholder="Enter or resolve trunk ID"
                        placeholderTextColor={colors.placeholder}
                        keyboardType="number-pad"
                        onChangeText={handleTrunkIdChange}
                      />

                      {trunkResolveError ? <Text style={styles.trunkErrorText}>{trunkResolveError}</Text> : null}

                      <Pressable
                        style={({ pressed }) => [
                          styles.resolveButton,
                          (!isConnected || trunkResolveStatus === "resolving" || trunkResolveStatus === "redirecting") &&
                            styles.resolveButtonDisabled,
                          pressed && styles.resolveButtonPressed,
                        ]}
                        disabled={!isConnected || trunkResolveStatus === "resolving" || trunkResolveStatus === "redirecting"}
                        onPress={() => {
                          void resolveTrunk().catch((error) => {
                            console.warn("[SettingsScreen] Resolve trunk failed:", error);
                          });
                        }}
                      >
                        <RefreshCcw size={14} color={colors.resolveButtonText} />
                        <Text style={styles.resolveButtonText}>Resolve Trunk</Text>
                      </Pressable>
                    </View>
                  )}
                </CardContent>
              </Card>
            </View>

            {/* Video Settings Section */}
            <View style={styles.section}>
              <View style={styles.sectionHeaderRow}>
                <Text style={[styles.sectionTitle, styles.sectionTitleCompact]}>Video Quality</Text>
                <View style={styles.summaryBadge}>
                  <Text style={styles.summaryText}>{currentVideoSummary}</Text>
                </View>
              </View>

              <Card style={styles.card}>
                <CardContent style={styles.cardContent}>
                  <Text style={styles.inputLabel}>Resolution</Text>
                  <View style={styles.grid}>
                    {RESOLUTION_OPTIONS.map((option) => (
                      <Pressable
                        key={option.value}
                        style={[styles.gridItem, isTablet && styles.gridItemTablet, videoResolution === option.value && styles.gridItemActive]}
                        onPress={() => handleVideoResolutionChange(option.value)}
                      >
                        <Text style={[styles.gridItemLabel, videoResolution === option.value && styles.gridItemLabelActive]}>{option.label}</Text>
                        <Text style={[styles.gridItemSub, videoResolution === option.value && styles.gridItemSubActive]}>
                          {formatBitrate(BITRATE_MAP[option.value])}
                        </Text>
                      </Pressable>
                    ))}
                  </View>

                  <View style={styles.settingDivider} />

                  <Text style={styles.inputLabel}>Frame Rate</Text>
                  <View style={styles.frameRateRow}>
                    {FRAMERATE_OPTIONS.map((option) => (
                      <Pressable
                        key={option.value}
                        style={[styles.frameRateItem, videoFrameRate === option.value && styles.gridItemActive]}
                        onPress={() => handleVideoFrameRateChange(option.value)}
                      >
                        <Text style={[styles.gridItemLabel, videoFrameRate === option.value && styles.gridItemLabelActive]}>{option.label}</Text>
                      </Pressable>
                    ))}
                  </View>
                </CardContent>
              </Card>
            </View>
          </View>
        </ScrollView>
      </SafeAreaView>
    </AnimatedTabScreen>
  );
}
