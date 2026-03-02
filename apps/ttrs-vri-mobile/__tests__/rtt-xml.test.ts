import { decodeRttXml, encodeRttXmlEvents } from "@/lib/rtt/rtt-xml";

describe("decodeRttXml", () => {
  it("decodes seq, event and insert action", () => {
    const xml = "<rtt xmlns=\"urn:xmpp:rtt:0\" seq=\"7\" event=\"new\"><t p=\"1\">h</t></rtt>";
    const envelope = decodeRttXml(xml);
    expect(envelope).toEqual({
      seq: 7,
      event: "new",
      actions: [{ type: "insert", text: "h", position: 1 }],
    });
  });

  it("decodes erase count and position", () => {
    const xml = "<rtt xmlns=\"urn:xmpp:rtt:0\"><e p=\"4\" n=\"2\"/></rtt>";
    const envelope = decodeRttXml(xml);
    expect(envelope).toEqual({
      seq: undefined,
      event: undefined,
      actions: [{ type: "backspace", count: 2, position: 4 }],
    });
  });

  it("accepts IETF namespace for compatibility", () => {
    const xml = "<rtt xmlns=\"urn:ietf:params:xml:ns:rtt\"><t>h</t></rtt>";
    expect(decodeRttXml(xml)).toEqual({
      seq: undefined,
      event: undefined,
      actions: [{ type: "insert", text: "h", position: undefined }],
    });
  });

  it("accepts missing namespace for gateway compatibility", () => {
    const xml = "<rtt seq=\"8\" event=\"new\"><t>ok</t></rtt>";
    expect(decodeRttXml(xml)).toEqual({
      seq: 8,
      event: "new",
      actions: [{ type: "insert", text: "ok", position: undefined }],
    });
  });

  it("handles self-closing t before normal t without leaking markup", () => {
    const xml = "<rtt event=\"reset\" seq=\"3154\"><t>helloworl</t><t p=\"0\"/><t>d</t></rtt>";
    const envelope = decodeRttXml(xml);
    expect(envelope).toEqual({
      seq: 3154,
      event: "reset",
      actions: [
        { type: "insert", text: "helloworl", position: undefined },
        { type: "insert", text: "d", position: undefined },
      ],
    });
  });
});

describe("encodeRttXmlEvents", () => {
  it("encodes xep-0301 insert and erase with event", () => {
    const xml = encodeRttXmlEvents([
      { type: "insert", text: "hi", position: 0 },
      { type: "backspace", count: 2, position: 2 },
    ], 3, "reset");
    expect(xml).toContain("xmlns=\"urn:xmpp:rtt:0\"");
    expect(xml).toContain("seq=\"3\"");
    expect(xml).toContain("event=\"reset\"");
    expect(xml).toContain("<t p=\"0\">hi</t>");
    expect(xml).toContain("<e p=\"2\" n=\"2\"/>");
  });

  it("encodes without xmlns for SIP fallback compatibility", () => {
    const xml = encodeRttXmlEvents([{ type: "insert", text: "hello", position: 0 }], 9, "reset", { includeXmlns: false });
    expect(xml).toContain("<rtt seq=\"9\" event=\"reset\">");
    expect(xml).not.toContain("xmlns=");
    expect(xml).toContain("<t p=\"0\">hello</t>");
  });
});
