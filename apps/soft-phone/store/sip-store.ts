/**
 * SIP Store - Zustand Store for Gateway-based SIP
 *
 * Global state management for SIP using Zustand
 * Uses K2 Gateway WebSocket approach (simplified from Janus/Asterisk dual backend)
 */

import InCallManager from 'react-native-incall-manager';
import { MediaStream } from 'react-native-webrtc';
import { create } from 'zustand';

import {
  reportCallAnswered,
  reportCallEnded,
  reportIncomingCall,
  reportMuteState,
  reportOutgoingCall,
  reportOutgoingCallConnecting,
} from '@/lib/callkeep';
import {
  CallState,
  ConnectionState,
  GatewayClient,
  ReconnectConfig,
  getGatewayClient,
  type IncomingCallInfo,
  type PublicCallAuth,
} from '@/lib/gateway';
import { SavedCallState, getNetworkMonitor } from '@/lib/network';
import { getSettingsSync } from '@/store/settings-store';

// Re-export types
export { CallState } from '@/lib/gateway';
export type { ConnectionState } from '@/lib/gateway';

// Safe wrapper functions for InCallManager
const safeInCallManager = {
  start: (options?: { media?: string }) => {
    try {
      InCallManager?.start?.(options);
    } catch (e) {
      console.warn('[SipStore] InCallManager.start failed:', e);
    }
  },
  stop: () => {
    try {
      InCallManager?.stop?.();
    } catch (e) {
      console.warn('[SipStore] InCallManager.stop failed:', e);
    }
  },
  setForceSpeakerphoneOn: (flag: boolean) => {
    try {
      InCallManager?.setForceSpeakerphoneOn?.(flag);
    } catch (e) {
      console.warn(
        '[SipStore] InCallManager.setForceSpeakerphoneOn failed:',
        e,
      );
    }
  },
};

const FOREGROUND_RECOVERY_GRACE_MS = 30000;
const FOREGROUND_RECOVERY_RETRY_OFFSETS_MS = [0, 3000, 7000, 12000] as const;
const RESUME_REGISTRATION_WAIT_TIMEOUT_MS = 800; // Reduced from 1500ms - server echoes 'registered' in <200ms typically
const RESUME_RESPONSE_WAIT_TIMEOUT_MS = 2000;
const FORCED_RESUME_LOCAL_STALL_COOLDOWN_MS = 15000;
const FORCED_RESUME_LOCAL_STALL_WINDOW_MS = 90000;
const FORCED_RESUME_LOCAL_STALL_MAX_ATTEMPTS = 2;

let forcedResumeLocalStallLastAt = 0;
let forcedResumeLocalStallWindowStartedAt = 0;
let forcedResumeLocalStallAttemptsInWindow = 0;

type RecoveryMode = 'idle' | 'soft_recovering' | 'hard_terminating';
type RecoverableError = 'PUBLIC_IDENTITY_CHANGED';

const PUBLIC_IDENTITY_CHANGED_ERROR_SUBSTRING = 'Public SIP identity changed';
const PUBLIC_IDENTITY_ACTIONABLE_ERROR =
  'Session เดิมไม่ตรงกับ user ใหม่ กรุณาโทรใหม่';

function isPublicIdentityChangedError(error: string): boolean {
  return error.includes(PUBLIC_IDENTITY_CHANGED_ERROR_SUBSTRING);
}

// SIP Config type
interface SipConfig {
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipDisplayName?: string;
  sipPort?: number;
}

// Chat message type (in-memory only, cleared on call end)
interface ChatMessage {
  id: string;
  from: string;
  to: string;
  body: string;
  contentType?: string;
  direction: 'incoming' | 'outgoing';
  status: 'sending' | 'sent' | 'failed';
  timestamp: number;
  read: boolean;
}

// Export for components
export type { ChatMessage };

interface SipState {
  // Gateway Client
  gatewayClient: GatewayClient | null;

  // Connection state
  isConnected: boolean;
  isConnecting: boolean;
  connectionError: string | null;
  isAutoRecovering: boolean;
  lastRecoverableError: RecoverableError | null;
  isReconnecting: boolean;
  reconnectAttempt: number;
  connectionState: ConnectionState;
  maxReconnectAttempts: number;
  reconnectFailed: boolean;

  // SIP state
  isRegistered: boolean;
  isRegistering: boolean;
  registrationError: string | null;

  // Call state
  callState: CallState;
  remoteNumber: string | null;
  remoteDisplayName: string | null;
  callDuration: number;

  // Media streams for video
  localStream: MediaStream | null;
  remoteStream: MediaStream | null;
  localStreamVersion: number;

  // Audio state
  isMuted: boolean;
  isSpeaker: boolean;

  // Video state
  isVideoEnabled: boolean;
  cameraFacing: 'front' | 'back';

  // Config
  sipConfig: SipConfig | null;

  // Chat messages (in-memory, cleared on call end)
  messages: ChatMessage[];
  unreadMessageCount: number;

  // Network state for auto-reconnect
  isNetworkConnected: boolean;
  networkReconnecting: boolean;
  callResumePending: boolean;

  // Permission error state
  permissionError: string | null;
  permissionRetryCount: number;
  missingPermissions: ('camera' | 'microphone')[];

  // Internal refs
  _callTimer: ReturnType<typeof setInterval> | null;
  _callStartTime: Date | null;
  _callDirection: 'incoming' | 'outgoing' | null;
  _activeRecoveryId: string | null;
  _recoveryTerminationInProgress: boolean;
  _recoveryMode: RecoveryMode;
  _foregroundRecoveryStartedAt: number | null;
  _foregroundRecoveryAttempts: number;
  _foregroundRecoveryTimer: ReturnType<typeof setTimeout> | null;
  _foregroundRecoveryRetryTimer: ReturnType<typeof setTimeout> | null;
  _resumeAttemptStartedAt: number | null;
  _resumeAttemptSource: string | null;
  _publicCallAuthInMemory: PublicCallAuth | null;

  // Trunk resolve state (soft-phone specific)
  trunkResolveStatus: 'idle' | 'resolving' | 'resolved' | 'failed';
  trunkResolveError: string | null;
  resolvedTrunkId: number | null;

  // Incoming call state (soft-phone specific)
  incomingCall: IncomingCallInfo | null;

  // RTT state (soft-phone specific)
  outgoingRttDraft: string;
  remoteRttPreview: string;
}

interface SipActions {
  // Connection actions
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;

  // SIP actions
  register: (config: SipConfig) => Promise<void>;
  unregister: () => Promise<void>;

  // Call actions
  call: (
    number: string,
    auth?: import('@/lib/gateway').CallAuth,
  ) => Promise<void>;
  hangup: () => Promise<void>;
  resetCallRuntimeState: () => void;

  // Audio actions
  toggleMute: () => void;
  toggleSpeaker: () => void;
  sendDtmf: (digit: string) => void;

  // Video actions
  toggleVideo: () => void;
  switchCamera: () => Promise<void>;
  refreshRemoteVideo: (reason?: string) => void;

  // Stream actions
  setLocalStream: (stream: MediaStream | null) => void;
  setRemoteStream: (stream: MediaStream | null) => void;

  // Config actions
  setSipConfig: (config: SipConfig | null) => void;

  // Chat actions
  sendMessage: (body: string) => void;
  markMessagesAsRead: () => void;

  // Reconnection actions
  setReconnectConfig: (config: Partial<ReconnectConfig>) => void;
  manualReconnect: () => Promise<void>;

  // Network reconnect actions
  setupNetworkMonitor: () => void;
  handleAppForegroundRecovery: () => Promise<void>;
  handleNetworkReconnect: () => Promise<void>;
  handleCallResume: (
    savedState: SavedCallState,
    recoveryId?: string,
    options?: { bypassRegistrationWait?: boolean; source?: string },
  ) => Promise<void>;

  // Permission error actions
  setPermissionError: (
    error: string | null,
    missingPerms?: ('camera' | 'microphone')[],
  ) => void;
  incrementPermissionRetry: () => void;
  resetPermissionRetry: () => void;

