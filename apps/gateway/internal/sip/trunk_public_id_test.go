package sip

import "testing"

func TestNormalizeTrunkPublicID(t *testing.T) {
	t.Parallel()

	got, ok := NormalizeTrunkPublicID("3F2504E0-4F89-11D3-9A0C-0305E82C3301")
	if !ok {
		t.Fatalf("expected NormalizeTrunkPublicID to succeed")
	}
	if got != "3f2504e0-4f89-11d3-9a0c-0305e82c3301" {
		t.Fatalf("expected canonical uuid, got=%s", got)
	}
}

