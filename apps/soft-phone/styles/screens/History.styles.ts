import { StyleSheet } from "react-native";

import type { ThemeColors } from "@/theme";

export const createHistoryStyles = (c: ThemeColors) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: c.background,
    },
    header: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "center",
      paddingHorizontal: 20,
      paddingTop: 16,
      paddingBottom: 12,
    },
    headerTitle: {
      fontSize: 28,
      fontWeight: "700",
      color: c.text,
    },
    clearButton: {
      padding: 8,
      borderRadius: 8,
      backgroundColor: "rgba(239, 68, 68, 0.1)",
    },
    listContent: {
      paddingHorizontal: 12,
      paddingBottom: 24,
    },
    listContentEmpty: {
      flex: 1,
      justifyContent: "center",
    },
    historyItem: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 14,
      paddingHorizontal: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: c.historyItemBorder,
      gap: 12,
    },
    historyItemPressed: {
      backgroundColor: c.historyItemPressedBg,
    },
    iconContainer: {
      width: 40,
      height: 40,
      borderRadius: 20,
      justifyContent: "center",
      alignItems: "center",
    },
    itemContent: {
      flex: 1,
      gap: 2,
    },
    phoneNumber: {
      fontSize: 16,
      fontWeight: "500",
      color: c.text,
    },
    itemMeta: {
      flexDirection: "row",
      alignItems: "center",
    },
    resultLabel: {
      fontSize: 13,
      fontWeight: "500",
    },
    duration: {
      fontSize: 13,
      color: c.muted,
    },
    itemRight: {
      alignItems: "flex-end",
      gap: 6,
    },
    timestamp: {
      fontSize: 12,
      color: c.muted,
    },
    callIcon: {
      opacity: 0.8,
    },
    emptyContainer: {
      alignItems: "center",
      justifyContent: "center",
      gap: 12,
      paddingVertical: 60,
    },
    emptyTitle: {
      fontSize: 18,
      fontWeight: "600",
      color: c.emptyTitle,
      marginTop: 8,
    },
    emptySubtitle: {
      fontSize: 14,
      color: c.emptySubtitle,
    },
  });
