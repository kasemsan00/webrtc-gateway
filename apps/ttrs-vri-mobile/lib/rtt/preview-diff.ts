import { RttEvent } from "./rtt-events";

function commonPrefixLength(a: string[], b: string[]): number {
  const minLength = Math.min(a.length, b.length);
  let prefix = 0;
  while (prefix < minLength && a[prefix] === b[prefix]) {
    prefix += 1;
  }
  return prefix;
}

function commonSuffixLength(a: string[], b: string[], prefix: number): number {
  let suffix = 0;
  while (
    suffix < a.length - prefix &&
    suffix < b.length - prefix &&
    a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
  ) {
    suffix += 1;
  }
  return suffix;
}

export function diffToPreviewEvents(previousText: string, nextText: string): RttEvent[] {
  if (previousText === nextText) {
    return [];
  }

  const previousChars = Array.from(previousText);
  const nextChars = Array.from(nextText);
  const prefix = commonPrefixLength(previousChars, nextChars);
  const suffix = commonSuffixLength(previousChars, nextChars, prefix);

  const removedCount = previousChars.length - prefix - suffix;
  const insertedChars = nextChars.slice(prefix, nextChars.length - suffix);
  const events: RttEvent[] = [];

  if (removedCount > 0) {
    events.push({
      type: "backspace",
      count: removedCount,
      position: prefix + removedCount,
    });
  }

  if (insertedChars.length > 0) {
    events.push({
      type: "insert",
      text: insertedChars.join(""),
      position: prefix,
    });
  }

  return events;
}
