// Package dbbootstrap safely initializes and migrates the gateway PostgreSQL schema.
package dbbootstrap

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Keep this numeric value stable so pre-rename and canonical release images
// serialize database bootstrap through the same PostgreSQL advisory lock.
const advisoryLockKey int64 = 0x4b325f44425f4253

var (
	coreTables = []string{
		"call_sessions", "call_events", "call_payloads", "call_stats",
		"sip_dialogs", "sip_trunks", "session_directory", "gateway_instances",
	}
	criticalColumns = map[string][]string{
		"call_sessions": {"session_id", "direction"},
		"sip_trunks":    {"id", "name", "domain", "username", "password"},
	}
	migrationName = regexp.MustCompile(`^(\d+)_.*\.sql$`)
)

type State string

const (
	StateFresh             State = "fresh"
	StateLegacyUnversioned State = "legacy-unversioned"
	StateManaged           State = "managed"
	StatePartial           State = "partial"
	StateInconsistent      State = "inconsistent"
	StateImageTooOld       State = "image-too-old"
)

type Config struct {
	DSN              string
	BaselinePath     string
	MigrationsDir    string
	GooseBinary      string
	LockTimeout      time.Duration
	MigrationTimeout time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		DSN:              strings.TrimSpace(os.Getenv("DB_DSN")),
		BaselinePath:     envOr("DB_BOOTSTRAP_BASELINE", "./schema/bootstrap-baseline.sql"),
		MigrationsDir:    envOr("DB_BOOTSTRAP_MIGRATIONS_DIR", "./migrations"),
		GooseBinary:      envOr("DB_BOOTSTRAP_GOOSE_BINARY", "./goose"),
		LockTimeout:      durationEnv("DB_BOOTSTRAP_LOCK_TIMEOUT", 30*time.Second),
		MigrationTimeout: durationEnv("DB_BOOTSTRAP_MIGRATION_TIMEOUT", 5*time.Minute),
	}
}

type Result struct {
	State        State
	FinalVersion int64
}

func Run(ctx context.Context, cfg Config) (Result, error) {
	if cfg.DSN == "" {
		return Result{}, fmt.Errorf("DB_DSN is required for database bootstrap")
	}
	if cfg.LockTimeout <= 0 || cfg.MigrationTimeout <= 0 {
		return Result{}, fmt.Errorf("bootstrap timeouts must be positive")
	}
	if cfg.BaselinePath == "" || cfg.MigrationsDir == "" || cfg.GooseBinary == "" {
		return Result{}, fmt.Errorf("baseline path, migrations directory, and Goose binary are required")
	}

	conn, err := pgx.Connect(ctx, cfg.DSN)
	if err != nil {
		return Result{}, fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close(context.Background())

	if err := acquireLock(ctx, conn, cfg.LockTimeout); err != nil {
		return Result{}, err
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey) }()

	state, err := classify(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if state == StatePartial || state == StateInconsistent {
		return Result{State: state}, fmt.Errorf("database schema is %s; refusing automatic repair", state)
	}

	versions, err := migrationVersions(cfg.MigrationsDir)
	if err != nil {
		return Result{State: state}, err
	}
	if state == StateManaged {
		if err := validateHistory(ctx, conn, versions); err != nil {
			return Result{State: StateImageTooOld}, err
		}
	}
	if state == StateFresh {
		if err := installBaseline(ctx, conn, cfg.BaselinePath); err != nil {
			return Result{State: state}, err
		}
		fmt.Println("database bootstrap: installed baseline schema")
	} else {
		fmt.Printf("database bootstrap: using %s schema\n", state)
	}

	migrationCtx, cancel := context.WithTimeout(ctx, cfg.MigrationTimeout)
	defer cancel()
	cmd := exec.CommandContext(migrationCtx, cfg.GooseBinary, "-dir", cfg.MigrationsDir, "postgres", cfg.DSN, "up")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if migrationCtx.Err() != nil {
			return Result{State: state}, fmt.Errorf("database migration timed out after %s", cfg.MigrationTimeout)
		}
		return Result{State: state}, fmt.Errorf("run database migrations: %w", err)
	}

	if err := validateComplete(ctx, conn); err != nil {
		return Result{State: state}, err
	}
	version, err := currentVersion(ctx, conn)
	if err != nil {
		return Result{State: state}, err
	}
	fmt.Printf("database bootstrap: complete state=%s version=%d\n", state, version)
	return Result{State: state, FinalVersion: version}, nil
}

