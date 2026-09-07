## Purpose

Defines safe and deterministic routing of incoming SIP INVITEs to the correct Gateway-owned trunk when Asterisk or another registrar rewrites the Request-URI to the Gateway Contact and SIP usernames are not globally unique.

## ADDED Requirements

### Requirement: Local Gateway Contact recognition
The Gateway SHALL recognize an incoming Request-URI or To URI that targets its own configured SIP public address and listening port as a local Contact route. The Gateway MUST NOT treat that local Contact host as proof that the originating registrar has the same domain.

#### Scenario: Asterisk routes an INVITE to the registered Contact
- **WHEN** an INVITE for user `00025` has Request-URI `sip:00025@203.151.21.121:5090` and `203.151.21.121:5090` is the Gateway SIP Contact
- **THEN** the Gateway treats `203.151.21.121:5090` as its local delivery address and does not select a trunk solely because its configured domain equals `203.151.21.121`

#### Scenario: Request-URI targets a non-local registrar identity
- **WHEN** an INVITE Request-URI host and port do not identify the Gateway Contact
- **THEN** the Gateway may use the Request-URI user, host, and port as registrar-routing evidence according to the deterministic matching rules

### Requirement: Registration-derived registrar identity
For each successfully registered trunk, the Gateway SHALL retain a runtime registrar identity containing the trunk identifier, SIP username, configured registrar hostname, effective transport and port, and observed or resolved registrar network addresses. The identity MUST represent the successful registration generation and MUST NOT contain SIP credentials.

#### Scenario: Hostname registration receives a response from an IP address
- **WHEN** trunk user `00025` registers to `sipagent.ttrs.or.th:5060` and the successful REGISTER exchange is observed from `203.150.245.41:5060`
- **THEN** the Gateway records both the configured hostname and `203.150.245.41:5060` as registrar identity evidence for that trunk

#### Scenario: Registration fails
- **WHEN** a REGISTER or refresh REGISTER does not complete successfully
- **THEN** the Gateway does not publish the failed attempt as a current successful registrar identity

#### Scenario: Registration identity becomes invalid
- **WHEN** a trunk unregisters, loses its lease, is disabled, is removed from the loaded enabled set, or its registration is superseded
- **THEN** the Gateway invalidates the corresponding current registrar identity before that identity can route a later INVITE

### Requirement: Owned and registered candidate eligibility
The Gateway SHALL first restrict incoming trunk candidates to enabled trunks for the requested SIP username that are owned by the current instance and have a current successful registration identity. An enabled but unowned, unregistered, stale, or superseded trunk MUST NOT make an otherwise unique eligible match ambiguous.

#### Scenario: One owned registered trunk and one unowned duplicate
- **WHEN** two enabled trunks have username `00025`, only one is owned and currently registered by the receiving instance, and an INVITE for `00025` arrives
- **THEN** the unowned duplicate does not prevent selection of the owned registered trunk

#### Scenario: Stale enabled row remains in the database
- **WHEN** an old enabled trunk row has the requested username but has no current successful registration identity
- **THEN** the old row is excluded from the eligible routing set

#### Scenario: No eligible owned registered trunk exists
- **WHEN** an INVITE targets a known username but the receiving instance has no enabled, owned, currently registered trunk for that username
- **THEN** the Gateway rejects the INVITE without routing it to an arbitrary stale or unowned trunk

### Requirement: Origin-aware deterministic selection
When multiple eligible trunks share a SIP username, the Gateway SHALL compare normalized INVITE origin evidence with each candidate's current registrar identity. Origin evidence SHALL include the observed transport peer and may include the top Via and Contact sent-by addresses. A hostname and its resolved IPv4 or IPv6 address SHALL be considered equivalent only when that equivalence was established outside the INVITE hot path.

#### Scenario: Duplicate username across two PBXs
- **WHEN** two owned and registered trunks share username `00025`, one is registered with `sipagent.ttrs.or.th` at `203.150.245.41:5060`, the other is registered with a different PBX, and the INVITE transport peer or top Via is `203.150.245.41:5060`
- **THEN** the Gateway selects only the trunk registered with `sipagent.ttrs.or.th`

#### Scenario: Registrar hostname resolves to the observed origin IP
- **WHEN** a candidate is configured with registrar hostname `sipagent.ttrs.or.th` and its current registration identity includes resolved address `203.150.245.41`
- **THEN** an INVITE observed from `203.150.245.41` can match that hostname-configured candidate without performing DNS lookup during INVITE handling

