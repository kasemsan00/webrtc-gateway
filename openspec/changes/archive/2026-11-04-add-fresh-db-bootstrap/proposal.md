## Why

Fresh production databases currently fail at the first Goose migration because the migration history assumes that `init.sql` has already created the baseline tables. The production split and unified deploy profiles do not provide that baseline step, so they can expose a healthy frontend/proxy while the gateway has already exited after a database error.

## What Changes

- Add a single, explicit database bootstrap command that detects whether the target PostgreSQL database is empty or already initialized.
- For an empty database, install the baseline schema and establish a correct Goose version before applying later migrations.
- For an existing database, preserve data and run only pending Goose migrations; never replay the baseline destructively.
- Serialize bootstrap with a PostgreSQL advisory lock so concurrent deploy attempts cannot initialize or migrate the same database simultaneously.
- Make bootstrap failures fatal to the gateway/stack startup path and prevent the unified proxy from reporting readiness without a running gateway.
- Package all required schema and bootstrap assets in both gateway and unified images.
- Change production guidance and compose defaults to use a one-shot bootstrap/migration deployment step, while retaining an explicit opt-in startup mode for single-instance deployments.
- Add verification for empty, current, legacy, partially initialized, and concurrent database states.

## Capabilities

### New Capabilities

- `database-bootstrap`: Defines safe, repeatable initialization and migration behavior for fresh and existing gateway PostgreSQL databases.

### Modified Capabilities

None.

## Impact

- Gateway container entrypoint and database deployment scripts.
- Gateway and unified Docker image contents.
- Split/unified compose profiles, deployment documentation, health checks, and operator workflow.
- PostgreSQL schema version bookkeeping and migration execution.
- No WebSocket, REST, SIP, RTP, or frontend application contract changes.
