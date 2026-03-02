/**
 * Gateway Client
 * WebSocket-based WebRTC client for K2 Gateway
 * Based on webrtc-gateway-sip approach
 */

import {
  MediaStream,
  RTCIceCandidate,
  RTCPeerConnection,
  RTCSessionDescription,
  mediaDevices,
} from 'react-native-webrtc';
import type RtcMessageEvent from 'react-native-webrtc/lib/typescript/MessageEvent';
import { Platform } from 'react-native';

import { VIDEO_BITRATE_KBPS } from '@/constants/webrtc';
import { ensureMediaPermissions } from '@/lib/request-permissions';
import {
  getBitrateForResolution,
  getGatewayServerFromEnv,
  getSettingsSync,
} from '@/store/settings-store';

import {
  CallState,
  ConnectionState,
  DEFAULT_RECONNECT_CONFIG,
  GatewayCallbacks,
  GatewayConfig,
  GatewayRttMessage,
  IncomingMessage,
  MessageResponse,
  OutgoingMessage,
  ReconnectConfig,
  RecoverableGatewayErrorCode,
} from './types';

type RtcDataChannel = ReturnType<RTCPeerConnection['createDataChannel']>;
type LocalRtpSender = ReturnType<RTCPeerConnection['getSenders']>[number];

/**
 * Strip port from SIP domain for register payload.
 * Handles hostname:port, IPv4:port, and [IPv6]:port.
 */
function stripPortFromDomain(domain: string): string {
  if (!domain) {
    return domain;
  }

  // [IPv6]:port -> IPv6
  if (domain.startsWith('[') && domain.includes(']')) {
    const closingIndex = domain.indexOf(']');
    const hostPart = domain.substring(1, closingIndex);
    return hostPart;
  }

  // hostname:port or IPv4:port -> hostname / IPv4
  if (/:[0-9]+$/.test(domain)) {
    return domain.replace(/:[0-9]+$/, '');
  }

  return domain;
}

type RtcDataChannelWithEventListeners = RtcDataChannel & {
  addEventListener: (type: string, listener: (event: unknown) => void) => void;
};

function hasAddEventListener(
  channel: RtcDataChannel,
): channel is RtcDataChannelWithEventListeners {
  return (
    typeof channel === 'object' &&
    channel !== null &&
    'addEventListener' in channel &&
    typeof (channel as Record<string, unknown>).addEventListener === 'function'
  );
}

// Call auth types
export interface PublicCallAuth {
  mode: 'public';
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipPort: number;
  from?: string;
}

export interface TrunkCallAuth {
  mode: 'siptrunk';
  trunkId: number;
  from?: string;
}

export type CallAuth = PublicCallAuth | TrunkCallAuth;

export type LocalVideoRecoveryStatus =
  | 'not_ios'
  | 'healthy'
  | 'recovered'
  | 'exhausted'
  | 'error';
export interface LocalVideoRecoveryResult {
  status: LocalVideoRecoveryStatus;
  reason: string;
  senderSummary?: string;
}

const PUBLIC_IDENTITY_CHANGED_ERROR_MESSAGE =
  'Public SIP identity changed (username/domain). Send a new offer to create a new session.';
const LOCAL_VIDEO_RECOVERY_THROTTLE_MS = 2000;
const LOCAL_VIDEO_SENDER_HEALTH_SAMPLE_INTERVAL_MS = 350;
const LOCAL_VIDEO_RECOVERY_MAX_ATTEMPTS = 2;
const LOCAL_VIDEO_SENDER_RESET_DELAY_MS = 120;
const LOCAL_VIDEO_POST_REPLACE_WARMUP_MS = 700;
const LOCAL_VIDEO_ENABLE_TOGGLE_DELAY_MS = 80;
const DEFAULT_ICE_GATHER_TIMEOUT_MS = 3000;
const RESUME_ICE_GATHER_TIMEOUT_MS = 1800;

interface PendingCallIntent {
  destination: string;
  auth: CallAuth | null;
  attempt: number;
}

/**
 * SDP Manipulation Utilities for Linphone Compatibility
 * Forces H264 Baseline Profile (42e01f) which is compatible with Linphone
 * and sets appropriate video bitrate
 */

/**
 * Prefer H264 codec and remove other video codecs (VP8, VP9, AV1)
 * This ensures Linphone receives H264-only which it handles well
 */
function preferH264InSdp(sdp: string): string {
  const lines = sdp.split('\r\n');
  const result: string[] = [];

  let inVideoSection = false;
  const h264PayloadTypes: string[] = [];
  const otherPayloadTypes: string[] = [];
  const rtpMapLines: Map<string, string> = new Map();
  const fmtpLines: Map<string, string> = new Map();

  // First pass: collect payload type information
  for (const line of lines) {
    if (line.startsWith('m=video')) {
      inVideoSection = true;
    } else if (line.startsWith('m=') && !line.startsWith('m=video')) {
      inVideoSection = false;
    }

    if (inVideoSection) {
      // Parse rtpmap lines
      const rtpMapMatch = line.match(/^a=rtpmap:(\d+)\s+(.+)/);
      if (rtpMapMatch) {
        const pt = rtpMapMatch[1];
        const codec = rtpMapMatch[2];
        rtpMapLines.set(pt, line);

        if (codec.toLowerCase().startsWith('h264/')) {
          h264PayloadTypes.push(pt);
        } else if (
          codec.toLowerCase().startsWith('vp8/') ||
          codec.toLowerCase().startsWith('vp9/') ||
          codec.toLowerCase().startsWith('av1/')
        ) {
          otherPayloadTypes.push(pt);
        }
      }

      // Parse fmtp lines
      const fmtpMatch = line.match(/^a=fmtp:(\d+)\s+(.+)/);
      if (fmtpMatch) {
        fmtpLines.set(fmtpMatch[1], line);
      }
    }
  }

  // Second pass: rebuild SDP with H264-only
  inVideoSection = false;
  for (const line of lines) {
    if (line.startsWith('m=video')) {
      inVideoSection = true;

      // Rebuild m=video line with only H264 payload types
      const parts = line.split(' ');
      const newPayloads = parts.slice(3).filter((pt: string) => {
        // Keep if it's H264 or RTX for H264
        if (h264PayloadTypes.includes(pt)) return true;
        // Check if it's RTX for H264
        const fmtp = fmtpLines.get(pt);
        if (fmtp && fmtp.includes('apt=')) {
          const aptMatch = fmtp.match(/apt=(\d+)/);
          if (aptMatch && h264PayloadTypes.includes(aptMatch[1])) return true;
        }
        return false;
      });

      result.push(
        `${parts[0]} ${parts[1]} ${parts[2]} ${newPayloads.join(' ')}`,
      );
      continue;
    } else if (line.startsWith('m=') && !line.startsWith('m=video')) {
      inVideoSection = false;
    }

    if (inVideoSection) {
      // Skip non-H264 rtpmap lines
      const rtpMapMatch = line.match(/^a=rtpmap:(\d+)\s+(.+)/);
      if (rtpMapMatch && otherPayloadTypes.includes(rtpMapMatch[1])) {
        continue;
      }

      // Skip fmtp for non-H264
      const fmtpMatch = line.match(/^a=fmtp:(\d+)\s+(.+)/);
      if (fmtpMatch && otherPayloadTypes.includes(fmtpMatch[1])) {
        continue;
      }

      // Skip rtcp-fb for non-H264
      const rtcpFbMatch = line.match(/^a=rtcp-fb:(\d+)\s+(.+)/);
      if (rtcpFbMatch && otherPayloadTypes.includes(rtcpFbMatch[1])) {
        continue;
      }
    }

    result.push(line);
  }

  return result.join('\r\n');
}

/**
 * Force H264 Baseline Profile (42e01f) in SDP fmtp lines
 * This ensures compatibility with Linphone which may crash with other profiles
 */
function forceH264BaselineProfile(sdp: string): string {
  // Replace all profile-level-id with Baseline Profile (42e01f)
  // 42 = Baseline profile
  // e0 = level 3.0 constraints
  // 1f = level 3.1
  let result = sdp.replace(
    /profile-level-id=[0-9a-fA-F]{6}/g,
    'profile-level-id=42e01f',
  );

  // Also ensure packetization-mode=1 is present
  result = result.replace(
    /(a=fmtp:\d+\s+.*profile-level-id=42e01f)/g,
    (match) => {
      if (!match.includes('packetization-mode=1')) {
        return match + ';packetization-mode=1';
      }
      return match;
    },
  );

  return result;
}

/**
 * Limit incoming video bandwidth by adding/replacing b=AS parameter in video media section
 * The b=AS parameter specifies the Application Specific Maximum Bandwidth in kbps
 * @param sdp - The SDP string to modify
 * @param bitrateKbps - Bandwidth limit in kilobits per second (default: 1500)
 * @returns Modified SDP string with b=AS parameter set
 */
function limitIncomingVideoBandwidth(
  sdp: string,
  bitrateKbps: number = 1500,
): string {
  const lines = sdp.split('\r\n');
  const result: string[] = [];
  let inVideoSection = false;
  let hasBandwidthLine = false;
  let videoSectionStartIdx = -1;

  // First pass: find video section and check if b=AS already exists
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.startsWith('m=video')) {
      inVideoSection = true;
      videoSectionStartIdx = i;
      hasBandwidthLine = false;
    } else if (line.startsWith('m=') && !line.startsWith('m=video')) {
      inVideoSection = false;
    }

    if (inVideoSection && line.startsWith('b=AS:')) {
      hasBandwidthLine = true;
    }
  }

  // Second pass: rebuild SDP with b=AS parameter
  inVideoSection = false;
  let insertedBandwidth = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    if (line.startsWith('m=video')) {
      inVideoSection = true;
      insertedBandwidth = false;
      result.push(line);
      continue;
    } else if (line.startsWith('m=') && !line.startsWith('m=video')) {
      inVideoSection = false;
    }

    if (inVideoSection) {
      // Replace existing b=AS line
      if (line.startsWith('b=AS:')) {
        result.push(`b=AS:${bitrateKbps}`);
        insertedBandwidth = true;
        console.log(
          `[Gateway] 📊 Replaced bandwidth limit: b=AS:${bitrateKbps} (${bitrateKbps}kbps)`,
        );
        continue;
      }

      // Insert b=AS after connection (c=) line if it exists, otherwise after m=video
      if (!insertedBandwidth && !hasBandwidthLine) {
        // Check if we've passed the connection line or other key attributes
        // Insert after m=video if we haven't seen c= line, or after c= line if we have
        const isConnectionLine = line.startsWith('c=');
        const isAttributeLine = line.startsWith('a=');

        // If we see a connection line, insert after it
        if (isConnectionLine) {
          result.push(line);
          result.push(`b=AS:${bitrateKbps}`);
          insertedBandwidth = true;
          console.log(
            `[Gateway] 📊 Added bandwidth limit: b=AS:${bitrateKbps} (${bitrateKbps}kbps)`,
          );
          continue;
        }

        // If we see an attribute line and haven't inserted yet, insert before first attribute
        if (isAttributeLine && !insertedBandwidth) {
          // Check if there was a connection line before this
          let hasConnectionLine = false;
          for (let j = i - 1; j >= 0 && j >= videoSectionStartIdx; j--) {
            if (lines[j].startsWith('c=')) {
              hasConnectionLine = true;
              break;
            }
          }

          // Insert b=AS before first attribute if connection line exists, otherwise after m=video
          if (
            !hasConnectionLine ||
            (i === videoSectionStartIdx + 1 && !lines[i - 1]?.startsWith('c='))
          ) {
            result.push(`b=AS:${bitrateKbps}`);
            insertedBandwidth = true;
            console.log(
              `[Gateway] 📊 Added bandwidth limit: b=AS:${bitrateKbps} (${bitrateKbps}kbps)`,
            );
          }
        }
      }

      result.push(line);
    } else {
      result.push(line);
    }
  }

  // If video section exists but we never inserted b=AS, add it at the end of video section
  if (
    inVideoSection &&
    !insertedBandwidth &&
    !hasBandwidthLine &&
    videoSectionStartIdx >= 0
  ) {
    // Find where video section ends
    let videoSectionEndIdx = lines.length;
    for (let i = videoSectionStartIdx + 1; i < lines.length; i++) {
      if (lines[i].startsWith('m=')) {
        videoSectionEndIdx = i;
        break;
      }
    }

    // Insert b=AS at the end of video section
    const insertIdx = result.length - (lines.length - videoSectionEndIdx);
    result.splice(insertIdx, 0, `b=AS:${bitrateKbps}`);
    console.log(
      `[Gateway] 📊 Added bandwidth limit at end of video section: b=AS:${bitrateKbps} (${bitrateKbps}kbps)`,
    );
  }

  return result.join('\r\n');
}

