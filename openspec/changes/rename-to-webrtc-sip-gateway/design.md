## Context

The repository currently has several overlapping identities:

- root package and workspace path: `webrtc-gateway`;
- product prose: `K2 Gateway` and `K2 WebRTC Gateway`;
- Go module, binary, process, logs, and backend image: `k2-gateway`;
- frontend and unified images: `k2-frontend` and `k2-stack`;
- split Compose service: `webrtc-gateway`;
- SIP User-Agent variants: `K2-Gateway`, `K2-Gateway-Trunk`, and a Linphone compatibility string containing `K2-Gateway`;
- browser persistence keys, local database examples, Unix user/group names, and bootstrap identifiers containing `k2`;
- deployed URLs and maintained clients that still point at `k2-gateway.kasemsan.com` or local paths named `webrtc-gateway`.

The system is bidirectional. WebRTC clients use JSON over WebSocket plus SRTP, while the gateway owns SIP transactions/dialogs and relays RTP/RTCP to SIP endpoints. `WebRTC-SIP Gateway` therefore describes both protocol boundaries without implying an outbound-only `WebRTC-to-SIP` flow. The rename must not alter public call-control contracts or media invariants.

This proposal may overlap text and deployment files touched by active changes. Implementation must rebase on their completed behavior and preserve unrelated edits instead of reverting them.

## Goals / Non-Goals

**Goals:**

- Establish **WebRTC-SIP Gateway** and `webrtc-sip-gateway` as the only canonical active product identity.
- Remove the temporary `K2` branding from maintained source, runtime-visible identifiers, distribution artifacts, current documentation, and maintained consumer references.
- Make the machine identity precise enough to distinguish this project from SFUs, TURN gateways, WebRTC ingest gateways, and generic API gateways.
- Preserve user preferences, database data, call behavior, and compatibility for already deployed clients during a controlled rollout.
- Provide an auditable scan that prevents active `K2` naming from remaining accidentally.

**Non-Goals:**

- Renaming WebSocket message types, JSON fields, REST/SSE paths, environment variable names, database tables/columns, or SIP trunk records that do not contain the temporary brand.
- Changing SIP call flows, authentication policy, push routing, translation behavior, SDP, codecs, SRTP/RTP forwarding, H.264 handling, or session lifecycle.
- Turning the gateway into an SBC, SFU, MCU, or general-purpose communications platform.
- Selecting a permanent company/product brand or a specific public domain owned by an operator.
- Automatically renaming operator-owned PostgreSQL databases/roles, Docker volumes, DNS records, TLS certificates, monitoring resources, or secrets in place.
- Rewriting archived OpenSpec changes and historical implementation plans merely to make history use the new name. Active normative references may be updated; immutable historical context remains historical.

## Decisions

### 1. Use one canonical human and machine identity

Use:

| Surface | Canonical identity |
| --- | --- |
| Product | `WebRTC-SIP Gateway` |
| Repository/root package | `webrtc-sip-gateway` |
| Backend binary/process/image | `webrtc-sip-gateway` |
| Go module/import prefix | `webrtc-sip-gateway` |
| Operations product | `WebRTC-SIP Gateway Console` |
| Console image | `webrtc-sip-gateway-console` |
| Unified image/stack | `webrtc-sip-gateway-stack` |
| SIP User-Agent product token | `WebRTC-SIP-Gateway` with the existing version format |
| Gateway log prefix | `webrtc-sip-gateway-` |

Compose service names inside the project may use `gateway`, `gateway-console`, and `gateway-stack`. These are scoped by the Compose project and avoid redundant long names while images and published artifacts retain the full identity.

Alternative considered: keep `webrtc-gateway`. Rejected as the canonical identity because it omits the SIP boundary and can be confused with unrelated WebRTC infrastructure.

Alternative considered: `webrtc-to-sip-gateway`. Rejected because the gateway supports incoming SIP-to-WebRTC calls as well as outbound WebRTC-to-SIP calls.