func acquireLock(ctx context.Context, conn *pgx.Conn, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var locked bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&locked); err != nil {
			return fmt.Errorf("acquire database bootstrap lock: %w", err)
		}
		if locked {
			return nil
		}
		if time.Now().Add(100 * time.Millisecond).After(deadline) {
			return fmt.Errorf("database bootstrap lock timeout after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func classify(ctx context.Context, conn *pgx.Conn) (State, error) {
	present, err := relationSet(ctx, conn)
	if err != nil {
		return "", err
	}
	columnsValid := verifyCriticalColumns(ctx, conn) == nil
	return classifyRelations(present, columnsValid), nil
}

func classifyRelations(present map[string]bool, columnsValid bool) State {
	goosePresent := present["goose_db_version"]
	count := 0
	for _, name := range coreTables {
		if present[name] {
			count++
		}
	}
	if count == 0 && !goosePresent {
		return StateFresh
	}
	if count != len(coreTables) || !columnsValid {
		if goosePresent {
			return StateInconsistent
		}
		return StatePartial
	}
	if goosePresent {
		return StateManaged
	}
	return StateLegacyUnversioned
}

func relationSet(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	names := append(append([]string{}, coreTables...), "goose_db_version")
	rows, err := conn.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("inspect database schema: %w", err)
	}
	defer rows.Close()
	present := make(map[string]bool, len(names))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	return present, rows.Err()
}

func verifyCriticalColumns(ctx context.Context, conn *pgx.Conn) error {
	for table, columns := range criticalColumns {
		for _, column := range columns {
			var exists bool
			err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`, table, column).Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("missing required column %s.%s", table, column)
			}
		}
	}
	return nil
}

func installBaseline(ctx context.Context, conn *pgx.Conn, path string) error {
	sql, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bootstrap baseline: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin baseline transaction: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("install bootstrap baseline: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap baseline: %w", err)
	}
	return nil
}

func migrationVersions(dir string) (map[int64]struct{}, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}
	versions := make(map[int64]struct{}, len(files))
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(file.Name())
		if len(match) != 2 {
			continue
		}
		var version int64
		if _, err := fmt.Sscan(match[1], &version); err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", file.Name(), err)
		}
		versions[version] = struct{}{}
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("no SQL migrations found in %s", dir)
	}
	return versions, nil
}

func validateHistory(ctx context.Context, conn *pgx.Conn, versions map[int64]struct{}) error {
	rows, err := conn.Query(ctx, "SELECT version_id FROM goose_db_version WHERE is_applied = true")
	if err != nil {
		return fmt.Errorf("read Goose migration history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return err
		}
		if version == 0 {
			continue
		}
		if _, known := versions[version]; !known {
			return fmt.Errorf("database migration version %d is not present in this image", version)
		}
	}
	return rows.Err()
}

func validateComplete(ctx context.Context, conn *pgx.Conn) error {
	state, err := classify(ctx, conn)
	if err != nil {
		return err
	}
	if state != StateManaged {
		return fmt.Errorf("bootstrap finished with unexpected database state %s", state)
	}
	return nil
}

func currentVersion(ctx context.Context, conn *pgx.Conn) (int64, error) {
	var version int64
	err := conn.QueryRow(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = true").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read final Goose version: %w", err)
	}
	return version, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		fmt.Fprintf(os.Stderr, "database bootstrap: invalid %s=%q; using %s\n", key, value, fallback)
		return fallback
	}
	return parsed
}

// RedactDSN removes passwords from a PostgreSQL URL before it is logged.
func RedactDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return "[redacted database DSN]"
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword(parsed.User.Username(), "****")
	}
	for _, key := range []string{"password", "pass"} {
		if parsed.Query().Has(key) {
			query := parsed.Query()
			query.Set(key, "****")
			parsed.RawQuery = query.Encode()
		}
	}
	return parsed.String()
}

// KnownVersions is intentionally exported for package-level tests and diagnostics.
func KnownVersions(dir string) ([]int64, error) {
	versions, err := migrationVersions(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	result := make([]int64, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}
