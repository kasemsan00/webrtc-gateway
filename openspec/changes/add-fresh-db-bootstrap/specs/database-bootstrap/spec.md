## Purpose

Provide a deterministic and recoverable way to prepare the gateway PostgreSQL schema for both first deployment and subsequent upgrades without risking existing data.

## ADDED Requirements

### Requirement: Fresh database initialization
The bootstrap operation SHALL detect a database with no gateway schema, create the complete baseline gateway schema, and apply all source-controlled migrations in version order.

#### Scenario: Initialize an empty database
- **WHEN** an operator runs bootstrap against a reachable database containing no gateway tables and no Goose migration history
- **THEN** the operation creates the baseline schema, applies every pending migration, records the resulting migration version, and exits successfully

#### Scenario: Fresh schema is immediately usable
- **WHEN** fresh database bootstrap completes successfully
- **THEN** the gateway can start with database persistence enabled without any missing-relation or missing-column error

### Requirement: Existing database upgrade
The bootstrap operation SHALL preserve existing gateway data and SHALL apply only migrations not already recorded as applied.

#### Scenario: Upgrade a legacy database
- **WHEN** bootstrap runs against a valid gateway schema whose recorded Goose version is behind the image migration set
- **THEN** it applies the pending migrations in order without recreating baseline tables or deleting existing rows

#### Scenario: Current database is bootstrapped again
- **WHEN** bootstrap runs against a valid gateway schema with all image migrations already applied
- **THEN** it performs no schema mutation and exits successfully

#### Scenario: Database version is newer than the image
- **WHEN** the database records a migration version not present in the running image
- **THEN** bootstrap fails before gateway startup and reports that the image is older than the database schema

### Requirement: Ambiguous schema protection
The bootstrap operation SHALL refuse to infer or repair a partially initialized or inconsistent gateway schema automatically.

#### Scenario: Some baseline tables exist without migration history
- **WHEN** bootstrap finds only a subset of the required baseline gateway tables and no valid Goose history
- **THEN** it makes no additional schema changes, exits non-zero, and identifies the database as partially initialized

#### Scenario: Migration history exists without required gateway tables
- **WHEN** bootstrap finds Goose migration history but one or more required baseline gateway tables are absent
- **THEN** it exits non-zero before applying migrations and reports the missing schema objects

### Requirement: Serialized execution
Bootstrap operations targeting the same database SHALL execute under a database-scoped lock that spans state detection, baseline creation, and migration application.

#### Scenario: Concurrent fresh deployment
- **WHEN** two deployment jobs start bootstrap concurrently against the same empty database
- **THEN** one operation initializes and migrates the database while the other waits and subsequently validates the completed schema without replaying the baseline

#### Scenario: Lock wait expires
- **WHEN** another bootstrap operation retains the database lock longer than the configured timeout
- **THEN** the waiting operation exits non-zero without changing the schema and reports a lock timeout

### Requirement: Transactional and fail-fast behavior
Each baseline or migration unit SHALL be transactional where PostgreSQL permits, and any bootstrap failure SHALL prevent the gateway deployment from becoming ready.

#### Scenario: Baseline statement fails
- **WHEN** any statement in fresh baseline installation fails
- **THEN** the baseline transaction is rolled back, bootstrap exits non-zero, and the gateway is not started

#### Scenario: Migration fails
- **WHEN** a pending migration fails
- **THEN** bootstrap exits non-zero, reports the failing migration, and the gateway deployment is not marked ready

#### Scenario: Unified gateway child exits during startup
- **WHEN** the unified stack starts but its gateway process exits before becoming healthy
- **THEN** the stack exits non-zero and does not continue serving a frontend-only ready state

### Requirement: Explicit production deployment workflow
Production deployment profiles SHALL provide a one-shot bootstrap operation that completes successfully before application instances start, and automatic per-instance migration SHALL be disabled by default.

#### Scenario: Deploy production profile
- **WHEN** an operator deploys a documented split or unified production profile
- **THEN** the bootstrap job runs once before the gateway starts and a failed job blocks the application rollout

#### Scenario: Single-instance startup migration is explicitly enabled
- **WHEN** an operator opts into startup bootstrap for a single gateway instance
- **THEN** the instance completes the same serialized bootstrap operation before starting the gateway process

### Requirement: Self-contained deployment artifact
Every gateway image capable of running bootstrap SHALL contain the matching baseline schema, migration files, and bootstrap executable for that image version.

#### Scenario: Bootstrap from a released image
- **WHEN** the one-shot bootstrap command is executed from a released gateway or unified image
- **THEN** it requires no schema file mounted from the source checkout and uses only assets packaged in that image

### Requirement: Actionable diagnostics
The bootstrap operation SHALL emit concise diagnostics that identify the selected database state, the action taken, the final migration version, and any failure without logging database credentials.

#### Scenario: Successful bootstrap log
- **WHEN** bootstrap succeeds
- **THEN** logs identify whether the database was freshly initialized or upgraded and include the final migration version

#### Scenario: Failure log redacts credentials
- **WHEN** bootstrap fails while using a credential-bearing DSN
- **THEN** the error output describes the failure without printing the DSN password
