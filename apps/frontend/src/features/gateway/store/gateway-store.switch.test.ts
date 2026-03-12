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

    await gatewayActions.sendSwitch('14131', '00025')

    expect(sendSwitchRequestMock).not.toHaveBeenCalled()
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'No active session',
    )
  })

  it('validates queue and agent before calling API', async () => {
    const { gatewayActions, gatewayStore } = await import('./gateway-store')
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'sess-1',
      },
      logs: [],
    }))

    await gatewayActions.sendSwitch('', '00025')
    expect(sendSwitchRequestMock).not.toHaveBeenCalled()
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Queue number is required',
    )

    await gatewayActions.sendSwitch('14131', '')
    expect(sendSwitchRequestMock).not.toHaveBeenCalled()
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Agent username is required',
    )
  })

  it('calls API with current session and logs success', async () => {
    sendSwitchRequestMock.mockResolvedValueOnce({
      status: 'accepted',
      sessionId: 'sess-1',
      queueNumber: '14131',
      agentUsername: '00025',
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

    await gatewayActions.sendSwitch(' 14131 ', ' 00025 ')

    expect(sendSwitchRequestMock).toHaveBeenCalledWith({
      sessionId: 'sess-1',
      queueNumber: '14131',
      agentUsername: '00025',
    })
    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Switch request accepted',
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

    await gatewayActions.sendSwitch('14131', '00025')

    expect(gatewayStore.state.logs.at(-1)?.message).toContain(
      'Switch request failed: trigger failed',
    )
  })
})
