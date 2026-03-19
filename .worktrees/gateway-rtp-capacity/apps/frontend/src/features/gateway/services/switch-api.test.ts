import { describe, expect, it, vi } from 'vitest'

const fetchJsonMock = vi.fn()

vi.mock('@/lib/http-client', () => ({
  fetchJson: fetchJsonMock,
  resolveGatewayApiBaseUrl: vi.fn(() => 'http://gateway.local/api'),
}))

describe('sendSwitchRequest', () => {
  it('posts switch payload to /api/switch', async () => {
    const response = {
      status: 'accepted',
      sessionId: 'sess-1',
      autoMode: true,
    }
    fetchJsonMock.mockResolvedValueOnce(response)

    const { sendSwitchRequest } = await import('./switch-api')
    const result = await sendSwitchRequest({
      sessionId: 'sess-1',
    })

    expect(result).toEqual(response)
    expect(fetchJsonMock).toHaveBeenCalledWith('http://gateway.local/api/switch', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        sessionId: 'sess-1',
      }),
    })
  })

  it('propagates backend error from fetchJson', async () => {
    fetchJsonMock.mockRejectedValueOnce(new Error('Session ID is required'))
    const { sendSwitchRequest } = await import('./switch-api')

    await expect(
      sendSwitchRequest({
        sessionId: '',
      }),
    ).rejects.toThrow('Session ID is required')
  })
})
