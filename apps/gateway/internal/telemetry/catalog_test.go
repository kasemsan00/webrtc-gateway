package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

type observabilityCatalog struct {
	Metrics []struct {
		Name       string   `json:"name"`
		Attributes []string `json:"attributes"`
	} `json:"metrics"`
	MetricAttributes struct {
		Allowed                 []string `json:"allowed"`
		ForbiddenNameFragments  []string `json:"forbiddenNameFragments"`
	} `json:"metricAttributes"`
}

func TestMetricCatalogRejectsForbiddenAttributesAndDuplicates(t *testing.T) {
	catalog := loadObservabilityCatalog(t)
	seen := map[string]bool{}
	allowed := map[string]bool{}
	for _, name := range catalog.MetricAttributes.Allowed {
		allowed[name] = true
	}
	namePattern := regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	for _, metric := range catalog.Metrics {
		if seen[metric.Name] {
			t.Fatalf("duplicate metric name %q", metric.Name)
		}
		seen[metric.Name] = true
		if !namePattern.MatchString(metric.Name) {
			t.Fatalf("invalid metric name %q", metric.Name)
		}
		attrSeen := map[string]bool{}
		for _, attribute := range metric.Attributes {
			if attrSeen[attribute] {
				t.Fatalf("duplicate attribute %q on %s", attribute, metric.Name)
			}
			attrSeen[attribute] = true
			if !allowed[attribute] {
				t.Fatalf("metric %s uses non-allowlisted attribute %s", metric.Name, attribute)
			}
			for _, fragment := range catalog.MetricAttributes.ForbiddenNameFragments {
				if attributeContainsFragment(attribute, fragment) {
					t.Fatalf("metric %s uses forbidden attribute %s", metric.Name, attribute)
				}
			}
		}
	}
	for _, forbidden := range []string{"session.id", "sip.call_id", "phone.number", "username", "client.ip", "device.id", "request.url", "error.message", "payload.body"} {
		rejected := !allowed[forbidden]
		for _, fragment := range catalog.MetricAttributes.ForbiddenNameFragments {
			if attributeContainsFragment(forbidden, fragment) {
				rejected = true
			}
		}
		if !rejected {
			t.Fatalf("cardinality policy accepted deliberate forbidden fixture %s", forbidden)
		}
	}
}

func loadObservabilityCatalog(t *testing.T) observabilityCatalog {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var path string
	for index := 0; index < 8; index++ {
		candidate := filepath.Join(dir, "docs", "gateway", "observability-catalog.json")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
		dir = filepath.Dir(dir)
	}
	if path == "" {
		t.Fatal("docs/gateway/observability-catalog.json not found")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog observabilityCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func attributeContainsFragment(attribute, fragment string) bool {
	pattern := regexp.MustCompile(`(^|[._])` + regexp.QuoteMeta(fragment) + `($|[._])`)
	return pattern.MatchString(attribute)
}
