export interface IncomingRttResult {
  nextText: string
  parsed: boolean
  seq: number | null
}

const DEFAULT_DELETE_COUNT = 1
const RTT_WRAPPED_PATTERN = /^<rtt\b([^>]*)>([\s\S]*)<\/rtt>\s*$/i
const RTT_SELF_CLOSED_PATTERN = /^<rtt\b([^>]*)\/>\s*$/i
const ATTR_PATTERN = /([a-zA-Z_:][\w:.-]*)\s*=\s*(['"])(.*?)\2/g
const RTT_CHILD_PATTERN =
  /<([tew])\b([^>]*)\/>|<([tew])\b([^\/>]*)>([\s\S]*?)<\/\3>/gi
const RTT_CHILDREN_ONLY_PATTERN =
  /^(?:\s*(?:<t\b[^>]*>[\s\S]*?<\/t>|<t\b[^>]*\/>|<e\b[^>]*\/>|<w\b[^>]*\/>)\s*)*$/i

function parseDeleteCount(raw: string | null) {
  if (!raw) return DEFAULT_DELETE_COUNT
  const parsed = Number.parseInt(raw, 10)
  if (!Number.isFinite(parsed) || parsed < 1) return DEFAULT_DELETE_COUNT
  return parsed
}

function parseSeq(raw: string | null) {
  if (!raw) return null
  const parsed = Number.parseInt(raw, 10)
  if (!Number.isFinite(parsed) || parsed < 0) return null
  return parsed
}

function parseAttrs(raw: string) {
  const out: Record<string, string> = {}
  let match: RegExpExecArray | null = ATTR_PATTERN.exec(raw)
  while (match) {
    out[match[1]] = match[3]
    match = ATTR_PATTERN.exec(raw)
  }
  ATTR_PATTERN.lastIndex = 0
  return out
}

function unescapeXmlText(text: string) {
  return text
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&amp;', '&')
}

export function isRttXmlPayload(body: string) {
  const trimmed = body.trim()
  return (
    RTT_WRAPPED_PATTERN.test(trimmed) || RTT_SELF_CLOSED_PATTERN.test(trimmed)
  )
}

export function applyIncomingRtt(
  currentText: string,
  xml: string,
): IncomingRttResult {
  if (!isRttXmlPayload(xml)) {
    return { nextText: currentText, parsed: false, seq: null }
  }

  try {
    const trimmed = xml.trim()
    const wrapped = RTT_WRAPPED_PATTERN.exec(trimmed)
    const selfClosed = RTT_SELF_CLOSED_PATTERN.exec(trimmed)
    if (!wrapped && !selfClosed) {
      return { nextText: currentText, parsed: false, seq: null }
    }

    const rootAttrs = parseAttrs(wrapped?.[1] ?? selfClosed?.[1] ?? '')
    let nextText = currentText
    const event = rootAttrs.event
    if (event === 'new' || event === 'reset') {
      nextText = ''
    }

    const body = wrapped?.[2] ?? ''
    if (!RTT_CHILDREN_ONLY_PATTERN.test(body)) {
      return { nextText: currentText, parsed: false, seq: null }
    }

    let childMatch: RegExpExecArray | null = RTT_CHILD_PATTERN.exec(body)
    while (childMatch) {
      const childTag = (childMatch[1] ?? childMatch[3] ?? '').toLowerCase()
      const childAttrsRaw = childMatch[2] ?? childMatch[4] ?? ''
      const childTextRaw = childMatch[5] ?? ''
      const childAttrs = parseAttrs(childAttrsRaw)

      if (childTag === 't') {
        const value = unescapeXmlText(childTextRaw)
        if (value) nextText += value
      }
      if (childTag === 'e') {
        const n = parseDeleteCount(childAttrs.n ?? null)
        nextText = nextText.slice(0, Math.max(0, nextText.length - n))
      }
      if (childTag === 'w') {
        // keep as no-op
      }

      childMatch = RTT_CHILD_PATTERN.exec(body)
    }
    RTT_CHILD_PATTERN.lastIndex = 0

    return {
      nextText,
      parsed: true,
      seq: parseSeq(rootAttrs.seq ?? null),
    }
  } catch {
    return { nextText: currentText, parsed: false, seq: null }
  }
}

export function escapeXmlText(text: string) {
  return text
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

export function buildOutgoingRttXml(
  text: string,
  seq: number,
  event: 'new' | 'reset',
) {
  const escaped = escapeXmlText(text)
  if (!escaped) {
    return `<rtt event='${event}' seq='${seq}'></rtt>`
  }
  return `<rtt event='${event}' seq='${seq}'><t>${escaped}</t></rtt>`
}
