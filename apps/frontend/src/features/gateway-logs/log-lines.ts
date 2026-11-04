export type LogLevel = 'error' | 'warn' | 'ok' | 'info'

const ERROR_PATTERN =
  /❌|🔴|\b(error|errors|failed|failure|fatal|panic|refused|timeout|timed out|unreachable)\b/i
const WARN_PATTERN =
  /⚠️|⚠|🟡|\b(warn|warning|retry|retrying|dropp(ed|ing)|expired|reconnect)/i
const OK_PATTERN = /✅|🟢|\b(success|successfully|registered|connected)\b/i

// "Received 401, retrying with auth" is the normal digest challenge.
const BENIGN_PATTERN = /received 40[17], retrying with auth/i

/** Classifies one gateway log line by its emoji / keywords. */
export function classifyLogLine(line: string): LogLevel {
  if (BENIGN_PATTERN.test(line)) return 'info'
  if (ERROR_PATTERN.test(line)) return 'error'
  if (WARN_PATTERN.test(line)) return 'warn'
  if (OK_PATTERN.test(line)) return 'ok'
  return 'info'
}

/** `[TrunkManager]` → `TrunkManager`; first bracketed tag on the line. */
export function extractLogComponent(line: string): string | null {
  const match = /\[([A-Za-z][\w .:-]{1,40})\]/.exec(line)
  return match ? match[1].trim() : null
}

export type LevelFilter = 'all' | 'problems' | 'errors'

export interface ParsedLogLine {
  index: number
  text: string
  level: LogLevel
  component: string | null
}

export function parseLogLines(lines: Array<string>): Array<ParsedLogLine> {
  return lines.map((text, index) => ({
    index,
    text,
    level: classifyLogLine(text),
    component: extractLogComponent(text),
  }))
}

export function filterLogLines(
  lines: Array<ParsedLogLine>,
  options: { query?: string; level?: LevelFilter; component?: string },
): Array<ParsedLogLine> {
  const query = options.query?.trim().toLowerCase() ?? ''
  const level = options.level ?? 'all'
  return lines.filter((line) => {
    if (level === 'errors' && line.level !== 'error') return false
    if (
      level === 'problems' &&
      line.level !== 'error' &&
      line.level !== 'warn'
    ) {
      return false
    }
    if (options.component && line.component !== options.component) return false
    if (query && !line.text.toLowerCase().includes(query)) return false
    return true
  })
}

/** Splits `text` into parts with `match: true` where `query` occurs (case-insensitive). */
export function highlightParts(
  text: string,
  query: string,
): Array<{ text: string; match: boolean }> {
  const q = query.trim()
  if (!q) return [{ text, match: false }]
  const parts: Array<{ text: string; match: boolean }> = []
  const lower = text.toLowerCase()
  const needle = q.toLowerCase()
  let cursor = 0
  for (;;) {
    const found = lower.indexOf(needle, cursor)
    if (found === -1) break
    if (found > cursor)
      parts.push({ text: text.slice(cursor, found), match: false })
    parts.push({ text: text.slice(found, found + q.length), match: true })
    cursor = found + q.length
  }
  if (cursor < text.length)
    parts.push({ text: text.slice(cursor), match: false })
  return parts
}
