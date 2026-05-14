import { describe, expect, it } from 'vitest'

import {
  buildTerminalOutcomeChartData,
  summarizeTerminalOutcomes,
} from './terminal-outcomes'

describe('terminal outcome helpers', () => {
  it('aggregates production SIP terminal outcomes', () => {
    const summary = summarizeTerminalOutcomes([
      {
        outcome: 'busy',
        direction: 'inbound',
        sipStatusCode: 486,
        count: 2,
      },
      {
        outcome: 'decline',
        direction: 'inbound',
        sipStatusCode: 486,
        count: 1,
      },
      {
        outcome: 'no_answer',
        direction: 'inbound',
        sipStatusCode: 480,
        count: 3,
      },
    ])

    expect(summary).toEqual([
      expect.objectContaining({
        key: 'busy',
        label: 'Declined / Busy',
        count: 3,
        sipStatusCode: 486,
      }),
      expect.objectContaining({
        key: 'no_answer',
        label: 'No Answer / Offline',
        count: 3,
        sipStatusCode: 480,
      }),
    ])
  })

  it('labels chart rows by SIP behavior outcome', () => {
    expect(
      buildTerminalOutcomeChartData([
        {
          outcome: 'caller_cancelled',
          direction: 'inbound',
          sipStatusCode: 487,
          count: 1,
        },
      ])[0],
    ).toMatchObject({
      outcomeLabel: 'Caller Cancelled',
      directionLabel: 'inbound',
    })
  })
})
