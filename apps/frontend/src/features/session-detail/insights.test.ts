import { describe, expect, it } from 'vitest'

import {
  eventDirection,
  eventSeverity,
  formatOffset,
  humanizeEventName,
  interpretStats,
} from './insights'
import type { SessionStats } from './types'

function stats(overrides: Partial<SessionStats> = {}): SessionStats {
  return {
    id: 1,
    timestamp: '2026-11-04T10:00:00Z',
    sessionId: 's-1',
    pliSent: 0,
    pliResponse: 0,
    audioRtcpRr: 5,
    audioRtcpSr: 5,
    videoRtcpRr: 0,
    videoRtcpSr: 0,
    ...overrides,
  }
}

describe('eventDirection', () => {
  it('reads direction from the event name', () => {
    expect(eventDirection('sip_invite_sent')).toBe('out')
    expect(eventDirection('sip_message_send_request')).toBe('out')
    expect(eventDirection('sip_bye_received')).toBe('in')
    expect(eventDirection('sip_terminal_action')).toBeNull()
  })
})

describe('eventSeverity', () => {
  it('treats digest challenges as normal and 4xx/5xx as errors', () => {
    expect(eventSeverity({ name: 'sip_invite_sent', sipStatusCode: 401 })).toBe(
      'info',
    )
    expect(eventSeverity({ name: 'sip_invite_sent', sipStatusCode: 407 })).toBe(
      'info',
    )
    expect(eventSeverity({ name: 'x', sipStatusCode: 486 })).toBe('error')
    expect(eventSeverity({ name: 'x', sipStatusCode: 200 })).toBe('success')
  })

  it('flags failure and cancel names without a status code', () => {
    expect(eventSeverity({ name: 'sip_invite_timeout' })).toBe('error')
    expect(eventSeverity({ name: 'sip_message_send_failed' })).toBe('error')
    expect(eventSeverity({ name: 'sip_cancel_received' })).toBe('warning')
    expect(eventSeverity({ name: 'sip_ack_sent' })).toBe('info')
  })
})

describe('humanizeEventName', () => {
  it('upper-cases SIP methods', () => {
    expect(humanizeEventName('sip_invite_sent')).toBe('INVITE sent')
    expect(humanizeEventName('sip_bye_received')).toBe('BYE received')
    expect(humanizeEventName('sip_terminal_action')).toBe('SIP terminal action')
    expect(humanizeEventName('client_ws_open')).toBe('client ws open')
  })
})

describe('formatOffset', () => {
  it('shows signed seconds from the base', () => {
    expect(
      formatOffset('2026-11-04T10:00:01.250Z', '2026-11-04T10:00:00Z'),
    ).toBe('+1.250s')
    expect(formatOffset('2026-11-04T09:59:59Z', '2026-11-04T10:00:00Z')).toBe(
      '-1.000s',
    )
    expect(formatOffset('2026-11-04T10:00:00Z', undefined)).toBe('')
  })
})

describe('interpretStats', () => {
  const now = Date.parse('2026-11-04T10:00:10Z')

  it('explains an empty sample list', () => {
    expect(interpretStats([], now)[0].title).toMatch(/No media stats/)
  })

  it('warns when no audio RTCP arrived', () => {
    const result = interpretStats(
      [stats({ audioRtcpRr: 0, audioRtcpSr: 0 })],
      now,
    )
    expect(result[0]).toMatchObject({ tone: 'warning' })
  })

  it('flags unanswered keyframe requests as an error', () => {
    const result = interpretStats(
      [stats({ pliSent: 4, pliResponse: 0, videoRtcpRr: 3 })],
      now,
    )
    expect(
      result.some((i) => i.tone === 'danger' && /unanswered/.test(i.title)),
    ).toBe(true)
  })

  it('uses the newest sample', () => {
    const result = interpretStats(
      [
        stats({ timestamp: '2026-11-04T10:00:09Z', audioRtcpRr: 9 }),
        stats({
          timestamp: '2026-11-04T09:00:00Z',
          audioRtcpRr: 0,
          audioRtcpSr: 0,
        }),
      ],
      now,
    )
    expect(result[0]).toMatchObject({ tone: 'success' })
  })
})
