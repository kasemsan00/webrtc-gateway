import { RttEncoder } from "@/lib/rtt/encoder";

describe("RttEncoder", () => {
  it("batches events within throttle window", () => {
    jest.useFakeTimers();
    const outputs: Array<{ format: string; data: string; event?: string }> = [];

    const encoder = new RttEncoder({ format: "xep-0301", throttleMs: 50 }, (payload) => {
      outputs.push({ format: payload.format, data: payload.data, event: payload.event });
    });

    encoder.enqueue([{ type: "insert", text: "h", position: 0 }], { event: "new" });
    encoder.enqueue([{ type: "insert", text: "i", position: 1 }], { event: "edit" });

    expect(outputs).toHaveLength(0);
    jest.advanceTimersByTime(60);
    expect(outputs).toHaveLength(1);
    expect(outputs[0].format).toBe("xep-0301");
    expect(outputs[0].event).toBe("new");
    expect(outputs[0].data).toContain("urn:xmpp:rtt:0");
    expect(outputs[0].data).toContain("<t p=\"0\">h</t>");
    expect(outputs[0].data).toContain("<t p=\"1\">i</t>");

    jest.useRealTimers();
  });
});