#### Scenario: Port distinguishes registrars on one address
- **WHEN** multiple eligible trunks use the same registrar address but different effective SIP ports
- **THEN** the Gateway uses the observed or advertised origin port and transport to select a unique matching trunk when possible

#### Scenario: Origin fields disagree
- **WHEN** the observed transport peer conflicts with a Via or Contact address
- **THEN** the Gateway prefers evidence from the actual transport peer and does not allow a lower-confidence header alone to override a unique transport-peer match

### Requirement: Safe ambiguity handling
The Gateway MUST NOT use map iteration order, numeric trunk identifier, newest registration timestamp, agent naming convention, or another unrelated tie-breaker to select among multiple eligible trunks. If deterministic registrar-origin evidence does not produce exactly one candidate, the Gateway SHALL reject the INVITE with `503 Service Unavailable` and record the ambiguity.

#### Scenario: Two eligible trunks remain indistinguishable
- **WHEN** two owned and currently registered trunks share the requested username and both match the same available registrar-origin evidence
- **THEN** the Gateway responds `503 Service Unavailable` and does not offer the call to either client

#### Scenario: Origin cannot be mapped and one eligible candidate exists
- **WHEN** registrar-origin evidence is absent or unresolved but exactly one owned and currently registered candidate exists for the username
- **THEN** the Gateway selects that unique eligible candidate

#### Scenario: Origin cannot be mapped and multiple eligible candidates exist
- **WHEN** registrar-origin evidence is absent or unresolved and multiple owned and currently registered candidates exist for the username
- **THEN** the Gateway responds `503 Service Unavailable` rather than selecting the most recently registered candidate

### Requirement: Non-blocking INVITE matching
Incoming INVITE matching SHALL use in-memory normalized state only. It MUST NOT perform DNS resolution, database queries, lease acquisition, registration, or other network I/O while matching an INVITE or while holding the trunk manager lock.

#### Scenario: Cached DNS data is available
- **WHEN** an INVITE arrives for a hostname-configured trunk whose registrar identity was resolved during registration
- **THEN** matching completes using the cached identity without a DNS query

#### Scenario: DNS data is unavailable
- **WHEN** no resolved registrar address is cached
- **THEN** matching continues using available observed identity and safe candidate cardinality rules without blocking on DNS

### Requirement: Registration identity refresh consistency
The Gateway SHALL replace registrar identity state atomically after a successful register or refresh operation and SHALL preserve the last successful identity until a newer successful generation replaces it or an invalidating lifecycle event occurs.

#### Scenario: DNS target changes during refresh
- **WHEN** a refresh REGISTER successfully uses a different registrar address than the previous generation
- **THEN** subsequent INVITEs use the new identity and no longer match solely through the superseded address

#### Scenario: Refresh attempt temporarily fails
- **WHEN** a refresh REGISTER fails but the existing registration has not been invalidated or expired
- **THEN** the Gateway retains the last successful identity while applying the existing registration failure and lease policy

### Requirement: Trunk match observability
For every selected, ambiguous, or rejected incoming trunk match, the Gateway SHALL emit diagnostics containing the SIP username, whether Request-URI and To target the local Gateway Contact, normalized observed origins, eligible candidate trunk IDs, selected trunk ID when present, match rule, and rejection reason. Diagnostics MUST NOT include passwords, authorization headers, push tokens, or other credentials.

#### Scenario: Origin selects a hostname-configured trunk
- **WHEN** an INVITE from `203.150.245.41:5060` selects `00025@sipagent.ttrs.or.th:5060`
- **THEN** diagnostics identify the local Contact target, origin evidence, selected trunk ID, and origin-based rule without logging credentials

#### Scenario: Ambiguous eligible candidates are rejected
- **WHEN** multiple eligible candidates remain after origin matching
- **THEN** diagnostics list only the relevant candidate identifiers and the reason deterministic selection failed

### Requirement: Signaling and media compatibility
The routing change SHALL preserve existing SIP transaction behavior after a trunk is selected and SHALL not change the WebSocket call-control contract or media negotiation and forwarding behavior.

#### Scenario: Correct trunk is selected
- **WHEN** origin-aware matching selects an online `/ws-agent` or mobile trunk
- **THEN** the existing incoming-call offer, accept, reject, cancel, timeout, and hangup flows continue unchanged

#### Scenario: Media session starts after selection
- **WHEN** the client accepts the incoming call
- **THEN** existing Opus passthrough, H.264-only negotiation, SPS/PPS handling, RTP/RTCP forwarding, and keyframe recovery behavior remain unchanged