/**
 * Process SDP for Linphone compatibility
 * Applies all necessary transformations
 */
function processSDPForLinphone(sdp: string, bitrateKbps?: number): string {
  let processed = sdp;

  // 2. Prefer H264 and remove other video codecs
  processed = preferH264InSdp(processed);

  // 3. Force H264 Baseline profile
  processed = forceH264BaselineProfile(processed);

  // 4. Limit incoming video bandwidth to configured kbps
  processed = limitIncomingVideoBandwidth(
    processed,
    bitrateKbps ?? VIDEO_BITRATE_KBPS,
  );

  console.log('[Gateway] 📝 SDP processed for Linphone compatibility');
  return processed;
}

/**
 * Extract and log codec information from SDP
 * Logs audio and video codecs with their payload types and parameters
 */
function logCodecsFromSdp(sdp: string, context: string): void {
  const lines = sdp.split('\r\n');
  const audioCodecs: { pt: string; codec: string; fmtp?: string }[] = [];
  const videoCodecs: {
    pt: string;
    codec: string;
    fmtp?: string;
    profile?: string;
  }[] = [];

  let inAudioSection = false;
  let inVideoSection = false;
  const fmtpMap = new Map<string, string>();

  // First pass: collect all codecs and fmtp lines
  for (const line of lines) {
    if (line.startsWith('m=audio')) {
      inAudioSection = true;
      inVideoSection = false;
    } else if (line.startsWith('m=video')) {
      inVideoSection = true;
      inAudioSection = false;
    } else if (line.startsWith('m=')) {
      inAudioSection = false;
      inVideoSection = false;
    }

    // Parse rtpmap lines
    const rtpMapMatch = line.match(/^a=rtpmap:(\d+)\s+(.+)/);
    if (rtpMapMatch) {
      const pt = rtpMapMatch[1];
      const codec = rtpMapMatch[2];

      if (inAudioSection) {
        audioCodecs.push({ pt, codec });
      } else if (inVideoSection) {
        videoCodecs.push({ pt, codec });
      }
    }

    // Parse fmtp lines
    const fmtpMatch = line.match(/^a=fmtp:(\d+)\s+(.+)/);
    if (fmtpMatch) {
      fmtpMap.set(fmtpMatch[1], fmtpMatch[2]);
    }
  }

  // Second pass: attach fmtp to codecs
  audioCodecs.forEach((codec) => {
    const fmtp = fmtpMap.get(codec.pt);
    if (fmtp) {
      codec.fmtp = fmtp;
    }
  });

  videoCodecs.forEach((codec) => {
    const fmtp = fmtpMap.get(codec.pt);
    if (fmtp) {
      codec.fmtp = fmtp;
      // Extract H264 profile if present
      const profileMatch = fmtp.match(/profile-level-id=([0-9a-fA-F]{6})/);
      if (profileMatch) {
        const profile = profileMatch[1];
        const profileType = profile.substring(0, 2);
        codec.profile = profile;
        codec.codec += ` (${profileType === '42' ? 'Baseline' : profileType === '4d' ? 'Main' : profileType === '64' ? 'High' : 'Unknown'})`;
      }
    }
  });

  // Log audio codecs
  if (audioCodecs.length > 0) {
    console.log(`[Gateway] 🎵 ${context} - Audio codecs:`);
    audioCodecs.forEach((codec) => {
      const info = `  PT${codec.pt}: ${codec.codec}`;
      console.log(`[Gateway] ${info}${codec.fmtp ? ` (${codec.fmtp})` : ''}`);
    });
  } else {
    console.log(`[Gateway] 🎵 ${context} - No audio codecs found`);
  }

  // Log video codecs
  if (videoCodecs.length > 0) {
    console.log(`[Gateway] 📹 ${context} - Video codecs:`);
    videoCodecs.forEach((codec) => {
      const info = `  PT${codec.pt}: ${codec.codec}`;
      console.log(`[Gateway] ${info}${codec.fmtp ? ` (${codec.fmtp})` : ''}`);
    });
  } else {
    console.log(`[Gateway] 📹 ${context} - No video codecs found`);
  }
}

export class GatewayClient {
  private ws: WebSocket | null = null;
  private pc: RTCPeerConnection | null = null;
  private rttDataChannel: RtcDataChannel | null = null;
  private localStream: MediaStream | null = null;
  private remoteStream: MediaStream | null = null;
  private config: GatewayConfig | null = null;
  private callbacks: GatewayCallbacks = {};
  private pingInterval: ReturnType<typeof setInterval> | null = null;
  private reconnectAttempts = 0;
  private isRegistered = false;
  private pendingCandidates: RTCIceCandidate[] = [];
  private sessionId: string | null = null; // Session ID from server
  private pendingDestination: string | null = null; // Destination to call after session is created
  private pendingCallAuth: CallAuth | null = null; // Call auth to send after session is created
  private pendingCallIntent: PendingCallIntent | null = null; // Last outbound intent for one-shot recovery
  private lastSentMessageType: string = ''; // Track last sent message for debugging
  private rttListeners: ((message: GatewayRttMessage) => void)[] = [];
  private keyframeRetryTimer: ReturnType<typeof setInterval> | null = null;
  private backgroundVideoRecoveryTimer: ReturnType<typeof setInterval> | null =
    null;
  private backgroundVideoRecoveryAttempts = 0;
  private hasReceivedVideoFrame = false;
  private localVideoRecoveryInProgress = false;
  private lastLocalVideoRecoveryAt = 0;

  // State
  private _callState: CallState = CallState.IDLE;
  private _isConnected = false;
  private _isConnecting = false;
  private wasRegisteredBeforeDisconnect = false; // Track if we were registered before disconnect for auto-register on reconnect

  // Reconnection state
  private reconnectConfig: ReconnectConfig = { ...DEFAULT_RECONNECT_CONFIG };
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private connectionState: ConnectionState = 'disconnected';
  private isManualDisconnect = false;
  private lastServerUrl: string = '';

  // Network-triggered disconnect flag (to preserve call state during network switches)
  private _isNetworkTriggeredDisconnect = false;
  private isPublicIdentityRecoveryInProgress = false;

  constructor() {
    console.log('[Gateway] Client initialized');
  }

  // Getters
  get callState(): CallState {
    return this._callState;
  }

  get isConnected(): boolean {
    return this._isConnected;
  }

  get isConnecting(): boolean {
    return this._isConnecting;
  }

  get isRegisteredState(): boolean {
    return this.isRegistered;
  }

  /**
   * Check if the last disconnect was triggered by network change
   * Used by sip-store to preserve call state during network switches
   */
  get isNetworkTriggeredDisconnect(): boolean {
    return this._isNetworkTriggeredDisconnect;
  }

  /**
   * Check if the last disconnect was manual (user-initiated)
   * Used by sip-store to determine if call state should be preserved
   */
  get wasManualDisconnect(): boolean {
    return this.isManualDisconnect;
  }

  /**
   * Clear the network-triggered disconnect flag
   * Should be called after handling the disconnect appropriately
   */
  clearNetworkTriggeredDisconnect(): void {
    this._isNetworkTriggeredDisconnect = false;
  }

  getLocalStream(): MediaStream | null {
    return this.localStream;
  }

  getRemoteStream(): MediaStream | null {
    return this.remoteStream;
  }

  /**
   * Get current session ID (for call resumption after network change)
   */
  getSessionId(): string | null {
    return this.sessionId;
  }

  getPeerConnectionTransportSnapshot(): {
    hasPeerConnection: boolean;
    connectionState: string | null;
    iceConnectionState: string | null;
  } {
    return {
      hasPeerConnection: !!this.pc,
      connectionState: this.pc?.connectionState ?? null,
      iceConnectionState: this.pc?.iceConnectionState ?? null,
    };
  }

  addRttListener(listener: (message: GatewayRttMessage) => void): () => void {
    this.rttListeners.push(listener);
    return () => {
      this.rttListeners = this.rttListeners.filter((item) => item !== listener);
    };
  }

  isRttDataChannelOpen(): boolean {
    return this.rttDataChannel?.readyState === 'open';
  }

  sendRttData(payload: string | Uint8Array): void {
    if (!this.rttDataChannel || this.rttDataChannel.readyState !== 'open') {
      console.warn('[Gateway] RTT DataChannel is not open');
      return;
    }

    if (typeof payload === 'string') {
      this.rttDataChannel.send(payload);
      return;
    }

    // Ensure we send only the intended byte range (avoid SharedArrayBuffer typing + offsets)
    this.rttDataChannel.send(payload.slice());
  }

  /**
   * Force disconnect WebSocket (for network change triggered reconnection)
   * This closes the WebSocket immediately without waiting for timeout
   */
  forceDisconnect(): void {
    console.log('[Gateway] 📡 Force disconnect for network change');
    this._isNetworkTriggeredDisconnect = true; // Mark as network-triggered disconnect
    if (this.ws) {
      this.isManualDisconnect = false; // Allow auto-reconnect
      this.ws.close(1000, 'Network change');
    }
  }

  // Set callbacks
  setCallbacks(callbacks: GatewayCallbacks): void {
    this.callbacks = callbacks;
  }

