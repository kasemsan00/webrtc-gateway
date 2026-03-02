import { T140Decoder, encodeT140Events } from "@/lib/rtt/t140";

const decoder = new T140Decoder();

describe("T140Decoder", () => {
  it("decodes insert and backspace", () => {
    const payload = Uint8Array.from([0x68, 0x69, 0x08]);
    const events = decoder.decode(payload);
    expect(events).toEqual([
      { type: "insert", text: "hi" },
      { type: "backspace", count: 1 },
    ]);
  });

  it("decodes CRLF as newline", () => {
    const payload = Uint8Array.from([0x61, 0x0d, 0x0a, 0x62]);
    const events = decoder.decode(payload);
    expect(events).toEqual([
      { type: "insert", text: "a" },
      { type: "newline" },
      { type: "insert", text: "b" },
    ]);
  });
});

describe("encodeT140Events", () => {
  it("encodes insert and newline", () => {
    const bytes = encodeT140Events([
      { type: "insert", text: "ok" },
      { type: "newline" },
    ]);
    expect(Array.from(bytes)).toEqual([0x6f, 0x6b, 0x0d, 0x0a]);
  });
});
