import { ms } from '@/lib/scale';
import { clearPipRef, setPipRef } from '@/lib/pip';
import { CallState, useSipStore } from '@/store/sip-store';
import { styles } from '@/styles/components/InCallScreen.styles';
import { getPipEnabled } from '@/constants/webrtc';
import {
  MessageCircleMore,
  Volume1Icon,
  Volume2Icon,
} from 'lucide-react-native';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  AppState,
  type AppStateStatus,
  Keyboard,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  View,
} from 'react-native';
import { MediaStream, RTCView } from 'react-native-webrtc';
import { Avatar, AvatarFallback } from '../ui/avatar';
import { AppIcon } from '../ui/icon';
import { Text } from '../ui/text';
import { Chat } from './chat';
import { DtmfKeypad } from './dtmf-keypad';
import { LiveTextViewer } from './live-text-viewer';

interface InCallScreenProps {
  visible: boolean;
  phoneNumber: string;
  contactName?: string;
  callState: CallState;
  duration: number;
  isMuted: boolean;
  isSpeaker: boolean;
  isVideoEnabled: boolean;
  localStream?: MediaStream | null;
  remoteStream?: MediaStream | null;
  networkReconnecting?: boolean;
  onMuteToggle: () => void;
  onSpeakerToggle: () => void;
  onVideoToggle: () => void;
  onSwitchCamera: () => void;
  onHangup: () => void;
}

function formatDuration(seconds: number): string {
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
}

function getInitials(name?: string, phone?: string): string {
  if (name) {
    const parts = name.split(' ');
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.substring(0, 2).toUpperCase();
  }
  return phone?.substring(0, 2) || '??';
}

function getCallStatusText(callState: CallState): string | null {
  switch (callState) {
    case CallState.CONNECTING:
      return 'Connecting...';
    case CallState.CALLING:
      return 'Calling...';
    case CallState.RINGING:
      return 'Ringing...';
    default:
      return null;
  }
}

