import { GatewayClient } from "@/lib/gateway";

export type RttTransportKind = "datachannel" | "sip";

export interface RttTransportMessage {
  data: string | Uint8Array;
  contentType?: string;
}

export interface RttTransport {
  kind: RttTransportKind;
  isOpen(): boolean;
  send(data: string | Uint8Array, contentType?: string): void;
  onMessage(handler: (message: RttTransportMessage) => void): () => void;
}

export class DataChannelTransport implements RttTransport {
  kind: RttTransportKind = "datachannel";
  private client: GatewayClient;

  constructor(client: GatewayClient) {
    this.client = client;
  }

  isOpen(): boolean {
    return this.client.isRttDataChannelOpen();
  }

  send(data: string | Uint8Array): void {
    this.client.sendRttData(data);
  }

  onMessage(handler: (message: RttTransportMessage) => void): () => void {
    return this.client.addRttListener((msg) => {
      if (msg.via !== "datachannel") return;
      handler({ data: msg.data });
    });
  }
}

export class SipMessageTransport implements RttTransport {
  kind: RttTransportKind = "sip";
  private client: GatewayClient;

  constructor(client: GatewayClient) {
    this.client = client;
  }

  isOpen(): boolean {
    return this.client.isConnected;
  }

  send(data: string | Uint8Array, contentType?: string): void {
    if (typeof data === "string") {
      this.client.sendMessage(data, contentType);
      return;
    }

    let text = "";
    for (let i = 0; i < data.length; i++) {
      text += String.fromCharCode(data[i]);
    }
    this.client.sendMessage(text, contentType);
  }

  onMessage(handler: (message: RttTransportMessage) => void): () => void {
    return this.client.addRttListener((msg) => {
      if (msg.via !== "sip") return;
      handler({ data: msg.data, contentType: msg.contentType });
    });
  }
}
