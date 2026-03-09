/**
 * Gateway Types
 * Types for K2 Gateway WebSocket communication
 */

// WebSocket message types sent TO the server
export interface RegisterMessage {
  type: 'register';
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipPort?: number;
}

export interface CallMessage {
  type: 'call';
  sessionId: string;
  destination: string;
  from?: string;
  // Public mode fields
  sipDomain?: string;
  sipUsername?: string;
  sipPassword?: string;
  sipPort?: number;
  // Trunk mode field
  trunkId?: number;
  trunkPublicId?: string;
}

export interface TrunkResolveMessage {
  type: 'trunk_resolve';
  trunkId?: number;
  trunkPublicId?: string;
  sipDomain?: string;
  sipUsername?: string;
  sipPassword?: string;
  sipPort?: number;
}

export interface AnswerMessage {
  type: 'answer';
  sdp: string;
}

export interface HangupMessage {
  type: 'hangup';
  sessionId?: string;
}

export interface AcceptMessage {
  type: 'accept';
  sessionId?: string;
}

export interface RejectMessage {
  type: 'reject';
  sessionId?: string;
  reason?: string;
}

export interface IceCandidateMessage {
  type: 'ice';
  candidate: RTCIceCandidateInit;
}

export interface OfferMessage {
  type: 'offer';
  sdp: string;
}

export interface UnregisterMessage {
  type: 'unregister';
}

export interface PingMessage {
  type: 'ping';
}

// DTMF - Send DTMF digits during call
export interface DtmfMessage {
  type: 'dtmf';
  sessionId: string;
  digits: string;
}

// SIP MESSAGE - Send text message during call
export interface SendMessageMessage {
  type: 'send_message';
  body: string;
  contentType?: string; // Default: 'text/plain;charset=UTF-8'
}

// Resume call after network change
export interface ResumeMessage {
  type: 'resume';
  sessionId: string;
  sdp?: string; // SDP offer for PeerConnection renegotiation
}

export interface RequestKeyframeMessage {
  type: 'request_keyframe';
  sessionId: string;
}

export type OutgoingMessage =
  | RegisterMessage
  | CallMessage
  | TrunkResolveMessage
  | AnswerMessage
  | HangupMessage
  | AcceptMessage
  | RejectMessage
  | IceCandidateMessage
  | OfferMessage
  | UnregisterMessage
  | PingMessage
  | DtmfMessage
  | SendMessageMessage
  | ResumeMessage
  | RequestKeyframeMessage;

// WebSocket message types received FROM the server
export interface RegisteredResponse {
  type: 'registered';
  username?: string;
}

export interface UnregisteredResponse {
  type: 'unregistered';
}

export interface RingingResponse {
  type: 'ringing';
}

export interface AnsweredResponse {
  type: 'answered';
  sdp: string;
}
export interface AnswerResponse {
  type: 'answer';
  sdp: string;
}
export interface StateResponse {
  type: 'state';
  state: string;
}

export interface IncomingCallResponse {
  type: 'incoming';
  caller: string;
  from?: string; // Alternative field name for caller
  to?: string;
  sessionId?: string;
  sdp?: string;
}

export interface EndedResponse {
  type: 'ended';
  reason?: string;
}

export interface IceResponse {
  type: 'ice';
  candidate: RTCIceCandidateInit;
}

export interface ErrorResponse {
  type: 'error';
  code?: number;
  message?: string;
  error?: string; // Server may send error in this field
}

export interface PongResponse {
  type: 'pong';
}

export interface RegisterStatusResponse {
  type: 'registerStatus';
  registered: boolean;
  sipDomain?: string;
  trunkId?: number;
  trunkid?: number;
  trunk_id?: number;
}

export interface TrunkResolvedResponse {
  type: 'trunk_resolved';
  trunkId?: number;
  trunkPublicId?: string;
}

export interface TrunkRedirectResponse {
  type: 'trunk_redirect';
  redirectUrl?: string;
}

export interface TrunkNotFoundResponse {
  type: 'trunk_not_found';
  reason?: string;
}

export interface TrunkNotReadyResponse {
  type: 'trunk_not_ready';
  reason?: string;
}

// SIP MESSAGE - Incoming text message
export interface MessageResponse {
  type: 'message';
  from: string;
  body: string;
  contentType?: string;
}

export interface GatewayRttMessage {
  via: 'datachannel' | 'sip';
  data: string | Uint8Array;
  contentType?: string;
}

