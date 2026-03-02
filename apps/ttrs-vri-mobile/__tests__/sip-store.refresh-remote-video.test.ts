import { GatewayClient } from "@/lib/gateway";
import { CallState, useSipStore } from "@/store/sip-store";

jest.mock("react-native-incall-manager", () => ({
  start: jest.fn(),
  stop: jest.fn(),
  setForceSpeakerphoneOn: jest.fn(),
}));

jest.mock("@/lib/callkeep", () => ({
  reportCallAnswered: jest.fn(),
  reportCallEnded: jest.fn(),
  reportMuteState: jest.fn(),
  reportOutgoingCall: jest.fn(),
  reportOutgoingCallConnecting: jest.fn(),
}));

jest.mock("@/lib/network", () => ({
  getNetworkMonitor: jest.fn(() => ({
    setReconnectHandler: jest.fn(),
    setResumeCallHandler: jest.fn(),
    saveCallState: jest.fn(),
  })),
}));

jest.mock("@/store/settings-store", () => ({
  getGatewayServerFromEnv: jest.fn(() => "ws://localhost:18080"),
}));

jest.mock("@/lib/gateway", () => {
  class GatewayClientMock {
    refreshRemoteVideo = jest.fn();
  }

  return {
    CallState: {
      IDLE: "idle",
      CALLING: "calling",
      RINGING: "ringing",
      CONNECTING: "connecting",
      INCALL: "incall",
      ENDED: "ended",
    },
    GatewayClient: GatewayClientMock,
    getGatewayClient: jest.fn(() => new GatewayClientMock()),
  };
});

describe("useSipStore.refreshRemoteVideo", () => {
  beforeEach(() => {
    useSipStore.getState()._reset();
    jest.clearAllMocks();
  });

  it("calls gateway refresh when call is active", () => {
    const gatewayClient = new GatewayClient();
    const refreshSpy = jest.spyOn(gatewayClient, "refreshRemoteVideo");

    useSipStore.setState({
      gatewayClient,
      callState: CallState.INCALL,
    });

    useSipStore.getState().refreshRemoteVideo();
    expect(refreshSpy).toHaveBeenCalledWith("manual");
  });

  it("does not call gateway refresh when call is not active", () => {
    const gatewayClient = new GatewayClient();
    const refreshSpy = jest.spyOn(gatewayClient, "refreshRemoteVideo");

    useSipStore.setState({
      gatewayClient,
      callState: CallState.IDLE,
    });

    useSipStore.getState().refreshRemoteVideo();
    expect(refreshSpy).not.toHaveBeenCalled();
  });

  it("does not throw when gateway client is missing", () => {
    useSipStore.setState({
      gatewayClient: null,
      callState: CallState.INCALL,
    });

    expect(() => useSipStore.getState().refreshRemoteVideo()).not.toThrow();
  });
});
