package sip

import "testing"

func TestStapaContainsIDR(t *testing.T) {
	// STAP-A (24) with two NAL units: SPS (7) then IDR (5)
	payload := []byte{
		24,
		0x00, 0x02, 0x67, 0x64,
		0x00, 0x03, 0x65, 0x88, 0x84,
	}
	if !stapaContainsIDR(payload) {
		t.Fatalf("expected STAP-A payload to report IDR")
	}
}

func TestStapaContainsIDRFalseForNonIDR(t *testing.T) {
	// STAP-A (24) with SPS (7) and PPS (8), no IDR.
	payload := []byte{
		24,
		0x00, 0x02, 0x67, 0x42,
		0x00, 0x02, 0x68, 0xce,
	}
	if stapaContainsIDR(payload) {
		t.Fatalf("expected STAP-A payload without IDR to return false")
	}
}
