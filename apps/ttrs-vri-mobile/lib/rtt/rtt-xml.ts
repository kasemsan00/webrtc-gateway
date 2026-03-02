import { RttEnvelope, RttEvent, RttSessionEvent } from "./rtt-events";

export const RTT_NAMESPACE = "urn:xmpp:rtt:0";
export const RTT_NAMESPACE_IETF = "urn:ietf:params:xml:ns:rtt";

interface EncodeRttXmlOptions {
  includeXmlns?: boolean;
  xmlns?: string;
}

interface OpenTag {
  name: "t" | "e";
  attrs: Record<string, string>;
  textStart: number;
}

function parseAttributes(raw: string): Record<string, string> {
  const attrs: Record<string, string> = {};
  const attrRegex = /([a-zA-Z_:][a-zA-Z0-9_:\-\.]*)\s*=\s*["']([^"']*)["']/g;
  let match: RegExpExecArray | null = null;
  while ((match = attrRegex.exec(raw))) {
    attrs[match[1]] = match[2];
  }
  return attrs;
}

function parseInteger(value: string | undefined): number | undefined {
  if (value === undefined) return undefined;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) return undefined;
  return parsed;
}

function parsePositiveInteger(value: string | undefined, fallback: number): number {
  const parsed = parseInteger(value);
  if (parsed === undefined || parsed <= 0) return fallback;
  return parsed;
}

function parseSessionEvent(value: string | undefined): RttSessionEvent | undefined {
  if (!value) return undefined;
  const normalized = value.trim().toLowerCase();
  if (normalized === "new" || normalized === "reset" || normalized === "edit" || normalized === "init" || normalized === "cancel") {
    return normalized;
  }
  return undefined;
}

function normalizeLineBreaks(text: string): string {
  return text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
}

function decodeXmlEntities(text: string): string {
  return text
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, "\"")
    .replace(/&apos;/g, "'")
    .replace(/&amp;/g, "&");
}

function escapeXml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

function applyEraseAction(actions: RttEvent[], attrs: Record<string, string>): void {
  const count = parsePositiveInteger(attrs.n, 1);
  const position = parseInteger(attrs.p);
  actions.push({
    type: "backspace",
    count,
    position,
  });
}

export function decodeRttXml(xml: string): RttEnvelope | null {
  if (!xml.includes("<rtt")) {
    return null;
  }

  const rootMatch = xml.match(/<\s*rtt\b([^>]*)>/);
  if (!rootMatch) {
    return null;
  }

  const rootAttrs = parseAttributes(rootMatch[1] ?? "");
  const namespace = rootAttrs.xmlns;
  if (
    namespace &&
    namespace !== RTT_NAMESPACE &&
    namespace !== RTT_NAMESPACE_IETF
  ) {
    return null;
  }

  const actions: RttEvent[] = [];
  const tagRegex = /<\s*(\/?)\s*([a-zA-Z0-9:]+)([^>]*)>/g;
  let currentOpenTag: OpenTag | null = null;
  let match: RegExpExecArray | null = null;

  while ((match = tagRegex.exec(xml))) {
    const isClosingTag = match[1] === "/";
    const fullName = match[2];
    const name = fullName.includes(":") ? fullName.split(":")[1] : fullName;
    const rawAttrs = match[3] ?? "";
    const attrs = parseAttributes(rawAttrs);
    const isSelfClosing = rawAttrs.trim().endsWith("/");

    if (!isClosingTag) {
      if (name === "t") {
        if (!isSelfClosing) {
          currentOpenTag = { name: "t", attrs, textStart: tagRegex.lastIndex };
        }
        continue;
      }

      if (name === "e") {
        if (isSelfClosing) {
          applyEraseAction(actions, attrs);
        } else {
          currentOpenTag = { name: "e", attrs, textStart: tagRegex.lastIndex };
        }
        continue;
      }

      if (name === "w" || name === "br") {
        continue;
      }
    }

    if (!currentOpenTag || name !== currentOpenTag.name || !isClosingTag) {
      continue;
    }

    if (name === "t") {
      const rawText = xml.slice(currentOpenTag.textStart, match.index);
      const text = normalizeLineBreaks(decodeXmlEntities(rawText));
      if (text.length > 0) {
        actions.push({
          type: "insert",
          text,
          position: parseInteger(currentOpenTag.attrs.p),
        });
      }
    } else if (name === "e") {
      applyEraseAction(actions, currentOpenTag.attrs);
    }

    currentOpenTag = null;
  }

  return {
    seq: parseInteger(rootAttrs.seq),
    event: parseSessionEvent(rootAttrs.event),
    actions,
  };
}

export function encodeRttXmlEnvelope(
  envelope: RttEnvelope,
  options?: EncodeRttXmlOptions,
): string {
  const parts: string[] = [];
  const includeXmlns = options?.includeXmlns ?? true;
  const xmlns = options?.xmlns ?? RTT_NAMESPACE;
  const xmlnsAttr = includeXmlns ? ` xmlns="${xmlns}"` : "";
  const seqAttr = typeof envelope.seq === "number" ? ` seq="${envelope.seq}"` : "";
  const eventAttr = envelope.event ? ` event="${envelope.event}"` : "";
  parts.push(`<rtt${xmlnsAttr}${seqAttr}${eventAttr}>`);

  for (const action of envelope.actions) {
    if (action.type === "insert") {
      if (action.text.length === 0) continue;
      const pAttr = typeof action.position === "number" ? ` p="${action.position}"` : "";
      parts.push(`<t${pAttr}>${escapeXml(action.text)}</t>`);
      continue;
    }

    if (action.type === "backspace") {
      const count = Math.max(1, action.count);
      const pAttr = typeof action.position === "number" ? ` p="${action.position}"` : "";
      const nAttr = count > 1 ? ` n="${count}"` : "";
      parts.push(`<e${pAttr}${nAttr}/>`);
      continue;
    }

    if (action.type === "newline") {
      parts.push("<t>\n</t>");
    }
  }

  parts.push("</rtt>");
  return parts.join("");
}

export function encodeRttXmlEvents(
  events: RttEvent[],
  seq?: number,
  event?: RttSessionEvent,
  options?: EncodeRttXmlOptions,
): string {
  return encodeRttXmlEnvelope(
    {
      seq,
      event,
      actions: events,
    },
    options,
  );
}
