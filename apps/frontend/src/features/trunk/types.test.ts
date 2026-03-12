import { describe, expect, it } from 'vitest'

import { normalizeTrunkUid } from './types'

describe('normalizeTrunkUid', () => {
  it('prefers snake_case public_id', () => {
    expect(
      normalizeTrunkUid({
        public_id: 'snake-id',
        publicId: 'camel-id',
      }),
    ).toBe('snake-id')
  })

  it('falls back to camelCase publicId', () => {
    expect(
      normalizeTrunkUid({
        publicId: 'camel-id',
      }),
    ).toBe('camel-id')
  })

  it('returns empty string when no uid fields are provided', () => {
    expect(
      normalizeTrunkUid({
        public_id: undefined,
        publicId: undefined,
      }),
    ).toBe('')
  })
})
