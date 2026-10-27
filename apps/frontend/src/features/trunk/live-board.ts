import type { ActiveSession } from '@/features/active-sessions/types'
import type { Trunk } from '@/features/trunk/types'
import type { WSClient } from '@/features/ws-clients/types'

/** Phase of one live call as seen from GET /api/sessions `state`. */
export type CallPhase = 'dialing' | 'incoming' | 'talking' | 'reconnecting'

/**
 * Overall state of one trunk on the live board, highest priority first:
 * talking > reconnecting > dialing/incoming > busy > online > error > offline > disabled.
 * `busy` means /api/trunks reports active calls that this gateway instance does
 * not list in /api/sessions (e.g. the call lives on another instance).
 */
export type TrunkLiveStatus =
  | 'talking'
  | 'reconnecting'
  | 'dialing'
  | 'incoming'
  | 'busy'
  | 'online'
  | 'error'
  | 'offline'
  | 'disabled'

export type LiveFilter = 'all' | 'online' | 'calling' | 'talking' | 'offline'

export interface SipParty {
  /** Main label: number / SIP user part, or the raw value if it is not a URI. */
  label: string
  /** Optional display name or host. */
  detail?: string
}

export interface LiveCall {
  sessionId: string
  phase: CallPhase
  direction: 'inbound' | 'outbound' | 'unknown'
  remote: SipParty
  local: SipParty
  durationSec: number
  trunkId: number
  trunkName: string
  sipUsername: string
}

export interface TrunkLiveRow {
  trunk: Trunk
  status: TrunkLiveStatus
  calls: Array<LiveCall>
  /** Connected WS clients (apps/agents) bound to this trunk. */
  clients: Array<WSClient>
  /** Destinations from /api/trunks when sessions are not visible here. */
  fallbackDestinations: Array<string>
}

export interface LiveSummary {
  trunks: number
  online: number
  calling: number
  talking: number
  offline: number
  appsConnected: number
}

export function sessionCallPhase(
  session: Pick<ActiveSession, 'state' | 'direction'>,
): CallPhase | null {
  const state = session.state.trim().toLowerCase()
  switch (state) {
    case 'active':
      return 'talking'
    case 'reconnecting':
      return 'reconnecting'
    case 'incoming':
      return 'incoming'
    case 'new':
    case 'connecting':
    case 'ringing':
      return session.direction === 'inbound' ? 'incoming' : 'dialing'
    default:
      return null
  }
}

/**
 * Turns `"Alice" <sip:1001@pbx.example.com;transport=udp>` into
 * `{ label: '1001', detail: 'Alice' }`. Plain numbers pass through.
 */
export function parseSipParty(raw?: string): SipParty {
  const value = (raw ?? '').trim()
  if (!value) return { label: 'Unknown' }

  let displayName: string | undefined
  let uri = value
  const angle = value.match(/^(.*)<([^>]*)>/)
  if (angle) {
    displayName =
      angle[1]
        .trim()
        .replace(/^"(.*)"$/, '$1')
        .trim() || undefined
    uri = angle[2].trim()
  }

  const sip = uri.match(/^(?:sips?|tel):([^@;?]*)(?:@([^;?>]*))?/i)
  if (!sip) {
    return displayName
      ? { label: uri || displayName, detail: displayName }
      : { label: uri }
  }
  const user = decodeURIComponentSafe(sip[1])
  // The host group is optional, so it can be undefined at runtime.
  const host = (sip[2] as string | undefined)?.trim()
  const label = user || host || uri
  const detail =
    displayName && displayName !== label ? displayName : user ? host : undefined
  return detail ? { label, detail } : { label }
}

