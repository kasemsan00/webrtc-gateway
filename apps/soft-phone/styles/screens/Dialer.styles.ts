import { StyleSheet } from "react-native";

import type { ThemeColors } from "@/theme";
import { s } from "react-native-size-matters";

export const createDialerStyles = (c: ThemeColors) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: c.background,
    },
    header: {
      flexDirection: "row",
      justifyContent: "flex-end",
      alignItems: "center",
      paddingHorizontal: 24,
      paddingTop: 16,
      paddingBottom: 8,
    },
    statusIndicatorContainer: {
      position: "absolute",
      top: 40,
      right: 16,
      zIndex: 100,
      width: 24,
      height: 24,
      alignItems: "center",
      justifyContent: "center",
    },
    statusBadge: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 12,
      paddingVertical: 6,
      borderRadius: 16,
      gap: 8,
    },
    statusDot: {
      width: 8,
      height: 8,
      borderRadius: 4,
    },
    statusText: {
      fontSize: 12,
      fontWeight: "500",
    },
    numberDisplay: {
      height: 100, // Fixed height to prevent layout shift
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 32,
      marginBottom: 20,
    },
    numberRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      width: "100%",
    },
    phoneNumber: {
      fontSize: 36,
      fontWeight: "300",
      color: c.text,
      letterSpacing: 2,
      textAlign: "center",
    },
    phoneNumberTablet: {
      fontSize: s(24),
      letterSpacing: 3,
    },
    dialpadContainer: {
      flex: 1,
      justifyContent: "center",
      width: "100%",
    },
    callButtonContainer: {
      paddingBottom: 32,
      paddingTop: 16,
      alignItems: "center",
    },
    callButtonRow: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
    },
    callButton: {
      backgroundColor: "#10B981",
      justifyContent: "center",
      alignItems: "center",
    },
    callButtonDisabled: {
      backgroundColor: c.callButtonDisabled,
      shadowOpacity: 0,
    },
    callButtonPressed: {
      backgroundColor: "#059669",
      transform: [{ scale: 0.98 }],
    },
    backspaceButton: {
      justifyContent: "center",
      alignItems: "center",
    },
    backspaceButtonInline: {
      marginLeft: 12,
    },
    backspaceButtonHidden: {
      opacity: 0,
    },

    // Tablet Split Layout Styles
    splitContainer: {
      flex: 1,
      flexDirection: "row",
    },
    leftPanel: {
      flex: 1,
      justifyContent: "center",
      paddingRight: 32,
    },
    rightPanel: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
    },
    numberDisplayTabletLandscape: {
      height: "auto",
      flexDirection: "row",
      justifyContent: "flex-end",
      alignItems: "center",
      paddingHorizontal: 0,
      marginBottom: 0,
      marginLeft: 70,
    },
    actionRowLandscape: {
      flexDirection: "row",
      justifyContent: "flex-end", // Align backspace to right
      paddingRight: 24,
    },
  });