  // Connect to Gateway server
  async connect(serverUrl?: string): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      console.log('[Gateway] Already connected');
      return;
    }

    const url = serverUrl || getGatewayServerFromEnv();

    if (!url) {
      throw new Error('Gateway server URL is required');
    }

    console.log('[Gateway] Connecting to:', url);
    this._isConnecting = true;
    this.isManualDisconnect = false;
    this.lastServerUrl = url;
    this.setConnectionState('connecting');

    return new Promise((resolve, reject) => {
      try {
        this.ws = new WebSocket(url);

        this.ws.onopen = () => {
          console.log('[Gateway] ✅ Connected');
          this._isConnected = true;
          this._isConnecting = false;
          this.reconnectAttempts = 0;
          this.setConnectionState('connected');
          this.startPing();
          this.callbacks.onConnected?.();

          // Auto-register if we were registered before disconnect (app sleep/websocket drop)
          if (this.wasRegisteredBeforeDisconnect && this.config) {
            console.log('[Gateway] 🔄 Auto-registering after reconnect...');
            this.wasRegisteredBeforeDisconnect = false; // Reset flag
            // Small delay to ensure connection is stable
            setTimeout(() => {
              this.register().catch((err) => {
                console.error('[Gateway] ❌ Auto-register failed:', err);
              });
            }, 500);
          }

          resolve();
        };

        this.ws.onclose = (event) => {
          console.log('[Gateway] ❌ Disconnected:', event.code, event.reason);
          this._isConnected = false;
          this._isConnecting = false;
          this.setConnectionState('disconnected');

          // Remember if we were registered before disconnect for auto-register on reconnect
          if (this.isRegistered) {
            this.wasRegisteredBeforeDisconnect = true;
            console.log(
              '[Gateway] 📝 Was registered, will auto-register on reconnect',
            );
          }
          this.isRegistered = false;
          this.stopPing();
          if (this.isPublicIdentityRecoveryInProgress) {
            console.log(
              '[Gateway] ℹ️ Suppressing onDisconnected during public identity recovery',
            );
          } else {
            this.callbacks.onDisconnected?.(event.reason);
          }

          // Auto reconnect only if not manual disconnect
          if (!this.isManualDisconnect) {
            this.scheduleReconnect(url);
          }
        };

        this.ws.onerror = (error) => {
          // Extract meaningful error info instead of logging Event object
          // WebSocket error events don't have a message property, so we log the type
          const errorType = (error as Event)?.type || 'unknown';
          console.error('[Gateway] WebSocket error: type=' + errorType);
          this._isConnecting = false;
          reject(new Error('WebSocket connection failed'));
        };

        this.ws.onmessage = (event) => {
          this.handleMessage(event.data);
        };
      } catch (error) {
        this._isConnecting = false;
        reject(error);
      }
    });
  }

  // Disconnect from Gateway server
  disconnect(): void {
    console.log('[Gateway] Disconnecting...');
    this.isManualDisconnect = true;
    this.isPublicIdentityRecoveryInProgress = false;
    this.cancelReconnect();
    this.stopPing();
    this.cleanup();

    if (this.ws) {
      this.ws.close(1000, 'User disconnect');
      this.ws = null;
    }

    this._isConnected = false;
    this.isRegistered = false;
    this.wasRegisteredBeforeDisconnect = false; // Don't auto-register on intentional disconnect
    this.reconnectAttempts = 0;
    this.setConnectionState('disconnected');
  }

  // Register SIP account
  async register(config?: Partial<GatewayConfig>): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to Gateway');
    }

    const settings = getSettingsSync();
    this.config = {
      serverUrl: getGatewayServerFromEnv(),
      sipDomain: config?.sipDomain || settings.sipDomain,
      sipUsername: config?.sipUsername || settings.sipUsername,
      sipPassword: config?.sipPassword || settings.sipPassword,
      sipPort: config?.sipPort || settings.sipPort || 5060,
      turnUrl: settings.turnUrl,
      turnUsername: settings.turnUsername,
      turnPassword: settings.turnPassword,
    };

    // Validate SIP credentials before sending
    if (!this.config.sipDomain) {
      throw new Error(
        'SIP domain is not configured. Please configure SIP settings.',
      );
    }
    if (!this.config.sipUsername) {
      throw new Error(
        'SIP username is not configured. Please configure SIP settings.',
      );
    }
    if (!this.config.sipPassword) {
      throw new Error(
        'SIP password is not configured. Please configure SIP settings.',
      );
    }

    const sipPort = this.config.sipPort ?? 5060;
    const hostOnlyDomain = stripPortFromDomain(this.config.sipDomain);

    console.log(
      '[Gateway] Registering:',
      this.config.sipUsername,
      '@',
      hostOnlyDomain,
      'port:',
      sipPort,
    );

    this.send({
      type: 'register',
      sipDomain: hostOnlyDomain,
      sipUsername: this.config.sipUsername,
      sipPassword: this.config.sipPassword,
      sipPort,
    });
  }

  // Unregister SIP account
  unregister(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return;
    }

    console.log('[Gateway] Unregistering...');
    this.send({ type: 'unregister' });
    this.isRegistered = false;
    this.wasRegisteredBeforeDisconnect = false; // Don't auto-register after intentional unregister
    this.callbacks.onUnregistered?.();
  }

  // Make a call
  // Flow: 1) Send offer -> 2) Receive answer with sessionId -> 3) Send call with sessionId (+ auth if provided)
  async call(destination: string, auth?: CallAuth): Promise<void> {
    const intent: PendingCallIntent = {
      destination,
      auth: auth || null,
      attempt: 0,
    };
    await this.startCallIntent(intent);
  }

  private async startCallIntent(intent: PendingCallIntent): Promise<void> {
    // If auth is provided, skip registration check (per-call auth)
    if (!intent.auth && !this.isRegistered) {
      throw new Error('Not registered');
    }

    if (intent.auth) {
      console.log(
        `[Gateway] 📞 Calling: ${intent.destination} (mode: ${intent.auth.mode})`,
      );
    } else {
      console.log('[Gateway] 📞 Calling:', intent.destination);
    }
    this._callState = CallState.CALLING;
    this.callbacks.onCalling?.();
    this.pendingCallIntent = { ...intent };

    // Store destination and auth to send after session is created
    this.pendingDestination = intent.destination;
    this.pendingCallAuth = intent.auth;

    try {
      // Get local media
      await this.getLocalMedia();

      // Create peer connection
      await this.createPeerConnection();

      // Get settings for bitrate and apply env cap
      const settings = getSettingsSync();
      const profileBitrate = getBitrateForResolution(settings.videoResolution);
      const bitrateKbps = Math.min(profileBitrate, VIDEO_BITRATE_KBPS);

      if (bitrateKbps < profileBitrate) {
        console.log(
          `[Gateway] 📊 Bitrate capped by env: ${profileBitrate}kbps → ${bitrateKbps}kbps`,
        );
      }

      // Apply outgoing video bitrate limit based on resolution profile (with env cap)
      await this.applyOutgoingVideoBitrateLimit(bitrateKbps);

      // Create offer - receive remote video
      const offer = await this.pc!.createOffer({
        offerToReceiveAudio: true,
        offerToReceiveVideo: true,
      });

      await this.pc!.setLocalDescription(offer);
      console.log('[Gateway] ⏳ Waiting for ICE gathering to complete...');

      // Wait for ICE gathering to complete
      // This ensures all ICE candidates are included in the SDP
      await this.waitForIceGathering();

      console.log(
        '[Gateway] ➡️ Sending offer (video enabled, ICE complete)...',
      );

      // Process SDP for Linphone compatibility (H264 Baseline) with env-capped bitrate
      const processedSdp = processSDPForLinphone(
        this.pc!.localDescription!.sdp!,
        bitrateKbps,
      );

      // Log codecs in the offer
      logCodecsFromSdp(processedSdp, 'Outgoing call offer');

      // Send offer with processed SDP
      this.send({
        type: 'offer',
        sdp: processedSdp,
      });

      // Note: The actual call message will be sent after receiving answer with sessionId
      // See handleAnswer() method
    } catch (error) {
      console.error('[Gateway] Call failed:', error);
      this._callState = CallState.IDLE;
      this.pendingDestination = null;
      this.pendingCallAuth = null;
      this.pendingCallIntent = null;
      this.cleanup();
      throw error;
    }
  }

  // Hangup call
  hangup(): void {
    console.log('[Gateway] 📴 Hanging up...', 'sessionId:', this.sessionId);
    this.send({ type: 'hangup', sessionId: this.sessionId || undefined });
    this.pendingCallIntent = null;
    this.isPublicIdentityRecoveryInProgress = false;
    this._callState = CallState.ENDED;
    this.callbacks.onCallEnded?.('User hangup');
    this.cleanup();
    this._callState = CallState.IDLE;
  }

  // Toggle mute
  toggleMute(): boolean {
    if (this.localStream) {
      const audioTracks = this.localStream.getAudioTracks();
      audioTracks.forEach((track) => {
        track.enabled = !track.enabled;
      });
      const isMuted = audioTracks.length > 0 && !audioTracks[0].enabled;
      console.log('[Gateway] 🔇 Mute:', isMuted);
      return isMuted;
    }
    return false;
  }

  // Toggle video
  toggleVideo(): boolean {
    if (this.localStream) {
      const videoTracks = this.localStream.getVideoTracks();
      videoTracks.forEach((track) => {
        track.enabled = !track.enabled;
      });
      const isVideoEnabled = videoTracks.length > 0 && videoTracks[0].enabled;
      console.log('[Gateway] 📹 Video:', isVideoEnabled);
      return isVideoEnabled;
    }
    return true;
  }

  // Switch camera
  async switchCamera(): Promise<void> {
    if (this.localStream) {
      const videoTracks = this.localStream.getVideoTracks();
      if (videoTracks.length > 0) {
        // @ts-ignore - React Native WebRTC specific
        videoTracks[0]._switchCamera();
        console.log('[Gateway] 🔄 Camera switched');
      }
    }
  }

  // Send DTMF
  sendDtmf(digit: string): void {
    if (!this.sessionId) {
      console.warn('[Gateway] Cannot send DTMF - no active session');
      return;
    }

    console.log('[Gateway] 🔢 DTMF:', digit, 'sessionId:', this.sessionId);
    this.send({
      type: 'dtmf',
      sessionId: this.sessionId,
      digits: digit,
    });
  }

  /**
   * Send SIP MESSAGE during call
   * @param body - Message content
   * @param contentType - MIME type (default: text/plain;charset=UTF-8)
   */
  sendMessage(
    body: string,
    contentType: string = 'text/plain;charset=UTF-8',
  ): void {
    if (!this._isConnected) {
      console.warn('[Gateway] Cannot send message - not connected');
      return;
    }

    console.log(
      '[Gateway] 💬 Sending message:',
      body.substring(0, 50) + (body.length > 50 ? '...' : ''),
    );
    this.send({
      type: 'send_message',
      body,
      contentType,
    });
  }

  refreshRemoteVideo(reason: string = 'manual'): boolean {
    const hasActiveCall =
      this._callState === CallState.CONNECTING ||
      this._callState === CallState.CALLING ||
      this._callState === CallState.RINGING ||
      this._callState === CallState.INCALL;

    if (!this.pc || !hasActiveCall) {
      console.log('[Gateway] 📺 refreshRemoteVideo skipped:', {
        reason,
        hasPeerConnection: !!this.pc,
        callState: this._callState,
      });
      return false;
    }

    console.log('[Gateway] 📺 Refreshing remote video:', reason);
    this.startKeyframeRetry();

    if (this.remoteStream) {
      this.callbacks.onRemoteStream?.(this.remoteStream);
    }

    return true;
  }

  async ensureLocalVideoBeforeForegroundResume(
    reason: string = 'app_foreground',
  ): Promise<LocalVideoRecoveryResult> {
    try {
      return await this.ensureLocalVideoHealthyForIOS(reason);
    } catch (error) {
      console.warn('[Gateway] ⚠️ Local video foreground recovery failed:', {
        reason,
        error: error instanceof Error ? error.message : String(error),
      });
      return {
        status: 'error',
        reason,
        senderSummary: error instanceof Error ? error.message : String(error),
      };
    }
  }

  // ===== RECONNECTION METHODS =====

  /**
   * Configure reconnection settings
   */
  setReconnectConfig(config: Partial<ReconnectConfig>): void {
    this.reconnectConfig = { ...this.reconnectConfig, ...config };
    console.log('[Gateway] Reconnect config updated:', this.reconnectConfig);
  }

  /**
   * Get current connection state
   */
  getConnectionState(): ConnectionState {
    return this.connectionState;
  }

  /**
   * Manual reconnect (resets attempt counter)
   */
  async reconnect(): Promise<void> {
    console.log('[Gateway] 🔄 Manual reconnect requested');
    this.cancelReconnect();
    this.cleanup();
    this.reconnectAttempts = 0;
    this.isManualDisconnect = false;
    return this.connect(this.lastServerUrl || undefined);
  }

  /**
   * Reconnect for network change - preserves localStream to prevent video flicker
   * Used when switching between WiFi and Cellular during a call
   */
  async reconnectForNetworkChange(): Promise<void> {
    console.log(
      '[Gateway] 🔄 Network change reconnect (preserving localStream)',
    );
    this.cancelReconnect();
    this.cleanupForResume(); // Use cleanupForResume instead of cleanup
    this.reconnectAttempts = 0;
    this.isManualDisconnect = false;
    return this.connect(this.lastServerUrl || undefined);
  }

  /**
   * Cancel any pending reconnection
   */
  cancelReconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
      console.log('[Gateway] Reconnection cancelled');
    }
  }

  /**
   * Resume a call after network change
   * Reuses existing localStream to prevent video flicker
   * Creates new PeerConnection for new ICE candidates and sends SDP offer
   * Server will respond with SDP answer to complete renegotiation
   */
  async resumeCall(sessionId: string): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }

    if (!this.isRegistered) {
      throw new Error('Not registered - cannot resume call');
    }

    console.log(
      '[Gateway] 📞 Attempting to resume call with sessionId:',
      sessionId,
    );

    try {
      // Clean up PeerConnection but PRESERVE localStream to prevent video flicker
      this.cleanupForResume();

      // Store the session ID
      this.sessionId = sessionId;

      // Check if we can reuse existing localStream
      const hasActiveLocalStream =
        this.localStream &&
        this.localStream
          .getTracks()
          .some((track) => track.readyState === 'live');

      if (hasActiveLocalStream) {
        console.log(
          '[Gateway] ✅ Reusing existing localStream (no video flicker)',
        );
      } else {
        // Only recreate if we don't have active tracks
        console.log('[Gateway] 🔄 No active localStream - recreating media...');
        await this.getLocalMedia();
      }

      if (this.shouldAbortResume(sessionId)) {
        throw new Error(
          'Resume aborted: session or call state changed before local video check',
        );
      }

      await this.ensureLocalVideoHealthyForIOS('resume_call');

      if (this.shouldAbortResume(sessionId)) {
        throw new Error(
          'Resume aborted: session or call state changed before PeerConnection creation',
        );
      }

      // Create new PeerConnection for new ICE candidates
      console.log('[Gateway] 🔄 Creating new PeerConnection for resume...');
      await this.createPeerConnection();

      if (this.shouldAbortResume(sessionId)) {
        throw new Error(
          'Resume aborted: session or call state changed after PeerConnection creation',
        );
      }

      // Get settings for bitrate and apply env cap
      const settings = getSettingsSync();
      const profileBitrate = getBitrateForResolution(settings.videoResolution);
      const bitrateKbps = Math.min(profileBitrate, VIDEO_BITRATE_KBPS);

      if (bitrateKbps < profileBitrate) {
        console.log(
          `[Gateway] 📊 Bitrate capped by env: ${profileBitrate}kbps → ${bitrateKbps}kbps`,
        );
      }

      // Apply outgoing video bitrate limit based on resolution profile (with env cap)
      await this.applyOutgoingVideoBitrateLimit(bitrateKbps);

      if (this.shouldAbortResume(sessionId)) {
        throw new Error(
          'Resume aborted: session or call state changed before offer creation',
        );
      }

      // Create SDP offer for renegotiation
      console.log('[Gateway] 🔄 Creating SDP offer for resume...');
      const pc = this.pc;
      if (!pc) {
        throw new Error(
          'Resume aborted: PeerConnection missing before createOffer',
        );
      }
      const offer = await pc.createOffer({
        offerToReceiveAudio: true,
        offerToReceiveVideo: true,
      });
      await pc.setLocalDescription(offer);

      // Wait for ICE gathering to complete
      console.log('[Gateway] ⏳ Waiting for ICE gathering...');
      await this.waitForIceGathering(RESUME_ICE_GATHER_TIMEOUT_MS, 'resume');

      if (this.shouldAbortResume(sessionId)) {
        throw new Error(
          'Resume aborted: session or call state changed before resume send',
        );
      }

      // Process SDP for compatibility with env-capped bitrate
      const localDescriptionSdp = this.pc?.localDescription?.sdp;
      if (!localDescriptionSdp) {
        throw new Error(
          'Resume aborted: localDescription missing before resume send',
        );
      }
      const processedSdp = processSDPForLinphone(
        localDescriptionSdp,
        bitrateKbps,
      );

      // Log codecs in the resume offer
      logCodecsFromSdp(processedSdp, 'Call resume offer');

      // Send resume message with SDP offer to server
      console.log('[Gateway] ➡️ Sending resume with SDP offer...');
      this.send({
        type: 'resume',
        sessionId: sessionId,
        sdp: processedSdp,
      });
      // Server will respond with "resumed" message containing SDP answer
      // handleMessage will process it in the "resumed" case
    } catch (error) {
      console.error('[Gateway] ❌ Failed to resume call:', error);
      throw error;
    }
  }

  private shouldAbortResume(expectedSessionId: string): boolean {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return true;
    }
    if (this.sessionId !== expectedSessionId) {
      return true;
    }
    if (
      this._callState === CallState.ENDED ||
      this._callState === CallState.IDLE
    ) {
      return true;
    }
    return false;
  }

  // ===== PRIVATE RECONNECTION HELPERS =====

  private setConnectionState(state: ConnectionState): void {
    if (this.connectionState !== state) {
      console.log(
        '[Gateway] 📶 Connection state:',
        this.connectionState,
        '→',
        state,
      );
      this.connectionState = state;
      this.callbacks.onConnectionStateChange?.(state);
    }
  }

  private getReconnectDelay(): number {
    const delay = Math.min(
      this.reconnectConfig.baseDelay *
        Math.pow(
          this.reconnectConfig.backoffMultiplier,
          this.reconnectAttempts,
        ),
      this.reconnectConfig.maxDelay,
    );
    // Add jitter (±20%) to prevent thundering herd
    const jitter = delay * 0.2 * (Math.random() * 2 - 1);
    return Math.round(delay + jitter);
  }

  private scheduleReconnect(url: string): void {
    // Give up after max attempts
    if (this.reconnectAttempts >= this.reconnectConfig.maxAttempts) {
      console.log('[Gateway] ❌ Max reconnect attempts reached - giving up');
      this.setConnectionState('disconnected');
      this.callbacks.onReconnectFailed?.();
      return;
    }

    const delay = this.getReconnectDelay();
    this.reconnectAttempts++;

    console.log(
      `[Gateway] 🔄 Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts}/${this.reconnectConfig.maxAttempts})`,
    );

    this.setConnectionState('reconnecting');
    this.callbacks.onReconnecting?.(
      this.reconnectAttempts,
      this.reconnectConfig.maxAttempts,
    );

    this.reconnectTimer = setTimeout(async () => {
      try {
        await this.connect(url);
      } catch (error) {
        console.error('[Gateway] Reconnect attempt failed:', error);
        // Will be called again via onclose if connection fails
      }
    }, delay);
  }

  // Private methods

  private sanitizeOutgoingMessageForLog(message: OutgoingMessage): string {
    const payload = JSON.parse(JSON.stringify(message)) as Record<
      string,
      unknown
    >;
    if (
      typeof payload.sipPassword === 'string' &&
      payload.sipPassword.length > 0
    ) {
      payload.sipPassword = '***';
    }
    return JSON.stringify(payload);
  }

  private isPublicIdentityChangedError(errorMessage: string): boolean {
    return (
      errorMessage === PUBLIC_IDENTITY_CHANGED_ERROR_MESSAGE ||
      (errorMessage.includes('Public SIP identity changed') &&
        errorMessage.includes('new offer'))
    );
  }

  private classifyRecoverableError(
    errorMessage: string,
  ): RecoverableGatewayErrorCode | null {
    if (this.isPublicIdentityChangedError(errorMessage)) {
      return 'PUBLIC_IDENTITY_CHANGED';
    }
    return null;
  }

  private async forceNewSessionForRecovery(): Promise<void> {
    const targetUrl = this.lastServerUrl || getGatewayServerFromEnv();
    this.disconnect();
    this.isManualDisconnect = false;
    await this.connect(targetUrl);
  }

  private async retryPublicIdentityCall(
    errorMessage: string,
  ): Promise<boolean> {
    const intent = this.pendingCallIntent;
    if (
      !intent ||
      intent.attempt !== 0 ||
      !intent.auth ||
      intent.auth.mode !== 'public'
    ) {
      return false;
    }

    if (this.isPublicIdentityRecoveryInProgress) {
      return true;
    }

    const retryIntent: PendingCallIntent = { ...intent, attempt: 1 };
    this.pendingCallIntent = retryIntent;
    this.isPublicIdentityRecoveryInProgress = true;
    this.callbacks.onRecoveryState?.('retrying_public_identity');

    try {
      console.warn(
        '[Gateway] 🔄 Public identity mismatch detected, forcing new offer/session',
      );
      await this.forceNewSessionForRecovery();
      await this.startCallIntent(retryIntent);
      this.isPublicIdentityRecoveryInProgress = false;
      return true;
    } catch (error) {
      this.isPublicIdentityRecoveryInProgress = false;
      this.pendingCallIntent = null;
      const retryMessage =
        error instanceof Error ? error.message : errorMessage;
      this.callbacks.onRecoveryState?.('retry_failed');
      this.callbacks.onError?.(retryMessage);
      return true;
    }
  }

  private send(message: OutgoingMessage): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const data = JSON.stringify(message);
      this.lastSentMessageType = message.type; // Track for debugging
      console.log('[Gateway] ➡️ Send:', message.type);
      console.log(
        '[Gateway] ➡️ Payload:',
        this.sanitizeOutgoingMessageForLog(message),
      );
      this.ws.send(data);
    } else {
      console.warn('[Gateway] Cannot send - not connected');
    }
  }

  private handleMessage(data: string): void {
    try {
      console.log('[Gateway] ⬅️ Raw:', data);
      const message: IncomingMessage = JSON.parse(data);
      console.log('[Gateway] ⬅️ Received:', message.type);

      switch (message.type) {
        case 'registered':
          this.isRegistered = true;
          this.callbacks.onRegistered?.(
            message.username || this.config?.sipUsername || '',
          );
          break;

        case 'unregistered':
          this.isRegistered = false;
          this.callbacks.onUnregistered?.();
          break;

        case 'ringing':
          this._callState = CallState.RINGING;
          this.callbacks.onRinging?.();
          break;

        case 'answer':
          // Server sends "answer" with sessionId after receiving offer
          this.handleAnswer((message as any).sdp, (message as any).sessionId);
          break;

        case 'answered':
          // Keep for backwards compatibility
          this.handleAnswer((message as any).sdp, (message as any).sessionId);
          break;

        case 'state':
          // Handle call state updates from server
          this.handleCallState((message as any).state);
          break;

        case 'ended':
          this._callState = CallState.ENDED;
          this.callbacks.onCallEnded?.(message.reason);
          this.cleanup();
          this._callState = CallState.IDLE;
          break;

        case 'ice':
          this.handleRemoteIceCandidate(message.candidate);
          break;

        case 'error': {
          // Server may send error in either 'message' or 'error' field
          const errorMessage =
            message.message || message.error || 'Unknown error';

          // Ignore "Unknown message type" errors - these are non-critical
          // They typically occur when client/server versions have different message type support
          // For example, ICE candidates may not be supported on older server versions
          if (
            errorMessage === 'Unknown message type' ||
            errorMessage.includes('Unknown message type')
          ) {
            console.log(
              `[Gateway] ℹ️ Server doesn't support message type: ${this.lastSentMessageType} (this is okay)`,
            );
            break;
          }

          // Log real errors
          console.error(
            '[Gateway] ❌ Error response:',
            JSON.stringify(message),
          );
          console.error('[Gateway] ❌ Error:', errorMessage);

          const recoverableError = this.classifyRecoverableError(errorMessage);
          if (recoverableError === 'PUBLIC_IDENTITY_CHANGED') {
            const pendingIntent = this.pendingCallIntent;
            const isPublicIntent = pendingIntent?.auth?.mode === 'public';
            const attempt = pendingIntent?.attempt ?? -1;

            if (isPublicIntent && attempt === 0) {
              // Fire-and-forget recovery path on first mismatch, no user-facing error yet.
              void this.retryPublicIdentityCall(errorMessage);
              break;
            }
            if (isPublicIntent && attempt > 0) {
              this.callbacks.onRecoveryState?.('retry_failed');
            }
          }

          this.callbacks.onError?.(errorMessage);
          if (errorMessage && errorMessage.includes('registration')) {
            this.callbacks.onRegistrationFailed?.(errorMessage);
          }
          break;
        }

        case 'pong':
          // Heartbeat response
          break;

        case 'registerStatus': {
          // Handle registration status from server (same as web client)
          const statusMsg = message as import('./types').RegisterStatusResponse;
          if (statusMsg.registered) {
            console.log(
              '[Gateway] ✅ SIP Registered:',
              statusMsg.sipDomain || this.config?.sipDomain,
            );
            this.isRegistered = true;
            this.callbacks.onRegistered?.(
              statusMsg.sipDomain || this.config?.sipUsername || '',
            );
          } else {
            console.log('[Gateway] SIP Unregistered');
            this.isRegistered = false;
            this.callbacks.onUnregistered?.();
          }
          break;
        }

        case 'message': {
          // Handle incoming SIP MESSAGE
          const msgData = message as MessageResponse;
          console.log(
            '[Gateway] 💬 Message from:',
            msgData.from,
            'body:',
            msgData.body?.substring(0, 50),
          );

          // Check if body contains RTT XML even if contentType is text/plain
          const isRttXml = msgData.body?.trimStart().startsWith('<rtt');
          const isRttContentType =
            msgData.contentType?.includes('t140') ||
            msgData.contentType?.includes('rtte+xml') ||
            msgData.contentType?.includes('xmpp+xml');

          if (isRttXml || isRttContentType) {
            const normalizedContentType =
              msgData.contentType ??
              (isRttXml ? 'application/xmpp+xml' : undefined);
            this.emitRttMessage({
              via: 'sip',
              data: msgData.body,
              contentType: normalizedContentType,
            });
          }

          // Always call onMessage callback (will be filtered in sip-store to exclude RTT)
          this.callbacks.onMessage?.(
            msgData.from,
            msgData.body,
            msgData.contentType,
          );
          break;
        }

        case 'resumed': {
          // Call successfully resumed after network change
          const sessionId = (message as any).sessionId;
          const sdpAnswer = (message as any).sdp;
          console.log(
            '[Gateway] ✅ Call resumed, sessionId:',
            sessionId,
            'hasAnswer:',
            !!sdpAnswer,
          );

          if (sessionId) {
            this.sessionId = sessionId;
          }

          // If server sent SDP answer, set it as remote description
          if (sdpAnswer && this.pc) {
            (async () => {
              try {
                console.log(
                  '[Gateway] 🔄 Setting remote description from resume answer...',
                );
                const remoteDesc = new RTCSessionDescription({
                  type: 'answer',
                  sdp: sdpAnswer,
                });
                await this.pc!.setRemoteDescription(remoteDesc);
                console.log(
                  '[Gateway] ✅ Remote description set for resumed call',
                );

                // Add any pending ICE candidates
                for (const candidate of this.pendingCandidates) {
                  await this.pc!.addIceCandidate(candidate);
                }
                this.pendingCandidates = [];
              } catch (error) {
                console.error(
                  '[Gateway] ❌ Failed to set remote description for resume:',
                  error,
                );
              }
            })();
          }

          this._callState = CallState.INCALL;
          this.callbacks.onCallResumed?.(sessionId || this.sessionId || '');
          break;
        }

        case 'resume_failed': {
          // Call resume failed
          const reason = (message as any).reason || 'Unknown error';
          console.log('[Gateway] ❌ Call resume failed:', reason);
          this.callbacks.onCallResumeFailed?.(reason);
          break;
        }

        default:
          console.log('[Gateway] Unknown message type:', message);
      }
    } catch (error) {
      console.error('[Gateway] Failed to parse message:', error);
    }
  }

  private async handleAnswer(sdp: string, sessionId?: string): Promise<void> {
    console.log('[Gateway] 📱 Received answer, sessionId:', sessionId);

    // Log codecs in the answer
    logCodecsFromSdp(sdp, 'Call answer');

    // Debug: Log remote SDP to analyze video content
    console.log('[Gateway] 📱 Remote SDP length:', sdp?.length || 0);

    // Check for video in remote SDP
    const hasVideoInSdp = sdp?.includes('m=video');
    const videoLineMatch = sdp?.match(/m=video\s+(\d+)\s+/);
    const videoPort = videoLineMatch ? videoLineMatch[1] : 'N/A';
    console.log(
      '[Gateway] 📹 Remote SDP has video:',
      hasVideoInSdp,
      'port:',
      videoPort,
    );

    // Check if video is disabled (port 0)
    if (videoPort === '0') {
      console.warn('[Gateway] ⚠️ VIDEO IS DISABLED in remote SDP (port=0)!');
    }

    // Log video codec info from remote SDP
    const h264Match = sdp?.match(/a=rtpmap:(\d+)\s+H264/gi);
    console.log('[Gateway] 📹 Remote H264 codecs:', h264Match?.length || 0);

    // Check video direction in SDP
    const videoSectionMatch = sdp?.match(/m=video[\s\S]*?(?=m=|$)/);
    if (videoSectionMatch) {
      const videoSection = videoSectionMatch[0];
      const direction = videoSection.match(
        /a=(sendrecv|sendonly|recvonly|inactive)/,
      );
      console.log(
        '[Gateway] 📹 Video direction:',
        direction ? direction[1] : 'not specified (default: sendrecv)',
      );

      // Check for H264 profile
      const profileMatch = videoSection.match(
        /profile-level-id=([0-9a-fA-F]{6})/,
      );
      if (profileMatch) {
        const profile = profileMatch[1];
        const profileType = profile.substring(0, 2);
        console.log(
          '[Gateway] 📹 H264 profile-level-id:',
          profile,
          profileType === '42'
            ? '(Baseline)'
            : profileType === '4d'
              ? '(Main)'
              : profileType === '64'
                ? '(High)'
                : '(Unknown)',
        );
      }
    }

    // Store session ID from server
    if (sessionId) {
      this.sessionId = sessionId;
    }

    if (this.pc) {
      try {
        const remoteDesc = new RTCSessionDescription({
          type: 'answer',
          sdp,
        });
        await this.pc.setRemoteDescription(remoteDesc);
        console.log('[Gateway] ✅ Remote description set');

        // Debug: Check transceivers after setting remote description
        // @ts-ignore - getTransceivers may not be in RN WebRTC types
        const transceivers = this.pc.getTransceivers?.() || [];
        console.log(
          '[Gateway] 📊 Transceivers after setRemoteDescription:',
          transceivers.length,
        );
        transceivers.forEach((t: any, i: number) => {
          console.log(
            `[Gateway] 📊 Transceiver ${i}: direction=${t.direction}, currentDirection=${t.currentDirection}, mid=${t.mid}`,
          );
          if (t.receiver?.track) {
            console.log(
              `[Gateway] 📊 Transceiver ${i} receiver track: kind=${t.receiver.track.kind}, enabled=${t.receiver.track.enabled}, readyState=${t.receiver.track.readyState}`,
            );
          }
        });

        // Debug: Check receivers
        const receivers = this.pc.getReceivers?.() || [];
        console.log('[Gateway] 📊 Receivers:', receivers.length);
        receivers.forEach((r: any, i: number) => {
          console.log(
            `[Gateway] 📊 Receiver ${i}: track kind=${r.track?.kind}, enabled=${r.track?.enabled}, readyState=${r.track?.readyState}`,
          );
        });

        // Add pending ICE candidates
        for (const candidate of this.pendingCandidates) {
          await this.pc.addIceCandidate(candidate);
        }
        this.pendingCandidates = [];

        // Check what type of call this is
        if (this.pendingDestination && this.sessionId && this.pendingCallAuth) {
          // OUTGOING CALL: Send the call message now with sessionId + auth
          console.log(
            `[Gateway] ➡️ Sending call with sessionId: ${this.sessionId} (mode: ${this.pendingCallAuth.mode})`,
          );

          const callMessage: any = {
            type: 'call',
            sessionId: this.sessionId,
            destination: this.pendingDestination,
          };

          // Add auth fields based on mode
          if (this.pendingCallAuth.mode === 'public') {
            callMessage.sipDomain = this.pendingCallAuth.sipDomain;
            callMessage.sipUsername = this.pendingCallAuth.sipUsername;
            callMessage.sipPassword = this.pendingCallAuth.sipPassword;
            callMessage.sipPort = this.pendingCallAuth.sipPort;
            if (this.pendingCallAuth.from) {
              callMessage.from = this.pendingCallAuth.from;
            }
          } else if (this.pendingCallAuth.mode === 'siptrunk') {
            callMessage.trunkId = this.pendingCallAuth.trunkId;
            if (this.pendingCallAuth.from) {
              callMessage.from = this.pendingCallAuth.from;
            }
          }

          this.send(callMessage);
          this.pendingDestination = null;
          this.pendingCallAuth = null;
        } else if (this.pendingDestination && this.sessionId) {
          // OUTGOING CALL: Send the call message now with sessionId (no auth - pre-registered)
          console.log(
            '[Gateway] ➡️ Sending call with sessionId:',
            this.sessionId,
          );
          this.send({
            type: 'call',
            sessionId: this.sessionId,
            destination: this.pendingDestination,
          });
          this.pendingDestination = null;
        } else {
          // Regular session established
          this._callState = CallState.INCALL;
          this.callbacks.onAnswered?.();
        }
      } catch (error) {
        console.error('[Gateway] Failed to set remote description:', error);
      }
    }
  }

  private handleCallState(state: string): void {
    console.log('[Gateway] 📞 Call state update:', state);

    switch (state) {
      case 'active':
      case 'answered':
        // Active media implies SIP dialog is established; keep registration state
        // true to avoid foreground resume deadlock waiting for late register events.
        this.isRegistered = true;
        this._callState = CallState.INCALL;
        this.pendingCallIntent = null;
        this.isPublicIdentityRecoveryInProgress = false;
        this.callbacks.onAnswered?.();
        break;
      case 'ringing':
      case 'trying':
        this._callState = CallState.RINGING;
        this.callbacks.onRinging?.();
        break;
      case 'ended':
      case 'failed':
        this._callState = CallState.ENDED;
        this.pendingCallIntent = null;
        this.isPublicIdentityRecoveryInProgress = false;
        this.callbacks.onCallEnded?.(state);
        this.cleanup();
        this._callState = CallState.IDLE;
        break;
      default:
        console.log('[Gateway] Unknown call state:', state);
    }
  }

  private async handleRemoteIceCandidate(
    candidate: RTCIceCandidateInit,
  ): Promise<void> {
    if (!candidate || !candidate.candidate) return;

    const iceCandidate = new RTCIceCandidate(candidate);

    if (this.pc && this.pc.remoteDescription) {
      try {
        await this.pc.addIceCandidate(iceCandidate);
      } catch (error) {
        console.error('[Gateway] Failed to add ICE candidate:', error);
      }
    } else {
      // Queue candidate until remote description is set
      this.pendingCandidates.push(iceCandidate);
    }
  }

  private async getLocalMedia(): Promise<void> {
    // Check if existing stream has active tracks
    if (this.localStream) {
      const tracks = this.localStream.getTracks();
      const hasActiveTracks = tracks.some(
        (track) => track.readyState === 'live',
      );
      if (hasActiveTracks) {
        console.log('[Gateway] Local stream already exists with active tracks');
        return;
      }
      // Stream exists but tracks are stopped - need to recreate
      console.log(
        '[Gateway] 🔄 Local stream exists but tracks are stopped - recreating',
      );
      this.localStream = null;
    }

    const settings = getSettingsSync();

    // ALWAYS require both camera and microphone permissions for video calls
    console.log('[Gateway] 📹 Requesting camera and microphone permissions...');
    const permissions = await ensureMediaPermissions();

    // Check both permissions - throw error if either is denied
    if (!permissions.microphone || !permissions.camera) {
      const missingPerms = [];
      if (!permissions.microphone) missingPerms.push('Microphone');
      if (!permissions.camera) missingPerms.push('Camera');

      const errorMessage = `${missingPerms.join(' and ')} permission required for video calls. Please grant permissions in settings and try again.`;
      console.error(`[Gateway] ❌ ${errorMessage}`);
      throw new Error(errorMessage);
    }

    // Always use video mode with settings from store
    const { videoResolution, videoFrameRate } = settings;
    console.log(
      `[Gateway] 📹 Getting local media: ${videoResolution}p @ ${videoFrameRate}fps`,
    );

    try {
      // Import helper to get video constraints from settings
      const { getVideoConstraints } = await import('@/store/settings-store');
      const videoConstraints = getVideoConstraints();

      this.localStream = await mediaDevices.getUserMedia({
        audio: true,
        video: videoConstraints,
      });

      // Log actual video track settings to debug portrait issue
      const videoTrack = this.localStream.getVideoTracks()[0];
      if (videoTrack) {
        // @ts-ignore - getSettings may not be in all RN WebRTC types
        const trackSettings = videoTrack.getSettings?.() || {};
        console.log('[Gateway] 📹 Actual video settings:', {
          width: trackSettings.width,
          height: trackSettings.height,
          frameRate: trackSettings.frameRate,
        });

        // Warn if video is in portrait mode (height > width)
        if (
          trackSettings.height &&
          trackSettings.width &&
          trackSettings.height > trackSettings.width
        ) {
          console.warn(
            '[Gateway] ⚠️ Video is in PORTRAIT mode! This may cause Linphone to crash.',
          );
        }
      }

      console.log(
        '[Gateway] ✅ Got local stream with',
        this.localStream.getTracks().length,
        'tracks',
      );
      this.callbacks.onLocalStream?.(this.localStream);
    } catch (error) {
      console.error('[Gateway] ❌ Failed to get local media:', error);
      throw error;
    }
  }

  private async createPeerConnection(): Promise<void> {
    // Close existing peer connection if it's not connected
    if (this.pc) {
      // @ts-ignore - connectionState may not be in RN WebRTC types
      const connectionState =
        this.pc.connectionState || this.pc.iceConnectionState;
      if (
        connectionState === 'connected' ||
        connectionState === 'new' ||
        connectionState === 'connecting'
      ) {
        console.log(
          '[Gateway] Peer connection already exists and is',
          connectionState,
        );
        return;
      }
      // Close stale peer connection
      console.log(
        '[Gateway] 🔄 Closing stale peer connection (state:',
        connectionState,
        ')',
      );
      this.pc.close();
      this.pc = null;
    }

    const settings = getSettingsSync();

    const iceServers: RTCIceServer[] = [];

    // Add TURN server if configured
    if (settings.turnEnabled && settings.turnUrl) {
      iceServers.push({
        urls: settings.turnUrl,
        username: settings.turnUsername || undefined,
        credential: settings.turnPassword || undefined,
      });
    }

    // Add default STUN servers
    iceServers.push({ urls: 'stun:stun.l.google.com:19302' });

    const config: RTCConfiguration = {
      iceServers,
      iceTransportPolicy: settings.turnEnabled ? 'relay' : 'all',
    };

    console.log('[Gateway] 🔗 Creating peer connection');

    this.pc = new RTCPeerConnection(config);

    // RTT DataChannel (ordered + partially reliable)
    const rttChannel = this.pc.createDataChannel('rtt', {
      ordered: true,
      maxPacketLifeTime: 1000,
    });
    this.setupRttDataChannel(rttChannel);

    // Handle remote-created data channels
    // @ts-ignore - react-native-webrtc types missing ondatachannel
    this.pc.ondatachannel = (event: { channel: RtcDataChannel }) => {
      if (event.channel.label === 'rtt') {
        this.setupRttDataChannel(event.channel);
      }
    };

    // ICE connection state handler
    // @ts-ignore - event handler exists at runtime but not in RN WebRTC types
    this.pc.oniceconnectionstatechange = () => {
      console.log(
        '[Gateway] 🧊 ICE connection state:',
        this.pc?.iceConnectionState,
      );
      if (this.pc?.iceConnectionState === 'connected') {
        console.log(
          '[Gateway] 🚀 ICE Connected - Starting keyframe retry mechanism',
        );
        this.startKeyframeRetry();
      }
    };

    // ICE gathering state handler
    // @ts-ignore - event handler exists at runtime but not in RN WebRTC types
    this.pc.onicegatheringstatechange = () => {
      console.log(
        '[Gateway] 🧊 ICE gathering state:',
        this.pc?.iceGatheringState,
      );
    };

    // Signaling state handler
    // @ts-ignore - event handler exists at runtime but not in RN WebRTC types
    this.pc.onsignalingstatechange = () => {
      console.log('[Gateway] 📶 Signaling state:', this.pc?.signalingState);
    };

    // Connection state handler
    // @ts-ignore - connectionState may not be in RN WebRTC types
    this.pc.onconnectionstatechange = () => {
      // @ts-ignore
      console.log('[Gateway] 🔌 Connection state:', this.pc?.connectionState);
    };

    // ICE candidate handler
    // @ts-ignore - event handler exists at runtime but not in RN WebRTC types
    this.pc.onicecandidate = (event: any) => {
      if (event.candidate) {
        console.log(
          '[Gateway] 🧊 ICE candidate:',
          event.candidate.candidate?.substring(0, 50),
        );
        this.send({
          type: 'ice',
          candidate: event.candidate,
        });
      }
    };
    // Add local tracks to PeerConnection
    // Note: track.clone() is NOT implemented in react-native-webrtc
    // Tracks should remain live after pc.close() - only track.stop() stops them
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => {
        this.pc!.addTrack(track, this.localStream!);
        console.log(
          `[Gateway] 🔗 Added ${track.kind} track to PeerConnection (readyState: ${track.readyState})`,
        );
      });
    }

    // Handle remote tracks
    // @ts-ignore - react-native-webrtc types missing ontrack
    this.pc.ontrack = (event: any) => {
      console.log('[Gateway] 🎵 Remote track received:', event.track.kind);
      console.log('[Gateway] 🎵 Track id:', event.track.id);
      console.log('[Gateway] 🎵 Track enabled:', event.track.enabled);
      console.log('[Gateway] 🎵 Track readyState:', event.track.readyState);
      console.log('[Gateway] 🎵 Track muted:', event.track.muted);
      console.log('[Gateway] 🎵 Streams count:', event.streams?.length || 0);

      const incomingTrack = event.track;

      // Add track state change listeners for debugging and recovery
      const trackWithEvents = incomingTrack as unknown as {
        addEventListener?: (type: string, listener: () => void) => void;
        removeEventListener?: (type: string, listener: () => void) => void;
      };

      const handleTrackEnded = () => {
        console.log(
          `[Gateway] ⚠️ ${incomingTrack.kind} track ENDED - may cause black screen`,
        );
        if (incomingTrack.kind === 'video') {
          console.log('[Gateway] 🔄 Video track ended - requesting keyframe');
          this.requestKeyframes();
        }
      };

      const handleTrackMute = () => {
        console.log(`[Gateway] 🔇 ${incomingTrack.kind} track MUTED`);
        if (incomingTrack.kind === 'video') {
          console.log(
            '[Gateway] 📸 Video track muted - requesting keyframe burst',
          );
          this.requestKeyframes();
          setTimeout(() => this.requestKeyframes(), 250);
          setTimeout(() => this.requestKeyframes(), 700);
          this.startBackgroundVideoRecovery('track_muted');
        }
      };

      const handleTrackUnmute = () => {
        console.log(`[Gateway] 🔊 ${incomingTrack.kind} track UNMUTED`);
        if (incomingTrack.kind === 'video') {
          this.hasReceivedVideoFrame = true;
          this.stopKeyframeRetry();
          this.stopBackgroundVideoRecovery();
          if (this.remoteStream) {
            this.callbacks.onRemoteStream?.(this.remoteStream);
          }
        }
      };

      trackWithEvents.addEventListener?.('ended', handleTrackEnded);
      trackWithEvents.addEventListener?.('mute', handleTrackMute);
      trackWithEvents.addEventListener?.('unmute', handleTrackUnmute);

      if (
        incomingTrack.kind === 'video' &&
        incomingTrack.readyState === 'live' &&
        !incomingTrack.muted &&
        incomingTrack.enabled
      ) {
        this.hasReceivedVideoFrame = true;
        console.log(
          '[Gateway] ✅ Video track is live and enabled - marking frame received',
        );
      }

      // Prefer remote-provided stream from ontrack. Building synthetic streams can
      // keep remote video track muted/frozen on some react-native-webrtc versions.
      const incomingStreams = Array.isArray(event.streams)
        ? (event.streams as MediaStream[])
        : [];
      const primaryIncomingStream = incomingStreams[0];

      if (primaryIncomingStream) {
        this.remoteStream = primaryIncomingStream;
        console.log('[Gateway] 📺 Using remote stream from ontrack:');
        console.log('[Gateway] 📺 - Stream ID:', this.remoteStream.id);
        console.log('[Gateway] 📺 - Stream URL:', this.remoteStream.toURL?.());
        console.log(
          '[Gateway] 📺 - Video tracks:',
          this.remoteStream.getVideoTracks().length,
        );
        console.log(
          '[Gateway] 📺 - Audio tracks:',
          this.remoteStream.getAudioTracks().length,
        );
        this.callbacks.onRemoteStream?.(this.remoteStream);
        return;
      }

      // Fallback when event.streams is empty on some stacks.
      if (!this.remoteStream) {
        // @ts-ignore - MediaStream constructor
        this.remoteStream = new MediaStream();
      }

      const hasTrackAlready = this.remoteStream
        .getTracks()
        .some((track) => track.id === incomingTrack.id);
      if (!hasTrackAlready) {
        this.remoteStream.addTrack(incomingTrack);
      }

      console.log('[Gateway] 📺 Using fallback remote stream assembly:');
      console.log('[Gateway] 📺 - Stream ID:', this.remoteStream.id);
      console.log(
        '[Gateway] 📺 - Video tracks:',
        this.remoteStream.getVideoTracks().length,
      );
      console.log(
        '[Gateway] 📺 - Audio tracks:',
        this.remoteStream.getAudioTracks().length,
      );
      this.callbacks.onRemoteStream?.(this.remoteStream);
    };

    // Fallback: onaddstream for older react-native-webrtc versions
    // @ts-ignore - deprecated event but still supported in some RN WebRTC versions
    this.pc.onaddstream = (event: any) => {
      console.log('[Gateway] 📺 onaddstream fired (fallback)');
      if (event.stream) {
        console.log(
          '[Gateway] 📺 Stream received via onaddstream, tracks:',
          event.stream.getTracks().length,
        );
        const videoTracks = event.stream.getVideoTracks();
        const audioTracks = event.stream.getAudioTracks();
        console.log(
          '[Gateway] 📺 Video tracks:',
          videoTracks.length,
          'Audio tracks:',
          audioTracks.length,
        );

        if (
          !this.remoteStream ||
          this.remoteStream.getVideoTracks().length === 0
        ) {
          this.remoteStream = event.stream;
          this.callbacks.onRemoteStream?.(this.remoteStream);
        }
      }
    };
  }

  /**
   * Apply outgoing video bitrate limit using RTCRtpSender.setParameters
   * This caps our outgoing video bitrate to the specified limit in kbps
   * @param bitrateKbps - Maximum bitrate in kilobits per second (default: 1500)
   */
  private async applyOutgoingVideoBitrateLimit(
    bitrateKbps: number = 1500,
  ): Promise<void> {
    if (!this.pc) {
      console.warn(
        '[Gateway] ⚠️ Cannot apply bitrate limit: PeerConnection not initialized',
      );
      return;
    }

    try {
      const senders = this.pc.getSenders();
      const videoSenders = senders.filter((sender) => {
        if (!sender.track) return false;
        return sender.track.kind === 'video';
      });

      if (videoSenders.length === 0) {
        console.log(
          '[Gateway] 📊 No video senders found - bitrate limit not applied',
        );
        return;
      }

      const bitrateBps = bitrateKbps * 1000; // Convert kbps to bps

      for (const sender of videoSenders) {
        try {
          // @ts-ignore - getParameters may not be in all RN WebRTC types
          const params = sender.getParameters();

          if (!params.encodings || params.encodings.length === 0) {
            // Create encodings array if it doesn't exist
            params.encodings = [{ active: true, maxBitrate: bitrateBps }];
          } else {
            // Set maxBitrate on the first encoding (main stream)
            params.encodings[0].maxBitrate = bitrateBps;
          }

          // @ts-ignore - setParameters may not be in all RN WebRTC types
          await sender.setParameters(params);

          console.log(
            `[Gateway] 📊 Applied outgoing video bitrate limit: ${bitrateKbps} kbps (${bitrateBps} bps) on sender ${sender.track?.id || 'unknown'}`,
          );
        } catch (error) {
          console.error(
            '[Gateway] ❌ Failed to set bitrate on video sender:',
            error,
          );
        }
      }
    } catch (error) {
      console.error(
        '[Gateway] ❌ Failed to apply outgoing video bitrate limit:',
        error,
      );
    }
  }

  /**
   * Wait for ICE gathering to complete
   * This is critical for stable connections - without this, the SDP may not include
   * all ICE candidates and the connection may be slow or freeze
   */
  private async waitForIceGathering(
    timeoutMs: number = DEFAULT_ICE_GATHER_TIMEOUT_MS,
    context: string = 'default',
  ): Promise<void> {
    if (!this.pc) return;

    // Already complete
    // @ts-ignore - iceGatheringState may not be in RN WebRTC types
    if (this.pc.iceGatheringState === 'complete') {
      console.log('[Gateway] 🧊 ICE gathering already complete');
      return;
    }

    return new Promise((resolve) => {
      // Timeout-based fallback keeps resume responsive on network transitions.
      const timeout = setTimeout(() => {
        console.log(
          `[Gateway] ⏰ ICE gathering timeout (${timeoutMs}ms, context=${context}) - proceeding with available candidates`,
        );
        resolve();
      }, timeoutMs);

      // @ts-ignore - event handler exists at runtime
      const originalHandler = this.pc!.onicegatheringstatechange;

      // @ts-ignore
      this.pc!.onicegatheringstatechange = (event: any) => {
        // Call original handler if exists
        if (originalHandler) {
          originalHandler.call(this.pc, event);
        }

        console.log(
          '[Gateway] 🧊 ICE gathering state:',
          this.pc?.iceGatheringState,
        );

        // @ts-ignore
        if (this.pc?.iceGatheringState === 'complete') {
          clearTimeout(timeout);
          console.log('[Gateway] ✅ ICE gathering complete');
          resolve();
        }
      };
    });
  }

  private async ensureLocalVideoHealthyForIOS(
    reason: string,
  ): Promise<LocalVideoRecoveryResult> {
    if (Platform.OS !== 'ios') {
      return { status: 'not_ios', reason };
    }

    const now = Date.now();
    if (this.localVideoRecoveryInProgress) {
      console.log(
        '[Gateway] ⏱️ Local video recovery skipped (already in progress):',
        reason,
      );
      return {
        status: 'healthy',
        reason,
        senderSummary: 'recovery_in_progress',
      };
    }
    if (
      now - this.lastLocalVideoRecoveryAt <
      LOCAL_VIDEO_RECOVERY_THROTTLE_MS
    ) {
      console.log('[Gateway] ⏱️ Local video recovery throttled:', reason);
      return { status: 'healthy', reason, senderSummary: 'recovery_throttled' };
    }

    const currentVideoTrack = this.localStream?.getVideoTracks()[0];
    const hasHealthyTrackFlags =
      !!currentVideoTrack &&
      currentVideoTrack.readyState === 'live' &&
      currentVideoTrack.enabled &&
      !currentVideoTrack.muted;
    if (!this.pc && hasHealthyTrackFlags) {
      console.log(
        '[Gateway] ✅ Local video track healthy on iOS (no PC yet):',
        reason,
      );
      return {
        status: 'healthy',
        reason,
        senderSummary: 'no_peer_connection_precheck',
      };
    }
    const senderHealth = await this.sampleLocalVideoSenderHealth();
    const isHealthy = hasHealthyTrackFlags && senderHealth.isHealthy;

    if (isHealthy) {
      console.log('[Gateway] ✅ Local video track healthy on iOS:', {
        reason,
        sender: senderHealth.summary,
      });
      return {
        status: 'healthy',
        reason,
        senderSummary: senderHealth.summary,
      };
    }

    console.log('[Gateway] ⚠️ Local iOS video unhealthy - forcing recovery:', {
      reason,
      hasHealthyTrackFlags,
      sender: senderHealth.summary,
    });

    this.localVideoRecoveryInProgress = true;
    this.lastLocalVideoRecoveryAt = now;
    try {
      let recovered = false;
      let finalSenderSummary = senderHealth.summary;

      for (
        let attempt = 1;
        attempt <= LOCAL_VIDEO_RECOVERY_MAX_ATTEMPTS;
        attempt += 1
      ) {
        const attemptReason = `${reason}:attempt_${attempt}`;
        const replaced = await this.reacquireLocalVideoTrack(attemptReason);
        if (!replaced) {
          console.warn(
            '[Gateway] ⚠️ Local video reacquire did not replace track:',
            attemptReason,
          );
          continue;
        }

        // Kick iOS encoder: brief enable toggle nudges the pipeline without camera restart
        await this.nudgeLocalVideoEncoderForIOS(attemptReason);

        // Wait for iOS encoder to warm up before reading stats
        console.log(
          '[Gateway] ⏳ Waiting for iOS encoder warm-up after replaceTrack:',
          attemptReason,
        );
        await new Promise<void>((resolve) => {
          setTimeout(resolve, LOCAL_VIDEO_POST_REPLACE_WARMUP_MS);
        });

        const afterReplaceHealth = await this.sampleLocalVideoSenderHealth();
        finalSenderSummary = afterReplaceHealth.summary;
        if (afterReplaceHealth.isHealthy) {
          console.log('[Gateway] ✅ Local iOS video recovery verified:', {
            reason: attemptReason,
            sender: afterReplaceHealth.summary,
          });
          recovered = true;
          break;
        }

        console.warn(
          '[Gateway] ⚠️ Local iOS video sender still stalled after warm-up:',
          {
            reason: attemptReason,
            sender: afterReplaceHealth.summary,
          },
        );

        const pipelineReset = await this.resetLocalVideoSenderPipeline();
        if (pipelineReset) {
          await new Promise<void>((resolve) => {
            setTimeout(resolve, LOCAL_VIDEO_POST_REPLACE_WARMUP_MS);
          });
          const afterResetHealth = await this.sampleLocalVideoSenderHealth();
          finalSenderSummary = afterResetHealth.summary;
          if (afterResetHealth.isHealthy) {
            console.log(
              '[Gateway] ✅ Local iOS sender pipeline reset recovered video:',
              {
                reason: attemptReason,
                sender: afterResetHealth.summary,
              },
            );
            recovered = true;
            break;
          }
        }
      }

      if (!recovered) {
        console.warn(
          '[Gateway] ❌ Local iOS video recovery exhausted attempts:',
          reason,
        );
        return {
          status: 'exhausted',
          reason,
          senderSummary: finalSenderSummary,
        };
      }
      return {
        status: 'recovered',
        reason,
        senderSummary: finalSenderSummary,
      };
    } finally {
      this.localVideoRecoveryInProgress = false;
    }
  }

  private async sampleLocalVideoSenderHealth(): Promise<{
    isHealthy: boolean;
    summary: string;
  }> {
    if (!this.pc) {
      return { isHealthy: false, summary: 'no_peer_connection' };
    }

    const videoSender = this.pc
      .getSenders()
      .find((sender) => sender.track?.kind === 'video');
    if (!videoSender) {
      return { isHealthy: false, summary: 'no_video_sender' };
    }

    const senderTrack = videoSender.track;
    if (!senderTrack || senderTrack.readyState !== 'live') {
      return {
        isHealthy: false,
        summary: `sender_track_unhealthy:${senderTrack?.readyState ?? 'missing'}`,
      };
    }

    const first = await this.readOutboundVideoSenderSnapshot(videoSender);
    if (!first) {
      return {
        isHealthy: false,
        summary: 'no_outbound_rtp_stats:first_sample',
      };
    }

    await new Promise<void>((resolve) => {
      setTimeout(resolve, LOCAL_VIDEO_SENDER_HEALTH_SAMPLE_INTERVAL_MS);
    });

    const second = await this.readOutboundVideoSenderSnapshot(videoSender);
    if (!second) {
      return {
        isHealthy: false,
        summary: 'no_outbound_rtp_stats:second_sample',
      };
    }

    const bytesDelta = second.bytesSent - first.bytesSent;
    const framesDelta = second.framesEncoded - first.framesEncoded;
    const packetsDelta = second.packetsSent - first.packetsSent;
    const isHealthy = bytesDelta > 0 || framesDelta > 0 || packetsDelta > 0;

    return {
      isHealthy,
      summary: `delta_bytes=${bytesDelta},delta_frames=${framesDelta},delta_packets=${packetsDelta}`,
    };
  }

  private async readOutboundVideoSenderSnapshot(
    sender: LocalRtpSender,
  ): Promise<{
    bytesSent: number;
    framesEncoded: number;
    packetsSent: number;
  } | null> {
    const senderWithStats = sender as LocalRtpSender & {
      getStats?: () => Promise<unknown>;
    };
    if (!senderWithStats.getStats) {
      return null;
    }

    try {
      const report = await senderWithStats.getStats();
      const entries = this.extractStatsEntries(report);

      let bytesSent = 0;
      let framesEncoded = 0;
      let packetsSent = 0;
      let foundOutboundVideoStat = false;

      for (const entry of entries) {
        const statType = typeof entry.type === 'string' ? entry.type : '';
        const statKind =
          typeof entry.kind === 'string'
            ? entry.kind
            : typeof entry.mediaType === 'string'
              ? entry.mediaType
              : '';

        if (statType !== 'outbound-rtp' || statKind !== 'video') {
          continue;
        }

        foundOutboundVideoStat = true;
        bytesSent = Math.max(bytesSent, this.toSafeStatNumber(entry.bytesSent));
        framesEncoded = Math.max(
          framesEncoded,
          this.toSafeStatNumber(entry.framesEncoded),
        );
        packetsSent = Math.max(
          packetsSent,
          this.toSafeStatNumber(entry.packetsSent),
        );
      }

      if (!foundOutboundVideoStat) {
        return null;
      }

      return { bytesSent, framesEncoded, packetsSent };
    } catch (error) {
      console.log(
        '[Gateway] ⚠️ Failed reading local video sender stats:',
        error,
      );
      return null;
    }
  }

  private extractStatsEntries(report: unknown): Array<Record<string, unknown>> {
    if (!report) {
      return [];
    }

    if (Array.isArray(report)) {
      return report.filter(
        (entry): entry is Record<string, unknown> =>
          typeof entry === 'object' && entry !== null,
      );
    }

    if (typeof report === 'object' && report !== null) {
      const reportWithForEach = report as {
        forEach?: (callback: (value: unknown) => void) => void;
      };
      if (typeof reportWithForEach.forEach === 'function') {
        const entries: Array<Record<string, unknown>> = [];
        reportWithForEach.forEach((value) => {
          if (typeof value === 'object' && value !== null) {
            entries.push(value as Record<string, unknown>);
          }
        });
        return entries;
      }
    }

    return [];
  }

  private toSafeStatNumber(value: unknown): number {
    if (typeof value !== 'number' || Number.isNaN(value)) {
      return 0;
    }
    return value;
  }

  private async nudgeLocalVideoEncoderForIOS(reason: string): Promise<void> {
    if (Platform.OS !== 'ios' || !this.localStream) {
      return;
    }

    const localVideoTrack = this.localStream.getVideoTracks()[0];
    if (!localVideoTrack || localVideoTrack.readyState !== 'live') {
      return;
    }

    try {
      localVideoTrack.enabled = false;
      await new Promise<void>((resolve) => {
        setTimeout(resolve, LOCAL_VIDEO_ENABLE_TOGGLE_DELAY_MS);
      });
      localVideoTrack.enabled = true;
      console.log('[Gateway] 🔧 Nudged iOS encoder via enable toggle:', reason);
    } catch (error) {
      console.warn('[Gateway] ⚠️ Failed to nudge iOS encoder:', error);
    }
  }

  private async resetLocalVideoSenderPipeline(): Promise<boolean> {
    if (!this.pc || !this.localStream) {
      return false;
    }

    const localVideoTrack = this.localStream.getVideoTracks()[0];
    if (!localVideoTrack) {
      return false;
    }

    const videoSender = this.pc
      .getSenders()
      .find((sender) => sender.track?.kind === 'video');
    if (!videoSender || !videoSender.replaceTrack) {
      return false;
    }

    try {
      await videoSender.replaceTrack(null);
      await new Promise<void>((resolve) => {
        setTimeout(resolve, LOCAL_VIDEO_SENDER_RESET_DELAY_MS);
      });
      await videoSender.replaceTrack(localVideoTrack);
      console.log('[Gateway] 🔧 Reset local video sender pipeline');
      return true;
    } catch (error) {
      console.warn(
        '[Gateway] ⚠️ Failed to reset local video sender pipeline:',
        error,
      );
      return false;
    }
  }

  private async reacquireLocalVideoTrack(reason: string): Promise<boolean> {
    if (Platform.OS !== 'ios') {
      return false;
    }

    const { getVideoConstraints } = await import('@/store/settings-store');
    console.log('[Gateway] 🔄 Reacquiring local iOS video track:', reason);
    const refreshedVideoStream = await mediaDevices.getUserMedia({
      audio: false,
      video: getVideoConstraints(),
    });

    const newVideoTrack = refreshedVideoStream.getVideoTracks()[0];
    if (!newVideoTrack) {
      refreshedVideoStream.getTracks().forEach((track) => track.stop());
      return false;
    }

    if (!this.localStream) {
      this.localStream = new MediaStream();
    }

    const localStreamWithOps = this.localStream as MediaStream & {
      removeTrack?: (track: any) => void;
      addTrack: (track: any) => void;
    };
    const oldVideoTracks = this.localStream.getVideoTracks();

    oldVideoTracks.forEach((track) => {
      try {
        localStreamWithOps.removeTrack?.(track);
      } catch (error) {
        console.log(
          '[Gateway] removeTrack unavailable or failed during video recovery:',
          error,
        );
      }
      track.stop();
    });

    localStreamWithOps.addTrack(newVideoTrack);
    this.callbacks.onLocalStream?.(this.localStream);

    refreshedVideoStream.getTracks().forEach((track) => {
      if (track.id !== newVideoTrack.id) {
        track.stop();
      }
    });

    await this.attachOrReplaceLocalVideoSender();

    console.log('[Gateway] ✅ Reacquired local iOS video track:', {
      reason,
      trackId: newVideoTrack.id,
      readyState: newVideoTrack.readyState,
      enabled: newVideoTrack.enabled,
      muted: newVideoTrack.muted,
    });
    return true;
  }

  private async attachOrReplaceLocalVideoSender(): Promise<void> {
    if (!this.pc || !this.localStream) {
      return;
    }

    const localVideoTrack = this.localStream.getVideoTracks()[0];
    if (!localVideoTrack) {
      console.warn(
        '[Gateway] ⚠️ No local video track available for sender recovery',
      );
      return;
    }

    const videoSender = this.pc
      .getSenders()
      .find((sender) => sender.track?.kind === 'video');

    if (videoSender?.replaceTrack) {
      await videoSender.replaceTrack(localVideoTrack);
      console.log('[Gateway] 🔁 Replaced local video sender track');
    } else {
      this.pc.addTrack(localVideoTrack, this.localStream);
      console.log('[Gateway] ➕ Added local video sender track');
    }
  }

  private requestKeyframes(): void {
    if (!this.pc) return;

    try {
      const receivers = this.pc.getReceivers?.() || [];
      receivers.forEach((receiver: any, index: number) => {
        if (receiver.track?.kind === 'video') {
          console.log(
            `[Gateway] 📸 Video receiver ${index}: requesting stats (may trigger PLI)`,
          );
          receiver
            .getStats?.()
            .then((stats: any) => {
              if (stats) {
                console.log(
                  `[Gateway] 📸 Video receiver ${index} stats received`,
                );
              }
            })
            .catch(() => {});
        }
      });

      console.log(
        '[Gateway] 📸 Skipping WS requestKeyframe (not supported by gateway protocol)',
      );
    } catch (error) {
      console.log('[Gateway] ⚠️ Could not request keyframes:', error);
    }
  }

  private startKeyframeRetry(): void {
    this.stopKeyframeRetry();
    this.hasReceivedVideoFrame = false;

    console.log('[Gateway] 🔄 Starting keyframe retry mechanism');
    this.requestKeyframes();

    let attempts = 0;
    const maxAttempts = 10;

    this.keyframeRetryTimer = setInterval(() => {
      attempts++;

      if (this.hasReceivedVideoFrame) {
        console.log(
          '[Gateway] ✅ Video frame received - stopping keyframe retry',
        );
        this.stopKeyframeRetry();
        return;
      }

      if (attempts >= maxAttempts) {
        console.log(
          '[Gateway] ⏰ Keyframe retry timeout - giving up after',
          maxAttempts,
          'attempts',
        );
        this.stopKeyframeRetry();
        return;
      }

      console.log(
        `[Gateway] 🔄 Keyframe retry attempt ${attempts}/${maxAttempts}`,
      );
      this.requestKeyframes();
    }, 1000);
  }

  private stopKeyframeRetry(): void {
    if (this.keyframeRetryTimer) {
      clearInterval(this.keyframeRetryTimer);
      this.keyframeRetryTimer = null;
    }
  }

  private startBackgroundVideoRecovery(reason: string = 'unknown'): void {
    if (!this.pc) {
      return;
    }

    this.stopBackgroundVideoRecovery();
    this.backgroundVideoRecoveryAttempts = 0;
    const maxAttempts = 8;

    console.log('[Gateway] 🩹 Starting background video recovery:', reason);

    this.backgroundVideoRecoveryTimer = setInterval(() => {
      this.backgroundVideoRecoveryAttempts += 1;

      if (this.hasReceivedVideoFrame) {
        console.log(
          '[Gateway] ✅ Background recovery complete - video frame received',
        );
        this.stopBackgroundVideoRecovery();
        return;
      }

      if (this.backgroundVideoRecoveryAttempts > maxAttempts) {
        console.log('[Gateway] ⏰ Background video recovery timeout');
        this.stopBackgroundVideoRecovery();
        return;
      }

      console.log(
        `[Gateway] 🩹 Background recovery attempt ${this.backgroundVideoRecoveryAttempts}/${maxAttempts}`,
      );
      this.requestKeyframes();
      if (this.remoteStream) {
        this.callbacks.onRemoteStream?.(this.remoteStream);
      }
    }, 1500);
  }

  private stopBackgroundVideoRecovery(): void {
    if (this.backgroundVideoRecoveryTimer) {
      clearInterval(this.backgroundVideoRecoveryTimer);
      this.backgroundVideoRecoveryTimer = null;
    }
    this.backgroundVideoRecoveryAttempts = 0;
  }

  private cleanup(): void {
    console.log('[Gateway] 🧹 Cleaning up...');
    this.stopKeyframeRetry();
    this.stopBackgroundVideoRecovery();
    this.hasReceivedVideoFrame = false;

    // Stop local stream
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => track.stop());
      this.localStream = null;
    }

    // Close peer connection
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    if (this.rttDataChannel) {
      this.rttDataChannel.close();
      this.rttDataChannel = null;
    }

    this.remoteStream = null;
    this.pendingCandidates = [];
    this.sessionId = null;
    this.pendingDestination = null;
    this.pendingCallAuth = null; // Clear pending auth
    this.pendingCallIntent = null;
    this.isPublicIdentityRecoveryInProgress = false;
  }

  private emitRttMessage(message: GatewayRttMessage): void {
    for (const listener of this.rttListeners) {
      listener(message);
    }
  }

  private setupRttDataChannel(channel: RtcDataChannel): void {
    if (this.rttDataChannel && this.rttDataChannel !== channel) {
      this.rttDataChannel.close();
    }

    this.rttDataChannel = channel;

    if ('binaryType' in channel) {
      channel.binaryType = 'arraybuffer';
    }

    if (!hasAddEventListener(channel)) {
      console.warn('[Gateway] RTT DataChannel missing addEventListener()');
      return;
    }

    channel.addEventListener('open', () => {
      console.log('[Gateway] ✅ RTT DataChannel open');
    });

    channel.addEventListener('close', () => {
      console.log('[Gateway] ❌ RTT DataChannel closed');
      if (this.rttDataChannel === channel) {
        this.rttDataChannel = null;
      }
    });

    channel.addEventListener('error', () => {
      console.warn('[Gateway] ⚠️ RTT DataChannel error');
    });

    channel.addEventListener('message', (event: unknown) => {
      const data =
        typeof event === 'object' && event !== null && 'data' in event
          ? (event as RtcMessageEvent<'message'>).data
          : undefined;
      if (typeof data === 'string') {
        this.emitRttMessage({ via: 'datachannel', data });
        return;
      }
      if (data instanceof ArrayBuffer) {
        this.emitRttMessage({ via: 'datachannel', data: new Uint8Array(data) });
      }
    });
  }

  /**
   * Cleanup for resume - preserves localStream to prevent video flicker
   * Only closes PeerConnection and clears remote state
   */
  private cleanupForResume(): void {
    console.log(
      '[Gateway] 🧹 Cleaning up for resume (preserving localStream)...',
    );
    this.stopKeyframeRetry();
    this.stopBackgroundVideoRecovery();
    this.hasReceivedVideoFrame = false;

    // DO NOT stop local stream - we want to reuse it to prevent video flicker
    // The localStream tracks are still valid even after network change

    // Close peer connection - we need a new one for new ICE candidates
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    if (this.rttDataChannel) {
      this.rttDataChannel.close();
      this.rttDataChannel = null;
    }

    // Clear remote stream - will get new one from new PeerConnection
    this.remoteStream = null;
    this.pendingCandidates = [];
    // Keep sessionId - we're resuming the same session
    this.pendingDestination = null;
    this.pendingCallAuth = null; // Clear pending auth on resume
    this.pendingCallIntent = null;
    this.isPublicIdentityRecoveryInProgress = false;
  }

  private startPing(): void {
    this.stopPing();

    // Send heartbeat ping every 20 seconds to keep connection alive
    this.pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        console.log('[Gateway] 💓 Sending heartbeat ping');
        this.send({ type: 'ping' });
      }
    }, 20000);
  }

  private stopPing(): void {
    if (this.pingInterval) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
  }
}

// Singleton instance
let gatewayInstance: GatewayClient | null = null;

export function getGatewayClient(): GatewayClient {
  if (!gatewayInstance) {
    gatewayInstance = new GatewayClient();
  }
  return gatewayInstance;
}

export function resetGatewayClient(): void {
  if (gatewayInstance) {
    gatewayInstance.disconnect();
    gatewayInstance = null;
  }
}
