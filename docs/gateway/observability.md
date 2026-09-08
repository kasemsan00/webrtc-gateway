# Gateway Observability Contract

This document is the review boundary for gateway telemetry. The machine-readable
catalog is [`observability-catalog.json`](./observability-catalog.json). Changes
to a name, attribute, disposition, or cardinality must update both files and pass
`deploy/observability/verify.ps1`.

## Source inventory and classification

The September 2026 inventory found 967 production `fmt.Print*`/`log.Print*`
call sites. Counts are a point-in-time aid, not an allowlist. The verification
script re-enumerates current production Go files so a new source cannot silently
bypass review.

| Source family | Current evidence | Data class | Central disposition |
|---|---:|---|---|
| `internal/api` | 157 print sites | Routes, WS type/state, session IDs, auth subjects, diagnostic errors | Allow bounded route templates, event types and safe session correlation. Redact auth subjects and normalized errors; exclude request/response bodies and arbitrary diagnostic payloads. |
| `internal/sip` | 409 print sites | SIP state/status, identities, Call-ID, requests, responses, SDP, MESSAGE bodies and network addresses | Allow bounded method/status/state and reviewed correlation. Mask/hash identities and Call-ID. Raw SIP/SDP/body/address values are excluded; debug-only dumps stay local and production-off. |
| `internal/session` | 187 print sites | Session/media state, ICE candidates, IP/port pairs, codec metadata and RTP/RTCP aggregate counters | Allow state, candidate type, codec name and aggregates. Session ID is log/trace-only. Raw addresses are excluded; address-bearing ICE/TURN detail is debug-only and local. |
| `internal/config` and startup | 152 print sites | Effective settings, endpoints, DSN, passwords and credential-presence facts | Allow booleans, ports, bounded mode names and redacted presence. Redact endpoints where policy requires it; exclude credentials, headers, DSN and key/cert paths. |
| `internal/logstore` and `dbbootstrap` | 36 print sites | Queue/partition/batch state and database errors | Allow bounded operation/outcome/count/duration. Normalize errors and redact DSN/query parameters; never centralize stored payload bodies. |
| `internal/push` | 19 print sites | User/device identifiers, push tokens, OAuth bearer token, URLs and provider responses | Allow provider, count, duration and normalized outcome only. Tokens, identities, URL query, response body and raw errors are excluded. Debug bearer-token logging is prohibited from centralized collection even when explicitly enabled. |
| `internal/translator` | 1 print site | Dependency lifecycle plus possible caption/transcript content in errors | Allow dependency, duration and normalized outcome. Transcript/body, endpoint credentials and raw provider errors are excluded. |
| `internal/telemetry` | 1 print site | Throttled SDK/export failure diagnostics | Allow normalized reason and bounded count/freshness only. Keep diagnostics local-only and exclude endpoint, headers, payloads and raw credential-bearing errors. |
| `internal/webrtc` and `internal/pkg/webrtc` | 5 print sites | ICE state and TURN URL/presence | Allow bounded ICE state and credential-presence boolean. TURN URL/user/password and network addresses are excluded. |

### Reviewed raw and high-risk sources

Every raw credential/body/dump family discovered by searches for
`Authorization`, `Proxy-Authorization`, credential, token, password, body,
payload, SIP request/response, SDP, ICE candidate, address, DSN and transcript
has an explicit disposition below.

