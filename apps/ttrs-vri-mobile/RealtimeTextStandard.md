# TTRS VRS Realtime Text (RTT) Integration Summary

This document is a migration-oriented summary of how realtime text is implemented in this project, so it can be ported to React Web, Android, iOS, and React Native with protocol compatibility.

## 1) Current Standard Usage (What is actually used)

The project contains 3 text modes in VRS flow:

| `textmode` | Behavior                                                      | Status in code                                                                         |
| ---------- | ------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `0`        | Normal SIP chat (`MESSAGE`)                                   | Implemented                                                                            |
| `1`        | RTT over SIP `MESSAGE` using RFC3428-style + IM-RTT emulation | **Default** (`linphone_gtk_get_ui_config_int("textmode", 1)`)                          |
| `2`        | RTT over RTP style RFC4103 APIs                               | Implemented in functions, but auto create/quit is currently commented in VRS auto flow |

Key source:

- `gtk/vrs_chat.c` (`vrs_create_auto_chatroom`, `vrs_quit_auto_chatroom`)
- `gtk/chat.c` (generic chat RTT mode switching)

## 2) Dual Runtime Controls (Important)

There are 2 different mode controls in the codebase:

1. `textmode` (VRS-side mode selector)

- Read by `vrs_create_auto_chatroom()`
- Default is `1`

2. `rtt_mode` (Linphone core mode selector)

- Read by `linphone_core_get_rtt_mode(...)`
- Used in generic chat and in VRS receive branch logic

Migration note:

- Keep a single normalized app-level mode model, but preserve both semantics while interoperating with legacy peers.

## 3) Callback/Event Routing (Core VTable)

Configured in `gtk/main.c`:

- `vtable.message_received = vrs_message_text_received`
  - Used for SIP `MESSAGE` receive path (includes RFC3428 full-message handling and fallback display path)
- `vtable.im_rtt_text_updated = vrs_im_rtt_text_updated`
- `vtable.im_rtt_full_text_received = vrs_im_rtt_full_text_received`
  - Used for IM-RTT style updates/finalization flow
- `vtable.realtime_text_received = vrs_realtime_text_received`
- `vtable.realtime_deleted = vrs_realtime_text_deleted`
- `vtable.realtime_insert_at_position = vrs_realtime_insert_at_position`
- `vtable.realtime_delete_at_position = vrs_realtime_delete_at_position`
  - Used for RFC4103-like stream operations

Also in `gtk/main.c`:

- Realtime text is explicitly enabled only when `rtt_mode == 2` via `linphone_core_enable_realtime_text(the_core, TRUE)`.

## 4) Protocol/Message Semantics You Must Keep

The system uses custom payload conventions in addition to RFC naming.

Defined in `gtk/linphone.h`:

- `RTT_CODE_SPACE = "&#s;"`
- `RTT_CODE_NEW_LINE = "&#n;"`
- `RTT_CODE_REDUNDANT_TAG_OPEN = "<rdd>"`
- `RTT_CODE_REDUNDANT_TAG_CLOSE = "</rdd>"`
- `RTT_CODE_OP_ADDR_OPEN = "<operator>"`
- `RTT_CODE_OP_ADDR_CLOSE = "</operator>"`

Special control message patterns seen in flow:

- `@open_chat` sent on RTT session init (IM-RTT path)
- `<operator>sip:...</operator>` sent to identify local operator
- Enter/finalization may include `&#n;<rdd>...</rdd>` wrapping

Compatibility requirement:

- Do not remove these tokens if you must interoperate with existing desktop clients/services.

## 5) Send/Receive Behavior by Mode

### Mode 1: RFC3428-style + IM-RTT emulation (current VRS default)

Send path:

- Create encoder/decoder (`linphone_core_create_rtt_encoder`, `linphone_core_create_rtt_decoder`)
- On typing:
  - Encode diff to XML (`linphone_rtt_encode` + `linphone_core_rtt_rootElement_toXml`)
  - Send via `linphone_chat_room_send_rtt(...)`
- On Enter/Send:
  - Send normal full text via `linphone_chat_room_send_chat_message(...)`
  - Move encoder to next message (`linphone_core_rtt_encoder_next_message`)

Receive path:

