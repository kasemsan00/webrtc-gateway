import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/hooks/use-theme-color";
import { CallDirection, CallHistoryEntry, CallResult, useCallHistoryStore } from "@/store/call-history-store";
import { useDialerStore } from "@/store/dialer-store";
import { useSipStore } from "@/store/sip-store";
import { createHistoryStyles } from "@/styles/screens/History.styles";
import { useRouter } from "expo-router";
import { PhoneCall, PhoneIncoming, PhoneOutgoing, Trash2 } from "lucide-react-native";
import React, { useCallback, useMemo } from "react";
import { Alert, FlatList, Pressable, View, useWindowDimensions } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

import { AnimatedTabScreen } from "@/components/ui/animated-tab-screen";

function formatDuration(seconds: number): string {
  if (seconds <= 0) return "";
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  if (m === 0) return `${s}s`;
  return `${m}m ${s.toString().padStart(2, "0")}s`;
}

function formatTimestamp(ts: number): string {
  const date = new Date(ts);
  const now = new Date();
  const isToday = date.getFullYear() === now.getFullYear() && date.getMonth() === now.getMonth() && date.getDate() === now.getDate();

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const isYesterday =
    date.getFullYear() === yesterday.getFullYear() && date.getMonth() === yesterday.getMonth() && date.getDate() === yesterday.getDate();

  const time = date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

  if (isToday) return time;
  if (isYesterday) return `Yesterday ${time}`;
  return `${date.toLocaleDateString([], { day: "2-digit", month: "short" })} ${time}`;
}

function getDirectionIcon(direction: CallDirection, result: CallResult) {
  if (direction === "outgoing") {
    return PhoneOutgoing;
  }
  if (result === "missed" || result === "declined") {
    return PhoneIncoming;
  }
  return PhoneIncoming;
}

function getResultColor(result: CallResult): string {
  switch (result) {
    case "answered":
      return "#10B981";
    case "missed":
      return "#EF4444";
    case "declined":
      return "#F59E0B";
    case "failed":
      return "#EF4444";
    default:
      return "#636366";
  }
}

function getResultLabel(result: CallResult, direction: CallDirection): string {
  if (direction === "outgoing") {
    switch (result) {
      case "answered":
        return "Outgoing";
      case "failed":
        return "Failed";
      default:
        return "Outgoing";
    }
  }
  switch (result) {
    case "answered":
      return "Incoming";
    case "missed":
      return "Missed";
    case "declined":
      return "Declined";
    default:
      return "Incoming";
  }
}

interface HistoryItemProps {
  entry: CallHistoryEntry;
  onCall: (number: string) => void;
}

function HistoryItem({ entry, onCall, styles }: HistoryItemProps & { styles: ReturnType<typeof createHistoryStyles> }) {
  const Icon = getDirectionIcon(entry.direction, entry.result);
  const color = getResultColor(entry.result);
  const label = getResultLabel(entry.result, entry.direction);
  const duration = formatDuration(entry.duration);
  const time = formatTimestamp(entry.timestamp);

  return (
    <Pressable style={({ pressed }) => [styles.historyItem, pressed && styles.historyItemPressed]} onPress={() => onCall(entry.phoneNumber)}>
      <View style={[styles.iconContainer, { backgroundColor: `${color}20` }]}>
        <Icon size={20} color={color} />
      </View>
      <View style={styles.itemContent}>
        <Text style={styles.phoneNumber} numberOfLines={1}>
          {entry.displayName || entry.phoneNumber}
        </Text>
        <View style={styles.itemMeta}>
          <Text style={[styles.resultLabel, { color }]}>{label}</Text>
          {duration ? <Text style={styles.duration}> · {duration}</Text> : null}
        </View>
      </View>
      <View style={styles.itemRight}>
        <Text style={styles.timestamp}>{time}</Text>
        <PhoneCall size={18} color="#10B981" style={styles.callIcon} />
      </View>
    </Pressable>
  );
}

export default function HistoryScreen() {
  const colors = useThemeColors();
  const styles = useMemo(() => createHistoryStyles(colors), [colors]);
  const entries = useCallHistoryStore((s) => s.entries);
  const clearHistory = useCallHistoryStore((s) => s.clearHistory);
  const setDraftNumber = useDialerStore((s) => s.setDraftNumber);
  const sipCall = useSipStore((s) => s.call);
  const isConnected = useSipStore((s) => s.isConnected);
  const router = useRouter();
  const { width, height } = useWindowDimensions();
  const isTablet = Math.min(width, height) >= 600;

  const handleCall = useCallback(
    async (number: string) => {
      if (!isConnected) {
        // Navigate to keypad and set the number for manual call
        setDraftNumber(number);
        router.navigate("/(tabs)");
        return;
      }
      try {
        setDraftNumber(number);
        await sipCall(number);
      } catch (error) {
        console.error("[HistoryScreen] Failed to make call:", error);
        // Fallback: navigate to keypad with number pre-filled
        setDraftNumber(number);
        router.navigate("/(tabs)");
      }
    },
    [isConnected, sipCall, setDraftNumber, router],
  );

  const handleClearHistory = useCallback(() => {
    Alert.alert("Clear History", "Are you sure you want to clear all call history?", [
      { text: "Cancel", style: "cancel" },
      { text: "Clear", style: "destructive", onPress: clearHistory },
    ]);
  }, [clearHistory]);

  const renderItem = useCallback(
    ({ item }: { item: CallHistoryEntry }) => <HistoryItem entry={item} onCall={handleCall} styles={styles} />,
    [handleCall, styles],
  );

  const keyExtractor = useCallback((item: CallHistoryEntry) => item.id, []);

  const ListEmptyComponent = useMemo(
    () => (
      <View style={styles.emptyContainer}>
        <PhoneOutgoing size={48} color={colors.emptyIcon} />
        <Text style={styles.emptyTitle}>No call history</Text>
        <Text style={styles.emptySubtitle}>Your recent calls will appear here</Text>
      </View>
    ),
    [colors.emptyIcon, styles.emptyContainer, styles.emptyTitle, styles.emptySubtitle],
  );

  return (
    <AnimatedTabScreen>
      <SafeAreaView style={styles.container} edges={["top"]}>
        <View style={styles.header}>
          <Text style={styles.headerTitle}>Recents</Text>
          {entries.length > 0 && (
            <Pressable onPress={handleClearHistory} style={styles.clearButton} hitSlop={8}>
              <Trash2 size={18} color="#EF4444" />
            </Pressable>
          )}
        </View>
        <FlatList
          data={entries}
          renderItem={renderItem}
          keyExtractor={keyExtractor}
          contentContainerStyle={[
            styles.listContent,
            isTablet && { maxWidth: 600, alignSelf: "center", width: "100%" },
            entries.length === 0 && styles.listContentEmpty,
          ]}
          ListEmptyComponent={ListEmptyComponent}
          showsVerticalScrollIndicator={false}
        />
      </SafeAreaView>
    </AnimatedTabScreen>
  );
}
