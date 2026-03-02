import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    backgroundColor: "#0B1220",
    borderRadius: 12,
    padding: 12,
    gap: 8,
  },
  title: {
    fontSize: 12,
    color: "#94A3B8",
  },
  input: {
    minHeight: 72,
    color: "#E2E8F0",
    fontSize: 15,
    padding: 0,
  },
  resetButton: {
    alignSelf: "flex-end",
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderRadius: 8,
    backgroundColor: "#1E293B",
  },
  resetText: {
    color: "#E2E8F0",
    fontSize: 12,
  },
});
