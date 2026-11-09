package sip

import "testing"

func TestEndReasonForSIPStatus(t *testing.T) {
	cases := map[int]string{
		486: "busy", 600: "busy", 603: "declined", 404: "not_found", 604: "not_found", 484: "invalid_number",
		403: "forbidden", 480: "unavailable", 408: "no_answer", 488: "incompatible", 606: "incompatible",
		500: "service_unavailable", 503: "service_unavailable", 410: "failed", 487: "failed",
	}
	for status, want := range cases {
		if got := endReasonForSIPStatus(status); got != want {
			t.Errorf("%d: got %q, want %q", status, got, want)
		}
	}
}
