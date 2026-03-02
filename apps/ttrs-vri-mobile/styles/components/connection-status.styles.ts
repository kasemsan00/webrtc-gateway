import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    zIndex: 1000,
  },
  statusRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    paddingVertical: 10,
    paddingHorizontal: 16,
    backgroundColor: "#2196F3",
    gap: 8,
  },
  reconnecting: {
    backgroundColor: "#FF9800",
  },
  disconnected: {
    backgroundColor: "#9E9E9E",
  },
  failed: {
    backgroundColor: "#F44336",
  },
  warning: {
    backgroundColor: "#FF9800",
  },
  pressedRow: {
    opacity: 0.8,
  },
  statusText: {
    color: "#FFFFFF",
    fontSize: 14,
    fontWeight: "500",
  },
});
