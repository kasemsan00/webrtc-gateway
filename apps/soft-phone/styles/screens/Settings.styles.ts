import { Platform, StyleSheet } from "react-native";

import type { ThemeColors } from "@/theme";

export const createSettingsStyles = (c: ThemeColors) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: c.background,
    },
    scrollView: {
      flex: 1,
    },
    scrollContent: {
      flexGrow: 1,
    },
    contentWrapper: {
      width: "100%",
      paddingTop: 12,
    },
    header: {
      marginBottom: 20,
    },
    headerTitle: {
      color: c.text,
      fontWeight: "800",
      fontSize: 30,
      lineHeight: 36,
    },
    headerSubtitle: {
      marginTop: 6,
      fontSize: 14,
      lineHeight: 20,
    },
    section: {
      marginBottom: 20,
    },
    sectionHeaderRow: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "flex-end",
      marginBottom: 10,
    },
    sectionTitle: {
      fontSize: 13,
      fontWeight: "700",
      color: c.mutedForeground,
      textTransform: "uppercase",
      letterSpacing: 1,
      marginBottom: 10,
    },
    sectionTitleCompact: {
      marginBottom: 0,
    },
    summaryBadge: {
      backgroundColor: c.badgeBg,
      paddingHorizontal: 10,
      paddingVertical: 5,
      borderRadius: 999,
      borderWidth: 1,
      borderColor: c.badgeBorder,
    },
    summaryText: {
      fontSize: 11,
      color: c.badgeText,
      fontWeight: "600",
    },
    card: {
      backgroundColor: c.card,
      borderColor: c.cardBorder,
      borderRadius: 18,
      borderWidth: 1,
      overflow: "hidden",
    },
    cardContent: {
      padding: 18,
      paddingTop: 18, // Override CardContent default paddingTop: 0
    },
    statusCardContent: {
      padding: 0,
      paddingTop: 0,
    },
    statusRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      padding: 16,
    },
    statusInfo: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
    },
    statusTextContainer: {
      gap: 2,
    },
    statusLabel: {
      fontSize: 12,
      color: c.mutedForeground,
      fontWeight: "500",
    },
    statusValue: {
      fontSize: 15,
      fontWeight: "700",
    },
    statusDivider: {
      height: 1,
      backgroundColor: c.divider,
      marginHorizontal: 16,
    },
    settingRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 14,
    },
    settingInfo: {
      flex: 1,
      gap: 4,
    },
    settingLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: c.text,
    },
    settingDescription: {
      fontSize: 13,
      color: c.mutedForeground,
    },
    settingDivider: {
      height: 1,
      backgroundColor: c.divider,
      marginVertical: 16,
    },
    readOnlyContainer: {
      flexDirection: "row",
      alignItems: "center",
      backgroundColor: c.readOnlyBg,
      borderRadius: 10,
      paddingHorizontal: 10,
      paddingVertical: 10,
      marginTop: 6,
      gap: 8,
      borderWidth: 1,
      borderColor: c.readOnlyBorder,
    },
    readOnlyText: {
      flex: 1,
      fontSize: 13,
      color: c.readOnlyText,
      fontFamily: Platform.OS === "ios" ? "Menlo" : "monospace",
    },
    segmentedControl: {
      flexDirection: "row",
      backgroundColor: c.segmentBg,
      borderRadius: 14,
      padding: 4,
      marginBottom: 14,
    },
    themeSegmentedControl: {
      marginTop: 16,
      marginBottom: 12,
    },
    segment: {
      flex: 1,
      paddingVertical: 11,
      alignItems: "center",
      borderRadius: 10,
    },
    segmentActive: {
      backgroundColor: c.segmentActiveBg,
      shadowColor: "#000",
      shadowOffset: { width: 0, height: 2 },
      shadowOpacity: 0.2,
      shadowRadius: 4,
      elevation: 2,
    },
    segmentText: {
      fontSize: 14,
      fontWeight: "600",
      color: c.segmentText,
    },
    segmentTextActive: {
      color: c.segmentTextActive,
    },
    modeDescriptionContainer: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
      paddingHorizontal: 2,
    },
    modeDescription: {
      fontSize: 13,
      lineHeight: 18,
      color: c.mutedForeground,
      flex: 1,
    },
    trunkInputContainer: {
      marginTop: 20,
      gap: 8,
    },
    trunkHeaderRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    trunkStatusBadge: {
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 999,
      backgroundColor: c.badgeBg,
      borderWidth: 1,
      borderColor: c.badgeBorder,
    },
    trunkStatusText: {
      fontSize: 11,
      fontWeight: "700",
      color: c.badgeText,
      textTransform: "uppercase",
      letterSpacing: 0.4,
    },
    inputLabel: {
      fontSize: 13,
      fontWeight: "600",
      color: c.mutedForeground,
      marginBottom: 10,
    },
    input: {
      height: 52,
      backgroundColor: c.inputBg,
      borderRadius: 12,
      paddingHorizontal: 16,
      fontSize: 16,
      color: c.text,
      borderWidth: 1,
      borderColor: c.inputBorder,
    },
    readOnlyInput: {
      color: c.subText,
      opacity: 0.95,
    },
    trunkErrorText: {
      fontSize: 12,
      color: "#FCA5A5",
      lineHeight: 18,
      marginTop: 2,
    },
    resolveButton: {
      marginTop: 6,
      height: 40,
      borderRadius: 10,
      backgroundColor: c.resolveButtonBg,
      borderWidth: 1,
      borderColor: c.resolveButtonBorder,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
    },
    resolveButtonDisabled: {
      backgroundColor: c.inputBg,
      borderColor: c.inputBorder,
    },
    resolveButtonPressed: {
      opacity: 0.85,
    },
    resolveButtonText: {
      fontSize: 13,
      fontWeight: "700",
      color: c.resolveButtonText,
    },
    grid: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 12,
    },
    gridItem: {
      width: "48%",
      backgroundColor: c.inputBg,
      borderRadius: 12,
      paddingVertical: 14,
      paddingHorizontal: 10,
      borderWidth: 1,
      borderColor: c.inputBorder,
      alignItems: "center",
      justifyContent: "center",
      gap: 4,
    },
    gridItemTablet: {
      width: "31.5%",
    },
    gridItemActive: {
      borderColor: c.muted,
      backgroundColor: c.segmentBg,
    },
    gridItemLabel: {
      fontSize: 14,
      fontWeight: "700",
      color: c.mutedForeground,
    },
    gridItemLabelActive: {
      color: c.text,
    },
    gridItemSub: {
      fontSize: 11,
      color: c.muted,
    },
    gridItemSubActive: {
      color: c.subText,
    },
    frameRateRow: {
      flexDirection: "row",
      gap: 10,
    },
    frameRateItem: {
      flex: 1,
      backgroundColor: c.inputBg,
      borderRadius: 12,
      paddingVertical: 14,
      borderWidth: 1,
      borderColor: c.inputBorder,
      alignItems: "center",
      justifyContent: "center",
    },
    themeDescriptionList: {
      gap: 8,
    },
    themeDescriptionRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
    },
    themeDescriptionBullet: {
      width: 8,
      height: 8,
      borderRadius: 999,
      backgroundColor: c.muted,
    },
    themeDescriptionText: {
      flex: 1,
      fontSize: 13,
      color: c.mutedForeground,
    },
  });
