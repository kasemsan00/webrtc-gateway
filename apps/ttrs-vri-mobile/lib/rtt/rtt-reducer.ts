import { RttEnvelope, RttEvent, RttSessionEvent } from "./rtt-events";

export interface RttState {
  remoteText: string;
  remoteCursor: number;
  isRemoteTyping: boolean;
  lastSeq: number | null;
  expectedSeq: number | null;
  isOutOfSync: boolean;
  activeMessage: boolean;
}

export const initialRttState: RttState = {
  remoteText: "",
  remoteCursor: 0,
  isRemoteTyping: false,
  lastSeq: null,
  expectedSeq: null,
  isOutOfSync: false,
  activeMessage: false,
};

export type RttReducerAction =
  | RttEvent
  | { type: "remote_envelope"; envelope: RttEnvelope }
  | { type: "clear_remote" };

function toChars(text: string): string[] {
  return Array.from(text);
}

function clampPosition(position: number, text: string): number {
  const length = toChars(text).length;
  return Math.min(Math.max(position, 0), length);
}

function applyInsert(text: string, insertText: string, position: number): { text: string; cursor: number } {
  const chars = toChars(text);
  const insertChars = toChars(insertText);
  const pos = clampPosition(position, text);
  chars.splice(pos, 0, ...insertChars);
  return { text: chars.join(""), cursor: pos + insertChars.length };
}

function applyBackspace(text: string, count: number, cursor: number): { text: string; cursor: number } {
  if (count <= 0) return { text, cursor };
  const chars = toChars(text);
  const safeCursor = clampPosition(cursor, text);
  const start = Math.max(0, safeCursor - count);
  chars.splice(start, safeCursor - start);
  return { text: chars.join(""), cursor: start };
}

function applySessionEvent(state: RttState, event: RttSessionEvent | undefined): RttState {
  if (event === "new" || event === "reset") {
    return {
      ...state,
      remoteText: "",
      remoteCursor: 0,
      isOutOfSync: false,
      activeMessage: true,
    };
  }

  if (event === "cancel") {
    return {
      ...state,
      remoteText: "",
      remoteCursor: 0,
      isRemoteTyping: false,
      isOutOfSync: false,
      activeMessage: false,
    };
  }

  if (event === "init") {
    return {
      ...state,
      isOutOfSync: false,
      activeMessage: true,
    };
  }

  return {
    ...state,
    activeMessage: true,
  };
}

function applyEvent(state: RttState, event: RttEvent): RttState {
  let next = state;

  switch (event.type) {
    case "reset":
      next = { ...state, remoteText: "", remoteCursor: 0 };
      break;
    case "insert": {
      const position = typeof event.position === "number" ? event.position : state.remoteCursor;
      const result = applyInsert(state.remoteText, event.text, position);
      next = { ...state, remoteText: result.text, remoteCursor: result.cursor, isRemoteTyping: true };
      break;
    }
    case "backspace": {
      const position = typeof event.position === "number" ? event.position : state.remoteCursor;
      const result = applyBackspace(state.remoteText, event.count, position);
      next = { ...state, remoteText: result.text, remoteCursor: result.cursor, isRemoteTyping: true };
      break;
    }
    case "newline": {
      const result = applyInsert(state.remoteText, "\n", state.remoteCursor);
      next = { ...state, remoteText: result.text, remoteCursor: result.cursor, isRemoteTyping: true };
      break;
    }
    case "cursor":
      next = { ...state, remoteCursor: clampPosition(event.position, state.remoteText) };
      break;
    case "typing":
      next = { ...state, isRemoteTyping: event.isTyping };
      break;
  }

  return next;
}

function applyEnvelope(state: RttState, envelope: RttEnvelope): RttState {
  const sequence = envelope.seq;

  // Minimal replay protection: drop exact duplicate sequence only.
  if (typeof sequence === "number" && state.lastSeq !== null && sequence === state.lastSeq) {
    return state;
  }

  let next = applySessionEvent(state, envelope.event);
  for (const action of envelope.actions) {
    next = applyEvent(next, action);
  }

  if (typeof sequence === "number") {
    next = {
      ...next,
      lastSeq: sequence,
      expectedSeq: sequence + 1,
      isOutOfSync: false,
    };
  }

  return next;
}

export function rttReducer(state: RttState, action: RttReducerAction): RttState {
  if (action.type === "clear_remote") {
    return {
      ...state,
      remoteText: "",
      remoteCursor: 0,
      isRemoteTyping: false,
      isOutOfSync: false,
      activeMessage: false,
    };
  }

  if (action.type === "remote_envelope") {
    return applyEnvelope(state, action.envelope);
  }

  return applyEvent(state, action);
}
