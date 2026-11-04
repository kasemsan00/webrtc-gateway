/** Helpers for TanStack `validateSearch`: every key is optional. */

export function optString(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    return trimmed ? trimmed : undefined
  }
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return undefined
}

export function optPositiveInt(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isSafeInteger(value) && value > 0) {
    return value
  }
  if (typeof value === 'string' && /^\d+$/.test(value)) {
    const parsed = Number(value)
    return parsed > 0 ? parsed : undefined
  }
  return undefined
}

export function optDate(value: unknown): string | undefined {
  const text = optString(value)
  return text && /^\d{4}-\d{2}-\d{2}$/.test(text) ? text : undefined
}

export function optEnum<T extends string>(
  value: unknown,
  allowed: ReadonlyArray<T>,
): T | undefined {
  return typeof value === 'string' &&
    (allowed as ReadonlyArray<string>).includes(value)
    ? (value as T)
    : undefined
}

/** Call Sessions (history) filters kept in the URL so links can be shared. */
export interface SessionsSearch {
  q?: string
  direction?: 'inbound' | 'outbound'
  state?: string
  endReason?: string
  /** Bangkok calendar date YYYY-MM-DD (inclusive). */
  from?: string
  /** Bangkok calendar date YYYY-MM-DD (inclusive). */
  to?: string
}

export function validateSessionsSearch(
  search: Record<string, unknown>,
): SessionsSearch {
  return {
    q: optString(search.q),
    direction: optEnum(search.direction, ['inbound', 'outbound'] as const),
    state: optString(search.state),
    endReason: optString(search.endReason),
    from: optDate(search.from),
    to: optDate(search.to),
  }
}

export type SessionDetailTab =
  | 'overview'
  | 'events'
  | 'payloads'
  | 'dialogs'
  | 'stats'
  | 'client-diagnostics'

export const SESSION_DETAIL_TABS: ReadonlyArray<SessionDetailTab> = [
  'overview',
  'events',
  'payloads',
  'dialogs',
  'stats',
  'client-diagnostics',
]

export interface SessionDetailSearch {
  tab?: SessionDetailTab
}

export function validateSessionDetailSearch(
  search: Record<string, unknown>,
): SessionDetailSearch {
  return { tab: optEnum(search.tab, SESSION_DETAIL_TABS) }
}

export interface LogsSearch {
  /** Pre-filled text filter, e.g. a session ID or SIP Call-ID. */
  q?: string
  file?: string
}

export function validateLogsSearch(
  search: Record<string, unknown>,
): LogsSearch {
  return { q: optString(search.q), file: optString(search.file) }
}

export interface SimpleSearch {
  search?: string
}

export function validateSimpleSearch(
  search: Record<string, unknown>,
): SimpleSearch {
  return { search: optString(search.search) }
}

export interface TrunksSearch {
  search?: string
  trunkPublicId?: string
  trunkId?: number
}

export function validateTrunksSearch(
  search: Record<string, unknown>,
): TrunksSearch {
  return {
    search: optString(search.search),
    trunkPublicId: optString(search.trunkPublicId),
    trunkId: optPositiveInt(search.trunkId),
  }
}

export interface ClientDiagnosticsSearch {
  clientTraceId?: string
  authSubject?: string
  source?: string
  level?: string
  name?: string
}

export function validateClientDiagnosticsSearch(
  search: Record<string, unknown>,
): ClientDiagnosticsSearch {
  return {
    clientTraceId: optString(search.clientTraceId),
    authSubject: optString(search.authSubject),
    source: optString(search.source),
    level: optString(search.level),
    name: optString(search.name),
  }
}
