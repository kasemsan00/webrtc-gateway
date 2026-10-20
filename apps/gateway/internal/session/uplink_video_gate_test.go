package session

import "testing"

func TestHoldUplinkVideoUntilIDR(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		open     bool
		idr      bool
		wantHold bool
	}{
		{name: "p-frame before first idr", open: false, idr: false, wantHold: true},
		{name: "idr opens uplink", open: false, idr: true, wantHold: false},
		{name: "p-frame after idr", open: true, idr: false, wantHold: false},
		{name: "later idr", open: true, idr: true, wantHold: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := holdUplinkVideoUntilIDR(tc.open, tc.idr); got != tc.wantHold {
				t.Fatalf("holdUplinkVideoUntilIDR(%v, %v) = %v, want %v", tc.open, tc.idr, got, tc.wantHold)
			}
		})
	}
}
