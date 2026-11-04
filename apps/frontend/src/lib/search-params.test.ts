import { describe, expect, it } from 'vitest'

import {
  validateLogsSearch,
  validateSessionDetailSearch,
  validateSessionsSearch,
  validateTrunksSearch,
} from './search-params'

describe('validateSessionsSearch', () => {
  it('keeps valid filters and drops junk', () => {
    expect(
      validateSessionsSearch({
        q: '  abc ',
        direction: 'inbound',
        state: 'failed',
        endReason: 'busy',
        from: '2026-11-01',
        to: 'yesterday',
      }),
    ).toEqual({
      q: 'abc',
      direction: 'inbound',
      state: 'failed',
      endReason: 'busy',
      from: '2026-11-01',
      to: undefined,
    })
    expect(
      validateSessionsSearch({ direction: 'sideways' }).direction,
    ).toBeUndefined()
  })
})

describe('validateTrunksSearch', () => {
  it('accepts numeric trunk ids from strings and numbers', () => {
    expect(validateTrunksSearch({ trunkId: '42' }).trunkId).toBe(42)
    expect(validateTrunksSearch({ trunkId: 7 }).trunkId).toBe(7)
    expect(validateTrunksSearch({ trunkId: '-1' }).trunkId).toBeUndefined()
    expect(validateTrunksSearch({ trunkId: 0 }).trunkId).toBeUndefined()
  })
})

describe('validateSessionDetailSearch', () => {
  it('only allows known tabs', () => {
    expect(validateSessionDetailSearch({ tab: 'stats' }).tab).toBe('stats')
    expect(validateSessionDetailSearch({ tab: 'nope' }).tab).toBeUndefined()
  })
})

describe('validateLogsSearch', () => {
  it('trims the query', () => {
    expect(validateLogsSearch({ q: ' call-1 ', file: '' })).toEqual({
      q: 'call-1',
      file: undefined,
    })
  })
})
