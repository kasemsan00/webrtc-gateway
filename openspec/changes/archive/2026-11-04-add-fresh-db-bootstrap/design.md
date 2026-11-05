## Context

The gateway has two schema mechanisms with different assumptions:

- `apps/gateway/init.sql` creates a complete schema and is mounted only by the development PostgreSQL compose service. PostgreSQL executes it only when creating a new data directory.
- `apps/gateway/migrations/` contains Goose deltas whose earliest files alter `sip_trunks` and `call_sessions`; they assume the baseline relations already exist.

Production split and unified images package Goose migrations but not `init.sql`. Their app entrypoint can run Goose automatically. In the unified image, that entrypoint is launched as a background child, so its failure does not currently stop frontend/nginx startup.

The current migrations are safe to replay over the schema represented by `init.sql`: schema additions use `IF NOT EXISTS`, data backfills target empty fresh tables, and table creation is guarded. This lets fresh bootstrap establish normal Goose history by executing migrations rather than directly fabricating rows in `goose_db_version`.

See `proposal.md` for motivation and `specs/database-bootstrap/spec.md` for required observable behavior.

## Goals / Non-Goals

**Goals:**

- Give operators one command with deterministic behavior for fresh, legacy-unversioned, and Goose-managed databases.
- Keep the fresh path and upgrade path convergent: both finish with the same schema and genuine Goose history.
- Hold one PostgreSQL advisory lock across classification, baseline installation, and migration execution.
- Make unsafe or ambiguous database states require explicit operator intervention.
- Make released gateway images self-contained for bootstrap.
- Prevent a failed gateway bootstrap or startup from producing a healthy unified stack.

**Non-Goals:**

- Automatically repair arbitrary schema drift or partially restored databases.
- Create the PostgreSQL server, database, role, or credentials.
- Move gateway tables to a new PostgreSQL schema namespace.
- Change retention partitions, application data models, API contracts, or SIP/media behavior.
- Run migrations independently from every replica during a normal multi-instance rollout.

## Decisions

### 1. Add a dedicated Go bootstrap command

Add a small command under the gateway module (for example `cmd/db-bootstrap`) and build it as `/app/gateway/db-bootstrap` in gateway and unified images. It will:

1. Validate `DB_DSN` and connect with a bounded timeout.
2. Acquire a stable, gateway-specific PostgreSQL session advisory lock with a configurable wait timeout.
3. Classify the database state from known relations and Goose metadata.
4. Install the packaged baseline when appropriate.
5. Execute Goose migrations programmatically while retaining the advisory-lock connection.
6. Validate required relations and report the final migration version.
7. Release the lock and exit with a meaningful status.

The command will use the gateway's existing PostgreSQL driver and the already-selected Goose version. A single command avoids parsing `psql` output, removes the need to install the PostgreSQL client in runtime images, and can redact DSNs consistently.

Alternative considered: extend `docker-entrypoint.sh` with `psql` and Goose commands. This adds another runtime package and cannot naturally retain one database advisory lock across separate processes.

### 2. Package an immutable bootstrap baseline

Copy the present complete schema into `apps/gateway/schema/bootstrap-baseline.sql` and embed or package it with the bootstrap command. Treat that file as an immutable baseline release: future schema changes are added only as Goose migrations. A future deliberate migration-history squash may replace the baseline under a separately reviewed change.

For a fresh database, bootstrap runs the baseline transaction and then runs every migration from the beginning. Existing migrations are expected to be idempotent over this baseline, so Goose creates authentic version history without direct writes to its internal bookkeeping table. CI will verify this invariant.

`init.sql` will no longer be an independently maintained schema source. Development PostgreSQL initialization will invoke or mount the same baseline asset, followed by the same bootstrap/migration command, so local and production paths do not drift. If Docker's `/docker-entrypoint-initdb.d` mechanism remains for convenience, its SQL input will be generated/copied from the canonical baseline rather than edited separately.

Alternative considered: run the latest `init.sql` and insert synthetic rows into `goose_db_version`. That couples bootstrap to Goose's internal table representation and can incorrectly mark data migrations as applied.

Alternative considered: add a new migration with a version earlier than all existing migrations. Existing databases already ahead of that version can reject or skip an out-of-order migration, making rollout behavior dependent on Goose flags.

### 3. Use explicit database-state classification

Bootstrap will define a manifest of required baseline relations and critical columns. It will inspect only gateway-owned relation names in the configured PostgreSQL schema; unrelated relations do not make an otherwise fresh gateway schema invalid.

States and actions:

| State | Detection | Action |
|---|---|---|
| `fresh` | No required gateway relation and no Goose table | Install baseline transaction, then run all migrations |
| `legacy-unversioned` | All required baseline relations and critical columns exist, no Goose table | Do not install baseline; run all idempotent migrations to establish history |
| `managed` | Required baseline relations and valid Goose history exist | Validate image/database compatibility, then run pending migrations |
| `partial` | Only a subset of required relations exists | Fail without mutation and list missing/present objects |
| `inconsistent` | Goose history exists but required schema objects are missing, or history is malformed | Fail without mutation and report the inconsistency |
| `image-too-old` | Applied migration versions are not represented by the image or exceed its maximum version | Fail before migration or gateway startup |

