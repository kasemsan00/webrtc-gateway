import { StyleSheet } from "react-native";

import type { ThemeColors } from "@/theme";

export const createDialpadStyles = (c: ThemeColors) =>
  StyleSheet.create({
    container: {
      alignItems: "center",
      justifyContent: "center",
      width: "100%",
    },
    row: {
      flexDirection: "row",
      justifyContent: "center",
    },
    key: {
      backgroundColor: c.dialpadKey,
      justifyContent: "center",
      alignItems: "center",
    },
    keyPressed: {
      backgroundColor: c.dialpadKeyPressed,
      transform: [{ scale: 0.95 }],
    },
    keyMain: {
      fontWeight: "300",
      color: c.dialpadKeyText,
      includeFontPadding: false,
      textAlignVertical: "center",
    },
    keySub: {
      fontWeight: "600",
      color: c.dialpadKeySub,
      opacity: 0.8,
    },
  });
