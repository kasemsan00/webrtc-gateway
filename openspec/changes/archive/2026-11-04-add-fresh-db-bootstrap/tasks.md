## 1. Canonical Schema Baseline

- [x] 1.1 Create the immutable `apps/gateway/schema/bootstrap-baseline.sql` from the current complete gateway schema and document its covered schema state.
- [x] 1.2 Make local PostgreSQL initialization consume the canonical baseline instead of maintaining an independent `init.sql` schema definition.
- [x] 1.3 Add a consistency check that fails when the canonical baseline cannot be followed by every migration in `apps/gateway/migrations`.

## 2. Bootstrap Core

- [x] 2.1 Add the gateway database-bootstrap package and command with bounded connection, lock, and migration timeouts.
- [x] 2.2 Implement PostgreSQL advisory-lock acquisition and release around the full classify, baseline, migrate, and validate lifecycle.
- [x] 2.3 Implement required-relation and critical-column inspection for `fresh`, `legacy-unversioned`, `managed`, `partial`, and `inconsistent` states.
- [x] 2.4 Implement image/database migration-version compatibility checks, including rejection of unknown or newer applied versions.
- [x] 2.5 Execute the baseline transaction only for the `fresh` state and guarantee rollback on a failed baseline statement.
- [x] 2.6 Run pending Goose migrations for fresh, legacy-unversioned, and managed states without directly fabricating Goose history.
- [x] 2.7 Add final schema/version validation and structured, credential-redacted success and failure diagnostics.

## 3. Runtime and Image Integration

- [x] 3.1 Build and package the bootstrap executable, canonical baseline, and migration directory in `apps/gateway/Dockerfile`.
- [x] 3.2 Build and package the identical bootstrap assets in `deploy/Dockerfile.unified`.
- [x] 3.3 Update the gateway entrypoint to support explicit `DB_BOOTSTRAP_ON_START`, run bootstrap synchronously, and temporarily map deprecated `DB_AUTO_MIGRATE` with a warning.
- [x] 3.4 Update the unified supervisor so bootstrap/gateway startup failure stops the stack and unexpected gateway or frontend child exit terminates the container.
- [x] 3.5 Tighten gateway and unified Docker health checks so a frontend/nginx-only process set cannot report healthy.

## 4. Deployment Workflow

- [x] 4.1 Add a one-shot bootstrap service to the split compose profile and require its successful completion before gateway startup.
- [x] 4.2 Add a one-shot bootstrap service to the unified compose profile and require its successful completion before stack startup.
- [x] 4.3 Disable per-instance startup migration by default in production examples while preserving the documented single-instance opt-in path.
- [x] 4.4 Update environment examples and configuration reference with bootstrap enablement, lock timeout, migration timeout, and deprecation behavior.
- [x] 4.5 Update deployment and operations documentation with fresh deploy, existing upgrade, failure recovery, multi-instance rollout, and rollback procedures.

## 5. Verification

- [x] 5.1 Add unit tests for database-state classification, version compatibility, timeout errors, and DSN credential redaction.
- [x] 5.2 Add PostgreSQL integration coverage for empty bootstrap, repeated no-op, and legacy-unversioned baseline adoption.
- [x] 5.3 Add PostgreSQL integration coverage for a legacy managed upgrade that preserves representative trunk and session rows.
- [x] 5.4 Add negative integration coverage for partial schema, migration history with missing core relations, newer database version, and baseline/migration failure rollback behavior.
- [x] 5.5 Add a concurrency integration test proving two bootstrap runners produce one complete initialization and valid Goose history.
- [x] 5.6 Verify gateway runtime database queries against the freshly bootstrapped schema and run `go test ./...` from `apps/gateway`.
- [x] 5.7 Build both gateway and unified images and smoke-test successful fresh startup plus fail-fast behavior with an intentionally broken database configuration.
