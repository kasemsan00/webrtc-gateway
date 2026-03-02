import { initialRttState, rttReducer } from "@/lib/rtt/rtt-reducer";

describe("rttReducer", () => {
  it("applies in-order envelopes", () => {
    const state1 = rttReducer(initialRttState, {
      type: "remote_envelope",
      envelope: {
        seq: 1,
        event: "new",
        actions: [{ type: "insert", text: "hi", position: 0 }],
      },
    });
    expect(state1.remoteText).toBe("hi");

    const state2 = rttReducer(state1, {
      type: "remote_envelope",
      envelope: {
        seq: 2,
        event: "edit",
        actions: [{ type: "backspace", count: 1, position: 2 }],
      },
    });
    expect(state2.remoteText).toBe("h");
    expect(state2.isOutOfSync).toBe(false);
  });

  it("remains tolerant on seq gaps and still applies text", () => {
    const state1 = rttReducer(initialRttState, {
      type: "remote_envelope",
      envelope: {
        seq: 1,
        event: "new",
        actions: [{ type: "insert", text: "ab", position: 0 }],
      },
    });
    const state2 = rttReducer(state1, {
      type: "remote_envelope",
      envelope: {
        seq: 3,
        event: "edit",
        actions: [{ type: "insert", text: "c", position: 2 }],
      },
    });
    expect(state2.isOutOfSync).toBe(false);
    expect(state2.remoteText).toBe("abc");
    expect(state2.lastSeq).toBe(3);
    expect(state2.expectedSeq).toBe(4);
  });

  it("drops exact duplicate seq as replay protection", () => {
    const state1 = rttReducer(initialRttState, {
      type: "remote_envelope",
      envelope: {
        seq: 10,
        event: "new",
        actions: [{ type: "insert", text: "xy", position: 0 }],
      },
    });

    const state2 = rttReducer(state1, {
      type: "remote_envelope",
      envelope: {
        seq: 10,
        event: "edit",
        actions: [{ type: "insert", text: "z", position: 2 }],
      },
    });
    expect(state2.remoteText).toBe("xy");
  });

  it("clears remote state with clear_remote action", () => {
    const state1 = rttReducer(initialRttState, {
      type: "remote_envelope",
      envelope: {
        seq: 1,
        event: "new",
        actions: [{ type: "insert", text: "hello", position: 0 }],
      },
    });
    const state2 = rttReducer(state1, { type: "clear_remote" });
    expect(state2.remoteText).toBe("");
    expect(state2.isRemoteTyping).toBe(false);
    expect(state2.activeMessage).toBe(false);
  });
});
