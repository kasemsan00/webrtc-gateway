export type LogType = 'info' | 'success' | 'warning' | 'error'
export type CallMode = 'public' | 'publicvrs' | 'siptrunk'
export type ConnectionStatus =
  | 'disconnected'
  | 'connecting'
  | 'reconnecting'
  | 'connected'
  | 'error'
export type MediaStatus =
  | 'not-ready'
  | 'getting-media'
  | 'offer-sent'
  | 'active'
export type CallStatus =
  | 'idle'
  | 'connecting'
  | 'ringing'
  | 'reconnecting'
  | 'active'
  | 'ended'
export type TrunkStatus =
  | 'not-resolved'
  | 'resolving'
  | 'resolved'
  | 'redirecting'
  | 'not-found'
  | 'not-ready'

export interface LogEntry {
  id: string
  time: string
  type: LogType
  message: string
}

export interface MessageEntry {
  id: string
  time: string
  from: string
  body: string
  direction: 'incoming' | 'outgoing'
}

export interface PublicCredentials {
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort: number
}

export interface VrsConfig {
  phone: string
  fullName: string
  agency: string
  emergency: number
  emergencyOptionsData: string | null
  userAgent: string
  mobileUID: string
}

export interface VrsApiResponse {
  status: string
  data: {
    domain: string
    domain_caption: string
    domain_video: string
    ext: string
    iss: string
    name: string
    secret: string
    websocket: string
    iat: number
    exp: number
    dtmcreated: string
    threadid: string
    uuid: string
    identification: string
  }
}

export type VrsFetchStatus = 'idle' | 'fetching' | 'fetched' | 'error'

export interface TrunkCredentials {
  trunkId: string
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort: number
}

export interface IncomingCallState {
  from: string
  to: string
  mode: CallMode
  sessionId: string
}

export interface VideoConfig {
  maxBitrate: number
  maxFramerate: number
  width: number
  height: number
  useConstrainedBaseline: boolean
}

export interface StatsState {
  rttMs: string
  packetLossPercent: string
  bitrateKbps: string
  codec: string
  resolution: string
}

export interface PendingCallRequest {
  destination: string
  sipDomain?: string
  sipUsername?: string
  sipPassword?: string
  sipPort?: number
  trunkId?: number
  trunkPublicId?: string
}

export interface GatewayState {
  config: {
    wssUrl: string
  }
  connection: {
    status: ConnectionStatus
    wsStateText: string
  }
  media: {
    status: MediaStatus
    rtcStateText: string
    localStream: MediaStream | null
    remoteVideoStream: MediaStream | null
    remoteAudioStream: MediaStream | null
    iceState: string
    signalingState: string
  }
  call: {
    sessionId: string | null
    state: CallStatus
    elapsedSeconds: number
    callCount: number
  }
  mode: CallMode
  publicCredentials: PublicCredentials
  vrs: {
    config: VrsConfig
    fetchStatus: VrsFetchStatus
    resolvedCredentials: PublicCredentials | null
  }
  trunk: {
    status: TrunkStatus
    credentials: TrunkCredentials
  }
  controls: {
    destination: string
    dialpadOpen: boolean
    statsOpen: boolean
    isMutedAudio: boolean
    isMutedVideo: boolean
    autoStartingSession: boolean
  }
  stats: StatsState
  rtt: {
    remotePreviewText: string
    remoteActive: boolean
    lastRemoteSeq: number | null
  }
  incomingCall: IncomingCallState | null
  logs: Array<LogEntry>
  messages: Array<MessageEntry>
  pendingCallRequest: PendingCallRequest | null
}
