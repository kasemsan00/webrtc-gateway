import type {
  GatewayWsEndpoint,
  InboundWsMessage,
  MultiVideoSdpValidation,
  OutboundAcceptMessage,
  OutboundAgentRegisterMessage,
  OutboundCallMessage,
  OutboundClientStateMessage,
  OutboundHangupMessage,
  OutboundIceMessage,
  OutboundOfferMessage,
  OutboundRejectMessage,
  OutboundRenegotiateAnswerMessage,
  OutboundStreamUpdateMessage,
  OutboundTrunkResolveMessage,
  OutboundWsMessage,
} from './types'

import { readAppEnvValue } from '@/lib/runtime-env'

export interface DeriveWsUrlOptions {
  origin?: string
  envGatewayUrl?: string
  skipEnv?: boolean
}

function normalizeHttpOriginToWs(origin: string): string {
  const trimmed = origin
    .trim()
    .replace(/\/+$/, '')
    .replace(/\/api$/i, '')
  if (/^wss?:\/\//i.test(trimmed)) return trimmed
  if (/^https:\/\//i.test(trimmed)) return `wss://${trimmed.slice(8)}`
  if (/^http:\/\//i.test(trimmed)) return `ws://${trimmed.slice(7)}`
  return `wss://${trimmed}`
}

/**
 * Derives a default Gateway WebSocket URL for `/ws-agent` or `/ws`.
 * Uses configured `VITE_GATEWAY_URL` when set, otherwise derives from the
 * current browser origin (or `ws://localhost:8080` during SSR).
 */
export function deriveDefaultGatewayWsUrl(
  endpoint: GatewayWsEndpoint,
  options: DeriveWsUrlOptions = {},
): string {
  const envUrl =
    options.envGatewayUrl?.trim() ||
    (options.skipEnv ? undefined : readAppEnvValue('VITE_GATEWAY_URL'))

  const baseOrigin =
    envUrl ||
    options.origin?.trim() ||
    (typeof window !== 'undefined'
      ? window.location.origin
      : 'http://localhost:8080')

  const wsBase = normalizeHttpOriginToWs(baseOrigin)
  return `${wsBase}${endpoint}`
}

/**
 * Replaces the endpoint path on an existing WS URL while preserving host/port.
 */
export function replaceWsUrlEndpoint(
  currentUrl: string,
  endpoint: GatewayWsEndpoint,
): string {
  const trimmed = currentUrl.trim()
  if (!trimmed) return deriveDefaultGatewayWsUrl(endpoint)
  try {
    const parsed = new URL(trimmed)
    parsed.pathname = endpoint
    return parsed.toString()
  } catch {
    return deriveDefaultGatewayWsUrl(endpoint)
  }
}

/**
 * Appends optional `access_token` query param to a WS URL without mutating
 * the editable base URL.
 */
export function buildConnectWsUrl(
  baseUrl: string,
  accessToken: string,
): string {
  const trimmedUrl = baseUrl.trim()
  const token = accessToken.trim()
  if (!token) return trimmedUrl
  try {
    const parsed = new URL(trimmedUrl)
    parsed.searchParams.set('access_token', token)
    return parsed.toString()
  } catch {
    const separator = trimmedUrl.includes('?') ? '&' : '?'
    return `${trimmedUrl}${separator}access_token=${encodeURIComponent(token)}`
  }
}

/** Masks sensitive query parameters (such as `access_token`) for logs. */
export function maskSensitiveUrl(rawUrl: string): string {
  try {
    const parsed = new URL(rawUrl)
    for (const key of ['access_token', 'token', 'password']) {
      if (parsed.searchParams.has(key)) {
        parsed.searchParams.set(key, '***')
      }
    }
    return parsed.toString()
  } catch {
    return rawUrl.replace(/(access_token|token|password)=([^&\s]+)/gi, '$1=***')
  }
}

export function parseSipPort(rawPort: string): number | undefined {
  const trimmed = rawPort.trim()
  if (!trimmed) return undefined
  const parsed = Number.parseInt(trimmed, 10)
  if (!Number.isSafeInteger(parsed) || parsed <= 0 || parsed > 65535) {
    return undefined
  }
  return parsed
}

export function buildAgentRegisterMessage(params: {
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort?: number
  multiCall?: boolean
  multiVideo?: boolean
}): OutboundAgentRegisterMessage {
  const msg: OutboundAgentRegisterMessage = {
    type: 'agent_register',
    sipDomain: params.sipDomain.trim(),
    sipUsername: params.sipUsername.trim(),
    sipPassword: params.sipPassword,
  }
  if (params.sipPort && params.sipPort > 0) msg.sipPort = params.sipPort
  if (params.multiCall) msg.multiCall = true
  if (params.multiVideo) msg.multiVideo = true
  return msg
}

export function buildClientStateMessage(params: {
  availability?: 'idle' | 'busy' | 'unavailable'
  callState?: string
  sessionId?: string
  multiCall?: boolean
  multiVideo?: boolean
}): OutboundClientStateMessage {
  const msg: OutboundClientStateMessage = {
    type: 'client_state',
  }
  if (params.availability) msg.availability = params.availability
  if (params.callState) msg.callState = params.callState
  if (params.sessionId) msg.sessionId = params.sessionId
  if (params.multiCall) msg.multiCall = true
  if (params.multiVideo) msg.multiVideo = true
  return msg
}

export function buildTrunkResolveMessage(params: {
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort?: number
}): OutboundTrunkResolveMessage {
  const msg: OutboundTrunkResolveMessage = {
    type: 'trunk_resolve',
    sipDomain: params.sipDomain.trim(),
    sipUsername: params.sipUsername.trim(),
    sipPassword: params.sipPassword,
  }
  if (params.sipPort && params.sipPort > 0) msg.sipPort = params.sipPort
  return msg
}

export function buildOfferMessage(
  sdp: string,
  sessionId?: string,
): OutboundOfferMessage {
  const msg: OutboundOfferMessage = {
    type: 'offer',
    sdp,
  }
  if (sessionId?.trim()) msg.sessionId = sessionId.trim()
  return msg
}

export function buildIceMessage(
  sessionId: string,
  candidate: RTCIceCandidateInit,
): OutboundIceMessage {
  return {
    type: 'ice',
    sessionId,
    candidate,
  }
}

export function buildCallMessage(params: {
  sessionId: string
  destination: string
  from?: string
  sipCredentials?: {
    sipDomain: string
    sipUsername: string
    sipPassword: string
    sipPort?: number
  }
}): OutboundCallMessage {
  const msg: OutboundCallMessage = {
    type: 'call',
    sessionId: params.sessionId,
    destination: params.destination.trim(),
  }
  if (params.from?.trim()) msg.from = params.from.trim()
  if (params.sipCredentials) {
    msg.sipDomain = params.sipCredentials.sipDomain.trim()
    msg.sipUsername = params.sipCredentials.sipUsername.trim()
    msg.sipPassword = params.sipCredentials.sipPassword
    if (params.sipCredentials.sipPort && params.sipCredentials.sipPort > 0) {
      msg.sipPort = params.sipCredentials.sipPort
    }
  }
  return msg
}

export function buildAcceptMessage(sessionId: string): OutboundAcceptMessage {
  return { type: 'accept', sessionId }
}

export function buildRejectMessage(
  sessionId: string,
  reason = 'busy',
): OutboundRejectMessage {
  return { type: 'reject', sessionId, reason }
}

export function buildHangupMessage(
  sessionId: string,
  reason?: string,
): OutboundHangupMessage {
  return reason
    ? { type: 'hangup', sessionId, reason }
    : { type: 'hangup', sessionId }
}

/**
 * Builds a `stream_update` `add` message for `role: "screen"`.
 * Pass `sdp` on the first share of a call; omit `sdp` when reusing an
 * already-negotiated screen transceiver on the same call.
 */
export function buildStreamUpdateAddMessage(
  sessionId: string,
  sdp?: string,
): OutboundStreamUpdateMessage {
  const msg: OutboundStreamUpdateMessage = {
    type: 'stream_update',
    sessionId,
    role: 'screen',
    action: 'add',
  }
  if (sdp && sdp.trim().length > 0) {
    msg.sdp = sdp
  }
  return msg
}

export function buildStreamUpdateRemoveMessage(
  sessionId: string,
): OutboundStreamUpdateMessage {
  return {
    type: 'stream_update',
    sessionId,
    role: 'screen',
    action: 'remove',
  }
}

export function buildRenegotiateAnswerMessage(params: {
  sessionId: string
  renegotiationId: string
  status: 'ok' | 'failed'
  sdp?: string
  reason?: string
}): OutboundRenegotiateAnswerMessage {
  const msg: OutboundRenegotiateAnswerMessage = {
    type: 'renegotiate_answer',
    sessionId: params.sessionId,
    renegotiationId: params.renegotiationId,
    status: params.status,
  }
  if (params.sdp && params.sdp.trim().length > 0) msg.sdp = params.sdp
  if (params.reason && params.reason.trim().length > 0) {
    msg.reason = params.reason
  }
  return msg
}

/** Counts `m=video` media sections in an SDP string. */
export function countVideoMLines(sdp: string): number {
  let count = 0
  for (const rawLine of sdp.split(/\r?\n/)) {
    if (rawLine.startsWith('m=video ')) {
      count += 1
    }
  }
  return count
}

/**
 * Validates that an SDP offer for a first `stream_update` `add` contains
 * exactly 2 `m=video` sections (1st camera, 2nd screen).
 */
export function validateMultiVideoOfferSdp(
  sdp: string,
): MultiVideoSdpValidation {
  return validateVideoMLineOfferSdp(sdp, { exactVideoCount: 2 })
}

/**
 * Validates that an SDP offer for inbound `renegotiate` (`reason: "stream_added"`)
 * contains at least `minVideoCount` `m=video` sections (default 2, or 3+ when
 * this peer previously negotiated a kept `sendonly` screen transceiver).
 */
export function validateStreamAddedOfferSdp(
  sdp: string,
  minVideoCount = 2,
): MultiVideoSdpValidation {
  return validateVideoMLineOfferSdp(sdp, {
    minVideoCount: Math.max(2, minVideoCount),
  })
}

function validateVideoMLineOfferSdp(
  sdp: string,
  constraint: { exactVideoCount: number } | { minVideoCount: number },
): MultiVideoSdpValidation {
  let videoCount = 0
  let audioCount = 0
  const videoMids: Array<string> = []
  let currentSection: 'session' | 'audio' | 'video' | 'other' = 'session'

  for (const rawLine of sdp.split(/\r?\n/)) {
    const line = rawLine.trim()
    if (line.startsWith('m=')) {
      if (line.startsWith('m=video ')) {
        videoCount += 1
        currentSection = 'video'
      } else if (line.startsWith('m=audio ')) {
        audioCount += 1
        currentSection = 'audio'
      } else {
        currentSection = 'other'
      }
      continue
    }
    if (currentSection === 'video' && line.startsWith('a=mid:')) {
      videoMids.push(line.slice('a=mid:'.length))
    }
  }

  if ('exactVideoCount' in constraint) {
    if (videoCount !== constraint.exactVideoCount) {
      return {
        valid: false,
        videoCount,
        audioCount,
        videoMids,
        error: `Expected exactly ${constraint.exactVideoCount} m=video sections (camera + screen), got ${videoCount}`,
      }
    }
  } else if (videoCount < constraint.minVideoCount) {
    return {
      valid: false,
      videoCount,
      audioCount,
      videoMids,
      error: `Expected at least ${constraint.minVideoCount} m=video sections for stream_added offer, got ${videoCount}`,
    }
  }

  return {
    valid: true,
    videoCount,
    audioCount,
    videoMids,
  }
}

function readOptionalString(
  obj: Record<string, unknown>,
  key: string,
): string | undefined {
  const val = obj[key]
  return typeof val === 'string' ? val : undefined
}

function readOptionalNumber(
  obj: Record<string, unknown>,
  key: string,
): number | undefined {
  const val = obj[key]
  return typeof val === 'number' && Number.isFinite(val) ? val : undefined
}

function readOptionalBoolean(
  obj: Record<string, unknown>,
  key: string,
): boolean | undefined {
  const val = obj[key]
  return typeof val === 'boolean' ? val : undefined
}

/** Validates an untyped JSON WebSocket payload at the network boundary. */
export function parseInboundWsMessage(raw: string): InboundWsMessage | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null
  }
  const obj = parsed as Record<string, unknown>
  const type = readOptionalString(obj, 'type')
  if (!type) return null

  return {
    type,
    sessionId: readOptionalString(obj, 'sessionId'),
    sdp: readOptionalString(obj, 'sdp'),
    state: readOptionalString(obj, 'state'),
    from: readOptionalString(obj, 'from'),
    to: readOptionalString(obj, 'to'),
    hasVideo: readOptionalString(obj, 'hasVideo'),
    endReason: readOptionalString(obj, 'endReason'),
    reason: readOptionalString(obj, 'reason'),
    error: readOptionalString(obj, 'error'),
    operation: readOptionalString(obj, 'operation'),
    operationSessionId: readOptionalString(obj, 'operationSessionId'),
    trunkId: readOptionalNumber(obj, 'trunkId'),
    trunkPublicId: readOptionalString(obj, 'trunkPublicId'),
    role: readOptionalString(obj, 'role'),
    action: readOptionalString(obj, 'action'),
    status: readOptionalString(obj, 'status'),
    active: readOptionalBoolean(obj, 'active'),
    renegotiationId: readOptionalString(obj, 'renegotiationId'),
    mediaDirection: readOptionalString(obj, 'mediaDirection'),
    requiresAnswer: readOptionalBoolean(obj, 'requiresAnswer'),
    kind: readOptionalString(obj, 'kind'),
    direction: readOptionalString(obj, 'direction'),
    held: readOptionalBoolean(obj, 'held'),
  }
}

/**
 * Produces a compact, human-readable summary for the diagnostics log.
 * Never exposes SIP passwords, tokens, or raw full SDP blobs.
 */
export function summarizeWsMessage(
  msg: OutboundWsMessage | InboundWsMessage,
): string {
  const parts: Array<string> = []
  const record = msg as unknown as Record<string, unknown>

  const sessionId = readOptionalString(record, 'sessionId')
  if (sessionId) parts.push(`session=${sessionId.slice(0, 12)}`)

  switch (msg.type) {
    case 'agent_register':
    case 'trunk_resolve': {
      const user = readOptionalString(record, 'sipUsername')
      const domain = readOptionalString(record, 'sipDomain')
      const port = readOptionalNumber(record, 'sipPort')
      if (user || domain) {
        parts.push(`${user ?? '?'}@${domain ?? '?'}${port ? `:${port}` : ''}`)
      }
      if (readOptionalString(record, 'sipPassword')) {
        parts.push('password=***')
      }
      if (readOptionalBoolean(record, 'multiVideo')) {
        parts.push('multiVideo=true')
      }
      if (readOptionalBoolean(record, 'multiCall')) {
        parts.push('multiCall=true')
      }
      break
    }
    case 'client_state': {
      const availability = readOptionalString(record, 'availability')
      if (availability) parts.push(`availability=${availability}`)
      if (readOptionalBoolean(record, 'multiVideo')) {
        parts.push('multiVideo=true')
      }
      if (readOptionalBoolean(record, 'multiCall')) {
        parts.push('multiCall=true')
      }
      break
    }
    case 'trunk_resolved': {
      const trunkId = readOptionalNumber(record, 'trunkId')
      const pubId = readOptionalString(record, 'trunkPublicId')
      if (trunkId !== undefined) parts.push(`trunkId=${trunkId}`)
      if (pubId) parts.push(`publicId=${pubId.slice(0, 8)}`)
      break
    }
    case 'call': {
      const dest = readOptionalString(record, 'destination')
      if (dest) parts.push(`dest=${dest}`)
      if (readOptionalString(record, 'sipPassword')) {
        parts.push('sipPassword=***')
      }
      break
    }
    case 'incoming': {
      const from = readOptionalString(record, 'from')
      const to = readOptionalString(record, 'to')
      const hasVideo = readOptionalString(record, 'hasVideo')
      if (from) parts.push(`from=${from}`)
      if (to) parts.push(`to=${to}`)
      if (hasVideo) parts.push(`hasVideo=${hasVideo}`)
      break
    }
    case 'state': {
      const state = readOptionalString(record, 'state')
      const endReason = readOptionalString(record, 'endReason')
      const reason = readOptionalString(record, 'reason')
      if (state) parts.push(`state=${state}`)
      if (endReason) parts.push(`endReason=${endReason}`)
      if (reason) parts.push(`reason=${reason}`)
      break
    }
    case 'stream_update':
    case 'stream_update_result': {
      const role = readOptionalString(record, 'role')
      const action = readOptionalString(record, 'action')
      const status = readOptionalString(record, 'status')
      const reason = readOptionalString(record, 'reason')
      const sdp = readOptionalString(record, 'sdp')
      if (role) parts.push(`role=${role}`)
      if (action) parts.push(`action=${action}`)
      if (status) parts.push(`status=${status}`)
      if (reason) parts.push(`reason=${reason}`)
      if (sdp) {
        parts.push(`sdp(${sdp.length}B, m=video:${countVideoMLines(sdp)})`)
      } else {
        parts.push('no-sdp')
      }
      break
    }
    case 'stream_state': {
      const role = readOptionalString(record, 'role')
      const active = readOptionalBoolean(record, 'active')
      const reason = readOptionalString(record, 'reason')
      if (role) parts.push(`role=${role}`)
      if (active !== undefined) parts.push(`active=${String(active)}`)
      if (reason) parts.push(`reason=${reason}`)
      break
    }
    case 'renegotiate':
    case 'renegotiate_answer':
    case 'renegotiate_result': {
      const renId = readOptionalString(record, 'renegotiationId')
      const reason = readOptionalString(record, 'reason')
      const role = readOptionalString(record, 'role')
      const status = readOptionalString(record, 'status')
      const dir = readOptionalString(record, 'mediaDirection')
      const sdp = readOptionalString(record, 'sdp')
      if (renId) parts.push(`id=${renId.slice(0, 10)}`)
      if (reason) parts.push(`reason=${reason}`)
      if (role) parts.push(`role=${role}`)
      if (dir) parts.push(`dir=${dir}`)
      if (status) parts.push(`status=${status}`)
      if (sdp) {
        parts.push(`sdp(${sdp.length}B, m=video:${countVideoMLines(sdp)})`)
      }
      break
    }
    case 'offer':
    case 'answer': {
      const sdp = readOptionalString(record, 'sdp')
      if (sdp) {
        parts.push(`sdp(${sdp.length}B, m=video:${countVideoMLines(sdp)})`)
      }
      break
    }
    case 'ice': {
      const candidate = record.candidate as
        | { candidate?: string; sdpMid?: string }
        | undefined
      if (candidate?.sdpMid) parts.push(`mid=${candidate.sdpMid}`)
      if (candidate?.candidate) {
        const typeMatch = /typ\s+(\S+)/.exec(candidate.candidate)
        if (typeMatch?.[1]) parts.push(`typ=${typeMatch[1]}`)
      }
      break
    }
    case 'media': {
      const kind = readOptionalString(record, 'kind')
      const direction = readOptionalString(record, 'direction')
      const state = readOptionalString(record, 'state')
      if (kind) parts.push(`kind=${kind}`)
      if (direction) parts.push(`dir=${direction}`)
      if (state) parts.push(`state=${state}`)
      break
    }
    case 'error': {
      const op = readOptionalString(record, 'operation')
      const err = readOptionalString(record, 'error')
      if (op) parts.push(`op=${op}`)
      if (err) parts.push(err)
      break
    }
    default: {
      const reason = readOptionalString(record, 'reason')
      const status = readOptionalString(record, 'status')
      if (status) parts.push(`status=${status}`)
      if (reason) parts.push(`reason=${reason}`)
      break
    }
  }

  return parts.join(' · ') || msg.type
}
