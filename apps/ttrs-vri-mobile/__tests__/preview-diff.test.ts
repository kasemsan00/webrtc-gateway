import { diffToPreviewEvents } from "@/lib/rtt/preview-diff";

describe("diffToPreviewEvents", () => {
  it("creates insert event for append", () => {
    const events = diffToPreviewEvents("", "abc");
    expect(events).toEqual([{ type: "insert", text: "abc", position: 0 }]);
  });

  it("creates erase and insert for middle replacement", () => {
    const events = diffToPreviewEvents("hallo", "hello");
    expect(events).toEqual([
      { type: "backspace", count: 1, position: 2 },
      { type: "insert", text: "e", position: 1 },
    ]);
  });
});
