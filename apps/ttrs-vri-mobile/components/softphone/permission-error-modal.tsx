import React from "react";
import { Linking, Modal, Platform, Pressable, View } from "react-native";

import { ms } from "@/lib/scale";
import { styles } from "@/styles/components/permission-error-modal.styles";
import { AlertCircle, Settings } from "lucide-react-native";

import { Text } from "../ui/text";

interface PermissionErrorModalProps {
  visible: boolean;
  missingPermissions: ("camera" | "microphone")[];
  retryCount: number;
  maxRetries: number;
  onRetry: () => void;
  onCancel: () => void;
}

export function PermissionErrorModal({ visible, missingPermissions, retryCount, maxRetries, onRetry, onCancel }: PermissionErrorModalProps) {
  const hasReachedMaxRetries = retryCount >= maxRetries;

  const permissionText =
    missingPermissions.length === 2
      ? "Camera and Microphone permissions"
      : missingPermissions.includes("camera")
        ? "Camera permission"
        : "Microphone permission";

  const handleOpenSettings = async () => {
    try {
      // Try React Native's built-in method first
      const canOpen = await Linking.canOpenURL("app-settings:");
      if (canOpen) {
        await Linking.openSettings();
      } else {
        // Fallback to platform-specific deep links
        if (Platform.OS === "ios") {
          await Linking.openURL("app-settings:");
        } else if (Platform.OS === "android") {
          await Linking.sendIntent("android.settings.APPLICATION_DETAILS_SETTINGS", [
            { key: "package", value: "com.softphonemobile" }, // Update with your actual package name
          ]);
        }
      }
    } catch (error) {
      console.error("[PermissionErrorModal] Failed to open settings:", error);
      // Fallback to general settings if specific app settings fail
      try {
        await Linking.openSettings();
      } catch (fallbackError) {
        console.error("[PermissionErrorModal] Failed to open general settings:", fallbackError);
      }
    }
  };

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel}>
      <View style={styles.overlay}>
        <View style={styles.modal}>
          {/* Icon */}
          <View style={styles.iconContainer}>
            <AlertCircle size={ms(48)} color="#EF4444" />
          </View>

          {/* Title */}
          <Text style={styles.title}>Permissions Required</Text>

          {/* Message */}
          <Text style={styles.message}>
            {permissionText} {missingPermissions.length > 1 ? "are" : "is"} required for video calls.
          </Text>

          {hasReachedMaxRetries && <Text style={styles.maxRetriesText}>Maximum retry attempts reached. Please grant permissions in settings.</Text>}

          {/* Retry Count */}
          {!hasReachedMaxRetries && (
            <Text style={styles.retryCount}>
              Retry attempt: {retryCount}/{maxRetries}
            </Text>
          )}

          {/* Buttons */}
          <View style={styles.buttonContainer}>
            {/* Open Settings Button */}
            <Pressable style={({ pressed }) => [styles.button, styles.primaryButton, pressed && styles.buttonPressed]} onPress={handleOpenSettings}>
              <Settings size={ms(18)} color="#fff" />
              <Text style={styles.primaryButtonText}>Open Settings</Text>
            </Pressable>

            {/* Retry Button (only if not max retries) */}
            {!hasReachedMaxRetries && (
              <Pressable style={({ pressed }) => [styles.button, styles.secondaryButton, pressed && styles.buttonPressed]} onPress={onRetry}>
                <Text style={styles.secondaryButtonText}>Retry ({maxRetries - retryCount} left)</Text>
              </Pressable>
            )}

            {/* Cancel Button */}
            <Pressable style={({ pressed }) => [styles.button, styles.cancelButton, pressed && styles.buttonPressed]} onPress={onCancel}>
              <Text style={styles.cancelButtonText}>Cancel</Text>
            </Pressable>
          </View>
        </View>
      </View>
    </Modal>
  );
}
