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

describe('GatewayClient incoming flow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends accept only (no unsupported incoming answer) when active session already exists', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    anyClient.incomingSessionId = 'incoming-1';
    anyClient.activeSessionId = 'active-1';
    anyClient.getLocalMedia = vi.fn(async () => {});
    anyClient.createPeerConnection = vi.fn(async () => {});
    anyClient.applyOutgoingVideoBitrateLimit = vi.fn(async () => {});
    anyClient.send = vi.fn();

    await client.answer();

    const sent = anyClient.send.mock.calls.map((call: [Record<string, unknown>]) => call[0]);
    expect(sent).toEqual([{ type: 'accept', sessionId: 'incoming-1' }]);
    expect(sent.some((msg: Record<string, unknown>) => msg.type === 'answer')).toBe(false);
  });

  it('creates offer then accepts incoming call when no active session exists', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    anyClient.incomingSessionId = 'incoming-2';
    anyClient.activeSessionId = null;
    anyClient.getLocalMedia = vi.fn(async () => {});
    anyClient.createPeerConnection = vi.fn(async () => {});
    anyClient.applyOutgoingVideoBitrateLimit = vi.fn(async () => {});
    anyClient.waitForIceGathering = vi.fn(async () => {});
    anyClient.waitForActiveSessionId = vi.fn(async () => {
      anyClient.activeSessionId = 'active-2';
    });
    anyClient.send = vi.fn();
    anyClient.pc = {
      localDescription: { sdp: 'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n' },
      createOffer: vi.fn(async () => ({ type: 'offer', sdp: 'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n' })),
      setLocalDescription: vi.fn(async () => {}),
    };

    await client.answer();

    const sent = anyClient.send.mock.calls.map((call: [Record<string, unknown>]) => call[0]);
    expect(sent[0].type).toBe('offer');
    expect(sent[1]).toEqual({ type: 'accept', sessionId: 'incoming-2' });
  });

  it('decline sends reject using incoming session id and clears incoming session', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    anyClient.incomingSessionId = 'incoming-3';
    anyClient.send = vi.fn();
    anyClient.cleanup = vi.fn();

    client.decline();

    expect(anyClient.send).toHaveBeenCalledWith({
      type: 'reject',
      sessionId: 'incoming-3',
    });
    expect(anyClient.incomingSessionId).toBeNull();
  });
});

