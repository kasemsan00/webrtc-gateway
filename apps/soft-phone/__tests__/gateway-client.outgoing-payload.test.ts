import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('react-native', () => ({
  Platform: { OS: 'ios' },
}));

vi.mock('react-native-webrtc', () => ({
  MediaStream: class {
    getTracks() {
      return [];
    }
    getAudioTracks() {
      return [];
    }
    getVideoTracks() {
      return [];
    }
    addTrack() {}
  },
  RTCIceCandidate: class {},
  RTCPeerConnection: class {},
  RTCSessionDescription: class {
    constructor(public value: unknown) {
      void value;
    }
  },
  mediaDevices: {
    getUserMedia: vi.fn(async () => ({
      getTracks: () => [],
      getAudioTracks: () => [],
      getVideoTracks: () => [],
    })),
  },
}));

vi.mock('@/store/settings-store', () => ({
  getSettingsSync: vi.fn(() => ({
    gatewayServer: 'ws://localhost:18080',
    videoResolution: 'vga',
    turnEnabled: false,
    turnUrl: '',
    turnUsername: '',
    turnPassword: '',
    sipDomain: '',
    sipUsername: '',
    sipPassword: '',
    sipPort: 5060,
  })),
  getBitrateForResolution: vi.fn(() => 512),
}));

function setupClientForAnswerFlow(client: any) {
  client.pendingCandidates = [];
  client.send = vi.fn();
  client.pc = {
    setRemoteDescription: vi.fn(async () => {}),
    addIceCandidate: vi.fn(async () => {}),
    getTransceivers: vi.fn(() => []),
    getReceivers: vi.fn(() => []),
  };
}

describe('GatewayClient outgoing call payload', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('prioritizes trunkPublicId over trunkId for siptrunk call payload', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    setupClientForAnswerFlow(anyClient);
    anyClient.pendingDestination = '9999';
    anyClient.activeSessionId = 'sess-1';
    anyClient.pendingCallAuth = {
      mode: 'siptrunk',
      trunkPublicId: '9f6fcf7d-5e80-4cb5-b429-2f77fe1893cb',
      trunkId: 42,
      from: 'Desk 1',
    };

    await anyClient.handleAnswer('v=0\r\n', 'sess-1');

    const sent = anyClient.send.mock.calls[0][0];
    expect(sent).toMatchObject({
      type: 'call',
      sessionId: 'sess-1',
      destination: '9999',
      trunkPublicId: '9f6fcf7d-5e80-4cb5-b429-2f77fe1893cb',
      from: 'Desk 1',
    });
    expect(sent.trunkId).toBeUndefined();
  });

  it('falls back to trunkId when trunkPublicId is not provided', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    setupClientForAnswerFlow(anyClient);
    anyClient.pendingDestination = '1001';
    anyClient.activeSessionId = 'sess-2';
    anyClient.pendingCallAuth = {
      mode: 'siptrunk',
      trunkId: 99,
    };

    await anyClient.handleAnswer('v=0\r\n', 'sess-2');

    expect(anyClient.send).toHaveBeenCalledWith({
      type: 'call',
      sessionId: 'sess-2',
      destination: '1001',
      trunkId: 99,
    });
  });
});
