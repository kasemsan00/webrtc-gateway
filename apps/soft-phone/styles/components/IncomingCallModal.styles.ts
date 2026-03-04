import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  overlay: {
    flex: 1,
    backgroundColor: "rgba(0, 0, 0, 0.8)",
    justifyContent: "center",
    alignItems: "center",
  },
  container: {
    width: "90%",
    maxWidth: 400,
    backgroundColor: "#1C1C1E",
    borderRadius: 32,
    overflow: "hidden",
  },
  animatedBg: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    height: 200,
    backgroundColor: "#48484A",
    opacity: 0.2,
    borderBottomLeftRadius: 100,
    borderBottomRightRadius: 100,
  },
  content: {
    padding: 32,
    alignItems: "center",
  },
  title: {
    fontSize: 16,
    fontWeight: "600",
    color: "#8E8E93",
    textTransform: "uppercase",
    letterSpacing: 2,
    marginBottom: 24,
  },
  callerSection: {
    alignItems: "center",
    marginBottom: 40,
  },
  avatarRing: {
    padding: 4,
    borderRadius: 60,
    borderWidth: 3,
    borderColor: "#48484A",
    marginBottom: 20,
  },
  avatarText: {
    fontSize: 32,
    fontWeight: "700",
    color: "#fff",
  },
  callerName: {
    fontSize: 26,
    fontWeight: "700",
    color: "#F2F2F7",
    textAlign: "center",
  },
  callerNumber: {
    fontSize: 16,
    color: "#8E8E93",
    marginTop: 8,
  },
  actions: {
    flexDirection: "row",
    justifyContent: "space-around",
    width: "100%",
    paddingHorizontal: 20,
  },
  actionWrapper: {
    alignItems: "center",
    gap: 12,
  },
  actionButton: {
    width: 72,
    height: 72,
    borderRadius: 36,
    justifyContent: "center",
    alignItems: "center",
  },
  declineButton: {
    backgroundColor: "#EF4444",
  },
  declineButtonPressed: {
    backgroundColor: "#DC2626",
    transform: [{ scale: 0.95 }],
  },
  answerButton: {
    backgroundColor: "#10B981",
  },
  answerButtonPressed: {
    backgroundColor: "#059669",
    transform: [{ scale: 0.95 }],
  },
  actionLabel: {
    fontSize: 14,
    fontWeight: "600",
    color: "#8E8E93",
  },
});
