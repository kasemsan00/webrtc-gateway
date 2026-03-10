import { describe, expect, it } from 'vitest'

import {
  applyIncomingRtt,
  buildOutgoingRttXml,
  escapeXmlText,
  isRttXmlPayload,
} from './rtt-xep0301'

function applySeq(xmlPackets: string[]) {
  let text = ''
  for (const packet of xmlPackets) {
    const next = applyIncomingRtt(text, packet)
    expect(next.parsed).toBe(true)
    text = next.nextText
  }
  return text
}

describe('isRttXmlPayload', () => {
  it('detects rtt xml payload', () => {
    expect(isRttXmlPayload("<rtt seq='1'><t>a</t></rtt>")).toBe(true)
    expect(isRttXmlPayload('hello')).toBe(false)
  })
})

describe('applyIncomingRtt', () => {
  it('builds english sample sequence to hello World', () => {
    const packets = [
      "<rtt event='new' seq='18345'><t>s</t></rtt>",
      "<rtt seq='18346'><e p='1' n='1'/></rtt>",
      "<rtt seq='18347'><t>h</t></rtt>",
      "<rtt seq='18348'><t>e</t></rtt>",
      "<rtt seq='18349'><t>l</t></rtt>",
      "<rtt seq='18350'><t>l</t></rtt>",
      "<rtt seq='18351'><t>o</t></rtt>",
      "<rtt event='reset' seq='18357'><t>hello World</t><t p='0'/><t p='6'/></rtt>",
    ]

    expect(applySeq(packets)).toBe('hello World')
  })

  it('builds thai sample sequence', () => {
    const packets = [
      "<rtt event='new' seq='21428'><t>ส</t></rtt>",
      "<rtt seq='21429'><t>ว</t></rtt>",
      "<rtt seq='21430'><t>ั</t></rtt>",
      "<rtt seq='21431'><t>ส</t></rtt>",
      "<rtt seq='21432'><t>ด</t></rtt>",
      "<rtt seq='21433'><t>ี</t></rtt>",
      "<rtt seq='21434'><t>ค</t></rtt>",
      "<rtt seq='21435'><t>ร</t></rtt>",
      "<rtt seq='21436'><t>ั</t></rtt>",
      "<rtt seq='21437'><t>บ</t></rtt>",
    ]

    expect(applySeq(packets)).toBe('สวัสดีครับ')
  })

  it('handles reset by replacing current buffer', () => {
    const first = applyIncomingRtt('', "<rtt event='new' seq='1'><t>abc</t></rtt>")
    const second = applyIncomingRtt(
      first.nextText,
      "<rtt event='reset' seq='2'><t>xy</t></rtt>",
    )
    expect(second.nextText).toBe('xy')
    expect(second.seq).toBe(2)
  })

  it('handles reset packet with self-closing t before normal t', () => {
    const result = applyIncomingRtt(
      '',
      "<rtt event='reset' seq='3154'><t>helloworl</t><t p='0'/><t>d</t></rtt>",
    )
    expect(result.parsed).toBe(true)
    expect(result.nextText).toBe('helloworld')
  })

  it('returns parsed=false on malformed xml', () => {
    const result = applyIncomingRtt('hello', "<rtt seq='9'><t>oops</rtt>")
    expect(result.parsed).toBe(false)
    expect(result.nextText).toBe('hello')
    expect(result.seq).toBeNull()
  })
})

describe('xml escaping and outgoing xml builder', () => {
  it('escapes xml entities', () => {
    expect(escapeXmlText(`a&b<c>d"e'f`)).toBe(
      'a&amp;b&lt;c&gt;d&quot;e&apos;f',
    )
  })

  it('builds outgoing xml payload', () => {
    expect(buildOutgoingRttXml('hello', 12, 'new')).toBe(
      "<rtt event='new' seq='12'><t>hello</t></rtt>",
    )
    expect(buildOutgoingRttXml('', 13, 'reset')).toBe(
      "<rtt event='reset' seq='13'></rtt>",
    )
  })
})
