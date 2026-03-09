import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('react-native', () => ({
  Platform: { OS: 'ios' },
}));

vi.mock('react-native-webrtc', () => ({
  MediaStream: class {},
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

describe('GatewayClient trunk resolve payloads', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends trunk_resolve with trunkPublicId when provided', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    anyClient.ws = { readyState: 1 };
    anyClient.send = vi.fn();

    client.resolveTrunk({ trunkPublicId: '  155f92e3-9d9f-480b-b4a4-4e6536a1d30e  ' });

    expect(anyClient.send).toHaveBeenCalledWith({
      type: 'trunk_resolve',
      trunkPublicId: '155f92e3-9d9f-480b-b4a4-4e6536a1d30e',
    });
  });

  it('sends trunk_resolve with trunkId when public id is absent', async () => {
    const { GatewayClient } = await import('@/lib/gateway/gateway-client');
    const client = new GatewayClient();
    const anyClient = client as any;

    anyClient.ws = { readyState: 1 };
    anyClient.send = vi.fn();

    client.resolveTrunk({ trunkId: 88 });

    expect(anyClient.send).toHaveBeenCalledWith({
      type: 'trunk_resolve',
      trunkId: 88,
    });
  });
});
