import { beforeEach, describe, expect, it, vi } from 'vitest';

const reportIncomingCall = vi.fn();
const reportCallEnded = vi.fn();

class FakeGatewayClient {
  callbacks: Record<string, (...args: any[]) => void> = {};
  isConnected = false;
  isRegisteredState = true;

  setCallbacks(callbacks: Record<string, (...args: any[]) => void>) {
    this.callbacks = callbacks;
  }

  async connect(_url?: string) {
    this.isConnected = true;
    this.callbacks.onConnected?.();
  }

  disconnect() {}
  unregister() {}
  async register() {}
  async call() {}
  hangup() {}
  toggleMute() {}
  toggleVideo() {}
  async switchCamera() {}
  sendDtmf() {}
  sendMessage() {}
  refreshRemoteVideo() {}
  setReconnectConfig() {}
  async reconnect() {}
  async answer() {}
  decline() {}
  forceDisconnect() {}
  async reconnectForNetworkChange() {}
  getSessionId() {
    return null;
  }
}

const fakeGatewayClient = new FakeGatewayClient();

vi.mock('@/lib/gateway', () => ({
  GatewayClient: FakeGatewayClient,
  getGatewayClient: () => fakeGatewayClient,
  CallState: {
    IDLE: 'idle',
    CALLING: 'calling',
    RINGING: 'ringing',
    CONNECTING: 'connecting',
    INCALL: 'incall',
    INCOMING: 'incoming',
    ENDED: 'ended',
  },
}));

vi.mock('@/lib/callkeep', () => ({
  reportCallAnswered: vi.fn(),
  reportCallEnded,
  reportIncomingCall,
  reportMuteState: vi.fn(),
  reportOutgoingCall: vi.fn(),
  reportOutgoingCallConnecting: vi.fn(),
}));

vi.mock('@/lib/network', () => ({
  getNetworkMonitor: () => ({
    setConnectionHandler: vi.fn(),
    setResumeCallHandler: vi.fn(),
  }),
}));

vi.mock('@/store/settings-store', () => ({
  getSettingsSync: () => ({
    gatewayServer: 'ws://localhost:18080',
    sipDomain: '',
    sipUsername: '',
    sipPassword: '',
    sipDisplayName: '',
    sipPort: 5060,
    callMode: 'public',
  }),
}));

vi.mock('react-native-incall-manager', () => ({
  default: {
    start: vi.fn(),
    stop: vi.fn(),
    setForceSpeakerphoneOn: vi.fn(),
  },
}));

vi.mock('react-native-webrtc', () => ({
  MediaStream: class {},
}));

describe('sip-store incoming state', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    fakeGatewayClient.callbacks = {};
    fakeGatewayClient.isConnected = false;
  });

  it('sets INCOMING state with incoming sessionId and reports CallKeep', async () => {
    const { useSipStore } = await import('@/store/sip-store');

    await useSipStore.getState().connect();
    fakeGatewayClient.callbacks.onIncomingCall?.({
      caller: '1001',
      sessionId: 'incoming-abc',
    });

    const state = useSipStore.getState();
    expect(state.callState).toBe('incoming');
    expect(state.incomingCall?.sessionId).toBe('incoming-abc');
    expect(state.remoteNumber).toBe('1001');
    expect(reportIncomingCall).toHaveBeenCalledWith('1001', '1001');
  });

  it('decline clears incoming state and returns to idle', async () => {
    const { useSipStore } = await import('@/store/sip-store');

    await useSipStore.getState().connect();
    fakeGatewayClient.callbacks.onIncomingCall?.({
      caller: '1002',
      sessionId: 'incoming-def',
    });

    useSipStore.getState().decline();

    const state = useSipStore.getState();
    expect(state.incomingCall).toBeNull();
    expect(state.callState).toBe('idle');
    expect(state._callDirection).toBeNull();
    expect(reportCallEnded).toHaveBeenCalled();
  });
});