- `vrs_message_text_received(...)` receives message
- For `rtt_mode == 1`: feed decoder using `linphone_core_im_rtt_text_received(...)`
- Decoder produces:
  - update callbacks: `vrs_im_rtt_text_updated(...)`
  - final callbacks: `vrs_im_rtt_full_text_received(...)`

### Mode 2: RFC4103-style stream operations

Send path:

- `linphone_core_send_rtt4103_text_stream(...)`
- `linphone_core_send_rtt4103_backspace(...)`
- `linphone_core_send_rtt4103_delete_position(...)`
- `linphone_core_send_rtt4103_insert_position(...)`
- Enter signal: `linphone_core_send_rtt4103_enter_signal(...)`

Receive path:

- Realtime callbacks apply insert/delete operations to current RTT line.
- `message_received` still exists as fallback for committed SIP text.

### Mode 0: Legacy normal/SIP message style

- Uses `linphone_chat_room_send_chat_message(...)`.
- In some RTT helper routines, text is transformed with custom markers.

## 6) VRS-Specific Notes

- VRS auto chatroom creation is driven by call state in `gtk/vrs.c` and delegates to `vrs_create_auto_chatroom(...)`.
- In current source, mode 2 auto create/quit calls are commented:
  - `//vrs_create_chatroom_rtt4103(call);`
  - `//vrs_quit_chatroom_rtt4103(call);`
- Practical implication: VRS production behavior is centered on mode 1 unless config/code is changed.

## 7) Migration Blueprint (React Web / Android / iOS / React Native)

## 7.1 Shared Domain Model (recommended)

Use a shared event model independent from UI framework:

- `RTT_SESSION_OPEN`
- `RTT_UPDATE_INSERT(text, pos?)`
- `RTT_UPDATE_DELETE(count|pos,length)`
- `RTT_COMMIT(fullText, timestamp)`
- `CHAT_MESSAGE(text)`
- `CONTROL_MESSAGE(type, payload)` for `@open_chat`, `<operator>...`

Keep parser/serializer adapters:

- Adapter A: RFC3428/IM-RTT + legacy tokens
- Adapter B: RFC4103 stream operations

## 7.2 React Web

Recommended first target:

- Implement mode 1 compatibility first (`MESSAGE` + IM-RTT-style handling), because browser RTP-T.140 support is usually limited without a gateway.

If mode 2 is required:

- Add a media/gateway layer that can translate between browser-capable transport and RFC4103 event semantics.

## 7.3 Android / iOS

Preferred approach:

- Reuse native Linphone SDK callbacks equivalent to current desktop flow.
- Keep mode behavior and token serialization identical to avoid interop breaks.

## 7.4 React Native

Recommended architecture:

- Bridge native Android/iOS RTT implementation to JS.
- Keep SIP/RTT protocol handling native-side; JS handles rendering/state orchestration.

## 8) Interop Contract for the New Clients

MUST:

- Preserve `@open_chat` handling in mode 1 sessions.
- Preserve `<operator>...</operator>` send/parse behavior.
- Preserve `&#s;`, `&#n;`, `<rdd>...</rdd>` compatibility.
- Support both incremental RTT updates and committed final messages.
- Keep UTF-8 safe insert/delete logic.

SHOULD:

- Throttle/queue UI updates for bursty RTT events.
- Keep per-conversation mutable line state (similar to `rtt_line` behavior).
- Log raw inbound/outbound RTT payloads for diagnostics during rollout.

## 9) Quick Verification Checklist After Port

1. Typing one character appears remotely in near realtime.
2. Backspace/delete updates remote text correctly.
3. Enter commits final message and resets incremental state.
4. Operator identity control message is exchanged correctly.
5. Legacy desktop peer can still chat with new client in default VRS mode.
6. UTF-8 (Thai/emoji/multibyte) edits do not corrupt text.

## 10) Source Mapping (for implementation traceability)

- `gtk/main.c`
  - vtable callback wiring for RTT/message receive
  - realtime text enable toggle by `rtt_mode`
- `gtk/vrs_chat.c`
  - VRS chatroom creation by `textmode`
  - mode-specific send/receive logic
  - RTT update/render helper behavior
- `gtk/chat.c`
  - generic chat RTT logic and mode split (`rtt_mode`)
- `gtk/friendlist.c`
  - RTT encoder/decoder initialization for chat view
- `gtk/linphone.h`
  - custom RTT token constants
