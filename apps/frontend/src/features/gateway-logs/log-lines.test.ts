import { describe, expect, it } from 'vitest'

import {
  classifyLogLine,
  extractLogComponent,
  filterLogLines,
  highlightParts,
  parseLogLines,
} from './log-lines'

const LINES = [
  '2026-10-14 09:01:55.004 📞 [TrunkManager] Trunk 1633 REGISTER target=203.151.21.121:5060',
  '2026-10-14 09:01:55.005 📞 [TrunkManager] Trunk 1633: Received 401, retrying with auth',
  '2026-10-14 09:01:55.032 ✅ [TrunkManager] Trunk 1633 registered successfully',
  '2026-10-14 09:02:00.000 ❌ [SIP] INVITE failed: 486 Busy Here call-id=abc123',
  '2026-10-14 09:02:01.000 ⚠️ [WS] client reconnect attempt 2',
]

describe('classifyLogLine', () => {
  it('uses emoji and keywords', () => {
    expect(classifyLogLine(LINES[0])).toBe('info')
    expect(classifyLogLine(LINES[2])).toBe('ok')
    expect(classifyLogLine(LINES[3])).toBe('error')
    expect(classifyLogLine(LINES[4])).toBe('warn')
  })

  it('does not treat the 401 digest challenge as a problem', () => {
    expect(classifyLogLine(LINES[1])).toBe('info')
  })
})

describe('extractLogComponent', () => {
  it('returns the first bracketed tag', () => {
    expect(extractLogComponent(LINES[0])).toBe('TrunkManager')
    expect(extractLogComponent('no tag here')).toBeNull()
  })
})

describe('filterLogLines', () => {
  const parsed = parseLogLines(LINES)

  it('filters by level', () => {
    expect(filterLogLines(parsed, { level: 'errors' })).toHaveLength(1)
    expect(filterLogLines(parsed, { level: 'problems' })).toHaveLength(2)
  })

  it('filters by component and case-insensitive text', () => {
    expect(filterLogLines(parsed, { component: 'TrunkManager' })).toHaveLength(
      3,
    )
    expect(filterLogLines(parsed, { query: 'CALL-ID=ABC' })).toHaveLength(1)
  })

  it('keeps original line indexes for line numbers', () => {
    const [match] = filterLogLines(parsed, { query: 'reconnect' })
    expect(match.index).toBe(4)
  })
})

describe('highlightParts', () => {
  it('splits around every case-insensitive match', () => {
    expect(highlightParts('Trunk trunk', 'TRUNK')).toEqual([
      { text: 'Trunk', match: true },
      { text: ' ', match: false },
      { text: 'trunk', match: true },
    ])
    expect(highlightParts('abc', '')).toEqual([{ text: 'abc', match: false }])
  })
})
