import { styles } from "@/styles/components/Chat.styles";
import { Send, Square } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
import { Animated, Easing, KeyboardAvoidingView, Platform, Pressable, ScrollView, Text, TextInput, View } from "react-native";
import { AppIcon } from "../ui/icon";

const AnimatedKeyboardAvoidingView = Animated.createAnimatedComponent(KeyboardAvoidingView);
const ANIMATION_DURATION = 220;

interface Message {
  id: string;
  body: string;
  direction: "incoming" | "outgoing";
  timestamp: number;
  status?: "sending" | "sent" | "failed";
}

interface ChatProps {
  visible: boolean;
  contactName?: string;
  phoneNumber: string;
  messages: Message[];
  messageText: string;
  setMessageText: (text: string) => void;
  handleSendMessage: () => void;
  toggleChat: () => void;
}

export function Chat({ visible, contactName, phoneNumber, messages, messageText, setMessageText, handleSendMessage, toggleChat }: ChatProps) {
  const [isMounted, setIsMounted] = useState(visible);
  const animation = useRef(new Animated.Value(visible ? 1 : 0)).current;
  const scrollViewRef = useRef<ScrollView>(null);

  useEffect(() => {
    if (visible) {
      setIsMounted(true);
      Animated.timing(animation, {
        toValue: 1,
        duration: ANIMATION_DURATION,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: true,
      }).start();
    } else {
      Animated.timing(animation, {
        toValue: 0,
        duration: ANIMATION_DURATION,
        easing: Easing.in(Easing.cubic),
        useNativeDriver: true,
      }).start(({ finished }) => {
        if (finished) {
          setIsMounted(false);
        }
      });
    }
  }, [animation, visible]);

  if (!isMounted) {
    return null;
  }

  const overlayStyle = {
    opacity: animation,
  };

  const containerStyle = {
    transform: [
      {
        translateY: animation.interpolate({
          inputRange: [0, 1],
          outputRange: [60, 0],
        }),
      },
    ],
  };

  return (
    <AnimatedKeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : "height"}
      style={[styles.chatOverlay, overlayStyle]}
      keyboardVerticalOffset={0}
      pointerEvents={visible ? "auto" : "none"}
    >
      <Pressable style={styles.backdrop} onPress={toggleChat} />
      <Animated.View style={[styles.chatContainer, containerStyle]}>
        <View style={styles.chatHeader}>
          <Text style={styles.chatTitle}>Chat with {contactName || phoneNumber}</Text>
          <Pressable onPress={toggleChat} style={styles.chatCloseButton}>
            <AppIcon name="x" size={24} color="#fff" />
          </Pressable>
        </View>

        <ScrollView ref={scrollViewRef} style={styles.chatMessages} contentContainerStyle={styles.chatMessagesContent}>
          {messages.length === 0 ? (
            <View style={styles.emptyChat}>
              <Square size={48} color="rgba(255,255,255,0.3)" />
              <Text style={styles.emptyChatText}>No messages yet</Text>
              <Text style={styles.emptyChatSubtext}>Send a message to start chatting</Text>
            </View>
          ) : (
            messages.map((msg) => (
              <View key={msg.id} style={[styles.messageBubble, msg.direction === "outgoing" ? styles.messageBubbleOut : styles.messageBubbleIn]}>
                <Text style={styles.messageText}>{msg.body}</Text>
                <View style={styles.messageFooter}>
                  <Text style={styles.messageTime}>{new Date(msg.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</Text>
                  {msg.direction === "outgoing" && (
                    <Text style={styles.messageStatus}>{msg.status === "sending" ? "..." : msg.status === "sent" ? "✓" : "!"}</Text>
                  )}
                </View>
              </View>
            ))
          )}
        </ScrollView>

        <View style={styles.chatInputContainer}>
          <TextInput
            style={styles.chatInput}
            placeholder="Type a message..."
            placeholderTextColor="rgba(255,255,255,0.5)"
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
            <Send size={20} color="#fff" />
          </Pressable>
        </View>
      </Animated.View>
    </AnimatedKeyboardAvoidingView>
  );
}