// Call resume response after network change
export interface ResumedResponse {
  type: 'resumed';
  sessionId?: string;
  sdp?: string; // SDP answer for PeerConnection renegotiation
}

export interface ResumeFailedResponse {
  type: 'resume_failed';
  reason?: string;
}

export type IncomingMessage =
  | RegisteredResponse
  | UnregisteredResponse
  | RingingResponse
  | AnsweredResponse
  | AnswerResponse
  | StateResponse
  | IncomingCallResponse
  | EndedResponse
  | IceResponse
  | ErrorResponse
  | PongResponse
  | RegisterStatusResponse
  | TrunkResolvedResponse
  | TrunkRedirectResponse
  | TrunkNotFoundResponse
  | TrunkNotReadyResponse
  | MessageResponse
  | ResumedResponse
  | ResumeFailedResponse;

// ===== CONNECTION STATE =====
export type ConnectionState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting';
export type RecoveryState = 'retrying_public_identity' | 'retry_failed';
export type RecoverableGatewayErrorCode = 'PUBLIC_IDENTITY_CHANGED';

// ===== RECONNECTION CONFIG =====
export interface ReconnectConfig {
  maxAttempts: number; // Default: 5
  baseDelay: number; // Base delay in ms (default: 1000)
  maxDelay: number; // Max delay in ms (default: 30000)
  backoffMultiplier: number; // Exponential multiplier (default: 2)
}

export const DEFAULT_RECONNECT_CONFIG: ReconnectConfig = {
  maxAttempts: 5,
  baseDelay: 1000,
  maxDelay: 30000,
  backoffMultiplier: 2,
};

// Gateway client state
export enum GatewayState {
  DISCONNECTED = 'disconnected',
  CONNECTING = 'connecting',
  CONNECTED = 'connected',
  REGISTERING = 'registering',
  REGISTERED = 'registered',
  CALLING = 'calling',
  RINGING = 'ringing',
  INCALL = 'incall',
  INCOMING = 'incoming',
}

// Call state (compatible with existing UI)
export enum CallState {
  IDLE = 'idle',
  CALLING = 'calling',
  RINGING = 'ringing',
  CONNECTING = 'connecting',
  INCALL = 'incall',
  INCOMING = 'incoming',
  ENDED = 'ended',
}

// Gateway configuration
export interface GatewayConfig {
  serverUrl: string;
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipPort?: number;
  turnUrl?: string;
  turnUsername?: string;
  turnPassword?: string;
}

// Incoming call info
export interface IncomingCallInfo {
  caller: string;
  displayName?: string;
  sessionId: string | null;
  to?: string;
}

// Gateway event callbacks
// Note: Using 'any' for MediaStream to avoid type conflicts with react-native-webrtc
export interface GatewayCallbacks {
  onConnected?: () => void;
  onDisconnected?: (reason?: string) => void;
  onRegistered?: (username: string) => void;
  onRegisterStatus?: (status: RegisterStatusResponse) => void;
  onTrunkResolved?: (payload: {
    trunkId?: number;
    trunkPublicId?: string;
  }) => void;
  onTrunkRedirect?: (redirectUrl: string) => void;
  onTrunkResolveFailed?: (
    reason: string,
    type: 'trunk_not_found' | 'trunk_not_ready' | 'trunk_redirect' | 'error',
  ) => void;
  onUnregistered?: () => void;
  onRegistrationFailed?: (error: string) => void;
  onCalling?: () => void;
  onRinging?: () => void;
  onAnswered?: () => void;
  onIncomingCall?: (info: IncomingCallInfo, sdp?: string) => void;
  onCallEnded?: (reason?: string) => void;
  onError?: (error: string) => void;
  onLocalStream?: (stream: any) => void;
  onRemoteStream?: (stream: any) => void;
  // SIP MESSAGE callback
  onMessage?: (from: string, body: string, contentType?: string) => void;
  // Reconnection callbacks
  onConnectionStateChange?: (state: ConnectionState) => void;
  onReconnecting?: (attempt: number, maxAttempts: number) => void;
  onReconnectFailed?: () => void;
  onRecoveryState?: (state: RecoveryState) => void;
  // Call resume callbacks (for network change recovery)
  onCallResumed?: (sessionId: string) => void;
  onCallResumeFailed?: (reason: string) => void;
}
