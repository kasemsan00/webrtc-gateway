import { describe, expect, it } from 'vitest'

import { describeComponents, overallHealth, reasonText } from './health'

describe('overallHealth', () => {
  it('is unknown without data', () => {
    expect(overallHealth(undefined)).toBe('unknown')
  })

  it('ignores disabled and unreported components', () => {
    expect(
      overallHealth({
        database: { state: 'connected' },
        translator: { state: 'disabled', reason: 'translator_not_configured' },
        sipListener: { state: 'unknown', reason: 'readiness_not_reported' },
      }),
    ).toBe('healthy')
  })

  it('reports degraded and down', () => {
    expect(
      overallHealth({
        database: { state: 'connected' },
        eventQueue: { state: 'degraded', reason: 'queue_dropping' },
      }),
    ).toBe('degraded')
    expect(
      overallHealth({
        database: { state: 'unavailable' },
        eventQueue: { state: 'degraded' },
      }),
    ).toBe('down')
  })
})

describe('describeComponents', () => {
  it('orders known components first and explains reasons and queue details', () => {
    const views = describeComponents({
      zeta: { state: 'connected' },
      eventQueue: {
        state: 'degraded',
        reason: 'queue_dropping',
        details: { depth: 3, capacity: 100, accepted: 10, dropped: 2 },
      },
      database: { state: 'connected' },
    })
    expect(views.map((v) => v.key)).toEqual(['database', 'eventQueue', 'zeta'])
    expect(views[1]).toMatchObject({
      label: 'Event write queue',
      tone: 'warning',
      stateLabel: 'Degraded',
      detailText: 'depth 3/100 · accepted 10 · dropped 2',
    })
    expect(views[2].label).toBe('Zeta')
  })
})

describe('reasonText', () => {
  it('maps known reasons and humanizes others', () => {
    expect(reasonText('database_pool_unavailable')).toMatch(/database/)
    expect(reasonText('some_new_reason')).toBe('Some new reason')
    expect(reasonText(undefined)).toBeUndefined()
  })
})
