export type RttFormat = "xep-0301";

export type RttSessionEvent = "new" | "reset" | "edit" | "init" | "cancel";

export type RttEvent =
  | { type: "insert"; text: string; position?: number }
  | { type: "backspace"; count: number; position?: number }
  | { type: "newline" }
  | { type: "cursor"; position: number }
  | { type: "reset" }
  | { type: "typing"; isTyping: boolean };

export interface RttEnvelope {
  seq?: number;
  event?: RttSessionEvent;
  actions: RttEvent[];
}

export interface RttIncomingPayload {
  format: RttFormat;
  payload: string | Uint8Array;
  contentType?: string;
  seq?: number;
  event?: RttSessionEvent;
}

export interface RttOutgoingPayload {
  format: RttFormat;
  payload: string | Uint8Array;
  contentType?: string;
  seq?: number;
  event?: RttSessionEvent;
}
