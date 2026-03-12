import { describe, expect, it } from 'vitest'

import { normalizeGatewayWssUrl } from './config'

describe('normalizeGatewayWssUrl', () => {
  it('appends /ws when URL has no path', () => {
    expect(normalizeGatewayWssUrl('wss://example.com')).toBe(
      'wss://example.com/ws',
    )
  })

  it('keeps URL when it already ends with /ws', () => {
    expect(normalizeGatewayWssUrl('wss://example.com/ws')).toBe(
      'wss://example.com/ws',
    )
  })

  it('appends /ws after existing path', () => {
    expect(normalizeGatewayWssUrl('wss://example.com/gateway')).toBe(
      'wss://example.com/gateway/ws',
    )
  })

  it('preserves query params while appending /ws', () => {
    expect(normalizeGatewayWssUrl('wss://example.com?token=abc')).toBe(
      'wss://example.com/ws?token=abc',
    )
  })
})
