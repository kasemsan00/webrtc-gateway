import { describe, expect, it } from 'vitest'

import { buildGatewayWebSocketUrl } from './ws-client'

describe('buildGatewayWebSocketUrl', () => {
  it('returns original url when token is missing', () => {
    expect(buildGatewayWebSocketUrl('wss://gateway.example.com/ws')).toBe(
      'wss://gateway.example.com/ws',
    )
  })

  it('appends access_token query param', () => {
    expect(
      buildGatewayWebSocketUrl('wss://gateway.example.com/ws', 'token-abc'),
    ).toBe('wss://gateway.example.com/ws?access_token=token-abc')
  })

  it('preserves existing query params', () => {
    expect(
      buildGatewayWebSocketUrl(
        'wss://gateway.example.com/ws?foo=bar',
        'token-abc',
      ),
    ).toBe('wss://gateway.example.com/ws?foo=bar&access_token=token-abc')
  })

  it('replaces existing access_token param', () => {
    expect(
      buildGatewayWebSocketUrl(
        'wss://gateway.example.com/ws?access_token=old&foo=bar',
        'token-abc',
      ),
    ).toBe('wss://gateway.example.com/ws?access_token=token-abc&foo=bar')
  })
})
