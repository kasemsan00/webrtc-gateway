import { Platform, StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  chatOverlay: {
    position: "absolute",
    bottom: 0,
    left: 0,
    right: 0,
    top: 0,
    justifyContent: "flex-end",
    zIndex: 10,
  },
  chatMessagesFloating: {
    position: "absolute",
    top: 135,
    right: 2,
    width: "52%",
    maxHeight: "55%",
    backgroundColor: "rgba(75, 75, 75, 0.5)",
    borderRadius: 8,
    overflow: "hidden",
  },
  chatContainer: {
    backgroundColor: "#1E293B",
    height: "10%",
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
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  chatMessages: {
    flex: 1,
    maxHeight: 200,
  },
  chatMessagesContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 4,
    gap: 12,
  },
  rttContainer: {
    paddingHorizontal: 8,
    paddingTop: 4,
    paddingBottom: 8,
  },
  emptyChat: {
    alignItems: "center",
    justifyContent: "center",
    gap: 4,
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
  },
  messageBubbleOut: {
    alignSelf: "flex-end",
    alignItems: "flex-end",
  },
  messageTextOut: {
    textAlign: "right",
  },
  messageBubbleIn: {
    alignSelf: "flex-start",
  },
  messageText: {
    fontSize: 15,
    color: "#fff",
    lineHeight: 2,
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
    backgroundColor: "#FFFFFF",
    display: "flex",
    justifyContent: "center",
    flexDirection: "row",
    alignItems: "flex-start",
    paddingHorizontal: 16,
    paddingVertical: 12,
    // paddingBottom: 20,
    gap: 10,
    height: 100,
  },
  chatInput: {
    flex: 1,
    // backgroundColor: "#334155",
    borderWidth: 1,
    borderColor: "rgba(104, 104, 104, 0.5)",
    borderRadius: 4,
    paddingHorizontal: 16,
    paddingVertical: 10,
    color: "#000000",
    fontSize: 18,
    height: 44,
    maxHeight: 100,
  },
  sendButton: {
    width: 44,
    height: 44,
    justifyContent: "center",
    alignItems: "center",
  },
  sendButtonDisabled: {
    // opacity: 0.5,
  },
  sendButtonText: {
    fontSize: 18,
    color: "rgba(30, 88, 169)",
    fontWeight: "600",
  },
});
