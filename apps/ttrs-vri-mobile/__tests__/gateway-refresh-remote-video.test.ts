import { GatewayClient } from "@/lib/gateway/gateway-client";
import { CallState } from "@/lib/gateway/types";

jest.mock("react-native-webrtc", () => ({
  MediaStream: class MediaStreamMock {},
  RTCIceCandidate: class RTCIceCandidateMock {},
  RTCPeerConnection: class RTCPeerConnectionMock {},
  RTCSessionDescription: class RTCSessionDescriptionMock {},
  mediaDevices: {
    getUserMedia: jest.fn(),
  },
}));

jest.mock("@/store/settings-store", () => ({
  getBitrateForResolution: jest.fn(() => 3000),
  getGatewayServerFromEnv: jest.fn(() => "ws://localhost:18080"),
  getSettingsSync: jest.fn(() => ({
    gatewayServer: "ws://localhost:18080",
    sipDomain: "example.com",
    sipUsername: "1001",
    sipPassword: "secret",
    sipPort: 5060,
    turnEnabled: false,
    turnUrl: "",
    turnUsername: "",
    turnPassword: "",
    videoResolution: "720",
  })),
}));

jest.mock("@/constants/webrtc", () => ({
  VIDEO_BITRATE_KBPS: 3000,
}));

jest.mock("@/lib/request-permissions", () => ({
  ensureMediaPermissions: jest.fn(async () => ({
    granted: true,
    missing: [],
  })),
}));

describe("GatewayClient.refreshRemoteVideo", () => {
  it("returns false when peer connection is missing", () => {
    const client = new GatewayClient();
    Reflect.set(client, "_callState", CallState.INCALL);

    const result = client.refreshRemoteVideo("manual");
    expect(result).toBe(false);
  });

  it("returns false when call is not active", () => {
    const client = new GatewayClient();
    Reflect.set(client, "pc", {});
    Reflect.set(client, "_callState", CallState.IDLE);

    const result = client.refreshRemoteVideo("manual");
    expect(result).toBe(false);
  });

  it("returns true and re-emits remote stream callback when active", () => {
    const client = new GatewayClient();
    const onRemoteStream = jest.fn();
    const remoteStream = { id: "remote-1" };

    client.setCallbacks({ onRemoteStream });
    Reflect.set(client, "pc", { getReceivers: () => [] });
    Reflect.set(client, "_callState", CallState.INCALL);
    Reflect.set(client, "remoteStream", remoteStream);

    const result = client.refreshRemoteVideo("manual");
    expect(result).toBe(true);
    expect(onRemoteStream).toHaveBeenCalledWith(remoteStream);

    const stopKeyframeRetry = Reflect.get(client, "stopKeyframeRetry");
    if (typeof stopKeyframeRetry === "function") {
      stopKeyframeRetry.call(client);
    }
  });
});
