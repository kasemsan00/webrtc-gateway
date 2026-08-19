package dbbootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyRelations(t *testing.T) {
	all := map[string]bool{}
	for _, table := range coreTables {
		all[table] = true
	}

	cases := []struct {
		name      string
		present   map[string]bool
		columnsOK bool
		want      State
	}{
		{name: "fresh", present: map[string]bool{}, columnsOK: true, want: StateFresh},
		{name: "partial", present: map[string]bool{"sip_trunks": true}, columnsOK: false, want: StatePartial},
		{name: "inconsistent history", present: map[string]bool{"sip_trunks": true, "goose_db_version": true}, columnsOK: false, want: StateInconsistent},
		{name: "legacy unversioned", present: all, columnsOK: true, want: StateLegacyUnversioned},
	}
	managed := make(map[string]bool, len(all)+1)
	for key, value := range all {
		managed[key] = value
	}
	managed["goose_db_version"] = true
	cases = append(cases, struct {
		name      string
		present   map[string]bool
		columnsOK bool
		want      State
	}{name: "managed", present: managed, columnsOK: true, want: StateManaged})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRelations(tc.present, tc.columnsOK); got != tc.want {
				t.Fatalf("classifyRelations() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestKnownVersionsSorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"202603010001_second.sql", "202602010001_first.sql", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- +goose Up\nSELECT 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := KnownVersions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != 202602010001 || versions[1] != 202603010001 {
		t.Fatalf("unexpected versions: %v", versions)
	}
}

func TestCanonicalBaselineMatchesLegacyInitSQL(t *testing.T) {
	baseline, err := os.ReadFile("../../schema/bootstrap-baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile("../../init.sql")
	if err != nil {
		t.Fatal(err)
	}
	baseline = bytes.TrimRight(baseline, "\r\n")
	legacy = bytes.TrimRight(legacy, "\r\n")
	if !bytes.Equal(baseline, legacy) {
		t.Fatal("bootstrap baseline and init.sql differ; update them together or remove the legacy compatibility file")
	}
}

func TestRedactDSN(t *testing.T) {
	got := RedactDSN("postgres://k2user:super-secret@db.example/k2?sslmode=disable&password=also-secret")
	if got == "" || containsAny(got, "super-secret", "also-secret") {
		t.Fatalf("DSN was not redacted: %q", got)
	}
}

func TestDurationEnv(t *testing.T) {
	t.Setenv("DB_BOOTSTRAP_TEST_TIMEOUT", "invalid")
	if got := durationEnv("DB_BOOTSTRAP_TEST_TIMEOUT", time.Second); got != time.Second {
		t.Fatalf("durationEnv() = %s, want fallback", got)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && contains(value, needle) {
			return true
		}
	}
	return false
}

func contains(value, needle string) bool {
	for start := 0; start+len(needle) <= len(value); start++ {
		if value[start:start+len(needle)] == needle {
			return true
		}
	}
	return false
}
