import { StyleSheet } from "react-native";

import { ms, mvs, s, vs } from "@/lib/scale";

export const styles = StyleSheet.create({
  overlay: {
    flex: 1,
    backgroundColor: "rgba(0, 0, 0, 0.75)",
    justifyContent: "center",
    alignItems: "center",
    padding: s(20),
  },
  modal: {
    backgroundColor: "#1E293B",
    borderRadius: s(20),
    padding: s(24),
    width: "100%",
    maxWidth: s(400),
    borderWidth: 1,
    borderColor: "rgba(239, 68, 68, 0.3)",
  },
  iconContainer: {
    alignItems: "center",
    marginBottom: mvs(16),
  },
  title: {
    fontSize: ms(22),
    fontWeight: "700",
    color: "#F1F5F9",
    textAlign: "center",
    marginBottom: mvs(12),
  },
  message: {
    fontSize: ms(15),
    color: "#94A3B8",
    textAlign: "center",
    marginBottom: mvs(16),
    lineHeight: ms(22),
  },
  maxRetriesText: {
    fontSize: ms(13),
    color: "#F59E0B",
    textAlign: "center",
    marginBottom: mvs(16),
    fontWeight: "600",
    lineHeight: ms(20),
  },
  retryCount: {
    fontSize: ms(13),
    color: "#64748B",
    textAlign: "center",
    marginBottom: mvs(20),
  },
  buttonContainer: {
    gap: mvs(12),
  },
  button: {
    height: vs(48),
    borderRadius: s(12),
    justifyContent: "center",
    alignItems: "center",
    flexDirection: "row",
    gap: s(8),
  },
  primaryButton: {
    backgroundColor: "#6366F1",
  },
  secondaryButton: {
    backgroundColor: "#10B981",
  },
  cancelButton: {
    backgroundColor: "transparent",
    borderWidth: 1,
    borderColor: "#475569",
  },
  buttonPressed: {
    opacity: 0.8,
    transform: [{ scale: 0.98 }],
  },
  primaryButtonText: {
    fontSize: ms(15),
    fontWeight: "600",
    color: "#fff",
  },
  secondaryButtonText: {
    fontSize: ms(15),
    fontWeight: "600",
    color: "#fff",
  },
  cancelButtonText: {
    fontSize: ms(15),
    fontWeight: "600",
    color: "#94A3B8",
  },
});
