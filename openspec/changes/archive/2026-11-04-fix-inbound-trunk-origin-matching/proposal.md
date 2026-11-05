## Why

Incoming SIP INVITEs routed through a registered Gateway Contact use the Gateway's own public address in the Request-URI, while the actual registrar/PBX identity appears in the top Via, Contact, and packet source. The current matcher treats the Request-URI host as the trunk domain and can reject a valid call as ambiguous when the same SIP username exists on multiple enabled trunks, including stale, unowned, or differently configured records.

## What Changes

- Recognize a Request-URI or To URI that targets the Gateway's own SIP public address as a local Contact route rather than registrar identity.
- Match incoming trunk calls using the SIP username plus a cached registrar identity derived when the trunk registers, including configured hostname, resolved IP addresses, effective SIP port, and transport.
- Prioritize owned and currently registered trunks before considering stale or unowned records.
- Preserve deterministic rejection when more than one eligible owned trunk still matches after origin and registration-state checks; never select an arbitrary trunk.
- Cache DNS-derived registrar identities outside the INVITE hot path and avoid network or database I/O while holding the trunk manager lock.
- Invalidate and refresh cached registrar identities across register, re-register, unregister, lease loss, trunk update, disable, and manager refresh operations.
- Add structured diagnostics that distinguish local Contact targets, observed INVITE origins, eligible owned candidates, resolved registrar identities, the selected rule, and ambiguity reasons without exposing credentials.
- Add focused tests for Asterisk `rewrite_contact`, hostname-to-IP matching, duplicate usernames across PBXs, stale/unowned duplicate rows, DNS changes, IPv4/IPv6 normalization, and unresolved hostnames.

## Capabilities

### New Capabilities

- `inbound-trunk-routing`: Deterministic routing of incoming SIP INVITEs to the correct owned trunk when Request-URI targets the Gateway Contact and SIP usernames may be duplicated across registrars.

### Modified Capabilities

- None.

## Impact

- Primary code: `apps/gateway/internal/sip/trunk_manager.go`, inbound INVITE handling and diagnostics in `apps/gateway/internal/sip/handlers.go`, and focused SIP tests.
- Runtime behavior: valid incoming calls from Asterisk/Kamailio can reach the online `/ws-agent` or mobile client even when the Gateway Contact is rewritten into the Request-URI.
- Data: no destructive migration is required; existing soft-deleted trunk semantics remain unchanged. Duplicate enabled records remain observable and are rejected only when the eligible owned set cannot be disambiguated safely.
- Protocol compatibility: no WebSocket or REST message shape changes are required.
- Media behavior: no SDP, RTP, RTCP, Opus, H.264, SPS/PPS, or PeerConnection behavior changes.
