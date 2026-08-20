## 1. Canonical source identity

- [x] 1.1 Rename the root package/repository references from `webrtc-gateway` to `webrtc-sip-gateway` in active workspace metadata and contributor guidance.
- [x] 1.2 Change the Go module from `k2-gateway` to `webrtc-sip-gateway` and mechanically rewrite all first-party Go imports without moving packages or changing behavior.
- [x] 1.3 Replace active product prose with `WebRTC-SIP Gateway` and add a repository scan/allowlist that rejects unintended temporary-brand references.
- [x] 1.4 Replace SIP User-Agent product tokens containing `K2` with `WebRTC-SIP-Gateway` and add focused header tests.

## 2. Runtime and distribution artifacts

- [x] 2.1 Rename the backend binary, process checks, entrypoints, build outputs, and log prefix to `webrtc-sip-gateway`, updating log APIs/tests consistently.
- [x] 2.2 Rename backend, console, and unified Docker images to `webrtc-sip-gateway`, `webrtc-sip-gateway-console`, and `webrtc-sip-gateway-stack`.
- [x] 2.3 Rename active Compose services and unified supervisor messages to the canonical or scoped service identities, preserving dependency and health-check behavior.
- [x] 2.4 Update CI/build scripts and registry documentation; document temporary old-image aliases pointing at the same immutable release digest.

## 3. Database and local infrastructure compatibility

- [x] 3.1 Introduce brand-neutral database, role, container, network, and test-resource defaults for fresh local deployments without altering existing production DSNs or data.
- [x] 3.2 Preserve the PostgreSQL bootstrap advisory-lock numeric value exactly and update only its brand-bearing source description/identifier.
- [x] 3.3 Document that existing databases, roles, volumes, and operator-owned infrastructure with legacy names remain supported and are not renamed automatically.
- [x] 3.4 Verify fresh bootstrap, existing managed-database bootstrap, and concurrent old/new compatibility assumptions against the unchanged lock contract.

## 4. Operations console identity and persistence

- [x] 4.1 Rename human-facing frontend branding to `WebRTC-SIP Gateway Console` and distribution/package references to `webrtc-sip-gateway-console` where externally visible.
- [x] 4.2 Replace active `k2-*` local/session storage keys with canonical namespaced keys and implement one-time validated migration with new-key precedence and legacy-key cleanup.
- [x] 4.3 Add frontend tests proving theme, trunk preferences, and password-auth state survive the storage-key migration and invalid legacy values fall back safely.

## 5. Documentation and maintained consumers

- [x] 5.1 Update current root, gateway, frontend, deploy, config, operations, troubleshooting, translator, and delivery documentation while leaving historical archives as provenance.
- [x] 5.2 Replace hard-coded developer-owned production URLs in examples with a neutral domain unless the document explicitly describes compatibility migration.
- [ ] 5.3 Audit and update current branding, workspace references, neutral test URLs, and endpoint defaults in `softphone-mobile`, `ems-agent-electron`, and `softphone-kmp-sdk` after the replacement endpoint is available.
- [x] 5.4 Document DNS/TLS and Git-repository redirects plus the explicit criteria for retiring old hostname and image aliases.

## 6. Verification and rollout

- [x] 6.1 Run the temporary-brand scan and review every allowlisted legacy occurrence for a compatibility or historical reason.
- [x] 6.2 Run `gofmt`, `go test ./...`, and builds for both the gateway and `cmd/db-bootstrap` from `apps/gateway`.
- [x] 6.3 Run frontend tests, type-check, lint, and production build; verify the generated route tree is regenerated only by the normal build tooling.
- [ ] 6.4 Build and smoke-test split and unified images, including process health checks, fresh/existing database startup, log listing, and rollback to the prior image.
- [ ] 6.5 Verify maintained consumer tests/builds in proportion to changed references and confirm WebSocket/SIP/media contracts have no diff.
- [ ] 6.6 Deploy canonical artifacts with old DNS/image aliases active, migrate consumer defaults, and retire aliases only after the documented compatibility criterion is met.
