import { beforeEach, describe, expect, it, vi } from 'vitest';

type CallAuth = {
  mode: 'public' | 'siptrunk';
  trunkId?: number;
  trunkPublicId?: string;
};

const runtimeSettings = {
  gatewayServer: 'ws://localhost:18080',
  sipDomain: 'example.org',
  sipUsername: 'user',
  sipPassword: 'pass',
  sipDisplayName: 'user',
  sipPort: 5060,
  callMode: 'siptrunk' as 'public' | 'siptrunk',
  trunkId: 77 as number | null,
  trunkPublicId: 'eb4649d8-1677-4ddf-b536-381f6d9210a8' as string | null,
};

const setResolvedTrunkId = vi.fn();

class FakeGatewayClient {
  callbacks: Record<string, (...args: any[]) => void> = {};
  isConnected = false;
  isRegisteredState = true;
  call = vi.fn(async (_number: string, _auth?: CallAuth) => {});

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
  resolveTrunk() {}
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

vi.mock('@/store/settings-store', () => ({
  getSettingsSync: () => runtimeSettings,
  useSettingsStore: {
    getState: () => ({
      setResolvedTrunkId,
      trunkId: runtimeSettings.trunkId,
      trunkPublicId: runtimeSettings.trunkPublicId,
      callMode: runtimeSettings.callMode,
    }),
  },
}));

vi.mock('@/lib/callkeep', () => ({
  reportCallAnswered: vi.fn(),
  reportCallEnded: vi.fn(),
  reportIncomingCall: vi.fn(),
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

describe('sip-store outbound auth selection', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    runtimeSettings.callMode = 'siptrunk';
    runtimeSettings.trunkPublicId = 'eb4649d8-1677-4ddf-b536-381f6d9210a8';
    runtimeSettings.trunkId = 77;
    fakeGatewayClient.callbacks = {};
    fakeGatewayClient.isConnected = false;
  });

  it('prefers trunkPublicId over trunkId for siptrunk calls', async () => {
    const { useSipStore } = await import('@/store/sip-store');

    await useSipStore.getState().connect();
    await useSipStore.getState().call('1234');

    const authArg = fakeGatewayClient.call.mock.calls[0][1];
    expect(authArg).toMatchObject({
      mode: 'siptrunk',
      trunkPublicId: 'eb4649d8-1677-4ddf-b536-381f6d9210a8',
      trunkId: 77,
    });
  });

  it('falls back to trunkId when trunkPublicId is not configured', async () => {
    runtimeSettings.trunkPublicId = null;
    runtimeSettings.trunkId = 99;

    const { useSipStore } = await import('@/store/sip-store');

    await useSipStore.getState().connect();
    await useSipStore.getState().call('5678');

    expect(fakeGatewayClient.call).toHaveBeenCalledWith(
      '5678',
      expect.objectContaining({ mode: 'siptrunk', trunkId: 99 }),
    );
  });
});
