import { describe, expect, it } from 'vitest'

import {
  bangkokDayBoundsIso,
  formatDurationSec,
  formatPreciseDateTime,
  formatRelativeTime,
  formatThaiDateTime,
  secondsBetween,
} from './date-time'

describe('formatRelativeTime', () => {
  const now = Date.parse('2026-11-04T10:00:00Z')

  it('formats past and future offsets compactly', () => {
    expect(formatRelativeTime('2026-11-04T09:59:58Z', now)).toBe('just now')
    expect(formatRelativeTime('2026-11-04T09:59:30Z', now)).toBe('30s ago')
    expect(formatRelativeTime('2026-11-04T09:55:00Z', now)).toBe('5m ago')
    expect(formatRelativeTime('2026-11-04T07:00:00Z', now)).toBe('3h ago')
    expect(formatRelativeTime('2026-11-02T10:00:00Z', now)).toBe('2d ago')
    expect(formatRelativeTime('2026-11-04T10:04:00Z', now)).toBe('in 4m')
  })

  it('accepts epoch milliseconds and tolerates bad input', () => {
    expect(formatRelativeTime(now - 120_000, now)).toBe('2m ago')
    expect(formatRelativeTime('', now)).toBe('-')
    expect(formatRelativeTime('not-a-date', now)).toBe('-')
    expect(formatRelativeTime(undefined, now)).toBe('-')
  })
})

describe('formatDurationSec', () => {
  it('uses m:ss under an hour and h:mm:ss above', () => {
    expect(formatDurationSec(0)).toBe('0:00')
    expect(formatDurationSec(65.9)).toBe('1:05')
    expect(formatDurationSec(3661)).toBe('1:01:01')
    expect(formatDurationSec(undefined)).toBe('-')
    expect(formatDurationSec(Number.NaN)).toBe('-')
  })
})

describe('secondsBetween', () => {
  it('measures to the end timestamp or to now', () => {
    expect(secondsBetween('2026-11-04T10:00:00Z', '2026-11-04T10:01:30Z')).toBe(
      90,
    )
    expect(
      secondsBetween(
        '2026-11-04T10:00:00Z',
        null,
        Date.parse('2026-11-04T10:00:10Z'),
      ),
    ).toBe(10)
    expect(secondsBetween('', '2026-11-04T10:00:00Z')).toBeNull()
  })
})

describe('bangkokDayBoundsIso', () => {
  it('maps a Bangkok calendar day to UTC bounds', () => {
    expect(bangkokDayBoundsIso('2026-11-04', 'start')).toBe(
      '2026-11-03T17:00:00.000Z',
    )
    expect(bangkokDayBoundsIso('2026-11-04', 'end')).toBe(
      '2026-11-04T16:59:59.999Z',
    )
    expect(bangkokDayBoundsIso('04/11/2026', 'start')).toBeUndefined()
  })
})

describe('formatThaiDateTime', () => {
  it('returns a dash for empty values and the input for invalid ones', () => {
    expect(formatThaiDateTime('')).toBe('-')
    expect(formatThaiDateTime(null)).toBe('-')
    expect(formatThaiDateTime('garbage')).toBe('garbage')
  })
})

describe('formatPreciseDateTime', () => {
  it('keeps the date and time, not just the milliseconds', () => {
    const text = formatPreciseDateTime('2026-11-04T02:02:10.400Z')
    expect(text).toContain('09:02:10')
    expect(text).toContain('400')
    expect(text.length).toBeGreaterThan(12)
  })
})