| Source | Example location | Disposition | Required handling |
|---|---|---|---|
| OAuth bearer token | `internal/push/ttrs_client.go` | **exclude** | Never export the debug token record. Production must keep `PUSH_DEBUG_TTRS_TOKEN=false`. |
| Push tokens and token arrays | `internal/push/service.go` | **exclude** | Emit token counts/provider/outcome only. Do not hash a device token into a reusable identifier. |
| SIP username/password context | `internal/sip/call.go`, `message.go`, `registration.go` | **redact** | Omit password and identity. Password length may be retained; username must be masked or one-way correlated for logs/traces only. |
| SIP Authorization and Proxy-Authorization | SIP request/header dumps | **exclude** | Source sanitizer must remove the headers before serialization; Collector filters are defense in depth. |
| Full SIP INVITE/request/response | `internal/sip/handlers.go`, `call.go` | **debug-only** | Keep `DEBUG_SIP_INVITE=false` in production. Do not route a raw dump record to the centralized stream. |
| Full SIP MESSAGE and message body | `internal/sip/handlers.go`, `message.go` | **debug-only** | Keep `DEBUG_SIP_MESSAGE=false`; central records may contain only byte length and normalized result. |
| Full SDP offers/answers | `internal/sip/sdp.go`, `call.go`; API WS offer/resume paths | **exclude** | Emit direction, media presence, codec/profile and byte length only. Raw SDP never enters centralized output. |
| ICE/TURN candidate addresses | `internal/session/session.go`, `renegotiate.go`, `ice_diagnostics.go`; `internal/sip/ice.go` | **debug-only** | Keep `DEBUG_TURN=false`; central output may retain candidate type/protocol/state but no address or port pair. |
| TURN credentials and server URL | config and `internal/pkg/webrtc/utils.go` | **exclude** | Export a configured boolean only. Never export username, password or URI. |
| JWT/access token and auth headers | `internal/api/auth_http.go`, auth verification paths | **exclude** | Retain realm and normalized result only; omit token, header and subject identity. |
| Database DSN | config/startup/bootstrap error paths | **redact** | Use the source DSN sanitizer; centralized fields contain at most DB enabled/state. |
| Client diagnostic payload/arbitrary maps | `internal/api/client_diagnostics.go`, `internal/logstore` payload records | **exclude** | PostgreSQL remains authoritative. Do not flatten arbitrary maps into telemetry. |
| Translation/caption/transcript body | `internal/api/ws_translate.go`, `internal/translator` | **exclude** | Emit language/mode only if from bounded vocabulary; no body or raw dependency error. |
| SIP identity, phone number and Call-ID | SIP/API/session paths | **redact** | Mask/omit identities. A reviewed one-way correlation may be used in logs/traces; never in metrics. |
| HTTP/WS URLs and errors | API/dependency paths | **redact** | Use route templates and dependency name. Strip query strings and normalize errors to catalog reasons. |
| Private keys, APNs/FCM files and credentials | config/push initialization | **exclude** | Emit configured/enabled state only. |

`DB_LOG_FULL_SIP`, `DEBUG_SIP_MESSAGE`, `DEBUG_SIP_INVITE`, `DEBUG_TURN`, and
`PUSH_DEBUG_TTRS_TOKEN` must remain `false` in production examples. Enabling them creates residual local
exposure and does not authorize centralized export.

## Stable schema

All centralized records carry the resource fields `service.name`,
`service.version`, `deployment.environment`, and `service.instance.id`.
Structured logs additionally carry timestamp, severity, component and
`event.name`. Correlation and measurements are included only when applicable
and typed. `session.id`, reviewed call correlation and numeric `trunk.id` may be
used in logs and sampled traces, but never as metric attributes.

Names, components, event names, normalized outcomes/reasons, instruments,
units, metric attributes and span attributes are defined in the JSON catalog.
Values not in a bounded vocabulary map to `unknown` or `internal_error`; raw
error strings never become attributes. Event and instrument names are stable,
lowercase dotted identifiers.

Metric attributes are allowlisted per instrument. The catalog verifier rejects
duplicates, attributes outside the global allowlist, and attribute names that
suggest unbounded identifiers or payload values. Media metrics are periodic
aggregates or existing counters; no metric or span is emitted per packet.

Span names are bounded operation names such as `HTTP <route-template>`,
`WS <message-type>`, `SIP <method>`, and named call/dependency phases. Raw URLs,
queries, SIP/SDP, bodies, identities, addresses and arbitrary errors are not span
attributes or events. Trace context is local to the gateway when protocols do
not already carry it; this feature adds no WebSocket fields or SIP headers.

## OpenTelemetry dependency licenses

Gateway observability depends on the OpenTelemetry Go API/SDK and OTLP HTTP/gRPC
exporters (Apache License 2.0), plus existing `google.golang.org/grpc` and
protobuf modules already used by the translator client. No OpenObserve-specific
Go client is included. A review of `apps/gateway/go.mod` on 2026-09-07 found no
copyleft or proprietary observability dependency.