  // Internal actions
  _terminateCallAfterRecoveryFailure: (
    reason: string,
    context?: { recoveryId?: string; source?: string },
  ) => Promise<void>;
  _isCallLikelyAlive: () => boolean;
  _clearForegroundRecoveryTimers: () => void;
  _clearForegroundRecoveryRetryTimer: () => void;
  _scheduleForegroundRecoveryRetry: (
    savedState: SavedCallState,
    recoveryId: string,
    reason: string,
    source: string,
  ) => void;
  _startCallTimer: () => void;
  _stopCallTimer: () => void;
  _reset: () => void;

  // Trunk resolve actions (soft-phone specific)
  resolveTrunk: () => void;
  clearTrunkResolveState: () => void;

  // Incoming call actions (soft-phone specific)
  answer: () => Promise<void>;
  decline: () => void;

  // RTT actions (soft-phone specific)
  updateOutgoingRttDraft: (text: string) => void;
  clearRemoteRttPreview: () => void;
}

type SipStore = SipState & SipActions;

const initialState: SipState = {
  gatewayClient: null,
  isConnected: false,
  isConnecting: false,
  connectionError: null,
  isAutoRecovering: false,
  lastRecoverableError: null,
  isReconnecting: false,
  reconnectAttempt: 0,
  connectionState: 'disconnected',
  maxReconnectAttempts: 5,
  reconnectFailed: false,
  isRegistered: false,
  isRegistering: false,
  registrationError: null,
  callState: CallState.IDLE,
  remoteNumber: null,
  remoteDisplayName: null,
  callDuration: 0,
  localStream: null,
  remoteStream: null,
  localStreamVersion: 0,
  isMuted: false,
  isSpeaker: false,
  isVideoEnabled: true,
  cameraFacing: 'front',
  sipConfig: null,
  messages: [],
  unreadMessageCount: 0,
  isNetworkConnected: true,
  networkReconnecting: false,
  callResumePending: false,
  permissionError: null,
  permissionRetryCount: 0,
  missingPermissions: [],
  _callTimer: null,
  _callStartTime: null,
  _callDirection: null,
  _activeRecoveryId: null,
  _recoveryTerminationInProgress: false,
  _recoveryMode: 'idle',
  _foregroundRecoveryStartedAt: null,
  _foregroundRecoveryAttempts: 0,
  _foregroundRecoveryTimer: null,
  _foregroundRecoveryRetryTimer: null,
  _resumeAttemptStartedAt: null,
  _resumeAttemptSource: null,
  _publicCallAuthInMemory: null,

  // Trunk resolve state (soft-phone specific)
  trunkResolveStatus: 'idle',
  trunkResolveError: null,
  resolvedTrunkId: null,

  // Incoming call state (soft-phone specific)
  incomingCall: null,

  // RTT state (soft-phone specific)
  outgoingRttDraft: '',
  remoteRttPreview: '',
};

