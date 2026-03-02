import { GatewayClient } from "@/lib/gateway/gateway-client";

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

const IDENTITY_ERROR = "Public SIP identity changed (username/domain). Send a new offer to create a new session.";

const PUBLIC_AUTH = {
  mode: "public",
  sipDomain: "example.com",
  sipUsername: "1001",
  sipPassword: "secret",
  sipPort: 5060,
};

describe("GatewayClient public identity recovery", () => {
  it("triggers one-shot recovery on first identity mismatch", () => {
    const client = new GatewayClient();
    const onError = jest.fn();

    client.setCallbacks({ onError });
    Reflect.set(client, "pendingCallIntent", {
      destination: "14131",
      auth: PUBLIC_AUTH,
      attempt: 0,
    });

    const retrySpy = jest.spyOn(client, "retryPublicIdentityCall").mockResolvedValue(true);
    const handleMessage = Reflect.get(client, "handleMessage");

    handleMessage(JSON.stringify({ type: "error", message: IDENTITY_ERROR }));

    expect(retrySpy).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
  });

  it("marks retry failed and forwards error after retry attempt is exhausted", () => {
    const client = new GatewayClient();
    const onError = jest.fn();
    const onRecoveryState = jest.fn();

    client.setCallbacks({ onError, onRecoveryState });
    Reflect.set(client, "pendingCallIntent", {
      destination: "14131",
      auth: PUBLIC_AUTH,
      attempt: 1,
    });

    const handleMessage = Reflect.get(client, "handleMessage");
    handleMessage(JSON.stringify({ type: "error", message: IDENTITY_ERROR }));

    expect(onRecoveryState).toHaveBeenCalledWith("retry_failed");
    expect(onError).toHaveBeenCalledWith(IDENTITY_ERROR);
  });

  it("does not invoke recovery for non-identity errors", () => {
    const client = new GatewayClient();
    const onError = jest.fn();

    client.setCallbacks({ onError });
    Reflect.set(client, "pendingCallIntent", {
      destination: "14131",
      auth: PUBLIC_AUTH,
      attempt: 0,
    });

    const retrySpy = jest.spyOn(client, "retryPublicIdentityCall").mockResolvedValue(true);
    const handleMessage = Reflect.get(client, "handleMessage");
    handleMessage(JSON.stringify({ type: "error", message: "Call failed: busy" }));

    expect(retrySpy).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith("Call failed: busy");
  });
});
