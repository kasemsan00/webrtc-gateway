import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Keyboard, Modal, Platform, Pressable, ScrollView, View } from "react-native";

import { Image } from "expo-image";
import { MessageCircleMore, Volume1Icon, Volume2Icon } from "lucide-react-native";
import Animated, {
  cancelAnimation,
  Easing,
  FadeIn,
  FadeInDown,
  FadeInUp,
  FadeOut,
  FadeOutDown,
  interpolate,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { MediaStream } from "react-native-webrtc";

import { useRtt } from "@/hooks/use-rtt";
import { CallState } from "@/lib/gateway";
import { resolveMobileNetworkCode, sendLocationToLis } from "@/lib/location/save-location";
import { ms, vs } from "@/lib/scale";
import { useLocationStore } from "@/store/location-store";
import { useSipStore } from "@/store/sip-store";
import { styles } from "@/styles/components/in-call-screen.styles";

import { AppIcon } from "../ui/icon";
import { Text } from "../ui/text";
import { Chat } from "./chat";
import { DtmfKeypad } from "./dtmf-keypad";
import { LocationPickerModal } from "./location-picker-modal";

interface InCallScreenProps {
  visible: boolean;
  userMobileNumber: string;
  phoneNumber: string;
  contactName?: string;
  callState: CallState;
  duration: number;
  isMuted: boolean;
  isSpeaker: boolean;
  isVideoEnabled: boolean;
  cameraFacing: "front" | "back";
  localStream?: MediaStream | null;
  remoteStream?: MediaStream | null;
  networkReconnecting?: boolean;
  autoRecoveryMessage?: string | null;
  onMuteToggle: () => void;
  onSpeakerToggle: () => void;
  onVideoToggle: () => void;
  onSwitchCamera: () => void;
  onHangup: () => void;
}

type StableRTCViewProps = {
  streamURL: string;
  style?: any;
  objectFit?: "contain" | "cover";
  zOrder?: number;
  mirror?: boolean;
};

const NativeRTCView =
  Platform.OS === "web"
    ? null
    : (() => {
        try {
          return require("react-native-webrtc").RTCView as React.ComponentType<StableRTCViewProps>;
        } catch {
          return null;
        }
      })();

const StableRTCView = React.memo(function StableRTCView(props: StableRTCViewProps) {
  if (!NativeRTCView) return null;
  return <NativeRTCView {...props} />;
});

function formatDuration(seconds: number): string {
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins.toString().padStart(2, "0")}:${secs.toString().padStart(2, "0")}`;
}

export function InCallScreen({
  visible,
  userMobileNumber,
  phoneNumber,
  contactName,
  callState,
  duration,
  isMuted,
  isSpeaker,
  isVideoEnabled,
  cameraFacing,
  localStream,
  remoteStream,
  networkReconnecting = false,
  autoRecoveryMessage = null,
  onMuteToggle,
  onSpeakerToggle,
  onVideoToggle,
  onSwitchCamera,
  onHangup,
}: InCallScreenProps) {
  const CHAT_FONT_MIN = 13;
  const CHAT_FONT_MAX = 24;
  const CHAT_FONT_STEP = 1;
  const CHAT_FONT_DEFAULT = 15;
  const CONTROL_VISIBILITY_DURATION = 240;

  const insets = useSafeAreaInsets();
  const currentLocation = useLocationStore((s) => s.currentLocation);

  const [showChat, setShowChat] = useState(false);
  const [showKeypad, setShowKeypad] = useState(false);
  const [showLocationPicker, setShowLocationPicker] = useState(false);
  const [chatMessageFontSize, setChatMessageFontSize] = useState(CHAT_FONT_DEFAULT);
  const [messageText, setMessageText] = useState("");
  const controlsVisibility = useSharedValue(1);
  const reconnectingDotOpacity = useSharedValue(1);
  const [pinnedRemoteStreamUrl, setPinnedRemoteStreamUrl] = useState<string | null>(null);
  const lastLoggedIncomingMessageIdRef = useRef<string | null>(null);
  const lastLoggedRemoteRttTextRef = useRef<string>("");
  const scrollViewRef = useRef<ScrollView>(null);
  const rtt = useRtt({ enabled: visible });
  const rttState = rtt.state;
  const clearRemoteRtt = rtt.clearRemote;
  const setRttLocalText = rtt.setLocalText;
  const resetRttLocal = rtt.resetLocal;
  const locationText = useMemo(() => currentLocation?.address ?? "ยังไม่ได้เลือกสถานที่", [currentLocation?.address]);

  // Get message state and actions from store
  const { messages, unreadMessageCount, sendMessage: storeSendMessage, markMessagesAsRead, sendDtmf } = useSipStore();

  // Force re-render when tracks change (needed for Janus where tracks are added to existing stream)
  const [remoteTrackCount, setRemoteTrackCount] = useState(0);
  const [localTrackCount, setLocalTrackCount] = useState(0);

  // Listen for track changes on remoteStream
  useEffect(() => {
    if (!remoteStream) {
      setRemoteTrackCount(0);
      return;
    }

    // Update track count immediately
    setRemoteTrackCount(remoteStream.getTracks().length);

    // Listen for track added/removed
    const handleTrackAdded = () => {
      console.log("[InCallScreen] Remote track added, forcing re-render");
      setRemoteTrackCount(remoteStream.getTracks().length);
    };
    const handleTrackRemoved = () => {
      console.log("[InCallScreen] Remote track removed, forcing re-render");
      setRemoteTrackCount(remoteStream.getTracks().length);
    };

    // @ts-ignore - React Native WebRTC event handling
    remoteStream.addEventListener?.("addtrack", handleTrackAdded);
    // @ts-ignore
    remoteStream.addEventListener?.("removetrack", handleTrackRemoved);

    return () => {
      // @ts-ignore
      remoteStream.removeEventListener?.("addtrack", handleTrackAdded);
      // @ts-ignore
      remoteStream.removeEventListener?.("removetrack", handleTrackRemoved);
    };
  }, [remoteStream]);

  useEffect(() => {
    if (!remoteStream) return;

    const videoTracks = remoteStream.getVideoTracks();
    const handleRemoteVideoEvent = () => {
      console.log("[InCallScreen] Remote video event");
    };

    videoTracks.forEach((track) => {
      const trackWithEvents = track as unknown as {
        addEventListener?: (type: string, listener: () => void) => void;
        removeEventListener?: (type: string, listener: () => void) => void;
      };
      trackWithEvents.addEventListener?.("mute", handleRemoteVideoEvent);
      trackWithEvents.addEventListener?.("unmute", handleRemoteVideoEvent);
      trackWithEvents.addEventListener?.("ended", handleRemoteVideoEvent);
    });

    return () => {
      videoTracks.forEach((track) => {
        const trackWithEvents = track as unknown as {
          removeEventListener?: (type: string, listener: () => void) => void;
        };
        trackWithEvents.removeEventListener?.("mute", handleRemoteVideoEvent);
        trackWithEvents.removeEventListener?.("unmute", handleRemoteVideoEvent);
        trackWithEvents.removeEventListener?.("ended", handleRemoteVideoEvent);
      });
    };
  }, [remoteStream, remoteTrackCount]);

  // Listen for track changes on localStream
  useEffect(() => {
    if (!localStream) {
      setLocalTrackCount(0);
      return;
    }

    setLocalTrackCount(localStream.getTracks().length);

    const handleTrackAdded = () => {
      setLocalTrackCount(localStream.getTracks().length);
    };
    const handleTrackRemoved = () => {
      setLocalTrackCount(localStream.getTracks().length);
    };

    // @ts-ignore
    localStream.addEventListener?.("addtrack", handleTrackAdded);
    // @ts-ignore
    localStream.addEventListener?.("removetrack", handleTrackRemoved);

    return () => {
      // @ts-ignore
      localStream.removeEventListener?.("addtrack", handleTrackAdded);
      // @ts-ignore
      localStream.removeEventListener?.("removetrack", handleTrackRemoved);
    };
  }, [localStream]);

  // Compute video availability using track counts to trigger re-render when they change
  const hasRemoteVideo = remoteStream && remoteStream.getVideoTracks().length > 0;
  const hasLocalVideo = localStream && localStream.getVideoTracks().length > 0;
  const remoteVideoTrack = remoteStream?.getVideoTracks?.()[0];

  // Keep remote key stable per stream identity to avoid RTCView remount flicker
  const remoteStreamUrl = remoteStream?.toURL?.();

  useEffect(() => {
    if (remoteStreamUrl) {
      setPinnedRemoteStreamUrl(remoteStreamUrl);
    }
  }, [remoteStreamUrl]);

  useEffect(() => {
    if (!visible) {
      setPinnedRemoteStreamUrl(null);
    }
  }, [visible]);

  // Debug logging
  useEffect(() => {
    const remoteURL = remoteStream?.toURL?.();
    const videoTracks = remoteStream?.getVideoTracks?.() || [];
    console.log("[InCallScreen] Video state:", {
      remoteStream: !!remoteStream,
      remoteStreamId: remoteStream?.id,
      remoteURL,
      remoteTrackCount,
      hasRemoteVideo,
      videoTrackDetails: videoTracks.map((t: any) => ({
        id: t.id,
        enabled: t.enabled,
        muted: t.muted,
        readyState: t.readyState,
      })),
      localStream: !!localStream,
      localTrackCount,
      hasLocalVideo,
    });
  }, [remoteStream, remoteTrackCount, hasRemoteVideo, localStream, localTrackCount, hasLocalVideo]);

  // No filtering needed - just show all messages during call

  // Debug: log messages when they change
  useEffect(() => {
    console.log("[InCallScreen] Messages updated:", messages.length, messages);
  }, [messages]);

  // Log latest incoming chat text for quick debugging in console
  useEffect(() => {
    const latestIncomingMessage = [...messages].reverse().find((message) => message.direction === "incoming");
    if (!latestIncomingMessage) {
      return;
    }

    if (latestIncomingMessage.id !== lastLoggedIncomingMessageIdRef.current) {
      console.log("[InCallScreen] Incoming chat text:", latestIncomingMessage.body);
      lastLoggedIncomingMessageIdRef.current = latestIncomingMessage.id;
      clearRemoteRtt();
    }
  }, [clearRemoteRtt, messages]);

  // Log incoming RTT text when remote text changes
  useEffect(() => {
    const incomingRttText = rttState.remoteText.trim();
    if (!incomingRttText) {
      lastLoggedRemoteRttTextRef.current = "";
      return;
    }

    if (incomingRttText !== lastLoggedRemoteRttTextRef.current) {
      console.log("[InCallScreen] Incoming RTT text:", incomingRttText);
      lastLoggedRemoteRttTextRef.current = incomingRttText;
    }
  }, [rttState.remoteText]);

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    if (showChat && messages.length > 0) {
      setTimeout(() => {
        scrollViewRef.current?.scrollToEnd({ animated: true });
      }, 100);
    }
  }, [messages.length, showChat]);

  // Animate controls when chat visibility changes
  useEffect(() => {
    controlsVisibility.value = withTiming(showChat ? 0 : 1, {
      duration: CONTROL_VISIBILITY_DURATION,
      easing: Easing.out(Easing.cubic),
    });
  }, [controlsVisibility, showChat]);

  useEffect(() => {
    if (networkReconnecting) {
      reconnectingDotOpacity.value = withRepeat(
        withTiming(0.2, {
          duration: 500,
          easing: Easing.inOut(Easing.ease),
        }),
        -1,
        true,
      );
      return;
    }

    cancelAnimation(reconnectingDotOpacity);
    reconnectingDotOpacity.value = withTiming(1, { duration: 180 });
  }, [networkReconnecting, reconnectingDotOpacity]);

  // Keep unread counter in sync while chat is visible
  useEffect(() => {
    if (showChat && unreadMessageCount > 0) {
      markMessagesAsRead();
    }
  }, [markMessagesAsRead, showChat, unreadMessageCount]);

  const handleMessageTextChange = useCallback(
    (text: string) => {
      setMessageText(text);
      setRttLocalText(text);
    },
    [setRttLocalText],
  );

  const handleSendMessage = useCallback(() => {
    if (!messageText.trim()) return;

    storeSendMessage(messageText.trim());
    resetRttLocal();
    setMessageText("");
    Keyboard.dismiss();
  }, [messageText, resetRttLocal, storeSendMessage]);

  const canDecreaseChatFontSize = chatMessageFontSize > CHAT_FONT_MIN;
  const canIncreaseChatFontSize = chatMessageFontSize < CHAT_FONT_MAX;

  const handleDecreaseChatFontSize = useCallback(() => {
    setChatMessageFontSize((prev) => Math.max(CHAT_FONT_MIN, prev - CHAT_FONT_STEP));
  }, []);

  const handleIncreaseChatFontSize = useCallback(() => {
    setChatMessageFontSize((prev) => Math.min(CHAT_FONT_MAX, prev + CHAT_FONT_STEP));
  }, []);

  const handleSendLocationToChat = useCallback(() => {
    const coordinates = currentLocation?.coordinates;
    if (!coordinates) {
      console.warn("[InCallScreen] Cannot send location - no coordinates selected");
      return;
    }

    const locationMessage = `ตำแหน่งปัจจุบัน: ${coordinates.latitude},${coordinates.longitude}`;
    storeSendMessage(locationMessage);

    const trimmedUserMobileNumber = userMobileNumber.trim();
    if (!trimmedUserMobileNumber) {
      console.warn("[InCallScreen] Skip save location API - empty user mobile number");
      return;
    }

    void (async () => {
      const network = await resolveMobileNetworkCode();
      const src = Platform.OS === "android" ? "android" : "ios";

      await sendLocationToLis({
        src,
        userid: trimmedUserMobileNumber,
        network,
        lat: coordinates.latitude,
        long: coordinates.longitude,
      });
    })();
  }, [currentLocation?.coordinates, storeSendMessage, userMobileNumber]);

  const toggleChat = useCallback(() => {
    setShowChat((prev) => {
      if (prev && unreadMessageCount > 0) {
        markMessagesAsRead();
      }
      return !prev;
    });
  }, [markMessagesAsRead, unreadMessageCount]);

  const controlsAnimatedStyle = useAnimatedStyle(() => {
    return {
      opacity: controlsVisibility.value,
      transform: [{ translateY: interpolate(controlsVisibility.value, [0, 1], [10, 0]) }],
    };
  });

  const reconnectingDotAnimatedStyle = useAnimatedStyle(() => {
    return {
      opacity: reconnectingDotOpacity.value,
    };
  });

  return (
    <Modal visible={visible} animationType="none" presentationStyle="fullScreen" transparent={false}>
      <Animated.View entering={FadeIn.duration(300)} exiting={FadeOut.duration(250)} style={styles.container}>
        <View style={[styles.locationBar, { paddingTop: insets.top + 4 }]}>
          <Pressable style={styles.locationButton} disabled>
            <Image source={require("@/assets/images/drawable-xhdpi/ic_location_statusbar.png")} contentFit="contain" style={styles.locationIcon} />
            <Text numberOfLines={1} style={styles.locationText}>
              {locationText}
            </Text>
          </Pressable>
        </View>

        {showChat && (
          <Animated.View
            entering={FadeInUp.duration(CONTROL_VISIBILITY_DURATION).easing(Easing.out(Easing.cubic))}
            exiting={FadeOutDown.duration(180)}
            style={[styles.chatFontBar, { top: insets.top + vs(34) }]}
          >
            <View style={styles.chatFontControls}>
              <Pressable style={styles.chatFontButton} onPress={handleSendLocationToChat}>
                <AppIcon name="mapPin" size={ms(24)} color="#fff" />
              </Pressable>
              <Pressable
                style={[styles.chatFontButton, !canDecreaseChatFontSize && styles.chatFontButtonDisabled]}
                onPress={handleDecreaseChatFontSize}
                disabled={!canDecreaseChatFontSize}
              >
                <Image source={require("@/assets/images/drawable-xhdpi/ic_lower_size.png")} contentFit="contain" style={styles.chatFontButtonImage} />
              </Pressable>
              <Pressable
                style={[styles.chatFontButton, !canIncreaseChatFontSize && styles.chatFontButtonDisabled]}
                onPress={handleIncreaseChatFontSize}
                disabled={!canIncreaseChatFontSize}
              >
                <Image source={require("@/assets/images/drawable-xhdpi/ic_upper_size.png")} contentFit="contain" style={styles.chatFontButtonImage} />
              </Pressable>
            </View>
          </Animated.View>
        )}

        {/* Remote Video */}
        {pinnedRemoteStreamUrl ? (
          <View style={styles.remoteVideoContainer}>
            {(() => {
              const streamUrl = pinnedRemoteStreamUrl;
              console.log("[InCallScreen] 🎬 Rendering RTCView with streamURL:", streamUrl);

              console.log("[InCallScreen] 🎬 Video track state:", {
                enabled: remoteVideoTrack?.enabled,
                muted: remoteVideoTrack?.muted,
                readyState: remoteVideoTrack?.readyState,
              });
              return <StableRTCView streamURL={streamUrl} style={styles.remoteVideo} objectFit="contain" zOrder={1} mirror={false} />;
            })()}
          </View>
        ) : null}

        {/* Local Video (Picture-in-Picture) */}
        {hasLocalVideo && (
          <View style={[styles.localVideoContainer, { top: insets.top + vs(46) }]}>
            <StableRTCView streamURL={localStream.toURL()} style={styles.localVideo} objectFit="cover" zOrder={2} mirror={true} />
          </View>
        )}

        <Animated.View
          style={[styles.controlsWrapper, controlsAnimatedStyle]}
          pointerEvents={showChat ? "none" : "auto"}
          accessibilityElementsHidden={showChat}
          importantForAccessibility={showChat ? "no-hide-descendants" : "auto"}
        >
          {/* Switch Camera Button (Top Right) */}
          <Pressable style={[styles.switchCameraButton, { top: insets.top + vs(46) }]} onPress={onSwitchCamera}>
            <Image
              source={
                cameraFacing === "front"
                  ? require("@/assets/images/drawable-hdpi/btn_camera_switch_front.png")
                  : require("@/assets/images/drawable-hdpi/btn_camera_switch_back.png")
              }
              contentFit="contain"
              style={styles.switchCameraButtonImage}
            />
          </Pressable>

          {/* Overlay Info (Contact name, duration) */}
          <View style={[styles.overlayInfo, { top: insets.top + vs(48) }]}>
            <Text style={styles.contactName}>TTRS VRI</Text>
            {contactName && <Text style={styles.phoneNumber}>{phoneNumber}</Text>}

            <View style={styles.statusContainer}>
              {networkReconnecting ? <Animated.View style={[styles.reconnectingDot, reconnectingDotAnimatedStyle]} /> : <View style={styles.statusDot} />}
              <Text style={styles.duration}>{formatDuration(duration)}</Text>
              {autoRecoveryMessage ? <Text style={styles.autoRecoveryText}>{autoRecoveryMessage}</Text> : null}
            </View>
          </View>

          {/* Action Buttons */}

          <View style={styles.actionsContainer}>
            {/* Send Location */}
            <Pressable style={styles.actionButton} onPress={handleSendLocationToChat}>
              <View style={[styles.iconPlaceholder]}>
                <AppIcon name="mapPin" size={ms(34)} color="#fff" />
              </View>
            </Pressable>
            {/* Video Toggle */}
            <Pressable style={styles.actionButton} onPress={onVideoToggle}>
              <View style={[styles.iconPlaceholder, !isVideoEnabled && styles.iconPlaceholderActive]}>
                <AppIcon name={isVideoEnabled ? "video" : "videoOff"} size={ms(34)} color="#fff" />
              </View>
            </Pressable>

            {/* Mute Toggle */}
            <Pressable style={styles.actionButton} onPress={onMuteToggle}>
              <View style={[styles.iconPlaceholder, isMuted && styles.iconPlaceholderActive]}>
                <AppIcon name={isMuted ? "micOff" : "mic"} size={ms(34)} color="#fff" />
              </View>
            </Pressable>

            {/* Speaker Toggle */}
            <Pressable style={styles.actionButton} onPress={onSpeakerToggle}>
              <View style={[styles.iconPlaceholder, isSpeaker && styles.iconPlaceholderActive]}>
                {isSpeaker ? <Volume2Icon size={ms(34)} color="#fff" /> : <Volume1Icon size={ms(34)} color="#fff" />}
              </View>
            </Pressable>

            {/* Chat */}
            <Pressable style={styles.actionButton} onPress={toggleChat}>
              <View style={styles.iconPlaceholder}>
                <MessageCircleMore size={ms(34)} color="#fff" />
                {unreadMessageCount > 0 && (
                  <View style={styles.unreadBadge}>
                    <Text style={styles.unreadBadgeText}>{unreadMessageCount > 9 ? "9+" : unreadMessageCount}</Text>
                  </View>
                )}
              </View>
            </Pressable>
          </View>

          {/* Hangup Button */}
          <Pressable style={({ pressed }) => [styles.hangupContainer, pressed && styles.hangupButtonPressed]} onPress={onHangup}>
            <Image source={require("@/assets/images/drawable-xhdpi/ic_hang_up.png")} contentFit="contain" style={styles.hangupIcon} />
          </Pressable>
        </Animated.View>

        {/* Chat Overlay */}
        {showChat && (
          <Animated.View
            entering={FadeInDown.duration(CONTROL_VISIBILITY_DURATION).easing(Easing.out(Easing.cubic))}
            exiting={FadeOutDown.duration(180)}
            style={styles.chatOverlayWrapper}
          >
            <Chat
              messages={messages}
              messageText={messageText}
              setMessageText={handleMessageTextChange}
              handleSendMessage={handleSendMessage}
              toggleChat={toggleChat}
              messageFontSize={chatMessageFontSize}
              rttState={rttState}
            />
          </Animated.View>
        )}

        {/* DTMF Keypad Overlay */}
        <DtmfKeypad visible={showKeypad} onClose={() => setShowKeypad(false)} onDigitPress={sendDtmf} />
      </Animated.View>
      <LocationPickerModal
        visible={showLocationPicker}
        onClose={() => setShowLocationPicker(false)}
        topOffset={insets.top + 30}
        presentation={Platform.OS === "android" ? "inline-overlay" : "native-modal"}
      />
    </Modal>
  );
}