export const useSipStore = create<SipStore>((set, get) => ({
  ...initialState,

  // Connect to Gateway server
  connect: async () => {
    const { gatewayClient: existingClient } = get();
    if (existingClient?.isConnected) {
      console.log('[SipStore] Already connected');
      return;
    }

    set({ isConnecting: true, connectionError: null });

    try {
      const client = getGatewayClient();

      // Set up callbacks
      client.setCallbacks({
        onConnected: () => {
          console.log('[SipStore] Gateway connected');
          const wasNetworkReconnecting = get().networkReconnecting;
          set({
            isConnected: true,
            connectionError: null,
            isAutoRecovering: false,
            lastRecoverableError: null,
            isReconnecting: false,
            reconnectAttempt: 0,
            // Clear network reconnecting flag on successful connection
            // but keep call state if we were in a call during network switch
          });

          if (wasNetworkReconnecting) {
            console.log(
              '[SipStore] Reconnected after network change - call state preserved',
            );
          }
        },
        onDisconnected: (reason) => {
          console.log('[SipStore] Gateway disconnected:', reason);
          const {
            callState,
            networkReconnecting: alreadyReconnecting,
            _recoveryTerminationInProgress,
            _recoveryMode,
          } = get();
          const client = get().gatewayClient;

          if (
            _recoveryTerminationInProgress ||
            _recoveryMode === 'hard_terminating'
          ) {
            console.log(
              '[SipStore] Disconnect received during recovery termination - skipping reconnect preservation',
            );
            set({
              isConnected: false,
              isRegistered: false,
              isRegistering: false,
              callResumePending: false,
              networkReconnecting: false,
              isAutoRecovering: false,
              lastRecoverableError: null,
              _publicCallAuthInMemory: null,
              _recoveryMode: 'idle',
            });
            return;
          }

          // Check if this is a network-triggered disconnect (WiFi <-> Cellular switch)
          const isNetworkDisconnect =
            client?.isNetworkTriggeredDisconnect ?? false;
          // Check if this was a manual/intentional disconnect
          const wasManual = client?.wasManualDisconnect ?? false;
          const isInCall =
            callState === CallState.INCALL ||
            callState === CallState.CALLING ||
            callState === CallState.RINGING ||
            callState === CallState.CONNECTING;

          // CRITICAL FIX: If we're in a call and it's NOT a manual disconnect,
          // preserve call state for potential reconnection.
          // This fixes the race condition where WebSocket dies before network monitor
          // has a chance to set networkReconnecting flag (WiFi -> Cellular switch)
          const shouldPreserveCallState = isInCall && !wasManual;

          if (
            _recoveryMode === 'soft_recovering' &&
            get()._isCallLikelyAlive()
          ) {
            console.log(
              '[SipStore] onDisconnected during soft recovery - preserving active call',
            );
            set({
              isConnected: false,
              isRegistered: false,
              isRegistering: false,
              networkReconnecting: true,
              callResumePending: false,
            });
            return;
          }

          if (shouldPreserveCallState) {
            // Unexpected disconnect during a call - preserve call state for reconnection
            console.log(
              '[SipStore] Unexpected disconnect during call - preserving call state for reconnection',
            );
            console.log(
              '[SipStore] Flags: isNetworkDisconnect=',
              isNetworkDisconnect,
              'alreadyReconnecting=',
              alreadyReconnecting,
              'wasManual=',
              wasManual,
            );
            client?.clearNetworkTriggeredDisconnect();

            set({
              isConnected: false,
              isRegistered: false,
              isRegistering: false,
              networkReconnecting: true,
              isAutoRecovering: false,
              // Keep call state - don't reset to IDLE
              // Keep remoteNumber and remoteDisplayName
              // Note: localStream and remoteStream will be recreated on reconnection
            });

            // Fallback timeout for non-foreground recovery paths.
            setTimeout(() => {
              const currentState = get();
              if (
                currentState.networkReconnecting &&
                !currentState.isConnected &&
                !currentState._recoveryTerminationInProgress &&
                currentState._recoveryMode !== 'soft_recovering' &&
                !currentState._isCallLikelyAlive()
              ) {
                const timeoutRecoveryId =
                  currentState._activeRecoveryId ?? undefined;
                console.log(
                  '[SipStore] Network reconnection timeout - force terminating call session',
                );
                void currentState
                  ._terminateCallAfterRecoveryFailure(
                    'Call dropped - network reconnection failed',
                    {
                      recoveryId: timeoutRecoveryId,
                      source: 'disconnect_timeout',
                    },
                  )
                  .catch((error) => {
                    console.error(
                      '[SipStore] Failed to terminate call after reconnect timeout:',
                      error,
                    );
                  });
              }
            }, 30000); // 30 second timeout
          } else if (!alreadyReconnecting) {
            // Manual disconnect OR not in call - reset all state
            console.log(
              '[SipStore] Manual disconnect or not in call - resetting state',
            );
            safeInCallManager.stop();
            get()._stopCallTimer();
            client?.clearNetworkTriggeredDisconnect();

            set({
              isConnected: false,
              isRegistered: false,
              isRegistering: false,
              isAutoRecovering: false,
              lastRecoverableError: null,
              callState: CallState.IDLE,
              remoteNumber: null,
              remoteDisplayName: null,
              isMuted: false,
              isSpeaker: false,
              isVideoEnabled: true,
              cameraFacing: 'front',
              localStream: null,
              remoteStream: null,
              _callDirection: null,
              _publicCallAuthInMemory: null,
            });
          } else {
            // Network reconnecting but not in call - just update connection state
            console.log(
              '[SipStore] Network disconnect (not in call) - preserving reconnect state',
            );
            set({
              isConnected: false,
              isRegistered: false,
              isRegistering: false,
              isAutoRecovering: false,
            });
          }
        },
        onRegistered: (username) => {
          console.log('[SipStore] Registered as:', username);
          set({
            isRegistered: true,
            isRegistering: false,
            registrationError: null,
          });
        },
        onUnregistered: () => {
          set({ isRegistered: false });
        },
        onRegistrationFailed: (error) => {
          console.error('[SipStore] Registration failed:', error);
          set({
            isRegistered: false,
            isRegistering: false,
            registrationError: error,
          });
        },
        onCalling: () => {
          set({ callState: CallState.CALLING, _callDirection: 'outgoing' });
          const number = get().remoteNumber;
          if (number) {
            reportOutgoingCall(number);
          }
        },
        onRinging: () => {
          set({ callState: CallState.RINGING });
          if (get()._callDirection === 'outgoing') {
            reportOutgoingCallConnecting();
          }
        },
        onAnswered: () => {
          const isOutgoing = get()._callDirection === 'outgoing';
          set({ callState: CallState.INCALL });
          get()._startCallTimer();
          safeInCallManager.start({ media: 'audio' });
          reportCallAnswered(isOutgoing);
        },
        onCallEnded: (reason) => {
          console.log('[SipStore] Call ended:', reason);
          get()._clearForegroundRecoveryTimers();
          safeInCallManager.stop();
          reportCallEnded(2);
          set({
            callState: CallState.IDLE,
            remoteNumber: null,
            remoteDisplayName: null,
            connectionError: null,
            isAutoRecovering: false,
            lastRecoverableError: null,
            isMuted: false,
            isSpeaker: false,
            isVideoEnabled: true,
            cameraFacing: 'front',
            localStream: null,
            remoteStream: null,
            _callDirection: null,
            // Clear messages on call end (in-memory only)
            messages: [],
            unreadMessageCount: 0,
            _activeRecoveryId: null,
            _recoveryMode: 'idle',
            _foregroundRecoveryStartedAt: null,
            _foregroundRecoveryAttempts: 0,
            _publicCallAuthInMemory: null,
          });
          get()._stopCallTimer();
          const disconnect = get().disconnect;
          void disconnect().catch((error) => {
            console.error(
              '[SipStore] Disconnect after call ended failed:',
              error,
            );
          });
        },
        onError: (error) => {
          console.error('[SipStore] Gateway Error:', error);
          const identityChanged = isPublicIdentityChangedError(error);
          set({
            connectionError: identityChanged
              ? PUBLIC_IDENTITY_ACTIONABLE_ERROR
              : error,
            isAutoRecovering: false,
            lastRecoverableError: identityChanged
              ? 'PUBLIC_IDENTITY_CHANGED'
              : null,
            _publicCallAuthInMemory: identityChanged
              ? null
              : get()._publicCallAuthInMemory,
          });
        },
        onRecoveryState: (state) => {
          if (state === 'retrying_public_identity') {
            set({
              isAutoRecovering: true,
              lastRecoverableError: 'PUBLIC_IDENTITY_CHANGED',
              connectionError: 'กำลังสร้าง session ใหม่...',
            });
            return;
          }

          set({
            isAutoRecovering: false,
            lastRecoverableError: 'PUBLIC_IDENTITY_CHANGED',
            connectionError: PUBLIC_IDENTITY_ACTIONABLE_ERROR,
            _publicCallAuthInMemory: null,
          });
        },
        onLocalStream: (stream) => {
          console.log('[SipStore] Local stream received');
          set((state) => ({
            localStream: stream,
            localStreamVersion: state.localStreamVersion + 1,
          }));
        },
        onRemoteStream: (stream) => {
          console.log('[SipStore] Remote stream received');
          set({ remoteStream: stream });
        },
        onMessage: (from, body, contentType) => {
          // Filter out RTT messages - they should only appear in LiveTextViewer, not Chat
          const isRttMessage =
            body?.trimStart().startsWith('<rtt') ||
            contentType?.includes('t140') ||
            contentType?.includes('rtte+xml') ||
            contentType?.includes('xmpp+xml');

          if (isRttMessage) {
            console.log(
              '[SipStore] RTT message filtered out from chat (will show in LiveTextViewer)',
            );
            return; // Don't add RTT messages to chat
          }

          // Filter out special control messages (e.g., @switch:, @open_chat)
          const isControlMessage = body?.startsWith('@');
          if (isControlMessage) {
            console.log(
              '[SipStore] Control message filtered out from chat:',
              body.substring(0, 30),
            );
            return; // Don't add control messages to chat
          }

          console.log('[SipStore] Message received from:', from);
          const newMessage: ChatMessage = {
            id: `msg_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
            from,
            to: get().sipConfig?.sipUsername || '',
            body,
            contentType,
            direction: 'incoming',
            status: 'sent',
            timestamp: Date.now(),
            read: false,
          };
          set((state) => ({
            messages: [...state.messages, newMessage],
            unreadMessageCount: state.unreadMessageCount + 1,
          }));
        },
        onConnectionStateChange: (state) => {
          set({ connectionState: state });
        },
        onReconnecting: (attempt, maxAttempts) => {
          set({
            isReconnecting: true,
            reconnectAttempt: attempt,
            maxReconnectAttempts: maxAttempts,
            reconnectFailed: false,
          });
        },
        onReconnectFailed: () => {
          set({
            isReconnecting: false,
            reconnectFailed: true,
            connectionError: 'Connection lost. Tap to reconnect.',
          });
        },
        // Call resume callbacks for network change recovery
        onCallResumed: (sessionId) => {
          const recoveryId = get()._activeRecoveryId;
          const recoveryTag = recoveryId ? `[Recovery:${recoveryId}] ` : '';
          const resumeStartedAt = get()._resumeAttemptStartedAt;
          const resumeSource = get()._resumeAttemptSource;
          if (resumeStartedAt) {
            const resumeElapsedMs = Date.now() - resumeStartedAt;
            console.log(
              `[SipStore] ${recoveryTag}resumed_latency_ms=${resumeElapsedMs} source=${resumeSource ?? 'unknown'}`,
            );
          }
          console.log(
            `[SipStore] ${recoveryTag}resumed_success sessionId:`,
            sessionId,
          );
          get()._clearForegroundRecoveryTimers();
          set({
            callResumePending: false,
            networkReconnecting: false,
            callState: CallState.INCALL,
            _activeRecoveryId: null,
            _recoveryMode: 'idle',
            _foregroundRecoveryStartedAt: null,
            _foregroundRecoveryAttempts: 0,
            _resumeAttemptStartedAt: null,
            _resumeAttemptSource: null,
          });
          get()._startCallTimer();
          safeInCallManager.start({ media: 'audio' });
        },
        onCallResumeFailed: (reason) => {
          const state = get();
          const recoveryId =
            state._activeRecoveryId ??
            `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
          const recoveryTag = recoveryId ? `[Recovery:${recoveryId}] ` : '';
          console.log(
            `[SipStore] ${recoveryTag}resume_failed_soft reason:`,
            reason,
          );
          set({
            callResumePending: false,
            _activeRecoveryId: recoveryId,
            _resumeAttemptStartedAt: null,
            _resumeAttemptSource: null,
          });

          if (
            state._recoveryMode === 'soft_recovering' &&
            state._isCallLikelyAlive()
          ) {
            const savedState: SavedCallState = {
              sessionId: state.gatewayClient?.getSessionId() ?? null,
              remoteNumber: state.remoteNumber,
              wasInCall: true,
              timestamp: Date.now(),
            };
            state._scheduleForegroundRecoveryRetry(
              savedState,
              recoveryId,
              reason,
              'resume_failed_event',
            );
            return;
          }

          void state
            ._terminateCallAfterRecoveryFailure(`Call dropped: ${reason}`, {
              recoveryId,
              source: 'resume_failed_event',
            })
            .catch((error) => {
              console.error(
                '[SipStore] Failed to terminate call after resume_failed event:',
                error,
              );
            });
        },

        // Trunk resolve callbacks (soft-phone specific)
        onTrunkResolved: (trunkId: number) => {
          console.log('[SipStore] Trunk resolved:', trunkId);
          set({
            trunkResolveStatus: 'resolved',
            trunkResolveError: null,
            resolvedTrunkId: trunkId,
          });
        },
        onTrunkRedirect: (redirectUrl: string) => {
          console.log('[SipStore] Trunk redirect:', redirectUrl);
          // State stays 'resolving' during redirect
        },
        onTrunkResolveFailed: (reason: string, _type: string) => {
          console.error('[SipStore] Trunk resolve failed:', reason, _type);
          set({
            trunkResolveStatus: 'failed',
            trunkResolveError: reason,
          });
        },
        onRegisterStatus: (
          status: import('@/lib/gateway').RegisterStatusResponse,
        ) => {
          // If registerStatus includes trunkId, save it
          if (status.trunkId) {
            console.log(
              '[SipStore] Trunk ID from registerStatus:',
              status.trunkId,
            );
            set({ resolvedTrunkId: status.trunkId });
          }
        },

        // Incoming call callback (soft-phone specific)
        onIncomingCall: (info: IncomingCallInfo, _sdp?: string) => {
          console.log('[SipStore] Incoming call from:', info.caller);
          set({
            callState: CallState.INCOMING,
            incomingCall: info,
            remoteNumber: info.caller,
            _callDirection: 'incoming',
          });
          // Report incoming call to CallKeep
          reportIncomingCall(info.caller, info.caller);
        },
      });

      set({ gatewayClient: client });
      const settings = getSettingsSync();
      await client.connect(settings.gatewayServer);

      set({ isConnected: true });
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : 'Connection failed';
      set({
        connectionError: errorMessage,
        isConnected: false,
        isAutoRecovering: false,
        gatewayClient: null,
      });
      throw error;
    } finally {
      set({ isConnecting: false });
    }
  },

  // Disconnect
  disconnect: async () => {
    get()._clearForegroundRecoveryTimers();
    const { gatewayClient } = get();
    if (gatewayClient) {
      gatewayClient.disconnect();
    }
    set({
      gatewayClient: null,
      isConnected: false,
      isRegistered: false,
      isAutoRecovering: false,
      lastRecoverableError: null,
      isReconnecting: false,
      reconnectAttempt: 0,
      _activeRecoveryId: null,
      _recoveryMode: 'idle',
      _foregroundRecoveryStartedAt: null,
      _foregroundRecoveryAttempts: 0,
      _resumeAttemptStartedAt: null,
      _resumeAttemptSource: null,
      callResumePending: false,
      networkReconnecting: false,
      _recoveryTerminationInProgress: false,
      _publicCallAuthInMemory: null,
    });
  },

  // Register with SIP server
  register: async (config: SipConfig) => {
    const { gatewayClient } = get();
    if (!gatewayClient) {
      throw new Error('Gateway client not connected');
    }

    set({ sipConfig: config, registrationError: null, isRegistering: true });

    try {
      await gatewayClient.register({
        sipDomain: config.sipDomain,
        sipUsername: config.sipUsername,
        sipPassword: config.sipPassword,
        sipPort: config.sipPort,
      });
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : 'Registration failed';
      set({ isRegistering: false, registrationError: errorMessage });
      throw error;
    }
  },

  // Unregister from SIP server
  unregister: async () => {
    const { gatewayClient } = get();
    if (gatewayClient) {
      gatewayClient.unregister();
    }
    set({ isRegistered: false });
  },

  // Make outgoing call
  call: async (number: string, auth?: import('@/lib/gateway').CallAuth) => {
    const { gatewayClient, sipConfig } = get();
    if (!gatewayClient) {
      throw new Error('Gateway client not connected');
    }

    try {
      console.log('[SipStore] Outbound call to:', number);

      // If auth is provided, use per-call auth (no registration needed)
      if (auth) {
        if (auth.mode === 'public') {
          get().resetCallRuntimeState();
          set({
            _publicCallAuthInMemory: { ...auth },
            lastRecoverableError: null,
          });
        }

        set({
          remoteNumber: number,
          _callDirection: 'outgoing',
          connectionError: null,
        });
        await gatewayClient.call(number, auth);
        return;
      }

      // Otherwise, use pre-registration flow (existing behavior)
      // Self-heal: re-register if registration was lost
      if (!gatewayClient.isRegisteredState) {
        if (!sipConfig) {
          set({
            isRegistered: false,
            isRegistering: false,
            registrationError: 'SIP not registered. Configure SIP in Settings.',
          });
          throw new Error('SIP not registered. Configure SIP in Settings.');
        }

        set({ registrationError: null });
        await gatewayClient.register({
          sipDomain: sipConfig.sipDomain,
          sipUsername: sipConfig.sipUsername,
          sipPassword: sipConfig.sipPassword,
          sipPort: sipConfig.sipPort,
        });
      }

      set({ remoteNumber: number, _callDirection: 'outgoing' });
      await gatewayClient.call(number);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : 'Failed to make call';
      console.error('[SipStore] Outbound call failed:', message);
      const identityChanged = isPublicIdentityChangedError(message);
      set({
        callState: CallState.IDLE,
        _callDirection: null,
        _publicCallAuthInMemory: null,
        isAutoRecovering: false,
        lastRecoverableError: identityChanged
          ? 'PUBLIC_IDENTITY_CHANGED'
          : null,
        connectionError: identityChanged
          ? PUBLIC_IDENTITY_ACTIONABLE_ERROR
          : message,
      });
      throw error;
    }
  },

  resetCallRuntimeState: () => {
    get()._clearForegroundRecoveryTimers();
    get()._stopCallTimer();
    safeInCallManager.stop();

    set({
      callState: CallState.IDLE,
      remoteNumber: null,
      remoteDisplayName: null,
      isAutoRecovering: false,
      lastRecoverableError: null,
      isMuted: false,
      isSpeaker: false,
      isVideoEnabled: true,
      cameraFacing: 'front',
      localStream: null,
      remoteStream: null,
      _callDirection: null,
      messages: [],
      unreadMessageCount: 0,
      callResumePending: false,
      networkReconnecting: false,
      _activeRecoveryId: null,
      _recoveryTerminationInProgress: false,
      _recoveryMode: 'idle',
      _foregroundRecoveryStartedAt: null,
      _foregroundRecoveryAttempts: 0,
      _publicCallAuthInMemory: null,
      connectionError: null,
    });
  },

  // Hangup current call
  hangup: async () => {
    const { gatewayClient } = get();
    get()._clearForegroundRecoveryTimers();

    // Optimistic cleanup
    const { _stopCallTimer } = get();
    _stopCallTimer();
    reportCallEnded(2);

    set({
      callState: CallState.IDLE,
      remoteNumber: null,
      remoteDisplayName: null,
      isAutoRecovering: false,
      lastRecoverableError: null,
      isMuted: false,
      isSpeaker: false,
      isVideoEnabled: true,
      cameraFacing: 'front',
      localStream: null,
      remoteStream: null,
      _callDirection: null,
      // Clear messages on hangup (in-memory only)
      messages: [],
      unreadMessageCount: 0,
      _activeRecoveryId: null,
      _recoveryMode: 'idle',
      _foregroundRecoveryStartedAt: null,
      _foregroundRecoveryAttempts: 0,
      callResumePending: false,
      networkReconnecting: false,
      _recoveryTerminationInProgress: false,
      _publicCallAuthInMemory: null,
    });

    if (gatewayClient) {
      gatewayClient.hangup();
    }
  },

  // Toggle mute
  toggleMute: () => {
    const { gatewayClient, isMuted } = get();
    if (gatewayClient) {
      gatewayClient.toggleMute();
      const newMuted = !isMuted;
      set({ isMuted: newMuted });
      reportMuteState(newMuted);
    }
  },

  // Toggle speaker
  toggleSpeaker: () => {
    const { isSpeaker } = get();
    const newSpeakerState = !isSpeaker;
    safeInCallManager.setForceSpeakerphoneOn(newSpeakerState);
    set({ isSpeaker: newSpeakerState });
  },

  // Toggle video
  toggleVideo: () => {
    const { gatewayClient, isVideoEnabled } = get();
    if (gatewayClient) {
      gatewayClient.toggleVideo();
      set({ isVideoEnabled: !isVideoEnabled });
    }
  },

  // Switch camera
  switchCamera: async () => {
    const { gatewayClient } = get();
    if (gatewayClient) {
      await gatewayClient.switchCamera();
      set((state) => ({
        cameraFacing: state.cameraFacing === 'front' ? 'back' : 'front',
      }));
    }
  },

  // Send DTMF
  sendDtmf: (digit: string) => {
    const { gatewayClient } = get();
    if (gatewayClient) {
      gatewayClient.sendDtmf(digit);
    }
  },

  refreshRemoteVideo: (reason = 'manual') => {
    const { gatewayClient, callState } = get();
    if (!gatewayClient) {
      return;
    }

    const isCallActive =
      callState === CallState.CONNECTING ||
      callState === CallState.CALLING ||
      callState === CallState.RINGING ||
      callState === CallState.INCALL;

    if (!isCallActive) {
      return;
    }

    gatewayClient.refreshRemoteVideo(reason);
  },

  // Send chat message
  sendMessage: (body: string) => {
    const { gatewayClient, remoteNumber, sipConfig } = get();

    if (!gatewayClient || !remoteNumber) {
      console.warn('[SipStore] Cannot send message - no active call');
      return;
    }

    const newMessage: ChatMessage = {
      id: `msg_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      from: sipConfig?.sipUsername || 'me',
      to: remoteNumber,
      body,
      direction: 'outgoing',
      status: 'sending',
      timestamp: Date.now(),
      read: true,
    };

    // Add to state immediately (optimistic update)
    set((state) => ({
      messages: [...state.messages, newMessage],
    }));

    // Send via gateway
    gatewayClient.sendMessage(body);

    // Mark as sent after a short delay (in real impl, wait for server confirmation)
    setTimeout(() => {
      set((state) => ({
        messages: state.messages.map((m) =>
          m.id === newMessage.id ? { ...m, status: 'sent' as const } : m,
        ),
      }));
    }, 100);
  },

  // Mark messages as read
  markMessagesAsRead: () => {
    set((state) => ({
      messages: state.messages.map((m) => ({ ...m, read: true })),
      unreadMessageCount: 0,
    }));
  },

  // Configure reconnection
  setReconnectConfig: (config: Partial<ReconnectConfig>) => {
    const { gatewayClient } = get();
    gatewayClient?.setReconnectConfig(config);
  },

  // Manual reconnect after failure
  manualReconnect: async () => {
    const { gatewayClient } = get();
    set({ reconnectFailed: false, connectionError: null });

    if (gatewayClient) {
      await gatewayClient.reconnect();
    } else {
      // Re-initialize if client was destroyed
      await get().connect();
    }
  },

  // Set SIP config
  setSipConfig: (config) => {
    set({ sipConfig: config });
  },

  // Set local stream
  setLocalStream: (stream) => {
    set((state) => ({
      localStream: stream,
      localStreamVersion: state.localStreamVersion + 1,
    }));
  },

  // Set remote stream
  setRemoteStream: (stream) => {
    set({ remoteStream: stream });
  },

  _isCallLikelyAlive: () => {
    const { callState, gatewayClient, localStream, remoteStream } = get();
    const callStateActive =
      callState === CallState.INCALL ||
      callState === CallState.CALLING ||
      callState === CallState.RINGING ||
      callState === CallState.CONNECTING;

    const hasSessionId = !!gatewayClient?.getSessionId();
    const hasLiveLocalTrack = !!localStream
      ?.getTracks()
      .some((track) => track.readyState === 'live');
    const hasLiveRemoteTrack = !!remoteStream
      ?.getTracks()
      .some((track) => track.readyState === 'live');

    return (
      callStateActive || hasSessionId || hasLiveLocalTrack || hasLiveRemoteTrack
    );
  },

  _clearForegroundRecoveryTimers: () => {
    const { _foregroundRecoveryTimer, _foregroundRecoveryRetryTimer } = get();

    if (_foregroundRecoveryTimer) {
      clearTimeout(_foregroundRecoveryTimer);
    }

    if (_foregroundRecoveryRetryTimer) {
      clearTimeout(_foregroundRecoveryRetryTimer);
    }

    set({
      _foregroundRecoveryTimer: null,
      _foregroundRecoveryRetryTimer: null,
    });
  },

  _clearForegroundRecoveryRetryTimer: () => {
    const { _foregroundRecoveryRetryTimer } = get();
    if (_foregroundRecoveryRetryTimer) {
      clearTimeout(_foregroundRecoveryRetryTimer);
      set({ _foregroundRecoveryRetryTimer: null });
    }
  },

  _scheduleForegroundRecoveryRetry: (
    savedState,
    recoveryId,
    reason,
    source,
  ) => {
    const state = get();
    const recoveryTag = `[Recovery:${recoveryId}]`;

    if (
      state._recoveryMode !== 'soft_recovering' ||
      state._activeRecoveryId !== recoveryId
    ) {
      return;
    }

    const startedAt = state._foregroundRecoveryStartedAt ?? Date.now();
    const elapsed = Date.now() - startedAt;
    const attempts = state._foregroundRecoveryAttempts;

    if (elapsed >= FOREGROUND_RECOVERY_GRACE_MS) {
      console.log(
        `[SipStore] ${recoveryTag} grace_expired_no_retry (${source})`,
        { reason, elapsedMs: elapsed },
      );
      return;
    }

    const nextAttempt = attempts + 1;
    const nextOffset = FOREGROUND_RECOVERY_RETRY_OFFSETS_MS[nextAttempt];
    if (nextOffset === undefined) {
      console.log(`[SipStore] ${recoveryTag} no_more_retry_slots (${source})`, {
        reason,
        attempts,
      });
      return;
    }

    const remaining = FOREGROUND_RECOVERY_GRACE_MS - elapsed;
    const delay = Math.max(0, Math.min(nextOffset - elapsed, remaining));

    state._clearForegroundRecoveryRetryTimer();
    console.log(
      `[SipStore] ${recoveryTag} scheduling_retry_${nextAttempt} in ${delay}ms`,
      {
        source,
        reason,
        elapsedMs: elapsed,
      },
    );

    const retryTimer = setTimeout(() => {
      const current = get();
      if (
        current._recoveryMode !== 'soft_recovering' ||
        current._activeRecoveryId !== recoveryId
      ) {
        return;
      }

      if (!current._isCallLikelyAlive()) {
        console.log(`[SipStore] ${recoveryTag} retry aborted - call not alive`);
        return;
      }

      set({ _foregroundRecoveryAttempts: nextAttempt });
      void current.handleCallResume(savedState, recoveryId).catch((error) => {
        console.error(`[SipStore] ${recoveryTag} retry attempt failed:`, error);
      });
    }, delay);

    set({ _foregroundRecoveryRetryTimer: retryTimer });
  },

  _terminateCallAfterRecoveryFailure: async (reason, context) => {
    const { _recoveryTerminationInProgress } = get();
    const recoveryId = context?.recoveryId;
    const recoveryTag = recoveryId ? `[Recovery:${recoveryId}] ` : '';

    if (_recoveryTerminationInProgress) {
      console.log(
        `[SipStore] ${recoveryTag}Recovery termination already in progress - skipping duplicate request`,
      );
      return;
    }

    get()._clearForegroundRecoveryTimers();
    set({
      _recoveryTerminationInProgress: true,
      _recoveryMode: 'hard_terminating',
    });
    console.log(
      `[SipStore] ${recoveryTag}Force terminating call after recovery failure`,
      {
        source: context?.source ?? 'unknown',
        reason,
      },
    );

    try {
      const { gatewayClient } = get();
      const sessionId = gatewayClient?.getSessionId();

      if (gatewayClient && sessionId) {
        try {
          gatewayClient.hangup();
          console.log(
            `[SipStore] ${recoveryTag}Hangup sent for sessionId:`,
            sessionId,
          );
        } catch (error) {
          console.error(
            `[SipStore] ${recoveryTag}Failed to send hangup during recovery termination:`,
            error,
          );
        }
      } else {
        console.log(
          `[SipStore] ${recoveryTag}Skipping hangup (no active session)`,
        );
      }

      await get().disconnect();
      console.log(
        `[SipStore] ${recoveryTag}Disconnect completed after recovery failure`,
      );
    } catch (error) {
      console.error(
        `[SipStore] ${recoveryTag}Disconnect failed during recovery termination:`,
        error,
      );
    } finally {
      safeInCallManager.stop();
      get()._stopCallTimer();
      set({
        callResumePending: false,
        networkReconnecting: false,
        callState: CallState.IDLE,
        remoteNumber: null,
        remoteDisplayName: null,
        isAutoRecovering: false,
        lastRecoverableError: null,
        isMuted: false,
        isSpeaker: false,
        isVideoEnabled: true,
        cameraFacing: 'front',
        localStream: null,
        remoteStream: null,
        _callDirection: null,
        messages: [],
        unreadMessageCount: 0,
        connectionError: reason,
        _activeRecoveryId: null,
        _recoveryTerminationInProgress: false,
        _recoveryMode: 'idle',
        _foregroundRecoveryStartedAt: null,
        _foregroundRecoveryAttempts: 0,
        _publicCallAuthInMemory: null,
      });
    }
  },

  // Start call timer
  _startCallTimer: () => {
    const now = new Date();
    set({ _callStartTime: now, callDuration: 0 });

    const timer = setInterval(() => {
      const { _callStartTime } = get();
      if (_callStartTime) {
        const elapsed = Math.floor(
          (Date.now() - _callStartTime.getTime()) / 1000,
        );
        set({ callDuration: elapsed });
      }
    }, 1000);

    set({ _callTimer: timer });
  },

  // Stop call timer
  _stopCallTimer: () => {
    const { _callTimer } = get();
    if (_callTimer) {
      clearInterval(_callTimer);
    }
    set({ _callTimer: null, _callStartTime: null, callDuration: 0 });
  },

  // Reset store
  _reset: () => {
    get()._clearForegroundRecoveryTimers();
    get()._stopCallTimer();
    set(initialState);
  },

  // ===== PERMISSION ERROR ACTIONS =====

  /**
   * Set permission error and missing permissions
   */
  setPermissionError: (
    error: string | null,
    missingPerms?: ('camera' | 'microphone')[],
  ) => {
    set({
      permissionError: error,
      missingPermissions: missingPerms || [],
    });
    console.log(
      '[SipStore] Permission error set:',
      error,
      'Missing:',
      missingPerms,
    );
  },

  /**
   * Increment permission retry count
   */
  incrementPermissionRetry: () => {
    const { permissionRetryCount } = get();
    const newCount = permissionRetryCount + 1;
    set({ permissionRetryCount: newCount });
    console.log('[SipStore] Permission retry count:', newCount);
  },

  /**
   * Reset permission retry count (on successful permission grant or app restart)
   */
  resetPermissionRetry: () => {
    set({
      permissionRetryCount: 0,
      permissionError: null,
      missingPermissions: [],
    });
    console.log('[SipStore] Permission retry count reset');
  },

  // ===== NETWORK RECONNECT ACTIONS =====

  /**
   * Setup network monitor handlers
   * Called once during app initialization
   */
  setupNetworkMonitor: () => {
    get().resetCallRuntimeState();
    const networkMonitor = getNetworkMonitor();

    // Set reconnect handler - called when network changes
    networkMonitor.setReconnectHandler(async () => {
      const { gatewayClient, callState, remoteNumber } = get();

      // Save call state before reconnecting
      const isInCall =
        callState === CallState.INCALL ||
        callState === CallState.CALLING ||
        callState === CallState.RINGING;
      if (isInCall && gatewayClient) {
        const sessionId = gatewayClient.getSessionId();
        networkMonitor.saveCallState(sessionId, remoteNumber, isInCall);

        // CRITICAL: Set networkReconnecting BEFORE forceDisconnect to prevent race condition
        // When WiFi disconnects faster than cellular, onDisconnected fires before the flag check
        // Setting this first ensures the in-call screen stays open during fast network switches
        console.log(
          '[SipStore] Setting networkReconnecting before forceDisconnect',
        );
        set({ networkReconnecting: true });
      }

      // Force disconnect and reconnect
      if (gatewayClient) {
        gatewayClient.forceDisconnect();

        // Wait a bit for the disconnect to complete
        await new Promise((resolve) => setTimeout(resolve, 200));

        // Reconnect - use reconnectForNetworkChange to preserve localStream
        await gatewayClient.reconnectForNetworkChange();
      }

      // Only clear networkReconnecting if we weren't in a call
      // For calls, onCallResumed will clear it
      if (!isInCall) {
        set({ networkReconnecting: false });
      }
    });

    // Set call resume handler - called after successful reconnection if there was an active call
    networkMonitor.setResumeCallHandler(async (savedState: SavedCallState) => {
      await get().handleCallResume(savedState);
    });

    console.log('[SipStore] Network monitor handlers configured');
  },

  /**
   * Handle app foreground recovery (when app wakes after long sleep/lock)
   * This path is independent from NetInfo changes and prevents missed in-call resume.
   */
  handleAppForegroundRecovery: async () => {
    const {
      gatewayClient,
      callState,
      remoteNumber,
      localStream,
      remoteStream,
      _recoveryTerminationInProgress,
      _recoveryMode,
    } = get();

    if (!gatewayClient?.isConnected) {
      const hasStaleRuntimeState =
        callState !== CallState.IDLE ||
        !!remoteNumber ||
        !!localStream ||
        !!remoteStream ||
        get().isAutoRecovering ||
        !!get()._publicCallAuthInMemory;
      if (hasStaleRuntimeState) {
        console.log(
          '[SipStore] Foreground stale state detected without active WebSocket - hard reset',
        );
        get().resetCallRuntimeState();
      }
    }

    const isInCall =
      callState === CallState.INCALL ||
      callState === CallState.CALLING ||
      callState === CallState.RINGING ||
      callState === CallState.CONNECTING;

    if (!isInCall) {
      return;
    }

    if (!gatewayClient) {
      console.log('[SipStore] Foreground recovery skipped - no gateway client');
      return;
    }

    if (_recoveryTerminationInProgress) {
      console.log(
        '[SipStore] Foreground recovery skipped - termination already in progress',
      );
      return;
    }

    if (_recoveryMode !== 'idle') {
      console.log(
        '[SipStore] Foreground recovery skipped - recovery already in progress',
      );
      return;
    }

    const sessionId = gatewayClient.getSessionId();
    if (!sessionId) {
      console.log('[SipStore] Foreground recovery skipped - no sessionId');
      return;
    }

    const recoveryId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    const recoveryTag = `[Recovery:${recoveryId}]`;
    const startedAt = Date.now();
    console.log(`[SipStore] ${recoveryTag} enter_soft_recovery`);
    get()._clearForegroundRecoveryTimers();
    set({
      networkReconnecting: true,
      _activeRecoveryId: recoveryId,
      _recoveryMode: 'soft_recovering',
      _foregroundRecoveryStartedAt: startedAt,
      _foregroundRecoveryAttempts: 0,
      callResumePending: false,
      connectionError: null,
    });

    const watchdogTimer = setTimeout(() => {
      const current = get();
      if (
        current._activeRecoveryId !== recoveryId ||
        current._recoveryMode !== 'soft_recovering'
      ) {
        return;
      }

      if (current._isCallLikelyAlive()) {
        console.log(`[SipStore] ${recoveryTag} grace_expired_alive`);
        current._clearForegroundRecoveryTimers();
        set({
          _recoveryMode: 'idle',
          _activeRecoveryId: null,
          _foregroundRecoveryStartedAt: null,
          _foregroundRecoveryAttempts: 0,
          networkReconnecting: false,
          callResumePending: false,
        });
        return;
      }

      console.log(`[SipStore] ${recoveryTag} grace_expired_terminate`);
      void current
        ._terminateCallAfterRecoveryFailure(
          'Call dropped - could not recover after app resumed',
          {
            recoveryId,
            source: 'grace_expired_terminate',
          },
        )
        .catch((error) => {
          console.error(
            '[SipStore] Failed to terminate after grace expiry:',
            error,
          );
        });
    }, FOREGROUND_RECOVERY_GRACE_MS);
    set({ _foregroundRecoveryTimer: watchdogTimer });

    try {
      if (!gatewayClient.isConnected) {
        console.log(
          `[SipStore] ${recoveryTag} Gateway not connected - reconnecting before resume`,
        );
        await gatewayClient.reconnectForNetworkChange();
      }

      const localVideoRecovery =
        await gatewayClient.ensureLocalVideoBeforeForegroundResume(
          'app_foreground',
        );
      console.log(
        `[SipStore] ${recoveryTag} Refreshing remote video after foreground`,
      );
      gatewayClient.refreshRemoteVideo('app_foreground');

      const transportSnapshot =
        gatewayClient.getPeerConnectionTransportSnapshot();
      const hasSessionId = !!gatewayClient.getSessionId();
      const hasLiveLocalTrack = !!localStream
        ?.getTracks()
        .some((track) => track.readyState === 'live');
      const hasLiveRemoteTrack = !!remoteStream
        ?.getTracks()
        .some((track) => track.readyState === 'live');
      const hasLiveMedia = hasLiveLocalTrack || hasLiveRemoteTrack;
      const pcState = transportSnapshot.connectionState;
      const iceState = transportSnapshot.iceConnectionState;
      const transportLikelyAlive =
        pcState === 'new' ||
        pcState === 'connecting' ||
        pcState === 'connected' ||
        iceState === 'checking' ||
        iceState === 'connected' ||
        iceState === 'completed';
      const shouldSkipResumeBase =
        gatewayClient.isConnected &&
        hasSessionId &&
        hasLiveMedia &&
        transportLikelyAlive;
      const localVideoRecoveryExhausted =
        localVideoRecovery.status === 'exhausted';
      let forceResumeForLocalStall = false;

      if (localVideoRecoveryExhausted) {
        const now = Date.now();
        const elapsedSinceLastForcedResume = now - forcedResumeLocalStallLastAt;
        if (
          elapsedSinceLastForcedResume < FORCED_RESUME_LOCAL_STALL_COOLDOWN_MS
        ) {
          console.log(
            `[SipStore] ${recoveryTag} resume_required_local_stall cooldown`,
            {
              elapsedSinceLastForcedResume,
              cooldownMs: FORCED_RESUME_LOCAL_STALL_COOLDOWN_MS,
              senderSummary: localVideoRecovery.senderSummary,
            },
          );
        } else {
          if (
            now - forcedResumeLocalStallWindowStartedAt >
            FORCED_RESUME_LOCAL_STALL_WINDOW_MS
          ) {
            forcedResumeLocalStallWindowStartedAt = now;
            forcedResumeLocalStallAttemptsInWindow = 0;
          }

          if (
            forcedResumeLocalStallAttemptsInWindow >=
            FORCED_RESUME_LOCAL_STALL_MAX_ATTEMPTS
          ) {
            console.log(
              `[SipStore] ${recoveryTag} resume_required_local_stall max_attempts_exceeded`,
              {
                attempts: forcedResumeLocalStallAttemptsInWindow,
                windowMs: FORCED_RESUME_LOCAL_STALL_WINDOW_MS,
                senderSummary: localVideoRecovery.senderSummary,
              },
            );
            await get()._terminateCallAfterRecoveryFailure(
              'Local video sender stalled after repeated forced resumes',
              {
                recoveryId,
                source: 'forced_resume_local_stall_max_attempts',
              },
            );
            return;
          }

          forcedResumeLocalStallAttemptsInWindow += 1;
          forcedResumeLocalStallLastAt = now;
          forceResumeForLocalStall = true;
          console.log(`[SipStore] ${recoveryTag} resume_required_local_stall`, {
            attempt: forcedResumeLocalStallAttemptsInWindow,
            maxAttempts: FORCED_RESUME_LOCAL_STALL_MAX_ATTEMPTS,
            senderSummary: localVideoRecovery.senderSummary,
          });
        }
      } else {
        forcedResumeLocalStallAttemptsInWindow = 0;
        forcedResumeLocalStallWindowStartedAt = 0;
      }

      const shouldSkipResume =
        shouldSkipResumeBase && !forceResumeForLocalStall;

      console.log(`[SipStore] ${recoveryTag} foreground_transport_check`, {
        hasSessionId,
        hasLiveLocalTrack,
        hasLiveRemoteTrack,
        hasPeerConnection: transportSnapshot.hasPeerConnection,
        connectionState: pcState,
        iceConnectionState: iceState,
        localVideoRecoveryStatus: localVideoRecovery.status,
        localVideoRecoverySenderSummary: localVideoRecovery.senderSummary,
        forceResumeForLocalStall,
        shouldSkipResume,
      });

      if (shouldSkipResume) {
        console.log(`[SipStore] ${recoveryTag} resume_skip_healthy`);
        get()._clearForegroundRecoveryTimers();
        set({
          _recoveryMode: 'idle',
          _activeRecoveryId: null,
          _foregroundRecoveryStartedAt: null,
          _foregroundRecoveryAttempts: 0,
          networkReconnecting: false,
          callResumePending: false,
          connectionError: null,
        });
        return;
      }

      if (forceResumeForLocalStall) {
        console.log(`[SipStore] ${recoveryTag} resume_forced_local_stall`);
      }

      await get().handleCallResume(
        {
          sessionId,
          remoteNumber,
          wasInCall: true,
          timestamp: Date.now(),
        },
        recoveryId,
        {
          bypassRegistrationWait: forceResumeForLocalStall,
          source: forceResumeForLocalStall
            ? 'forced_local_stall'
            : 'foreground_recovery',
        },
      );
    } catch (error) {
      console.error(
        `[SipStore] ${recoveryTag} foreground_recovery_exception:`,
        error,
      );
      const current = get();
      const savedState: SavedCallState = {
        sessionId: gatewayClient.getSessionId() ?? sessionId,
        remoteNumber,
        wasInCall: true,
        timestamp: Date.now(),
      };
      current._scheduleForegroundRecoveryRetry(
        savedState,
        recoveryId,
        'Foreground recovery exception',
        'foreground_recovery_exception',
      );
    }
  },

  /**
   * Handle network-triggered reconnection
   */
  handleNetworkReconnect: async () => {
    const { gatewayClient } = get();
    if (!gatewayClient) {
      console.log('[SipStore] No gateway client - cannot reconnect');
      return;
    }

    set({ networkReconnecting: true });

    try {
      gatewayClient.forceDisconnect();
      await new Promise((resolve) => setTimeout(resolve, 200));
      await gatewayClient.reconnect();
    } catch (error) {
      console.error('[SipStore] Network reconnect failed:', error);
    } finally {
      set({ networkReconnecting: false });
    }
  },

  /**
   * Handle call resumption after network reconnect
   */
  handleCallResume: async (
    savedState: SavedCallState,
    recoveryId?: string,
    options?: { bypassRegistrationWait?: boolean; source?: string },
  ) => {
    const { gatewayClient } = get();
    const activeRecoveryId =
      recoveryId ??
      get()._activeRecoveryId ??
      `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    const recoveryTag = `[Recovery:${activeRecoveryId}]`;

    if (get()._recoveryTerminationInProgress) {
      console.log(
        `[SipStore] ${recoveryTag} Resume skipped - termination already in progress`,
      );
      return;
    }

    if (!gatewayClient) {
      console.log(
        `[SipStore] ${recoveryTag} No gateway client - cannot resume call`,
      );
      return;
    }

    if (get()._recoveryMode !== 'soft_recovering') {
      set({
        _recoveryMode: 'soft_recovering',
        _foregroundRecoveryStartedAt: Date.now(),
        _foregroundRecoveryAttempts: 0,
      });
    }

    const currentAttempt = get()._foregroundRecoveryAttempts;
    const resumeAttemptStartedAt = Date.now();
    const resumeSource = options?.source ?? 'default';
    set({
      callResumePending: true,
      _activeRecoveryId: activeRecoveryId,
      _resumeAttemptStartedAt: resumeAttemptStartedAt,
      _resumeAttemptSource: resumeSource,
    });
    console.log(`[SipStore] ${recoveryTag} resume_attempt_${currentAttempt}`, {
      hasSessionId: !!savedState.sessionId,
      remoteNumber: savedState.remoteNumber,
      source: resumeSource,
      bypassRegistrationWait: options?.bypassRegistrationWait ?? false,
    });

    try {
      // Wait for registration to complete (with timeout)
      const waitForRegistration = async (
        timeoutMs: number,
      ): Promise<boolean> => {
        const startTime = Date.now();
        while (Date.now() - startTime < timeoutMs) {
          if (gatewayClient.isRegisteredState) {
            return true;
          }
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
        return false;
      };

      const shouldBypassRegistrationWait =
        options?.bypassRegistrationWait === true ||
        resumeSource === 'foreground_recovery';

      const registered = shouldBypassRegistrationWait
        ? true
        : await waitForRegistration(RESUME_REGISTRATION_WAIT_TIMEOUT_MS);
      console.log(
        `[SipStore] ${recoveryTag} registration_wait_ms=${Date.now() - resumeAttemptStartedAt} registered=${registered} bypassed=${shouldBypassRegistrationWait}`,
      );

      if (!registered) {
        console.log(
          `[SipStore] ${recoveryTag} resume_failed_soft registration_timeout`,
        );
        set({
          callResumePending: false,
          _resumeAttemptStartedAt: null,
          _resumeAttemptSource: null,
        });
        get()._scheduleForegroundRecoveryRetry(
          savedState,
          activeRecoveryId,
          'Not registered',
          'registration_timeout',
        );
        return;
      }

      // Try to resume with session ID
      if (savedState.sessionId) {
        console.log(
          `[SipStore] ${recoveryTag} Attempting call resume with sessionId:`,
          savedState.sessionId,
        );
        await gatewayClient.resumeCall(savedState.sessionId);

        // Wait for resumed/resume_failed response (timeout after configured threshold)
        await new Promise((resolve) =>
          setTimeout(resolve, RESUME_RESPONSE_WAIT_TIMEOUT_MS),
        );

        // If still pending, resume failed - end session instead of callback
        const currentState = get();
        const stillPendingForThisRecovery =
          currentState.callResumePending &&
          currentState._activeRecoveryId === activeRecoveryId &&
          !currentState._recoveryTerminationInProgress &&
          currentState._recoveryMode === 'soft_recovering';
        if (stillPendingForThisRecovery) {
          console.log(
            `[SipStore] ${recoveryTag} resume_failed_soft resume_timeout`,
          );
          set({
            callResumePending: false,
            _resumeAttemptStartedAt: null,
            _resumeAttemptSource: null,
          });
          currentState._scheduleForegroundRecoveryRetry(
            savedState,
            activeRecoveryId,
            'Resume timeout',
            'resume_timeout',
          );
        }
      } else {
        console.log(
          `[SipStore] ${recoveryTag} resume_failed_soft missing_session_id`,
        );
        set({
          callResumePending: false,
          _resumeAttemptStartedAt: null,
          _resumeAttemptSource: null,
        });
        if (get()._isCallLikelyAlive()) {
          get()._scheduleForegroundRecoveryRetry(
            savedState,
            activeRecoveryId,
            'No sessionId',
            'missing_session_id',
          );
          return;
        }
        await get()._terminateCallAfterRecoveryFailure(
          'Call dropped - no session to resume',
          {
            recoveryId: activeRecoveryId,
            source: 'missing_session_id_dead',
          },
        );
      }
    } catch (error) {
      console.error(
        `[SipStore] ${recoveryTag} resume_failed_soft resume_exception:`,
        error,
      );
      set({
        callResumePending: false,
        _resumeAttemptStartedAt: null,
        _resumeAttemptSource: null,
      });
      if (get()._isCallLikelyAlive()) {
        get()._scheduleForegroundRecoveryRetry(
          savedState,
          activeRecoveryId,
          'Resume exception',
          'resume_exception',
        );
        return;
      }
      await get()._terminateCallAfterRecoveryFailure(
        'Call dropped due to network change',
        {
          recoveryId: activeRecoveryId,
          source: 'resume_exception_dead',
        },
      );
    }
  },

  // ===== SOFT-PHONE SPECIFIC ACTIONS =====

  // Resolve SIP trunk from current SIP config
  resolveTrunk: () => {
    const { gatewayClient, sipConfig } = get();
    if (!gatewayClient) {
      console.warn('[SipStore] Cannot resolve trunk: not connected');
      return;
    }
    if (!sipConfig) {
      console.warn('[SipStore] Cannot resolve trunk: no SIP config');
      return;
    }

    set({ trunkResolveStatus: 'resolving', trunkResolveError: null });

    try {
      gatewayClient.resolveTrunk({
        sipDomain: sipConfig.sipDomain,
        sipUsername: sipConfig.sipUsername,
        sipPassword: sipConfig.sipPassword,
        sipPort: sipConfig.sipPort,
      });
    } catch (error) {
      console.error('[SipStore] resolveTrunk error:', error);
      set({
        trunkResolveStatus: 'failed',
        trunkResolveError:
          error instanceof Error ? error.message : 'Unknown error',
      });
    }
  },

  // Clear trunk resolve state
  clearTrunkResolveState: () => {
    set({
      trunkResolveStatus: 'idle',
      trunkResolveError: null,
      resolvedTrunkId: null,
    });
  },

  // Answer incoming call
  answer: async () => {
    const { gatewayClient } = get();
    if (!gatewayClient) {
      console.warn('[SipStore] Cannot answer: not connected');
      return;
    }

    try {
      await gatewayClient.answer();
      set({ incomingCall: null });
      reportCallAnswered();
      safeInCallManager.start({ media: 'audio' });
      get()._startCallTimer();
    } catch (error) {
      console.error('[SipStore] Answer failed:', error);
      set({ incomingCall: null, callState: CallState.IDLE, _callDirection: null });
    }
  },

  // Decline incoming call
  decline: () => {
    const { gatewayClient } = get();
    if (!gatewayClient) {
      console.warn('[SipStore] Cannot decline: not connected');
      return;
    }

    gatewayClient.decline();
    set({ incomingCall: null, callState: CallState.IDLE, _callDirection: null });
    reportCallEnded();
  },

  // Update outgoing RTT draft
  updateOutgoingRttDraft: (text: string) => {
    set({ outgoingRttDraft: text });
  },

  // Clear remote RTT preview
  clearRemoteRttPreview: () => {
    set({ remoteRttPreview: '' });
  },
}));

// Convenience hook
export const useSip = useSipStore;
