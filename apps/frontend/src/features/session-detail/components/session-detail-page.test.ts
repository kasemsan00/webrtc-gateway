import { describe, expect, it } from 'vitest'

import { formatEvidence } from './session-detail-page'

describe('session evidence formatting', () => {
  it('formats structured event, payload, and stats data as text', () => {
    expect(
      formatEvidence({ sipCallId: 'call-1', nested: { ok: true } }),
    ).toContain('"sipCallId": "call-1"')
    expect(formatEvidence({ mediaForwardReady: true })).toContain(
      '"mediaForwardReady": true',
    )
  })

  it('bounds large evidence previews', () => {
    const formatted = formatEvidence('x'.repeat(50_000))
    expect(formatted.length).toBeLessThan(16_100)
    expect(formatted).toContain('preview truncated')
  })

  it('keeps backend markup inert as plain text', () => {
    const value = '<img src=x onerror=alert(1)>'
    expect(formatEvidence(value)).toBe(value)
  })

  it('handles absent legacy evidence', () => {
    expect(formatEvidence(undefined)).toBe('(empty)')
    expect(formatEvidence('')).toBe('(empty)')
  })
})
