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

func TestMatchTrunkFromInvite_PrioritizesUserDomainPortOverDomainPort(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5090, Username: "other-user"},
			2: {ID: 2, Domain: "203.151.21.121", Port: 5090, Username: "00025"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		t.Fatalf("expected a matched trunk")
	}
	if result.Trunk.ID != 2 {
		t.Fatalf("expected trunk 2 to match by user-target, got %d", result.Trunk.ID)
	}
	if result.Rule != "ruri_user_domain_port" {
		t.Fatalf("expected ruri_user_domain_port rule, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_AmbiguousDomainPortFallbackReturnsNoTrunk(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5090, Username: "u1"},
			2: {ID: 2, Domain: "203.151.21.121", Port: 5090, Username: "u2"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected no trunk for ambiguous domain+port fallback, got trunk %d", result.Trunk.ID)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true for ambiguous fallback")
	}
	if result.Rule != "ruri_domain_port_fallback" {
		t.Fatalf("expected ruri_domain_port_fallback rule, got %s", result.Rule)
	}
	if len(result.CandidateIDs) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.CandidateIDs))
	}
}

func TestMatchTrunkFromInvite_UsernameOnlyOnlineFallbackMatchesSingleOwned(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "example.local", Port: 5060, Username: "00025"},
			2: {ID: 2, Domain: "example.local", Port: 5060, Username: "00026"},
		},
		ownedLeases: map[int64]bool{1: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		t.Fatalf("expected matched trunk via username_only_online")
	}
	if result.Trunk.ID != 1 {
		t.Fatalf("expected trunk 1, got %d", result.Trunk.ID)
	}
	if result.Rule != "username_only_online" {
		t.Fatalf("expected username_only_online rule, got %s", result.Rule)
	}
	if !result.Owned {
		t.Fatalf("expected owned=true for username_only_online match")
	}
	if result.SIPUser != "00025" {
		t.Fatalf("expected SIP user 00025, got %q", result.SIPUser)
	}
}

func TestMatchTrunkFromInvite_UsernameOnlyOnlineFallbackAmbiguous(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "a.local", Port: 5060, Username: "00025"},
			2: {ID: 2, Domain: "b.local", Port: 5060, Username: "00025"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected no trunk for ambiguous username-only fallback, got %d", result.Trunk.ID)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true for username-only fallback")
	}
	if result.Rule != "username_only_online" {
		t.Fatalf("expected username_only_online rule, got %s", result.Rule)
	}
	if len(result.CandidateIDs) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.CandidateIDs))
	}
}

func TestMatchTrunkFromInvite_UsernameOnlyOnlineFallbackIgnoresUnowned(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "example.local", Port: 5060, Username: "00025"},
		},
		ownedLeases: map[int64]bool{},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected no match when username is unowned, got %d", result.Trunk.ID)
	}
	if result.Rule != "no_match" {
		t.Fatalf("expected no_match rule, got %s", result.Rule)
	}
}
