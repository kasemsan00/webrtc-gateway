import { beforeEach, describe, expect, it, vi } from 'vitest'

const sendSwitchRequestMock = vi.fn()

vi.mock('../services/switch-api', () => ({
  sendSwitchRequest: sendSwitchRequestMock,
}))

describe('sendSwitch', () => {
  beforeEach(async () => {
    sendSwitchRequestMock.mockReset()
    const { gatewayStore } = await import('./gateway-store')
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: null,
      },
      logs: [],
    }))
  })

  it('does not call API when sessionId is missing', async () => {
    const { gatewayActions, gatewayStore } = await import('./gateway-store')

    await gatewayActions.sendSwitch()

    expect(sendSwitchRequestMock).not.toHaveBeenCalled()
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'No active session',
    )
  })

  it('calls API with current session and logs success', async () => {
    sendSwitchRequestMock.mockResolvedValueOnce({
      status: 'accepted',
      sessionId: 'sess-1',
      autoMode: true,
    })
    const { gatewayActions, gatewayStore } = await import('./gateway-store')
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'sess-1',
      },
      logs: [],
    }))

    await gatewayActions.sendSwitch()

    expect(sendSwitchRequestMock).toHaveBeenCalledWith({
      sessionId: 'sess-1',
    })
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Switch request accepted for current session',
    )
  })

  it('logs API error when request fails', async () => {
    sendSwitchRequestMock.mockRejectedValueOnce(new Error('trigger failed'))
    const { gatewayActions, gatewayStore } = await import('./gateway-store')
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'sess-1',
      },
      logs: [],
    }))

    await gatewayActions.sendSwitch()

    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Switch request failed: trigger failed',
    )
  })
})
