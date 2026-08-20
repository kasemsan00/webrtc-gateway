## Why

`K2` was introduced as a temporary project-specific label, but it is now embedded in the Go module, binaries, container images, process checks, log names, SIP User-Agent headers, frontend storage keys, deployment examples, and consumer documentation. The current repository name `webrtc-gateway` is also too broad: it does not identify SIP/RTP as the other protocol boundary. The project needs one descriptive, brand-neutral identity before more releases and client integrations make the temporary name more expensive to remove.

## What Changes

- **BREAKING:** Adopt **WebRTC-SIP Gateway** as the canonical human-readable product name and `webrtc-sip-gateway` as its canonical machine-readable identifier.
- **BREAKING:** Rename the Go module/import prefix, gateway binary/process, Docker images, unified stack, and active deployment service identifiers that currently use `k2-*` or the ambiguous `webrtc-gateway` product identifier.
- Rename the operations frontend to **WebRTC-SIP Gateway Console** and use `webrtc-sip-gateway-console` for its distribution image; app-local service names may remain concise (`gateway`, `gateway-console`).
- Replace active `K2 Gateway`, `K2 WebRTC Gateway`, `k2-gateway`, `k2-frontend`, and `k2-stack` branding in source, UI, SIP User-Agent values, logs, scripts, current documentation, and maintained client/SDK references.
- Introduce new brand-neutral defaults for fresh local/development database resources and frontend storage keys without destructively renaming existing databases, roles, volumes, or operator-owned infrastructure.
- Preserve the old public hostname and released image names as migration aliases for a documented compatibility window where infrastructure is under project control; new examples use operator-selected, brand-neutral hostnames.
- Keep HTTP paths, WebSocket message types and fields, environment variable names, SIP behavior, media behavior, database schema/data, and call semantics unchanged.

## Capabilities

### New Capabilities

- `gateway-product-identity`: Canonical brand-neutral naming for the repository, runtime, distribution artifacts, protocol identity, operations console, and compatibility behavior during the rename.

### Modified Capabilities

None.

## Impact

- Root workspace/package metadata and repository documentation.
- Go module declaration and all internal Go imports under `apps/gateway`.
- Gateway and unified Dockerfiles, Compose profiles, entrypoints, health checks, build scripts, registry image names, process names, and log-file discovery/tests.
- SIP `User-Agent` headers; no SIP method, status, SDP, codec, RTP/RTCP, or dialog behavior changes.
- Frontend page title/header wording, package/image identity, and `k2-*` browser storage keys.
- Local development database/container/network defaults and examples; existing deployed database names, roles, and volumes remain valid.
- Current gateway/frontend/deploy/config/operations documentation and maintained consumers such as `softphone-mobile`, `ems-agent-electron`, and `softphone-kmp-sdk` where they reference the temporary name, workspace path, image, or example endpoint.
- External rollout coordination for Git repository path, container registry aliases, DNS/TLS, deployment configuration, monitoring, and old client versions.