function decodeURIComponentSafe(value: string) {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function normalizeDirection(direction: string): LiveCall['direction'] {
  if (direction === 'inbound' || direction === 'outbound') return direction
  return 'unknown'
}

export function toLiveCall(session: ActiveSession): LiveCall | null {
  const phase = sessionCallPhase(session)
  if (!phase) return null
  const direction = normalizeDirection(session.direction)
  const remoteRaw =
    direction === 'inbound' ? session.from : session.to || session.from
  const localRaw = direction === 'inbound' ? session.to : session.from
  return {
    sessionId: session.id,
    phase,
    direction,
    remote: parseSipParty(remoteRaw),
    local: parseSipParty(localRaw || session.sipUsername),
    durationSec: Math.max(0, session.durationSec || 0),
    trunkId: session.trunkId || 0,
    trunkName: session.trunkName || '',
    sipUsername: session.sipUsername || '',
  }
}

const CALL_PHASE_RANK: Record<CallPhase, number> = {
  talking: 0,
  reconnecting: 1,
  dialing: 2,
  incoming: 2,
}

function sortCalls(calls: Array<LiveCall>) {
  return [...calls].sort(
    (left, right) =>
      CALL_PHASE_RANK[left.phase] - CALL_PHASE_RANK[right.phase] ||
      right.durationSec - left.durationSec,
  )
}

export function deriveTrunkLiveStatus(
  trunk: Pick<
    Trunk,
    'enabled' | 'isRegistered' | 'lastError' | 'activeCallCount'
  >,
  calls: Array<Pick<LiveCall, 'phase'>>,
): TrunkLiveStatus {
  const phases = new Set(calls.map((call) => call.phase))
  if (phases.has('talking')) return 'talking'
  if (phases.has('reconnecting')) return 'reconnecting'
  if (phases.has('dialing')) return 'dialing'
  if (phases.has('incoming')) return 'incoming'
  if (trunk.activeCallCount > 0) return 'busy'
  if (!trunk.enabled) return 'disabled'
  if (trunk.isRegistered) return 'online'
  if (trunk.lastError.trim()) return 'error'
  return 'offline'
}

const STATUS_RANK: Record<TrunkLiveStatus, number> = {
  talking: 0,
  reconnecting: 1,
  dialing: 2,
  incoming: 2,
  busy: 3,
  online: 4,
  error: 5,
  offline: 6,
  disabled: 7,
}

export function buildTrunkLiveRows(
  trunks: Array<Trunk>,
  sessions: Array<ActiveSession>,
  clients: Array<WSClient>,
): Array<TrunkLiveRow> {
  const callsByTrunk = new Map<number, Array<LiveCall>>()
  for (const session of sessions) {
    const call = toLiveCall(session)
    if (!call || call.trunkId <= 0) continue
    const list = callsByTrunk.get(call.trunkId) ?? []
    list.push(call)
    callsByTrunk.set(call.trunkId, list)
  }

  const clientsByTrunk = new Map<number, Array<WSClient>>()
  for (const client of clients) {
    const trunkId = client.resolvedTrunkId ?? 0
    if (!client.trunkResolved || trunkId <= 0) continue
    const list = clientsByTrunk.get(trunkId) ?? []
    list.push(client)
    clientsByTrunk.set(trunkId, list)
  }

  const rows = trunks.map((trunk, index) => {
    const calls = sortCalls(callsByTrunk.get(trunk.id) ?? [])
    const fallbackDestinations =
      calls.length === 0
        ? (trunk.activeDestinations ?? [])
            .map((destination) => destination.trim())
            .filter((destination) => destination.length > 0)
        : []
    return {
      index,
      row: {
        trunk,
        status: deriveTrunkLiveStatus(trunk, calls),
        calls,
        clients: clientsByTrunk.get(trunk.id) ?? [],
        fallbackDestinations,
      } satisfies TrunkLiveRow,
    }
  })

  return rows
    .sort(
      (left, right) =>
        STATUS_RANK[left.row.status] - STATUS_RANK[right.row.status] ||
        left.index - right.index,
    )
    .map((entry) => entry.row)
}

/** Live calls that are not bound to any trunk (public / user-auth sessions). */
export function collectNonTrunkCalls(
  sessions: Array<ActiveSession>,
): Array<LiveCall> {
  return sortCalls(
    sessions
      .map(toLiveCall)
      .filter((call): call is LiveCall => call !== null && call.trunkId <= 0),
  )
}

export function isCallingStatus(status: TrunkLiveStatus) {
  return status === 'dialing' || status === 'incoming'
}

export function isTalkingStatus(status: TrunkLiveStatus) {
  return status === 'talking' || status === 'reconnecting' || status === 'busy'
}

export function isOfflineStatus(status: TrunkLiveStatus) {
  return status === 'offline' || status === 'error' || status === 'disabled'
}

export function summarizeLiveRows(rows: Array<TrunkLiveRow>): LiveSummary {
  const summary: LiveSummary = {
    trunks: rows.length,
    online: 0,
    calling: 0,
    talking: 0,
    offline: 0,
    appsConnected: 0,
  }
  for (const row of rows) {
    if (row.trunk.enabled && row.trunk.isRegistered) summary.online += 1
    if (isOfflineStatus(row.status)) summary.offline += 1
    if (row.clients.length > 0) summary.appsConnected += 1
    if (row.calls.length === 0 && row.status === 'busy') {
      summary.talking += Math.max(1, row.trunk.activeCallCount)
      continue
    }
    for (const call of row.calls) {
      if (call.phase === 'dialing' || call.phase === 'incoming') {
        summary.calling += 1
      } else {
        summary.talking += 1
      }
    }
  }
  return summary
}

export function matchesLiveFilter(row: TrunkLiveRow, filter: LiveFilter) {
  switch (filter) {
    case 'online':
      return row.trunk.enabled && row.trunk.isRegistered
    case 'calling':
      return row.calls.some(
        (call) => call.phase === 'dialing' || call.phase === 'incoming',
      )
    case 'talking':
      return (
        row.status === 'busy' ||
        row.calls.some(
          (call) => call.phase === 'talking' || call.phase === 'reconnecting',
        )
      )
    case 'offline':
      return isOfflineStatus(row.status)
    case 'all':
    default:
      return true
  }
}

/** `65` → `01:05`, `3725` → `1:02:05`. */
export function formatCallDuration(totalSec: number) {
  const safe = Math.max(0, Math.floor(totalSec))
  const hours = Math.floor(safe / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const seconds = safe % 60
  const mm = String(minutes).padStart(2, '0')
  const ss = String(seconds).padStart(2, '0')
  return hours > 0 ? `${hours}:${mm}:${ss}` : `${mm}:${ss}`
}
