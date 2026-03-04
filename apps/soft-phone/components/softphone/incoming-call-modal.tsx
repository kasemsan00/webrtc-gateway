import type { IncomingCallInfo } from "@/lib/gateway";
import { styles } from "@/styles/components/IncomingCallModal.styles";
import React from "react";
import { Modal, Pressable, View } from "react-native";
import { Avatar, AvatarFallback } from "../ui/avatar";
import { AppIcon } from "../ui/icon";
import { Text } from "../ui/text";

interface IncomingCallModalProps {
  visible: boolean;
  callInfo: IncomingCallInfo | null;
  onAnswer: () => void;
  onDecline: () => void;
}

function getInitials(name?: string, phone?: string): string {
  if (name) {
    const parts = name.split(" ");
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.substring(0, 2).toUpperCase();
  }
  return phone?.substring(0, 2) || "??";
}

export function IncomingCallModal({ visible, callInfo, onAnswer, onDecline }: IncomingCallModalProps) {
  if (!callInfo) return null;

  return (
    <Modal visible={visible} animationType="fade" transparent>
      <View style={styles.overlay}>
        <View style={styles.container}>
          {/* Animated background */}
          <View style={styles.animatedBg} />

          {/* Content */}
          <View style={styles.content}>
            {/* Header */}
            <Text style={styles.title}>Incoming Call</Text>

            {/* Caller info */}
            <View style={styles.callerSection}>
              <View style={styles.avatarRing}>
                <Avatar size="xl">
                  <AvatarFallback>
                    <Text style={styles.avatarText}>{getInitials(callInfo.displayName, callInfo.caller)}</Text>
                  </AvatarFallback>
                </Avatar>
              </View>

              <Text style={styles.callerName}>{callInfo.displayName || callInfo.caller}</Text>

              {callInfo.displayName && <Text style={styles.callerNumber}>{callInfo.caller}</Text>}
            </View>

            {/* Action buttons */}
            <View style={styles.actions}>
              {/* Decline */}
              <View style={styles.actionWrapper}>
                <Pressable
                  style={({ pressed }) => [styles.actionButton, styles.declineButton, pressed && styles.declineButtonPressed]}
                  onPress={onDecline}
                >
                  <AppIcon name="phoneOff" size={28} color="#fff" style={{ transform: [{ rotate: "135deg" }] }} />
                </Pressable>
                <Text style={styles.actionLabel}>Decline</Text>
              </View>

              {/* Answer */}
              <View style={styles.actionWrapper}>
                <Pressable
                  style={({ pressed }) => [styles.actionButton, styles.answerButton, pressed && styles.answerButtonPressed]}
                  onPress={onAnswer}
                >
                  <AppIcon name="phone" size={28} color="#fff" />
                </Pressable>
                <Text style={styles.actionLabel}>Answer</Text>
              </View>
            </View>
          </View>
        </View>
      </View>
    </Modal>
  );
}