Alternative considered: `sip-gateway`, `media-gateway`, or `SBC`. Rejected because each creates inaccurate protocol or product expectations.

### 2. Change technical identifiers, but preserve external compatibility deliberately

The source release changes the Go module, binary, process checks, log prefix, images, and Compose service names together so a release does not contain a mixed identity. Build outputs using old names are not produced as the primary artifacts.

Container registries controlled by the project should temporarily publish or retain aliases from the old released image names to the same immutable digest. DNS controlled by the project should keep the old hostname resolving/proxying to the same service while supported client versions migrate. Alias retirement requires an explicit operator decision based on deployed-client inventory; it is not coupled to source merge.

No new canonical production hostname is hard-coded by this change. Documentation uses `gateway.example.com`/`wss://gateway.example.com/ws` or an operator-selected brand-neutral host. This separates product naming from one developer-owned domain.

Alternative considered: remove old image and DNS names immediately. Rejected because released mobile/desktop clients and deployment automation can retain endpoint or image references outside this repository.

### 3. Treat Go imports as an atomic mechanical migration

Change `module k2-gateway` to `module webrtc-sip-gateway` and mechanically rewrite all first-party imports. Run `gofmt`, the full Go test suite, and builds for the gateway and database-bootstrap commands. Do not combine this rewrite with package moves or behavior refactors.

A fully qualified VCS module path is deferred until the permanent Git host/owner is selected. Introducing a guessed organization would replace one temporary identity with another.

### 4. Preserve the database and bootstrap coordination contract

Fresh development examples use brand-neutral resource names, for example `webrtc_sip_gateway`, `gateway_user`, and a brand-neutral Compose network/container label. Existing `DB_DSN` values remain valid; the application performs no database/role rename and no data migration.

The PostgreSQL bootstrap advisory-lock **numeric value must remain unchanged** during this rename. Old and new release images can run concurrently during rollout and must contend on the same lock. Only its source comment/name may become brand-neutral. Changing the lock value would allow two bootstrap generations to classify or migrate one database concurrently.

Likewise, existing volumes and operator-managed database resources are not recreated. Documentation explains that their internal legacy names are harmless compatibility identifiers and may be changed later only through an operator-planned infrastructure migration.

### 5. Migrate browser persistence keys without losing operator state

Rename active frontend storage keys to a `webrtc-sip-gateway` namespace. On first load after upgrade:

1. read the new key;
2. if absent, read the corresponding legacy `k2-*` key;
3. validate and copy a valid value to the new key;
4. remove the migrated legacy key;
5. continue using only the new key.

This applies to persisted theme/trunk preferences and the password-auth session key where present. Invalid legacy data is discarded through the existing validation/default behavior. Tests cover migration, precedence of a new value, and cleanup of the old key.

Alternative considered: leave browser keys unchanged as invisible implementation details. Rejected because they remain active temporary-brand identifiers visible to operators and future maintainers.

Alternative considered: rename without migration. Rejected because it resets preferences and can unexpectedly log operators out during the deployment.

### 6. Update protocol-visible product identity without changing protocol behavior

Replace active SIP User-Agent product tokens containing `K2` with `WebRTC-SIP-Gateway` while preserving existing version semantics and any interoperability-required surrounding syntax. Add focused tests for REGISTER and relevant outbound requests so no old token is emitted.

WebSocket types/fields, REST routes, HTTP status behavior, SIP methods/status mapping, SDP, and media are unchanged. Client type names such as `GatewayClient` remain valid generic code concepts and do not need product prefixes.

### 7. Define the scan boundary explicitly

An automated case-insensitive scan must fail on unintended active occurrences of:

- `K2 Gateway` / `K2 WebRTC Gateway`;
- `k2-gateway`, `k2-frontend`, and `k2-stack`;
- active product use of `webrtc-gateway` where `webrtc-sip-gateway` is intended.

Allowlisted compatibility or historical occurrences must be explicit and documented, including:

- archived OpenSpec changes and historical plans;
- the unchanged numeric advisory-lock value;
- operator migration notes that necessarily name old images, hostnames, storage keys, or resource names;
- externally owned legacy DNS/database identifiers retained during the compatibility window.

The scan must distinguish branding from unrelated identifiers where the substring `k2` has an independent external meaning. Each exception needs a reason rather than a broad directory exclusion when practical.

### 8. Coordinate maintained consumers without changing the wire contract

Update maintained consumers that use the temporary name in current documentation, comments, default/example endpoints, tests, or workspace references. At minimum audit `softphone-mobile`, `ems-agent-electron`, and `softphone-kmp-sdk`. Endpoint-default changes require the replacement endpoint to exist first, and tests should use neutral example domains when a live environment is not the subject under test.

This repository's implementation can land before all consumers only if old DNS remains a compatibility alias. Removing the alias is a separate final rollout step after consumer inventory confirms migration.

## Risks / Trade-offs

- **[Old clients cannot reach a renamed hostname]** -> Keep the old DNS/TLS route as a compatibility alias, migrate default configs first, and retire it only after deployed-version inventory confirms safety.
- **[Deployment automation cannot pull renamed images]** -> Publish/retain old registry aliases to the same digest during the compatibility window and document the cutoff separately.
- **[Mixed old/new binary or health-check names break containers]** -> Change build output, copy path, entrypoint, supervisor, and health check atomically and smoke-test both split and unified images.
- **[Go import rewrite introduces accidental behavior changes]** -> Keep it mechanical, run `gofmt`, `go test ./...`, and build both commands; do not move packages.
- **[Frontend preferences or login state disappear]** -> Perform one-time validated storage-key migration with tests and legacy-key cleanup.
- **[Fresh defaults are mistaken for a required production DB rename]** -> State explicitly that existing DSNs/databases/roles/volumes remain supported and perform no automatic data migration.
- **[Bootstrap races across old and new releases]** -> Preserve the advisory-lock numeric value exactly through the rename.
- **[SIP peers rely on the old User-Agent string]** -> Keep method/header structure and version behavior stable, add interoperability-focused tests, and provide an opt-out/configurable token only if real peer evidence requires it.
- **[Historical documents keep old names]** -> Treat archive/history as intentional provenance; current normative docs and active changes use the canonical identity.

## Migration Plan

1. Reserve/rename the Git repository and create redirect support for the old repository URL if the host provides it; communicate the new clone path.
2. Add registry repositories and the brand-neutral DNS/TLS endpoint before changing defaults. Keep old image and hostname aliases active.
3. Land the atomic source rename: root metadata, Go module/imports, binary/process/logs, SIP User-Agent, frontend identity/storage migration, images, Compose, scripts, and active docs.
4. Build and publish canonical images, optionally tagging the same digest under compatibility image names.
5. Deploy bootstrap and gateway using the new artifacts while preserving the existing database, DSN, volumes, and advisory-lock value.
6. Update maintained mobile, desktop, and SDK consumers; release clients with the new display text and endpoint defaults.
7. Scan monitoring, external automation, secrets/config stores, DNS, TLS, and deployment platforms for old active references.
8. After supported deployed clients and automation have migrated, retire old image aliases and DNS deliberately. Historical documents and existing internal database resource names need not be rewritten.

Rollback uses the previous image and old client defaults. Because this change has no database schema or wire-contract migration, rollback does not require database changes. The old DNS/image aliases must remain available until rollback confidence and the compatibility window end.

## Open Questions

- What permanent Git host/owner should eventually become the fully qualified Go module prefix? Until decided, the module uses `webrtc-sip-gateway`.
- What operator-owned brand-neutral production hostname will replace `k2-gateway.kasemsan.com`, and how long must the old DNS/TLS alias remain?
- What is the compatibility-window policy for old registry image names: a fixed release count, a fixed date, or deployed-consumer inventory?

