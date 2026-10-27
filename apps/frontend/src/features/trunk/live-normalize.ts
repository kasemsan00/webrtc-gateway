import type { ActiveSession } from '@/features/active-sessions/types'
import type { Trunk } from '@/features/trunk/types'
import type { WSClient } from '@/features/ws-clients/types'

/*
 * Boundary normalisation for the trunk page. The gateway omits empty fields
 * (`omitempty`), sends pointers as `null` and nil slices as `null`, so raw
 * payloads are never trusted: every field is coerced to a safe type once,
 * when data enters, and the rest of the page can use plain string methods.
 */

type RawRecord = Record<string, unknown>

function isRecord(value: unknown): value is RawRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function str(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

function num(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

function bool(value: unknown): boolean {
  return value === true
}

/** Trimmed, non-empty strings; drops nulls and other junk entries. */
function strList(value: unknown): Array<string> {
  if (!Array.isArray(value)) return []
  return value
    .map((entry) => str(entry).trim())
    .filter((entry) => entry.length > 0)
}

function nullableStr(value: unknown): string | null {
  const text = str(value).trim()
  return text ? text : null
}

/** Records only; null / primitive entries in the array are dropped. */
function recordList(value: unknown): Array<RawRecord> {
  return Array.isArray(value) ? value.filter(isRecord) : []
}

export function normalizeTrunk(raw: unknown): Trunk {
  const r = isRecord(raw) ? raw : {}
  const publicId = str(r.public_id) || str(r.publicId)
  const inUseBy = nullableStr(r.inUseBy) ?? nullableStr(r.in_use_by)
  return {
    id: num(r.id),
    public_id: publicId,
    publicId,
    in_use_by: inUseBy,
    inUseBy,
    name: str(r.name),
    domain: str(r.domain),
    port: num(r.port),
    username: str(r.username),
    transport: str(r.transport),
    enabled: bool(r.enabled),
    isDefault: bool(r.isDefault),
    activeCallCount: Math.max(0, num(r.activeCallCount)),
    activeDestinations: strList(r.activeDestinations),
    leaseOwner: str(r.leaseOwner),
    leaseUntil: str(r.leaseUntil),
    lastRegisteredAt: str(r.lastRegisteredAt),
    lastUnregisteredAt: str(r.lastUnregisteredAt),
    isRegistered: bool(r.isRegistered),
    lastError: str(r.lastError),
    sipAutoRegister: bool(r.sipAutoRegister),
    pnAppId: str(r.pnAppId),
    pnType: str(r.pnType),
    pnTokenMasked: str(r.pnTokenMasked),
    pnUpdatedAt: str(r.pnUpdatedAt),
    lastOnlinePlatform: str(r.lastOnlinePlatform),
    lastOnlineAt: str(r.lastOnlineAt),
    pushContactReady: bool(r.pushContactReady),
    fcmTokenReady: bool(r.fcmTokenReady),
    fcmTokenMasked: str(r.fcmTokenMasked),
    fcmUpdatedAt: str(r.fcmUpdatedAt),
    notifyUserBound: bool(r.notifyUserBound),
    createdAt: str(r.createdAt),
    updatedAt: str(r.updatedAt),
  }
}

export function normalizeSession(raw: unknown): ActiveSession {
  const r = isRecord(raw) ? raw : {}
  return {
    id: str(r.id),
    state: str(r.state),
    direction: str(r.direction),
    from: str(r.from),
    to: str(r.to),
    sipCallId: str(r.sipCallId),
    authMode: str(r.authMode),
    trunkId: num(r.trunkId),
    trunkName: str(r.trunkName),
    sipUsername: str(r.sipUsername),
    durationSec: Math.max(0, num(r.durationSec)),
    createdAt: str(r.createdAt),
    answeredAt: nullableStr(r.answeredAt),
    updatedAt: str(r.updatedAt),
    translatorEnabled: bool(r.translatorEnabled),
    translatorSrcLang: str(r.translatorSrcLang),
    translatorTgtLang: str(r.translatorTgtLang),
    translatorTtsVoice: str(r.translatorTtsVoice),
  }
}

export function normalizeWSClient(raw: unknown): WSClient {
  const r = isRecord(raw) ? raw : {}
  return {
    clientId: str(r.clientId),
    sessionId: str(r.sessionId),
    connectedAt: str(r.connectedAt),
    trunkResolved: bool(r.trunkResolved),
    resolvedTrunkId: num(r.resolvedTrunkId),
    resolvedTrunkPublicId: str(r.resolvedTrunkPublicId),
    sipUsername: str(r.sipUsername),
    sipDomain: str(r.sipDomain),
    sipPort: num(r.sipPort),
    availability: str(r.availability),
    callState: str(r.callState),
    authSubject: str(r.authSubject),
    publicOnly: bool(r.publicOnly),
    agentOnly: bool(r.agentOnly),
    multiCall: bool(r.multiCall),
    activeCalls: num(r.activeCalls),
    presenceMode: str(r.presenceMode),
    agentTrunkRefCount: num(r.agentTrunkRefCount),
  }
}

export function normalizeTrunks(raw: unknown): Array<Trunk> {
  return recordList(raw).map(normalizeTrunk)
}

export function normalizeSessions(raw: unknown): Array<ActiveSession> {
  return recordList(raw).map(normalizeSession)
}

export function normalizeWSClients(raw: unknown): Array<WSClient> {
  return recordList(raw).map(normalizeWSClient)
}
