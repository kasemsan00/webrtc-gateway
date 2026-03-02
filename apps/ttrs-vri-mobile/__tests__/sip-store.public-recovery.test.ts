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
    call = jest.fn();
    disconnect = jest.fn();
    hangup = jest.fn();
    refreshRemoteVideo = jest.fn();
    getSessionId = jest.fn(() => null);
    isRegisteredState = true;
    isConnected = true;
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

const PUBLIC_AUTH = {
  mode: "public",
  sipDomain: "example.com",
  sipUsername: "1001",
  sipPassword: "secret",
  sipPort: 5060,
};

describe("useSipStore public recovery behavior", () => {
  beforeEach(() => {
    useSipStore.getState()._reset();
    jest.clearAllMocks();
  });

  it("resetCallRuntimeState clears stale runtime fields", () => {
    useSipStore.setState({
      callState: CallState.INCALL,
      remoteNumber: "14131",
      remoteDisplayName: "Test",
      localStream: { id: "local" },
      remoteStream: { id: "remote" },
      isAutoRecovering: true,
      lastRecoverableError: "PUBLIC_IDENTITY_CHANGED",
      connectionError: "boom",
      messages: [
        {
          id: "m1",
          from: "a",
          to: "b",
          body: "x",
          direction: "incoming",
          status: "sent",
          timestamp: Date.now(),
          read: false,
        },
      ],
      unreadMessageCount: 1,
      _publicCallAuthInMemory: { ...PUBLIC_AUTH },
    });

    useSipStore.getState().resetCallRuntimeState();
    const state = useSipStore.getState();

    expect(state.callState).toBe(CallState.IDLE);
    expect(state.remoteNumber).toBeNull();
    expect(state.localStream).toBeNull();
    expect(state.remoteStream).toBeNull();
    expect(state.messages).toEqual([]);
    expect(state.unreadMessageCount).toBe(0);
    expect(state.connectionError).toBeNull();
    expect(state.isAutoRecovering).toBe(false);
    expect(state.lastRecoverableError).toBeNull();
    expect(state._publicCallAuthInMemory).toBeNull();
  });

  it("resets stale state before public call and keeps auth in-memory during attempt", async () => {
    const gatewayClient = new GatewayClient();
    const callSpy = jest.spyOn(gatewayClient, "call").mockResolvedValue(undefined);

    useSipStore.setState({
      gatewayClient,
      callState: CallState.INCALL,
      remoteNumber: "old",
      messages: [
        {
          id: "m1",
          from: "a",
          to: "b",
          body: "x",
          direction: "incoming",
          status: "sent",
          timestamp: Date.now(),
          read: false,
        },
      ],
      unreadMessageCount: 1,
    });

    await useSipStore.getState().call("14131", PUBLIC_AUTH);
    const state = useSipStore.getState();

    expect(callSpy).toHaveBeenCalledWith("14131", PUBLIC_AUTH);
    expect(state.remoteNumber).toBe("14131");
    expect(state._publicCallAuthInMemory).toEqual(PUBLIC_AUTH);
    expect(state.messages).toEqual([]);
    expect(state.unreadMessageCount).toBe(0);
  });

  it("clears in-memory credentials and sets actionable message when public call fails", async () => {
    const gatewayClient = new GatewayClient();
    const identityError = "Public SIP identity changed (username/domain). Send a new offer to create a new session.";
    jest.spyOn(gatewayClient, "call").mockRejectedValue(new Error(identityError));

    useSipStore.setState({ gatewayClient });

    await expect(useSipStore.getState().call("14131", PUBLIC_AUTH)).rejects.toThrow(identityError);

    const state = useSipStore.getState();
    expect(state._publicCallAuthInMemory).toBeNull();
    expect(state.isAutoRecovering).toBe(false);
    expect(state.lastRecoverableError).toBe("PUBLIC_IDENTITY_CHANGED");
    expect(state.connectionError).toBe("Session เดิมไม่ตรงกับ user ใหม่ กรุณาโทรใหม่");
  });
});
