import { useEffect, useRef } from "react";
import { KeyboardAvoidingView, Platform, Pressable, ScrollView, Text, TextInput, View } from "react-native";

import type { RttState } from "@/lib/rtt/rtt-reducer";
import { styles } from "@/styles/components/chat.styles";
import { AppIcon } from "../ui/icon";
import { LiveTextViewer } from "./live-text-viewer";

interface Message {
  id: string;
  body: string;
  direction: "incoming" | "outgoing";
  timestamp: number;
  status?: "sending" | "sent" | "failed";
}

interface ChatProps {
  messages: Message[];
  messageText: string;
  setMessageText: (text: string) => void;
  handleSendMessage: () => void;
  toggleChat: () => void;
  messageFontSize: number;
  rttState: RttState;
}

export function Chat({ messages, messageText, setMessageText, handleSendMessage, toggleChat, messageFontSize, rttState }: ChatProps) {
  const scrollViewRef = useRef<ScrollView>(null);
  const messageLineHeight = Math.round(messageFontSize * 1.33);

  useEffect(() => {
    scrollViewRef.current?.scrollToEnd({ animated: true });
  }, [messages]);

  return (
    <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : "height"} style={styles.chatOverlay} keyboardVerticalOffset={0}>
      <View style={[styles.chatMessagesFloating, messages.length === 0 && { backgroundColor: "transparent" }]}>
        <ScrollView ref={scrollViewRef} style={styles.chatMessages} contentContainerStyle={styles.chatMessagesContent}>
          {messages.length === 0 ? (
            <View style={styles.emptyChat}></View>
          ) : (
            <View style={styles.emptyChat}>
              {messages.map((msg) => (
                <View key={msg.id} style={[styles.messageBubble, msg.direction === "outgoing" ? styles.messageBubbleOut : styles.messageBubbleIn]}>
                  <Text
                    style={[
                      styles.messageText,
                      { fontSize: messageFontSize, lineHeight: messageLineHeight },
                      msg.direction === "outgoing" ? styles.messageTextOut : undefined,
                    ]}
                  >
                    {msg.body}
                  </Text>
                  <View style={styles.messageFooter}></View>
                </View>
              ))}
            </View>
          )}
        </ScrollView>
        {(rttState.remoteText.trim().length > 0 || rttState.isRemoteTyping) && (
          <View style={styles.rttContainer}>
            <LiveTextViewer text={rttState.remoteText} isTyping={rttState.isRemoteTyping} />
          </View>
        )}
      </View>

      <View style={styles.chatInputContainer}>
        <Pressable onPress={toggleChat} style={styles.chatCloseButton}>
          <AppIcon name="x" size={24} color="rgba(30, 88, 169)" />
        </Pressable>
        <TextInput
          style={styles.chatInput}
          placeholder="พิมพ์ข้อความ..."
          placeholderTextColor="rgba(104, 104, 104, 0.5)"
          value={messageText}
          onChangeText={setMessageText}
          multiline
          maxLength={500}
          returnKeyType="send"
          onSubmitEditing={handleSendMessage}
          blurOnSubmit={false}
        />
        <Pressable
          style={[styles.sendButton, !messageText.trim() && styles.sendButtonDisabled]}
          onPress={handleSendMessage}
          disabled={!messageText.trim()}
        >
          <Text style={styles.sendButtonText}>ส่ง</Text>
        </Pressable>
      </View>
    </KeyboardAvoidingView>
  );
}
