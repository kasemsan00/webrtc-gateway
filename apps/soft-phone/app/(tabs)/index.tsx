import { Dialpad } from "@/components/softphone/dialpad";
import { AppIcon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/hooks/use-theme-color";
import { CallState } from "@/lib/gateway";
import { useDialerStore } from "@/store/dialer-store";
import { useSettingsStore } from "@/store/settings-store";
import { useSipStore } from "@/store/sip-store";
import { createDialerStyles } from "@/styles/screens/Dialer.styles";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Animated, Pressable, View, useWindowDimensions } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

import { AnimatedTabScreen } from "@/components/ui/animated-tab-screen";

export default function DialerScreen() {
  const colors = useThemeColors();
  const styles = useMemo(() => createDialerStyles(colors), [colors]);
  const phoneNumber = useDialerStore((s) => s.draftNumber);
  const appendDigit = useDialerStore((s) => s.appendDigit);
  const backspace = useDialerStore((s) => s.backspace);
  const setDraftNumber = useDialerStore((s) => s.setDraftNumber);
  const { width, height } = useWindowDimensions();
  const isTablet = Math.min(width, height) >= 600;
  const isLandscape = width > height;

  // Dialpad metrics (synced from Dialpad component)
  const [metrics, setMetrics] = useState({ keySize: 64, gap: 16 });

  // Settings store values
  const callMode = useSettingsStore((s) => s.callMode);
  const trunkId = useSettingsStore((s) => s.trunkId);

  // SIP store values
  const isConnected = useSipStore((s) => s.isConnected);
  const trunkResolveStatus = useSipStore((s) => s.trunkResolveStatus);
  const trunkResolveError = useSipStore((s) => s.trunkResolveError);
  const callState = useSipStore((s) => s.callState);

  // SIP store actions
  const call = useSipStore((s) => s.call);
  const sendDtmf = useSipStore((s) => s.sendDtmf);
  const setPermissionError = useSipStore((s) => s.setPermissionError);
  const incrementPermissionRetry = useSipStore((s) => s.incrementPermissionRetry);
  const resetPermissionRetry = useSipStore((s) => s.resetPermissionRetry);

  // Animation for call button
  const pulseAnim = useRef(new Animated.Value(1)).current;
  const hasPhoneNumber = phoneNumber.length > 0;

  useEffect(() => {
    if (hasPhoneNumber) {
      Animated.loop(
        Animated.sequence([
          Animated.timing(pulseAnim, {
            toValue: 1.05,
            duration: 1000,
            useNativeDriver: true,
          }),
          Animated.timing(pulseAnim, {
            toValue: 1,
            duration: 1000,
            useNativeDriver: true,
          }),
        ]),
      ).start();
    } else {
      pulseAnim.setValue(1);
    }
  }, [hasPhoneNumber, pulseAnim]);

  const handleKeyPress = useCallback(
    (key: string) => {
      appendDigit(key);
      // Send DTMF if in call
      if (callState === CallState.INCALL) {
        sendDtmf(key);
      }
    },
    [appendDigit, callState, sendDtmf],
  );

  const handleBackspace = useCallback(() => {
    backspace();
  }, [backspace]);

  const handleBackspaceLongPress = useCallback(() => {
    setDraftNumber("");
  }, [setDraftNumber]);

  const formatPhoneNumber = (number: string): string => {
    return number;
  };

  const handleCall = useCallback(async () => {
    if (phoneNumber.length > 0) {
      try {
        // Reset permission error state before attempting call
        resetPermissionRetry();
        await call(phoneNumber);
      } catch (error) {
        console.error("[DialerScreen] Failed to make call:", error);

        // Check if error is permission-related
        const errorMessage = error instanceof Error ? error.message : String(error);
        if (errorMessage.includes("permission") || errorMessage.includes("Permission")) {
          // Parse which permissions are missing
          const missing: ("camera" | "microphone")[] = [];
          if (errorMessage.includes("Camera") || errorMessage.includes("camera")) {
            missing.push("camera");
          }
          if (errorMessage.includes("Microphone") || errorMessage.includes("microphone")) {
            missing.push("microphone");
          }

          // Set permission error and increment retry count
          setPermissionError(errorMessage, missing.length > 0 ? missing : ["camera", "microphone"]);
          incrementPermissionRetry();
        }
      }
    }
  }, [phoneNumber, call, resetPermissionRetry, setPermissionError, incrementPermissionRetry]);

  // Determine status display
  const getStatusInfo = () => {
    if (!isConnected) {
      return { text: "Disconnected", color: "#EF4444", bg: "rgba(239, 68, 68, 0.15)" };
    }
    if (callMode === "siptrunk") {
      if (trunkResolveStatus === "resolved") {
        return { text: "Trunk resolved", color: "#10B981", bg: "rgba(16, 185, 129, 0.15)" };
      }
      if (trunkResolveStatus === "resolving" || trunkResolveStatus === "redirecting") {
        return { text: "Resolving trunk...", color: "#F59E0B", bg: "rgba(245, 158, 11, 0.15)" };
      }
      if (trunkResolveStatus === "not_ready" || trunkResolveStatus === "not_found" || trunkResolveStatus === "failed") {
        return {
          text: trunkResolveError ? "Trunk not ready" : "Trunk resolve failed",
          color: "#EF4444",
          bg: "rgba(239, 68, 68, 0.15)",
        };
      }
      return { text: "Waiting trunk resolve", color: "#636366", bg: "rgba(99, 99, 102, 0.15)" };
    }
    return { text: "Ready", color: "#10B981", bg: "rgba(16, 185, 129, 0.15)" };
  };

  // Check if call mode is ready
  const isCallModeReady = (() => {
    if (callMode === "public") return true; // Always ready - fetches credentials per call
    if (callMode === "siptrunk") return trunkResolveStatus === "resolved" && (trunkId || 0) > 0; // Needs trunk resolve + trunk ID
    return false;
  })();

  const statusInfo = getStatusInfo();
  const isInCall =
    callState === CallState.INCALL || callState === CallState.CALLING || callState === CallState.RINGING || callState === CallState.CONNECTING;

  // Tablet Landscape Layout (Split View)
  if (isTablet && isLandscape) {
    return (
      <AnimatedTabScreen>
        <SafeAreaView style={styles.container} edges={["top", "bottom", "right"]}>
          <View pointerEvents="none" style={styles.statusIndicatorContainer}>
            <View style={[styles.statusDot, { backgroundColor: statusInfo.color }]} />
          </View>
          <View style={styles.splitContainer}>
            <View style={styles.rightPanel}>
              <View style={styles.header} />

              {/* Phone Number Display */}
              <View style={[styles.numberDisplay, styles.numberDisplayTabletLandscape]}>
                <Text style={[styles.phoneNumber, styles.phoneNumberTablet]} numberOfLines={2} adjustsFontSizeToFit>
                  {phoneNumber.length > 0 ? formatPhoneNumber(phoneNumber) : "Enter number"}
                </Text>
                <Pressable
                  style={[
                    styles.backspaceButton,
                    styles.backspaceButtonInline,
                    phoneNumber.length === 0 && styles.backspaceButtonHidden,
                    { width: metrics.keySize, height: metrics.keySize },
                  ]}
                  onPress={handleBackspace}
                  onLongPress={handleBackspaceLongPress}
                  disabled={phoneNumber.length === 0}
                >
                  <AppIcon name="delete" size={metrics.keySize * 0.4} color="#EF4444" />
                </Pressable>
              </View>

              {/* Backspace Button (Moved to left panel for ergonomics in split view) */}
              <View style={styles.actionRowLandscape}></View>
              <View style={styles.dialpadContainer}>
                <Dialpad onPress={handleKeyPress} onMetricsChange={setMetrics} />
                <View style={[styles.callButtonContainer, { paddingBottom: 0, marginTop: Math.round(metrics.gap * 0.5) }]}>
                  <Pressable onPress={handleCall} disabled={!isConnected || !isCallModeReady || phoneNumber.length === 0 || isInCall}>
                    {({ pressed }) => (
                      <Animated.View
                        style={[
                          styles.callButton,
                          {
                            width: metrics.keySize,
                            height: metrics.keySize,
                            borderRadius: metrics.keySize / 2,
                            transform: [{ scale: pulseAnim }],
                          },
                          (!isConnected || !isCallModeReady || phoneNumber.length === 0 || isInCall) && styles.callButtonDisabled,
                          pressed && styles.callButtonPressed,
                        ]}
                      >
                        <AppIcon name="phone" size={metrics.keySize * 0.4} color="#fff" />
                      </Animated.View>
                    )}
                  </Pressable>
                </View>
              </View>
            </View>
          </View>
        </SafeAreaView>
      </AnimatedTabScreen>
    );
  }

  // Default Layout (Mobile Portrait/Landscape + Tablet Portrait)
  return (
    <AnimatedTabScreen>
      <SafeAreaView style={styles.container} edges={["top"]}>
        <View pointerEvents="none" style={styles.statusIndicatorContainer}>
          <View style={[styles.statusDot, { backgroundColor: statusInfo.color }]} />
        </View>
        {/* Header */}
        <View style={styles.header} />

        <View style={[styles.dialpadContainer, isTablet && { maxWidth: 520, alignSelf: "center", width: "100%" }]}>
          <View style={styles.numberDisplay}>
            <View style={[styles.numberRow, { gap: metrics.gap }]}>
              {/* Left placeholder to keep the number centered */}
              <View style={{ width: metrics.keySize, height: metrics.keySize }} />

              <Text style={[styles.phoneNumber, isTablet && styles.phoneNumberTablet]} numberOfLines={1} adjustsFontSizeToFit>
                {phoneNumber.length > 0 ? formatPhoneNumber(phoneNumber) : "Enter number"}
              </Text>

              {/* Right - Backspace button (inline with number display) */}
              <Pressable
                style={[
                  styles.backspaceButton,
                  phoneNumber.length === 0 && styles.backspaceButtonHidden,
                  { width: metrics.keySize, height: metrics.keySize },
                ]}
                onPress={handleBackspace}
                onLongPress={handleBackspaceLongPress}
                disabled={phoneNumber.length === 0}
              >
                <AppIcon name="delete" size={metrics.keySize * 0.35} color="#EF4444" />
              </Pressable>
            </View>
          </View>
          <Dialpad onPress={handleKeyPress} onMetricsChange={setMetrics} />
          <View style={[styles.callButtonRow, { gap: metrics.gap }]}>
            {/* Left placeholder to match dialpad alignment */}
            <View style={{ width: metrics.keySize, height: metrics.keySize }} />
            {/* Center - Call button */}
            <View style={styles.callButtonContainer}>
              <Pressable
                onPress={handleCall}
                disabled={!isConnected || !isCallModeReady || phoneNumber.length === 0 || isInCall}
                hitSlop={{ top: 20, bottom: 20, left: 20, right: 20 }}
                pressRetentionOffset={{ top: 20, bottom: 20, left: 20, right: 20 }}
              >
                {({ pressed }) => (
                  <Animated.View
                    style={[
                      styles.callButton,
                      {
                        width: metrics.keySize,
                        height: metrics.keySize,
                        borderRadius: metrics.keySize / 2,
                        transform: [{ scale: pulseAnim }],
                      },
                      (!isConnected || !isCallModeReady || phoneNumber.length === 0 || isInCall) && styles.callButtonDisabled,
                      pressed && styles.callButtonPressed,
                    ]}
                  >
                    <AppIcon name="phone" size={metrics.keySize * 0.4} color="#fff" />
                  </Animated.View>
                )}
              </Pressable>
            </View>

            {/* Right placeholder to keep call button centered */}
            <View style={{ width: metrics.keySize, height: metrics.keySize }} />
          </View>
        </View>

        {/* Call Button + Backspace */}
        <View style={styles.callButtonContainer}></View>
      </SafeAreaView>
    </AnimatedTabScreen>
  );
}
