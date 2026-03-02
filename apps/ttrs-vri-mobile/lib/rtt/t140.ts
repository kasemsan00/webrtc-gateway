import { RttEvent } from "./rtt-events";

const BACKSPACE = 0x08;
const CR = 0x0d;
const LF = 0x0a;

function decodeUtf8(bytes: Uint8Array): string {
  if (bytes.length === 0) return "";

  if (typeof TextDecoder !== "undefined") {
    const decoder = new TextDecoder("utf-8", { fatal: false });
    return decoder.decode(bytes);
  }

  let result = "";
  let i = 0;
  while (i < bytes.length) {
    const byte1 = bytes[i++];
    if (byte1 < 0x80) {
      result += String.fromCharCode(byte1);
      continue;
    }
    if (byte1 >= 0xc0 && byte1 < 0xe0) {
      const byte2 = bytes[i++] ?? 0;
      const code = ((byte1 & 0x1f) << 6) | (byte2 & 0x3f);
      result += String.fromCharCode(code);
      continue;
    }
    if (byte1 >= 0xe0 && byte1 < 0xf0) {
      const byte2 = bytes[i++] ?? 0;
      const byte3 = bytes[i++] ?? 0;
      const code = ((byte1 & 0x0f) << 12) | ((byte2 & 0x3f) << 6) | (byte3 & 0x3f);
      result += String.fromCharCode(code);
      continue;
    }
    if (byte1 >= 0xf0) {
      const byte2 = bytes[i++] ?? 0;
      const byte3 = bytes[i++] ?? 0;
      const byte4 = bytes[i++] ?? 0;
      let codePoint = ((byte1 & 0x07) << 18) | ((byte2 & 0x3f) << 12) | ((byte3 & 0x3f) << 6) | (byte4 & 0x3f);
      codePoint -= 0x10000;
      const high = 0xd800 + (codePoint >> 10);
      const low = 0xdc00 + (codePoint & 0x3ff);
      result += String.fromCharCode(high, low);
    }
  }

  return result;
}

export class T140Decoder {
  private pendingBytes: number[] = [];

  decode(payload: Uint8Array): RttEvent[] {
    const events: RttEvent[] = [];
    const flushPending = () => {
      if (this.pendingBytes.length === 0) return;
      const text = decodeUtf8(Uint8Array.from(this.pendingBytes));
      this.pendingBytes = [];
      if (text.length > 0) {
        events.push({ type: "insert", text });
      }
    };

    for (let i = 0; i < payload.length; i++) {
      const byte = payload[i];
      if (byte === BACKSPACE) {
        flushPending();
        events.push({ type: "backspace", count: 1 });
        continue;
      }
      if (byte === CR) {
        flushPending();
        const next = payload[i + 1];
        if (next === LF) {
          i++;
        }
        events.push({ type: "newline" });
        continue;
      }
      if (byte === LF) {
        flushPending();
        events.push({ type: "newline" });
        continue;
      }
      this.pendingBytes.push(byte);
    }

    if (this.pendingBytes.length > 0) {
      const { complete, pending } = splitUtf8Bytes(this.pendingBytes);
      const text = decodeUtf8(Uint8Array.from(complete));
      this.pendingBytes = pending;
      if (text.length > 0) {
        events.push({ type: "insert", text });
      }
    }
    return events;
  }
}

function splitUtf8Bytes(bytes: number[]): { complete: number[]; pending: number[] } {
  if (bytes.length === 0) return { complete: [], pending: [] };

  let i = bytes.length - 1;
  let continuation = 0;
  while (i >= 0 && (bytes[i] & 0xc0) === 0x80) {
    continuation += 1;
    i -= 1;
  }

  if (i < 0) {
    return { complete: [], pending: bytes };
  }

  const lead = bytes[i];
  let expected = 1;
  if (lead >= 0xf0) expected = 4;
  else if (lead >= 0xe0) expected = 3;
  else if (lead >= 0xc0) expected = 2;

  const actual = continuation + 1;
  if (expected > actual) {
    const complete = bytes.slice(0, i);
    const pending = bytes.slice(i);
    return { complete, pending };
  }

  return { complete: bytes, pending: [] };
}

export function encodeT140Events(events: RttEvent[]): Uint8Array {
  const bytes: number[] = [];
  const encoder = typeof TextEncoder !== "undefined" ? new TextEncoder() : null;
  for (const event of events) {
    if (event.type === "insert") {
      if (encoder) {
        const chunk = encoder.encode(event.text);
        for (const b of chunk) bytes.push(b);
      } else {
        for (let i = 0; i < event.text.length; i++) {
          bytes.push(event.text.charCodeAt(i));
        }
      }
    } else if (event.type === "backspace") {
      const count = Math.max(0, event.count);
      for (let i = 0; i < count; i++) bytes.push(BACKSPACE);
    } else if (event.type === "newline") {
      bytes.push(CR, LF);
    }
  }
  return Uint8Array.from(bytes);
}
