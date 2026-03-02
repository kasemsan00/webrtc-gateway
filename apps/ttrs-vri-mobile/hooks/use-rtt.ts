import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";

import { getGatewayClient } from "@/lib/gateway";
import { RttEvent, RttFormat, RttSessionEvent } from "@/lib/rtt/rtt-events";
import { decodeRttXml, encodeRttXmlEnvelope } from "@/lib/rtt/rtt-xml";
import {
  initialRttState,
  rttReducer,
  type RttState,
} from "@/lib/rtt/rtt-reducer";
import {
  DataChannelTransport,
  SipMessageTransport,
  type RttTransport,
} from "@/lib/rtt/transport";

interface UseRttOptions {
  enabled: boolean;
  preferredFormat?: RttFormat;
  throttleMs?: number;
}

interface UseRttReturn {
  state: RttState;
  localText: string;
  setLocalText: (text: string) => void;
  onSelectionChange: (start: number, end: number) => void;
  resetLocal: () => void;
  clearRemote: () => void;
}

function decodePayloadAsText(data: string | Uint8Array): string {
  if (typeof data === "string") {
    return data;
  }

  if (typeof TextDecoder !== "undefined") {
    return new TextDecoder("utf-8", { fatal: false }).decode(data);
  }

  let text = "";
  for (let i = 0; i < data.length; i++) {
    text += String.fromCharCode(data[i]);
  }
  return text;
}

export function useRtt({
  enabled,
  preferredFormat = "xep-0301",
  throttleMs = 100,
}: UseRttOptions): UseRttReturn {
  const [state, dispatch] = useReducer(rttReducer, initialRttState);
  const [localText, setLocalText] = useState("");
  const localCursor = useRef({ start: 0, end: 0 });
  const localTextRef = useRef("");
  const hasActiveMessageRef = useRef(false);
  const remoteTypingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const outgoingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const outgoingSeq = useRef(0);
  const outgoingPendingText = useRef("");
  const outgoingLastSentText = useRef("");

  const client = useMemo(() => getGatewayClient(), []);
  const dataTransport = useMemo<RttTransport>(
    () => new DataChannelTransport(client),
    [client],
  );
  const sipTransport = useMemo<RttTransport>(
    () => new SipMessageTransport(client),
    [client],
  );

  const sendSnapshot = useCallback(
    (text: string, event: "new" | "reset") => {
      if (preferredFormat !== "xep-0301") {
        return;
      }

      const actions: RttEvent[] =
        text.length > 0 ? [{ type: "insert", text, position: 0 }] : [];
      outgoingSeq.current += 1;

      if (dataTransport.isOpen()) {
        const xml = encodeRttXmlEnvelope({
          seq: outgoingSeq.current,
          event,
          actions,
        });
        dataTransport.send(xml, "application/xmpp+xml");
        return;
      }

      const xml = encodeRttXmlEnvelope(
        {
          seq: outgoingSeq.current,
          event,
          actions,
        },
        { includeXmlns: false },
      );
      sipTransport.send(xml, "text/plain;charset=UTF-8");
    },
    [dataTransport, preferredFormat, sipTransport],
  );

  const flushOutgoing = useCallback(() => {
    outgoingTimer.current = null;

    const nextText = outgoingPendingText.current;
    const lastText = outgoingLastSentText.current;
    const isActive = hasActiveMessageRef.current;

    if (nextText === lastText) return;
    if (!nextText && !isActive) return;

    const event: "new" | "reset" = isActive ? "reset" : "new";
    sendSnapshot(nextText, event);
    outgoingLastSentText.current = nextText;
    hasActiveMessageRef.current = nextText.length > 0;
  }, [sendSnapshot]);

  const scheduleFlush = useCallback(() => {
    if (outgoingTimer.current) return;
    outgoingTimer.current = setTimeout(flushOutgoing, throttleMs);
  }, [flushOutgoing, throttleMs]);

  useEffect(() => {
    if (!enabled) return;

    const handleRemoteTypingIndicator = (
      hasActions: boolean,
      event?: RttSessionEvent,
    ) => {
      if (!hasActions && event !== "new" && event !== "reset" && event !== "edit") {
        return;
      }

      dispatch({ type: "typing", isTyping: true });
      if (remoteTypingTimer.current) {
        clearTimeout(remoteTypingTimer.current);
      }
      remoteTypingTimer.current = setTimeout(() => {
        dispatch({ type: "typing", isTyping: false });
      }, 1200);
    };

    const handleIncoming = (data: string | Uint8Array) => {
      const textPayload = decodePayloadAsText(data);
      const envelope = decodeRttXml(textPayload);
      if (!envelope) {
        return;
      }

      dispatch({ type: "remote_envelope", envelope });
      handleRemoteTypingIndicator(envelope.actions.length > 0, envelope.event);
    };

    const unsubData = dataTransport.onMessage((msg) => handleIncoming(msg.data));
    const unsubSip = sipTransport.onMessage((msg) => handleIncoming(msg.data));

    return () => {
      unsubData();
      unsubSip();
      if (remoteTypingTimer.current) {
        clearTimeout(remoteTypingTimer.current);
        remoteTypingTimer.current = null;
      }
    };
  }, [dataTransport, enabled, sipTransport]);

  useEffect(() => {
    if (enabled) return;

    if (outgoingTimer.current) {
      clearTimeout(outgoingTimer.current);
      outgoingTimer.current = null;
    }
    if (remoteTypingTimer.current) {
      clearTimeout(remoteTypingTimer.current);
      remoteTypingTimer.current = null;
    }

    outgoingSeq.current = 0;
    outgoingPendingText.current = "";
    outgoingLastSentText.current = "";
    hasActiveMessageRef.current = false;
    localTextRef.current = "";
    setLocalText("");
    dispatch({ type: "clear_remote" });
  }, [enabled]);

  const handleSelectionChange = useCallback((start: number, end: number) => {
    localCursor.current = { start, end };
  }, []);

  const handleLocalText = useCallback(
    (text: string) => {
      setLocalText(text);
      localTextRef.current = text;
      outgoingPendingText.current = text;

      if (!text.length && hasActiveMessageRef.current) {
        if (outgoingTimer.current) {
          clearTimeout(outgoingTimer.current);
          outgoingTimer.current = null;
        }
        sendSnapshot("", "reset");
        hasActiveMessageRef.current = false;
        outgoingLastSentText.current = "";
        return;
      }

      scheduleFlush();
    },
    [scheduleFlush, sendSnapshot],
  );

  const resetLocal = useCallback(() => {
    setLocalText("");
    localTextRef.current = "";
    outgoingPendingText.current = "";
    if (outgoingTimer.current) {
      clearTimeout(outgoingTimer.current);
      outgoingTimer.current = null;
    }
    sendSnapshot("", "reset");
    hasActiveMessageRef.current = false;
    outgoingLastSentText.current = "";
  }, [sendSnapshot]);

  const clearRemote = useCallback(() => {
    if (remoteTypingTimer.current) {
      clearTimeout(remoteTypingTimer.current);
      remoteTypingTimer.current = null;
    }
    dispatch({ type: "clear_remote" });
  }, []);

  return {
    state,
    localText,
    setLocalText: handleLocalText,
    onSelectionChange: handleSelectionChange,
    resetLocal,
    clearRemote,
  };
}
