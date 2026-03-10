package sip

import (
	"testing"

	gosip "github.com/emiago/sipgo/sip"
)

func TestMatchTrunkFromInvite_ByRequestURIDomainPort(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490"},
		},
		ownedLeases: map[int64]bool{1: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5060})
	trunk, owned := tm.MatchTrunkFromInvite(req)
	if trunk == nil || trunk.ID != 1 {
		t.Fatalf("expected trunk 1 to match")
	}
	if !owned {
		t.Fatalf("expected trunk to be owned")
	}
}

func TestMatchTrunkFromInvite_ByToHeaderWhenRequestURIPortDiffers(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490"},
		},
		ownedLeases: map[int64]bool{1: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5090})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5060}})

	trunk, owned := tm.MatchTrunkFromInvite(req)
	if trunk == nil || trunk.ID != 1 {
		t.Fatalf("expected trunk 1 to match via To header")
	}
	if !owned {
		t.Fatalf("expected trunk to be owned")
	}
}

func TestMatchTrunkFromInvite_ByUserDomainFallback(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490"},
		},
		ownedLeases: map[int64]bool{1: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5090})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5090}})

	trunk, owned := tm.MatchTrunkFromInvite(req)
	if trunk == nil || trunk.ID != 1 {
		t.Fatalf("expected trunk 1 to match via user+domain fallback")
	}
	if !owned {
		t.Fatalf("expected trunk to be owned")
	}
}

func TestMatchTrunkFromInvite_ReturnsUnownedLeaseState(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490"},
		},
		ownedLeases: map[int64]bool{},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5060})
	trunk, owned := tm.MatchTrunkFromInvite(req)
	if trunk == nil || trunk.ID != 1 {
		t.Fatalf("expected trunk 1 to match")
	}
	if owned {
		t.Fatalf("expected trunk lease to be reported as unowned")
	}
}
