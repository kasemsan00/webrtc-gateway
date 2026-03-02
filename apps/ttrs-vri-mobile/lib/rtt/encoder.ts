import { RttEvent, RttFormat, RttSessionEvent } from "./rtt-events";
import { encodeRttXmlEnvelope } from "./rtt-xml";

export interface RttEncoderConfig {
  format: RttFormat;
  throttleMs: number;
}

export interface RttEnqueueOptions {
  event?: RttSessionEvent;
  immediate?: boolean;
}

const SESSION_EVENT_PRIORITY: Record<RttSessionEvent, number> = {
  edit: 1,
  init: 2,
  new: 2,
  reset: 3,
  cancel: 3,
};

function mergeSessionEvent(current: RttSessionEvent | undefined, incoming: RttSessionEvent | undefined): RttSessionEvent | undefined {
  if (!incoming) return current;
  if (!current) return incoming;
  return SESSION_EVENT_PRIORITY[incoming] > SESSION_EVENT_PRIORITY[current] ? incoming : current;
}

export class RttEncoder {
  private config: RttEncoderConfig;
  private queue: RttEvent[] = [];
  private timer: ReturnType<typeof setTimeout> | null = null;
  private seq = 0;
  private pendingEvent: RttSessionEvent | undefined;
  private onFlush: (payload: { format: RttFormat; data: string; seq: number; event?: RttSessionEvent }) => void;

  constructor(config: RttEncoderConfig, onFlush: (payload: { format: RttFormat; data: string; seq: number; event?: RttSessionEvent }) => void) {
    this.config = config;
    this.onFlush = onFlush;
  }

  updateFormat(format: RttFormat): void {
    this.config = { ...this.config, format };
  }

  enqueue(events: RttEvent[], options?: RttEnqueueOptions): void {
    if (events.length === 0 && !options?.event) return;
    if (events.length > 0) {
      this.queue.push(...events);
    }
    if (options?.event) {
      this.pendingEvent = mergeSessionEvent(this.pendingEvent, options.event);
    }
    if (options?.immediate) {
      this.flush();
      return;
    }
    this.scheduleFlush();
  }

  flush(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    if (this.queue.length === 0 && !this.pendingEvent) return;
    const events = this.queue;
    const event = this.pendingEvent;
    this.queue = [];
    this.pendingEvent = undefined;
    this.seq += 1;

    const xml = encodeRttXmlEnvelope({
      seq: this.seq,
      event,
      actions: events,
    });
    this.onFlush({
      format: this.config.format,
      data: xml,
      seq: this.seq,
      event,
    });
  }

  private scheduleFlush(): void {
    if (this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.flush();
    }, this.config.throttleMs);
  }
}