export function InCallScreen({
  visible,
  phoneNumber,
  contactName,
  callState,
  duration,
  isMuted,
  isSpeaker,
  isVideoEnabled,
  localStream,
  remoteStream,
  networkReconnecting = false,
  onMuteToggle,
  onSpeakerToggle,
  onVideoToggle,
  onSwitchCamera,
  onHangup,
}: InCallScreenProps) {
  const [showChat, setShowChat] = useState(false);
  const [showKeypad, setShowKeypad] = useState(false);
  const [messageText, setMessageText] = useState('');
  const [remoteVideoRenderVersion, setRemoteVideoRenderVersion] = useState(0);
  const scrollViewRef = useRef<ScrollView>(null);
  const remoteVideoRef = useRef<React.ElementRef<typeof RTCView>>(null);
  const appStateRef = useRef<AppStateStatus>(AppState.currentState);

  const handleSwitchCamera = useCallback(() => {
    onSwitchCamera();
  }, [onSwitchCamera]);

  // Get message state and actions from store (use individual selectors to avoid re-render on every store change)
  const messages = useSipStore((s) => s.messages);
  const unreadMessageCount = useSipStore((s) => s.unreadMessageCount);
  const remoteRttPreview = useSipStore((s) => s.remoteRttPreview);
  const storeSendMessage = useSipStore((s) => s.sendMessage);
  const updateOutgoingRttDraft = useSipStore((s) => s.updateOutgoingRttDraft);
  const clearRemoteRttPreview = useSipStore((s) => s.clearRemoteRttPreview);
  const markMessagesAsRead = useSipStore((s) => s.markMessagesAsRead);
  const sendDtmf = useSipStore((s) => s.sendDtmf);
  const refreshRemoteVideo = useSipStore((s) => s.refreshRemoteVideo);

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
      console.log('[InCallScreen] Remote track added, forcing re-render');
      setRemoteTrackCount(remoteStream.getTracks().length);
      setRemoteVideoRenderVersion((prev) => prev + 1);
    };
    const handleTrackRemoved = () => {
      console.log('[InCallScreen] Remote track removed, forcing re-render');
      setRemoteTrackCount(remoteStream.getTracks().length);
      setRemoteVideoRenderVersion((prev) => prev + 1);
    };

    // @ts-ignore - React Native WebRTC event handling
    remoteStream.addEventListener?.('addtrack', handleTrackAdded);
    // @ts-ignore
    remoteStream.addEventListener?.('removetrack', handleTrackRemoved);

    return () => {
      // @ts-ignore
      remoteStream.removeEventListener?.('addtrack', handleTrackAdded);
      // @ts-ignore
      remoteStream.removeEventListener?.('removetrack', handleTrackRemoved);
    };
  }, [remoteStream]);

  useEffect(() => {
    if (!remoteStream) return;

    const videoTracks = remoteStream.getVideoTracks();
    const handleRemoteVideoEvent = () => {
      setRemoteVideoRenderVersion((prev) => prev + 1);
    };

    videoTracks.forEach((track) => {
      const trackWithEvents = track as unknown as {
        addEventListener?: (type: string, listener: () => void) => void;
        removeEventListener?: (type: string, listener: () => void) => void;
      };
      trackWithEvents.addEventListener?.('mute', handleRemoteVideoEvent);
      trackWithEvents.addEventListener?.('unmute', handleRemoteVideoEvent);
      trackWithEvents.addEventListener?.('ended', handleRemoteVideoEvent);
    });

    return () => {
      videoTracks.forEach((track) => {
        const trackWithEvents = track as unknown as {
          removeEventListener?: (type: string, listener: () => void) => void;
        };
        trackWithEvents.removeEventListener?.('mute', handleRemoteVideoEvent);
        trackWithEvents.removeEventListener?.('unmute', handleRemoteVideoEvent);
        trackWithEvents.removeEventListener?.('ended', handleRemoteVideoEvent);
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
    localStream.addEventListener?.('addtrack', handleTrackAdded);
    // @ts-ignore
    localStream.addEventListener?.('removetrack', handleTrackRemoved);

    return () => {
      // @ts-ignore
      localStream.removeEventListener?.('addtrack', handleTrackAdded);
      // @ts-ignore
      localStream.removeEventListener?.('removetrack', handleTrackRemoved);
    };
  }, [localStream]);

  // Compute video availability using track counts to trigger re-render when they change
  const hasRemoteVideo =
    remoteStream && remoteStream.getVideoTracks().length > 0;
  const hasLocalVideo = localStream && localStream.getVideoTracks().length > 0;
  const remoteVideoTrack = remoteStream?.getVideoTracks?.()[0];
  const remoteStreamUrl = remoteStream?.toURL?.();
  const remoteStreamKeyBase = remoteStreamUrl
    ? `remote-${remoteStreamUrl}`
    : `remote-${remoteStream?.id || 'none'}`;
  const remoteStreamKey = `${remoteStreamKeyBase}-v${remoteVideoRenderVersion}`;

  // Register/clear PiP ref for remote video (iOS only, and only if PiP is enabled)
  useEffect(() => {
    const pipEnabled = getPipEnabled();
    if (hasRemoteVideo && Platform.OS === 'ios' && pipEnabled) {
      setPipRef(remoteVideoRef);
    }
    return () => {
      clearPipRef();
    };
  }, [hasRemoteVideo]);

  // Debug logging
  useEffect(() => {
    const remoteURL = remoteStream?.toURL?.();
    const videoTracks = remoteStream?.getVideoTracks?.() || [];
    console.log('[InCallScreen] Video state:', {
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
  }, [
    remoteStream,
    remoteTrackCount,
    hasRemoteVideo,
    localStream,
    localTrackCount,
    hasLocalVideo,
  ]);

  // No filtering needed - just show all messages during call

  // Debug: log messages when they change
  useEffect(() => {
    console.log('[InCallScreen] Messages updated:', messages.length, messages);
  }, [messages]);

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    if (showChat && messages.length > 0) {
      setTimeout(() => {
        scrollViewRef.current?.scrollToEnd({ animated: true });
      }, 100);
    }
  }, [messages.length, showChat]);

  // Mark messages as read when chat is opened
  useEffect(() => {
    if (showChat) {
      markMessagesAsRead();
    }
  }, [showChat, markMessagesAsRead]);

  const handleSendMessage = useCallback(() => {
    if (!messageText.trim()) return;

    storeSendMessage(messageText.trim());
    setMessageText('');
    Keyboard.dismiss();
  }, [messageText, storeSendMessage]);

  const handleMessageTextChange = useCallback(
    (text: string) => {
      setMessageText(text);
      updateOutgoingRttDraft(text);
    },
    [updateOutgoingRttDraft],
  );

  useEffect(() => {
    if (!visible) {
      clearRemoteRttPreview();
      updateOutgoingRttDraft('');
    }
  }, [clearRemoteRttPreview, updateOutgoingRttDraft, visible]);

  useEffect(() => {
    const subscription = AppState.addEventListener('change', (nextAppState) => {
      const prevAppState = appStateRef.current;
      appStateRef.current = nextAppState;

      const isReturningToForeground =
        (prevAppState === 'inactive' || prevAppState === 'background') &&
        nextAppState === 'active';
      const isCallActive =
        callState === CallState.CONNECTING ||
        callState === CallState.CALLING ||
        callState === CallState.RINGING ||
        callState === CallState.INCALL;

      if (!isReturningToForeground || !visible || !isCallActive) {
        return;
      }

      console.log(
        '[InCallScreen] App foregrounded during active call - refreshing remote video',
      );
      setRemoteVideoRenderVersion((prev) => prev + 1);
      refreshRemoteVideo('app_foreground');
    });

    return () => {
      subscription.remove();
    };
  }, [callState, refreshRemoteVideo, visible]);

  const toggleChat = () => {
    setShowChat(!showChat);
    if (!showChat) {
      markMessagesAsRead();
    }
  };

  return (
    <Modal
      visible={visible}
      animationType="slide"
      presentationStyle="fullScreen"
    >
      <View style={styles.container}>
        {/* Video or Avatar Background */}
        {hasRemoteVideo ? (
          <View style={styles.remoteVideoContainer}>
            {(() => {
              const streamUrl = remoteStream.toURL();
              console.log(
                '[InCallScreen] 🎬 Rendering RTCView with streamURL:',
                streamUrl,
                'key:',
                remoteStreamKey,
              );
              console.log('[InCallScreen] 🎬 Video track state:', {
                enabled: remoteVideoTrack?.enabled,
                muted: remoteVideoTrack?.muted,
                readyState: remoteVideoTrack?.readyState,
              });
              return (
                <RTCView
                  ref={remoteVideoRef}
                  key={remoteStreamKey}
                  streamURL={streamUrl}
                  style={styles.remoteVideo}
                  objectFit="contain"
                  zOrder={1}
                  mirror={false}
                />
              );
            })()}
          </View>
        ) : (
          <>
            {/* Gradient Background when no remote video is available */}
            <View style={styles.gradientBg} />
            <View style={styles.contactSection}>
              <Avatar size="xl">
                <AvatarFallback>
                  <Text style={styles.avatarText}>
                    {getInitials(contactName, phoneNumber)}
                  </Text>
                </AvatarFallback>
              </Avatar>
            </View>
          </>
        )}

        {/* Local Video (Picture-in-Picture) with flip animation */}
        {hasLocalVideo && (
          <View style={styles.localVideoContainer}>
            <RTCView
              key="local-video-stable"
              streamURL={localStream.toURL()}
              style={styles.localVideo}
              objectFit="cover"
              zOrder={2}
              mirror={true}
            />
          </View>
        )}

        {/* Overlay Info (Contact name, duration) */}
        <View style={styles.overlayInfo}>
          <Text style={styles.contactName}>{contactName || phoneNumber}</Text>
          {contactName && <Text style={styles.phoneNumber}>{phoneNumber}</Text>}
          {networkReconnecting ? (
            <View style={styles.reconnectingContainer}>
              <View style={styles.reconnectingDot} />
              <Text style={styles.reconnectingText}>Reconnecting...</Text>
            </View>
          ) : getCallStatusText(callState) ? (
            <View style={styles.connectingContainer}>
              <View style={styles.connectingDot} />
              <Text style={styles.connectingText}>
                {getCallStatusText(callState)}
              </Text>
            </View>
          ) : (
            <View style={styles.statusContainer}>
              <View style={styles.statusDot} />
              <Text style={styles.duration}>{formatDuration(duration)}</Text>
            </View>
          )}
        </View>

        {/* Action Buttons */}
        {remoteRttPreview.trim().length > 0 && (
          <View style={styles.rttContainer}>
            <LiveTextViewer
              text={remoteRttPreview}
              isTyping={remoteRttPreview.trim().length > 0}
            />
          </View>
        )}

        <View style={styles.actionsContainer}>
          {/* Row 1 */}
          <View style={styles.actionRow}>
            <Pressable style={styles.actionButton} onPress={onMuteToggle}>
              <View style={styles.iconPlaceholder}>
                <AppIcon
                  name={isMuted ? 'micOff' : 'mic'}
                  size={ms(24)}
                  color="#fff"
                />
              </View>
              <Text style={styles.actionLabel}>Mute</Text>
            </Pressable>

            <Pressable
              style={styles.actionButton}
              onPress={() => setShowKeypad(true)}
            >
              <View style={styles.iconPlaceholder}>
                <AppIcon name="dialpad" size={ms(24)} color="#fff" />
              </View>
              <Text style={styles.actionLabel}>Keypad</Text>
            </Pressable>

            <Pressable style={[styles.actionButton]} onPress={onSpeakerToggle}>
              <View style={styles.iconPlaceholder}>
                {isSpeaker ? (
                  <Volume2Icon size={ms(24)} color="#fff" />
                ) : (
                  <Volume1Icon size={ms(24)} color="#fff" />
                )}
              </View>
              <Text style={styles.actionLabel}>
                {isSpeaker ? 'Speaker On' : 'Speaker Off'}
              </Text>
            </Pressable>
          </View>

          {/* Row 2 */}
          <View style={styles.actionRow}>
            <Pressable style={[styles.actionButton]} onPress={toggleChat}>
              <View style={styles.iconPlaceholder}>
                <MessageCircleMore size={ms(24)} color="#fff" />
                {!showChat && unreadMessageCount > 0 && (
                  <View style={styles.unreadBadge}>
                    <Text style={styles.unreadBadgeText}>
                      {unreadMessageCount > 9 ? '9+' : unreadMessageCount}
                    </Text>
                  </View>
                )}
              </View>
              <Text style={styles.actionLabel}>Chat</Text>
            </Pressable>

            <Pressable style={styles.actionButton} onPress={handleSwitchCamera}>
              <View style={styles.iconPlaceholder}>
                <AppIcon name="refreshCw" size={ms(24)} color="#fff" />
              </View>
              <Text style={styles.actionLabel}>Switch Cam</Text>
            </Pressable>

            <Pressable
              style={[
                styles.actionButton,
                !isVideoEnabled && styles.actionButtonActive,
              ]}
              onPress={onVideoToggle}
            >
              <View style={styles.iconPlaceholder}>
                <AppIcon
                  name={isVideoEnabled ? 'video' : 'videoOff'}
                  size={ms(24)}
                  color="#fff"
                />
              </View>
              <Text style={styles.actionLabel}>
                {isVideoEnabled ? 'Video On' : 'Video Off'}
              </Text>
            </Pressable>
          </View>

          {/* Row 3 removed - Kickstart Video no longer needed with Gateway */}
        </View>

        {/* Chat Overlay */}
        <Chat
          visible={showChat}
          contactName={contactName}
          phoneNumber={phoneNumber}
          messages={messages}
          messageText={messageText}
          setMessageText={handleMessageTextChange}
          handleSendMessage={handleSendMessage}
          toggleChat={toggleChat}
        />

        {/* DTMF Keypad Overlay */}
        <DtmfKeypad
          visible={showKeypad}
          onClose={() => setShowKeypad(false)}
          onDigitPress={sendDtmf}
        />

        {/* Hangup Button */}
        <View style={styles.hangupContainer}>
          <Pressable
            style={({ pressed }) => [
              styles.hangupButton,
              pressed && styles.hangupButtonPressed,
            ]}
            onPress={onHangup}
          >
            <AppIcon name="phone" size={ms(28)} color="#fff" />
          </Pressable>
          <Text style={styles.hangupLabel}>End Call</Text>
        </View>
      </View>
    </Modal>
  );
}
