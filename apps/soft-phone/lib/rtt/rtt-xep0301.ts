export interface IncomingRttApplyResult {
  nextText: string
  parsed: boolean
  seq: number | null
}

function parseAttributes(raw: string): Record<string, string> {
  const attrs: Record<string, string> = {}
  const attrRegex = /([a-zA-Z_:][a-zA-Z0-9_:\-\.]*)\s*=\s*["']([^"']*)["']/g
  let match: RegExpExecArray | null = null
  while ((match = attrRegex.exec(raw))) {
    attrs[match[1]] = match[2]
  }
  return attrs
}

function parseInteger(value: string | undefined): number | null {
  if (value === undefined) return null
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : null
}

function decodeXmlText(text: string): string {
  return text
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&amp;/g, '&')
}

function parseRttEnvelope(xml: string): { body: string; attrs: Record<string, string> } | null {
  const match = xml.match(/<\s*rtt\b([^>]*)>([\s\S]*?)<\s*\/\s*rtt\s*>/i)
  if (!match) return null
  return {
    attrs: parseAttributes(match[1] ?? ''),
    body: match[2] ?? '',
  }
}

export function isRttXmlPayload(body: string): boolean {
  const trimmed = body.trim()
  return /^<\s*rtt\b/i.test(trimmed) || /^<\?xml[\s\S]*?<\s*rtt\b/i.test(trimmed)
}

export function applyIncomingRtt(currentText: string, xml: string): IncomingRttApplyResult {
  if (!isRttXmlPayload(xml)) {
    return { nextText: currentText, parsed: false, seq: null }
  }

  const envelope = parseRttEnvelope(xml)
  if (!envelope) {
    return { nextText: currentText, parsed: false, seq: null }
  }

  const seq = parseInteger(envelope.attrs.seq)
  let nextText = currentText
  const event = envelope.attrs.event?.toLowerCase()
  if (event === 'new' || event === 'reset') {
    nextText = ''
  }

  const tagRegex = /<\s*(\/?)\s*([a-zA-Z0-9:]+)([^>]*)>/g
  let openT: { attrs: Record<string, string>; textStart: number } | null = null
  let match: RegExpExecArray | null = null

  while ((match = tagRegex.exec(envelope.body))) {
    const isClosing = match[1] === '/'
    const rawName = match[2]
    const tagName = rawName.includes(':') ? rawName.split(':')[1] : rawName
    const rawAttrs = match[3] ?? ''
    const attrs = parseAttributes(rawAttrs)
    const isSelfClosing = rawAttrs.trim().endsWith('/')

    if (!isClosing) {
      if (tagName === 't') {
        if (!isSelfClosing) {
          openT = { attrs, textStart: tagRegex.lastIndex }
        }
        continue
      }

      if (tagName === 'e') {
        const deleteCount = Math.max(1, parseInteger(attrs.n) ?? 1)
        const chars = Array.from(nextText)
        chars.splice(Math.max(0, chars.length - deleteCount), deleteCount)
        nextText = chars.join('')
        continue
      }

      if (tagName === 'w' || tagName === 'br') {
        continue
      }
    }

    if (!isClosing || tagName !== 't' || !openT) {
      continue
    }

    const rawText = envelope.body.slice(openT.textStart, match.index)
    const decoded = decodeXmlText(rawText.replace(/\r\n/g, '\n').replace(/\r/g, '\n'))
    if (decoded.length > 0) {
      nextText += decoded
    }
    openT = null
  }

  return { nextText, parsed: true, seq }
}

export function escapeXmlText(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

export function buildOutgoingRttXml(text: string, seq: number, event: 'new' | 'reset'): string {
  const seqValue = Number.isFinite(seq) ? Math.max(0, Math.floor(seq)) : 0
  if (!text.length) {
    return `<rtt event='${event}' seq='${seqValue}'></rtt>`
  }
  return `<rtt event='${event}' seq='${seqValue}'><t>${escapeXmlText(text)}</t></rtt>`
}
