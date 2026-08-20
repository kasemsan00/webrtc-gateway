import { describe, expect, it } from 'vitest'

import { instanceSearchFrom, trunkSearchFrom } from './operations-navigation'

describe('operations navigation search', () => {
  it('prefers stable trunk public ID and falls back to numeric ID', () => {
    expect(
      trunkSearchFrom({ trunkPublicId: ' trunk-public ', trunkId: 42 }),
    ).toEqual({ trunkPublicId: 'trunk-public' })
    expect(trunkSearchFrom({ trunkId: 42 })).toEqual({ trunkId: 42 })
  })

  it('omits missing or invalid identifiers', () => {
    expect(trunkSearchFrom({ trunkId: 0 })).toEqual({})
    expect(instanceSearchFrom('   ')).toEqual({})
  })

  it('does not copy unrelated secret-bearing fields into search', () => {
    const source = {
      trunkId: 7,
      password: 'secret',
      token: 'token',
      dsn: 'postgres://secret',
    }
    const search = trunkSearchFrom(source)
    expect(search).toEqual({ trunkId: 7 })
    expect(JSON.stringify(search)).not.toMatch(
      /password|token|postgres|secret/i,
    )
  })

  it('preserves instance identifiers as data for router encoding', () => {
    expect(instanceSearchFrom('gw one&admin=true')).toEqual({
      search: 'gw one&admin=true',
    })
  })
})