The manifest should include stable core relations such as `call_sessions`, `call_events`, `call_payloads`, `call_stats`, `sip_dialogs`, `sip_trunks`, `session_directory`, and `gateway_instances`, plus the baseline's partition/default structures where required for runtime writes. Exact schema equality is not required because newer managed databases can legitimately contain later columns and indexes.

Alternative considered: consider the database initialized if only `sip_trunks` exists. This reproduces the current failure mode for partially restored databases and hides missing logging/session relations until runtime.

### 4. Serialize the complete operation with a PostgreSQL advisory lock

The bootstrap command will hold a session advisory lock derived from a fixed project identifier for the whole operation. Lock acquisition uses bounded polling or PostgreSQL lock timeout behavior so deploy jobs cannot wait forever. Goose may retain its own migration lock, but that is defense in depth and not the lifecycle lock.

Holding the outer lock makes the gap between fresh-state detection, baseline commit, and Goose migration execution safe. A concurrent runner waits, then reclassifies the post-bootstrap database and takes the no-op managed path.

The baseline is executed inside a transaction while the session lock remains held. Goose migrations keep their normal per-migration transactional semantics. The entire history cannot be one transaction because existing/future migrations may have their own transaction boundaries, so a failed migration leaves a recognizable managed database at the last successful Goose version and can be retried after correction.

### 5. Separate the production migration job from application startup

Production compose profiles will add a one-shot `db-bootstrap` service using the same release image and environment as the gateway. Gateway/stack services depend on successful completion of that job. Runtime application services default to startup bootstrap disabled.

For simple single-instance environments, `DB_BOOTSTRAP_ON_START=true` explicitly runs the same bootstrap executable in the foreground before `exec`-ing the gateway. The existing `DB_AUTO_MIGRATE` variable will be deprecated: during a compatibility window it maps to startup bootstrap with a warning, then documentation and example environments use only the new variable.

This preserves a convenient one-container mode without making per-replica migration the production default.

Alternative considered: always bootstrap in every gateway entrypoint. The advisory lock makes it technically safe, but replica startup becomes coupled to schema administration and a rolling deploy creates unnecessary lock contention.

### 6. Make unified process failure observable and fatal

The unified supervisor will verify that the gateway child remains alive before starting nginx and will terminate the stack if either required application child exits unexpectedly. Its Docker health check will require the proxy, frontend, and gateway processes rather than accepting an nginx-only response.

When startup bootstrap is selected, it runs synchronously before the gateway is placed in the background, so migration failure propagates directly. Under the production one-shot workflow, compose dependency completion prevents the unified container from starting at all.

This change does not require a new public REST endpoint; process and internal-port checks are sufficient for this scope.

### 7. Test against real PostgreSQL state transitions

Unit tests will cover state classification, DSN redaction, and error mapping. Integration tests will use disposable PostgreSQL databases to validate:

- empty database to fully migrated schema;
- repeated bootstrap no-op;
- current pre-Goose baseline adoption;
- legacy Goose schema upgrade with preserved rows;
- partial schema rejection with no additional objects created;
- Goose history with missing core relations rejection;
- image-too-old rejection;
- injected baseline/migration failure and retry behavior;
- two concurrent bootstrap processes producing one initialization;
- schema equivalence required by runtime queries.

CI will also create a database from the immutable baseline and replay the complete migration directory. This prevents future non-idempotent migration changes from silently breaking fresh deploys.

## Risks / Trade-offs

- **[Baseline and migrations drift apart]** → Make the baseline immutable, make migrations the only forward-change mechanism, and run baseline-plus-all-migrations in CI.
- **[A legacy unversioned schema only resembles the baseline]** → Require the full core relation/critical-column fingerprint; reject uncertain states instead of guessing.
- **[Long migration blocks deploy]** → Use an operator-configurable advisory-lock timeout and migration context timeout, with explicit logs of lock acquisition and current migration.
- **[Migration fails after earlier migrations commit]** → Preserve Goose's last successful version and make bootstrap retryable; do not pretend the whole migration chain is atomic.
- **[Multiple images target one database during rollout]** → Reject database versions unknown to an older image and serialize bootstrap; deploy bootstrap before rolling instances.
- **[Adding a second Go binary increases image/build complexity]** → Build it from the existing module and dependencies, and reuse it identically in gateway and unified images.
- **[Deprecated `DB_AUTO_MIGRATE` surprises existing operators]** → Support a documented compatibility mapping and warning for one transition period.

## Migration Plan

1. Add the canonical baseline asset, bootstrap command, classifier, advisory locking, and integration tests without changing production defaults.
2. Package the bootstrap command and schema assets in both gateway images.
3. Add one-shot bootstrap services to split/unified compose profiles and make application services depend on successful completion.
4. Update entrypoints, unified supervision, health checks, environment examples, and operator documentation. Keep the temporary `DB_AUTO_MIGRATE` compatibility mapping.
5. Validate on a disposable empty database and a copy of a production-like legacy database.
6. Deploy the bootstrap job once, confirm its final Goose version, then roll gateway/stack instances with startup bootstrap disabled.

Rollback of application images is safe only while the target older image recognizes every applied database migration. If a release adds a forward-only schema migration, roll forward to the fixed application image instead of automatically migrating down. The bootstrap command never invokes Goose `down`.
