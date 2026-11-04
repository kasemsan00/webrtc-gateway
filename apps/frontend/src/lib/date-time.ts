export type DateTimeFormatOptions = Intl.DateTimeFormatOptions

export const DISPLAY_TIME_ZONE = 'Asia/Bangkok'
export const DISPLAY_TIME_ZONE_LABEL = 'ICT (UTC+7)'

function parseIso(iso: string | null | undefined): Date | null {
  if (!iso) return null
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? null : date
}

/**
 * Absolute date-time in Asia/Bangkok using the Thai locale.
 * Every table in the console uses this so timestamps read the same everywhere.
 */
export function formatThaiDateTime(
  iso: string | null | undefined,
  options: DateTimeFormatOptions = {},
): string {
  if (!iso) return '-'
  const date = parseIso(iso)
  if (!date) return iso
  try {
    return date.toLocaleString('th-TH', {
      hour12: false,
      timeZone: DISPLAY_TIME_ZONE,
      ...options,
    })
  } catch {
    return iso
  }
}

/**
 * Full date-time with milliseconds, e.g. "4/11/2569 09:02:10.400". Passing
 * only `fractionalSecondDigits` to toLocaleString drops every other part, so
 * all components are listed explicitly.
 */
export function formatPreciseDateTime(iso: string | null | undefined): string {
  return formatThaiDateTime(iso, {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  })
}

/** Time of day only (HH:mm:ss) in Asia/Bangkok. */
export function formatClockTime(
  iso: string | number | null | undefined,
): string {
  if (iso === null || iso === undefined || iso === '') return '-'
  const date = typeof iso === 'number' ? new Date(iso) : parseIso(iso)
  if (!date || Number.isNaN(date.getTime())) return String(iso)
  return date.toLocaleTimeString('th-TH', {
    hour12: false,
    timeZone: DISPLAY_TIME_ZONE,
  })
}

/**
 * Compact relative time such as "12s ago", "5m ago", "3h ago", "2d ago" or
 * "in 4m" for future values.
 */
export function formatRelativeTime(
  iso: string | number | null | undefined,
  now: number = Date.now(),
): string {
  if (iso === null || iso === undefined || iso === '') return '-'
  const date = typeof iso === 'number' ? new Date(iso) : parseIso(iso)
  if (!date || Number.isNaN(date.getTime())) return '-'
  const diffSec = Math.round((now - date.getTime()) / 1000)
  const future = diffSec < 0
  const abs = Math.abs(diffSec)
  let text: string
  if (abs < 5) return 'just now'
  if (abs < 60) text = `${abs}s`
  else if (abs < 3600) text = `${Math.floor(abs / 60)}m`
  else if (abs < 86_400) text = `${Math.floor(abs / 3600)}h`
  else text = `${Math.floor(abs / 86_400)}d`
  return future ? `in ${text}` : `${text} ago`
}

/** Duration in seconds as "m:ss" or "h:mm:ss". */
export function formatDurationSec(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) {
    return '-'
  }
  const total = Math.max(0, Math.floor(seconds))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const ss = s.toString().padStart(2, '0')
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${ss}`
  return `${m}:${ss}`
}

/** Seconds elapsed between two ISO timestamps (end defaults to now). */
export function secondsBetween(
  startIso: string | null | undefined,
  endIso?: string | null,
  now: number = Date.now(),
): number | null {
  const start = parseIso(startIso)
  if (!start) return null
  const end = endIso ? parseIso(endIso) : null
  const endMs = end ? end.getTime() : now
  return Math.max(0, Math.round((endMs - start.getTime()) / 1000))
}

/** Value for `<input type="date">` → ISO start/end of that Bangkok day. */
export function bangkokDayBoundsIso(
  date: string,
  edge: 'start' | 'end',
): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return undefined
  const time = edge === 'start' ? '00:00:00.000' : '23:59:59.999'
  return new Date(`${date}T${time}+07:00`).toISOString()
}
