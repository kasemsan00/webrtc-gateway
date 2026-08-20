## Why

The operations console already covers the gateway's main administrative resources, but several observability surfaces are incomplete or misleading: database health can report connected while database logging is disabled, the session stats read path has no production recorder, and useful session, event, payload, client, and trunk metadata is either discarded by the REST layer or not presented by the frontend. Operators need one trustworthy path from runtime state and persisted call evidence to actionable console views without changing SIP, WebSocket, SDP, or media behavior.

## What Changes

- Make gateway database health distinguish disabled, connected, degraded, and unavailable states using actual store/pool readiness instead of interface presence.
- Add a non-blocking periodic session-stats collector that persists existing media-recovery and RTCP counters at `DB_STATS_INTERVAL_MS`, exposes queue/drop health, and stops cleanly with session lifecycle.
- Add typed operational-health data for database readiness, persistence queues, SIP listeners, translator connectivity, push-provider readiness, and gateway/session-directory freshness while excluding secrets and high-cardinality payloads.
- Return safe persisted session routing and negotiated-media metadata in session history/detail responses, including auth mode, trunk identity, SIP username, RTP/RTCP ports, Opus payload type, media profiles, and video rejection state.
- Add a Session Overview view and improve session Events, Payloads, and Stats views so structured event data, parsed payloads, binary payload metadata, and media metrics are inspectable.
- Surface currently unused active-session, WebSocket-client, trunk-presence, and client-diagnostic metadata, with navigation links between related sessions, trunks, clients, and instances.
- Keep existing REST paths and response fields compatible; new fields and operational endpoints are additive, and no SIP call flow, WebSocket message contract, SDP, codec, RTP/RTCP forwarding, database schema identity, or authentication policy is renamed.

## Capabilities

### New Capabilities

- `gateway-operations-observability`: Trustworthy runtime health, persisted session/media diagnostics, and connected console views for operating and troubleshooting the gateway.

### Modified Capabilities

None.

## Impact

- Gateway runtime health and LogStore readiness contracts under `apps/gateway/internal/api`, `internal/logstore`, `internal/session`, and service integrations for SIP, translator, and push readiness.
- Additive admin REST response fields and potentially a new detailed-health endpoint; existing mobile WebSocket and REST call-control contracts remain unchanged.
- Session stats collection adds bounded periodic work and database writes that must stay outside RTP/RTCP hot paths and respect existing queue/drop behavior.
- Frontend types, services, Session Detail, Dashboard/Instances, Active Sessions, WS Clients, Trunks, and Client Diagnostics views.
- Focused Go and Vitest coverage for health semantics, stats lifecycle, safe response shaping, structured details, and cross-resource navigation.
- Documentation for the admin observability contract and operational interpretation of health states and media metrics.
