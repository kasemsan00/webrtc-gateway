import { Platform, StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  chatOverlay: {
    position: "absolute",
    bottom: 0,
    left: 0,
    right: 0,
    top: 0,
    backgroundColor: "rgba(0,0,0,0.5)",
    justifyContent: "flex-end",
    zIndex: 10,
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
  },
  chatContainer: {
    backgroundColor: "#1C1C1E",
    height: "70%",
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    maxHeight: "70%",
    paddingBottom: Platform.OS === "ios" ? 20 : 0,
  },
  chatHeader: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    paddingHorizontal: 20,
    paddingVertical: 16,
    borderBottomWidth: 1,
    borderBottomColor: "rgba(255,255,255,0.1)",
  },
  chatTitle: {
    fontSize: 18,
    fontWeight: "600",
    color: "#fff",
  },
  chatCloseButton: {
    padding: 4,
  },
  chatMessages: {
    flex: 1,
    maxHeight: 400,
  },
  chatMessagesContent: {
    padding: 16,
    gap: 12,
  },
  emptyChat: {
    alignItems: "center",
    justifyContent: "center",
    paddingVertical: 60,
    gap: 12,
  },
  emptyChatText: {
    fontSize: 16,
    fontWeight: "600",
    color: "rgba(255,255,255,0.7)",
  },
  emptyChatSubtext: {
    fontSize: 14,
    color: "rgba(255,255,255,0.5)",
  },
  messageBubble: {
    maxWidth: "80%",
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: 16,
    gap: 4,
  },
  messageBubbleOut: {
    alignSelf: "flex-end",
    backgroundColor: "#48484A",
  },
  messageBubbleIn: {
    alignSelf: "flex-start",
    backgroundColor: "#2C2C2E",
  },
  messageText: {
    fontSize: 15,
    color: "#fff",
    lineHeight: 20,
  },
  messageFooter: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    marginTop: 2,
  },
  messageTime: {
    fontSize: 11,
    color: "rgba(255,255,255,0.6)",
  },
  messageStatus: {
    fontSize: 12,
  },
  chatInputContainer: {
    flexDirection: "row",
    alignItems: "flex-end",
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 12,
    borderTopWidth: 1,
    borderTopColor: "rgba(255,255,255,0.1)",
  },
  chatInput: {
    flex: 1,
    backgroundColor: "#2C2C2E",
    borderRadius: 20,
    paddingHorizontal: 16,
    paddingVertical: 10,
    color: "#fff",
    fontSize: 15,
    maxHeight: 100,
  },
  sendButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    backgroundColor: "#48484A",
    justifyContent: "center",
    alignItems: "center",
  },
  sendButtonDisabled: {
    backgroundColor: "#3A3A3C",
    opacity: 0.5,
  },
});
