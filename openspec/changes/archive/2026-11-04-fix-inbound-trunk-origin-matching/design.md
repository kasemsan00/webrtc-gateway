## Context

See `proposal.md` for motivation. Incoming Asterisk INVITEs commonly use the Gateway's registered Contact (`publicIP:localPort`) as both Request-URI and To URI. The current matcher interprets those hosts as trunk registrar domains and returns on the first ambiguous rule before origin-based fallback runs. It also compares hostname-configured trunks to numeric INVITE origins as raw strings, and the current registration transaction map is not a reliable source of successful live registration identity.

The matching path is latency-sensitive SIP signaling. It currently executes under `TrunkManager.mu.RLock`; production rules prohibit DNS, database, or SIP network I/O while that lock is held. The fix must preserve deterministic rejection, multi-instance lease ownership, soft deletion, `/ws-agent` binding by trunk ID, and all existing SIP transaction and media behavior.

## Goals / Non-Goals

**Goals:**

- Separate the Gateway Contact delivery address from registrar/PBX identity.
- Build authoritative, current registrar identity state from successful REGISTER exchanges.
- Match hostname-configured trunks to observed registrar IP addresses without DNS in the INVITE path.
- Exclude unowned and non-current registrations before ambiguity is evaluated.
- Make every selection deterministic, race-safe, and observable.
- Support IPv4, IPv6, explicit ports, default ports, TCP/UDP normalization, DNS A/AAAA, and SRV-derived registrar targets.

**Non-Goals:**

- Automatically merge, delete, or rewrite duplicate `sip_trunks` rows.
- Change `/ws-agent`, mobile WebSocket, REST, or SIP wire contracts.
- Add load balancing between multiple eligible trunks with the same identity.
- Infer trunk ownership from caller From identity, SDP connection addresses, display names, or arbitrary recency.
- Change SIP response policy after a trunk has been selected or alter media forwarding.

## Decisions

### 1. Classify self-targeted Request-URI and To URI before trunk-domain rules

Normalize the configured Gateway SIP Contact into a local identity set containing the public host/IP and listening port. During INVITE matching, classify Request-URI and To independently as local Contact targets. Rules based on user+domain or domain+port are skipped for a URI classified as local because it identifies packet delivery, not the registrar.

A non-local Request-URI retains the existing specific user+domain+port behavior for compatibility with deployments that route without Contact rewriting.

**Alternative considered:** Always ignore Request-URI/To host. Rejected because direct routed deployments may carry useful registrar identity there.

### 2. Introduce immutable per-trunk registration identity snapshots

Add an in-memory registration identity snapshot keyed by trunk ID. A snapshot contains:

- registration generation and success timestamp;
- configured username, normalized registrar hostname, effective port, and transport;
- normalized registrar endpoints derived before/during registration;
- the observed source endpoint of successful REGISTER responses when available;
- expiry/lifecycle state sufficient to determine whether the snapshot is current.

Snapshots contain no password, authorization material, or push tokens. Writers construct a complete snapshot without holding the trunk manager lock, then atomically replace the map entry under a short lock. Readers copy the small immutable snapshot while under `RLock` and perform selection on the copied data after releasing the lock.

**Alternative considered:** Reuse `registrations map[int64]*sip.ClientTransaction`. Rejected because a client transaction represents an operation, not an immutable successful registrar identity, and retaining transactions for routing complicates lifecycle and concurrency.

### 3. Collect registrar endpoints during registration, never during INVITE matching

Registration prepares an endpoint set from the configured domain, explicit/default port, transport, DNS A/AAAA results, and SRV target when applicable. A successful REGISTER response source is added as highest-confidence registrar evidence. DNS resolution occurs before acquiring the state-update lock and is refreshed on REGISTER/refresh.

Because Go's default resolver does not expose record TTL consistently, registration generation and refresh cadence define cache lifetime. A newer successful generation atomically replaces the previous endpoint set. Failed attempts do not publish partial endpoint sets.

**Alternative considered:** Resolve `trunk.Domain` synchronously during each INVITE. Rejected due to latency, failure amplification, and the prohibition on network I/O in the hot path.

**Alternative considered:** Persist resolved IP addresses in PostgreSQL. Rejected because they are ephemeral runtime routing state and would introduce stale cross-instance data and migration complexity.

### 4. Use confidence-ordered origin evidence

Normalize INVITE origin evidence into endpoint values with provenance:

1. actual transport peer from `req.Source()`;
2. top Via sent-by address and effective port;
3. Contact address and effective port.

The transport peer is highest confidence because it is observed by the Gateway. Via and Contact remain useful for trusted proxy/NAT topologies but cannot override a unique transport-peer match. Matching evaluates exact address+port+transport first, then address+transport when port rewriting is plausible. Any relaxed match must still yield exactly one candidate.

SDP `o=`/`c=` addresses and From URI are excluded because they identify media or caller identity, not the registered destination trunk.

**Alternative considered:** Trust top Via before socket source. Rejected because Via can contain advertised or rewritten values and is easier to spoof than the observed peer.

### 5. Filter eligibility before applying ambiguity

For the requested username, build the candidate set in this order:

1. loaded and enabled;
2. owned by this instance;
3. has a current successful registration identity;
4. username matches exactly after existing normalization.

Unowned or stale rows remain available for operations visibility but do not participate in routing ambiguity on this instance. If exactly one eligible candidate remains, it may be selected even when origin evidence is unavailable. If multiple remain, origin evidence must reduce the set to one.

**Alternative considered:** Let every enabled DB row contribute to ambiguity, then inspect ownership. Rejected because it allows stale and other-instance state to deny valid calls.

### 6. Remove unsafe tie-breakers from incoming selection

When multiple eligible candidates remain, do not select by trunk ID, map order, `LastRegisteredAt`, `sipclient-agent-` name, or latest connection. These values do not prove which PBX sent the INVITE and can silently route a call to the wrong tenant or client. Remaining ambiguity returns `503 Service Unavailable`.

This intentionally favors explicit failure over cross-trunk misrouting.

**Alternative considered:** Choose the newest registration. Rejected because multiple PBXs can legitimately host the same username and the latest timestamp is unrelated to INVITE origin.

### 7. Define explicit snapshot lifecycle and generation guards

Snapshot publication and invalidation follow trunk lifecycle:

- successful initial/forced/refresh REGISTER publishes a new generation;
- failed REGISTER leaves the last still-current successful generation unchanged;
- successful UNREGISTER, explicit unregister start, lease loss, disable, removal from loaded enabled trunks, and manager stop invalidate the snapshot;
- trunk domain/port/transport/username changes invalidate the old snapshot before re-registration;
- late completion from an older registration generation cannot overwrite newer state.

A monotonically increasing per-trunk generation or operation token guards races between register, refresh, unregister, reconnect, and lease loss.

**Alternative considered:** Infer current state from `LastRegisteredAt` in the database. Rejected because database timestamps do not encode in-process generation races or the actual registrar endpoint observed by this instance.

### 8. Keep lock scopes narrow and selection pure

Refactor matching into two phases:

1. under `RLock`, copy the relevant trunk metadata, ownership flags, current registration snapshots, and local Contact configuration;
2. after releasing the lock, normalize the already-present SIP fields and run a pure deterministic selector.

Registration endpoint discovery and DNS run outside locks. State mutation uses brief lock sections only. No database or SIP I/O is added to the INVITE path.

### 9. Expand structured match results and logs

Extend the detailed match result with safe diagnostic fields such as local-target flags, normalized origin summaries, eligible candidate IDs, selected registrar evidence, and ambiguity reason. Keep existing fields where practical so current log consumers remain compatible. `handlers.go` continues to own SIP response decisions and logstore events.

Never include credentials, Authorization headers, push tokens, or complete sensitive SIP payloads in new structured fields.

### 10. Keep operational cleanup separate from routing correctness

The matcher is correct even when old enabled rows exist, because stale/unowned rows are excluded. Operations tooling may continue to show and soft-disable duplicates. This change does not automatically disable records because a second PBX using the same username can be legitimate.

## Risks / Trade-offs

- [Registrar behind a SIP proxy presents a different source than DNS endpoints] → Include successful REGISTER response source and confidence-tagged Via/Contact evidence; require uniqueness before selection.
- [NAT or proxy rewrites source ports] → Try exact endpoint first, then a uniquely matching address+transport relaxation; never relax across multiple candidates.
- [DNS changes between registration refreshes] → Refresh the endpoint snapshot on each successful REGISTER generation and atomically replace old endpoints.
- [A failed refresh leaves stale identity active too long] → Tie snapshot currentness to existing registration expiry/lease policy and invalidate on explicit terminal lifecycle events.
- [Multiple registrations intentionally share one proxy endpoint and username] → Reject with `503`; deployments need a differentiating registrar route, unique username, or future explicit trunk-routing token.
- [More state increases concurrency complexity] → Use immutable snapshots and generation guards with focused race tests; avoid retaining mutable SIP transactions.
- [Behavior change exposes previously hidden duplicate configuration] → Emit precise ambiguity diagnostics and preserve soft-disable operational cleanup.

## Migration Plan

1. Add snapshot and pure matching types behind the existing `MatchTrunkFromInviteDetailed` API.
2. Add unit tests reproducing `00025@sipagent.ttrs.or.th` receiving an INVITE whose Request-URI is the Gateway Contact and Via/source is `203.150.245.41:5060`.
3. Wire successful REGISTER/refresh responses to publish snapshots and lifecycle operations to invalidate them.
4. Deploy to a non-production Gateway instance with debug INVITE logging and verify selection against duplicate fixture trunks.
5. Deploy one production instance and confirm new diagnostics show local Contact classification and origin-based selection before broad rollout.
6. Retain existing `503` rejection as the rollback-safe behavior for unresolved ambiguity.
7. Roll back by reverting the matcher/snapshot change; no schema rollback is needed because no persistent migration is introduced.

## Open Questions

- Whether all production SIP proxies preserve a useful `req.Source()` endpoint or require an allowlisted trusted Via hop can be measured after diagnostics are deployed; the design already supports both evidence sources without changing the contract.
