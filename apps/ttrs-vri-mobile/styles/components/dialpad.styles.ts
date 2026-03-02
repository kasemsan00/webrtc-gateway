import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
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
    backgroundColor: "rgba(51, 65, 85, 0.8)",
    justifyContent: "center",
    alignItems: "center",
  },
  keyPressed: {
    backgroundColor: "rgba(71, 85, 105, 0.9)",
    transform: [{ scale: 0.95 }],
  },
  keyMain: {
    fontWeight: "300",
    color: "#fff",
    includeFontPadding: false,
    textAlignVertical: "center",
  },
  keySub: {
    fontWeight: "600",
    color: "#94A3B8",
    opacity: 0.8,
  },
});
