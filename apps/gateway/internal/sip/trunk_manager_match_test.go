package sip

import (
	"strings"
	"testing"
	"time"

	gosip "github.com/emiago/sipgo/sip"
)

func setRegistrarIdentity(tm *TrunkManager, trunk *Trunk, extraHosts ...string) {
	trunk.Enabled = true
	if tm.registrarIdentities == nil {
		tm.registrarIdentities = make(map[int64]*registrarIdentity)
	}
	endpoints := make([]registrarEndpoint, 0, 1+len(extraHosts))
	if ep, ok := normalizeRegistrarEndpoint(trunk.Domain, trunk.Port, trunk.Transport); ok {
		endpoints = append(endpoints, ep)
	}
	for _, host := range extraHosts {
		if ep, ok := normalizeRegistrarEndpoint(host, trunk.Port, trunk.Transport); ok {
			endpoints = append(endpoints, ep)
		}
	}
	tm.registrarIdentities[trunk.ID] = &registrarIdentity{
		TrunkID:   trunk.ID,
		Username:  strings.TrimSpace(trunk.Username),
		Hostname:  normalizeSIPHost(trunk.Domain),
		Port:      normalizeSIPPort(trunk.Port),
		Transport: normalizeSIPTransport(trunk.Transport),
		Endpoints: mergeRegistrarEndpoints(endpoints),
		SuccessAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestMatchTrunkFromInvite_ByRequestURIDomainPort(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490", Enabled: true},
		},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])

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
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490", Enabled: true},
		},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])

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
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490", Enabled: true},
		},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])

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

func TestMatchTrunkFromInvite_RejectsUnownedDirectMatch(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5060, Username: "1100200363490", Enabled: true},
		},
		ownedLeases: map[int64]bool{},
	}
	setRegistrarIdentity(tm, tm.trunks[1])

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "1100200363490", Host: "203.151.21.121", Port: 5060})
	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected unowned direct match to be rejected, got trunk %d", result.Trunk.ID)
	}
}

func TestMatchTrunkFromInvite_PrioritizesUserDomainPortOverDomainPort(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5090, Username: "other-user", Enabled: true},
			2: {ID: 2, Domain: "203.151.21.121", Port: 5090, Username: "00025", Enabled: true},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

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

func TestMatchTrunkFromInvite_DomainPortFallbackRequiresRequestedUsername(t *testing.T) {
	t.Parallel()

	otherUser := &Trunk{ID: 1, Domain: "203.151.21.121", Port: 5090, Username: "other-user", Enabled: true}
	requestedUser := &Trunk{ID: 2, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: otherUser, 2: requestedUser},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, otherUser, "203.151.21.121")
	setRegistrarIdentity(tm, requestedUser, "203.150.245.41")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "203.151.21.121",
		Port: 5090,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil && result.Trunk.ID == otherUser.ID {
		t.Fatalf("domain/port fallback must not select another username, got trunk %d rule=%s", result.Trunk.ID, result.Rule)
	}
}

func TestMatchTrunkFromInvite_AmbiguousDomainPortFallbackReturnsNoTrunk(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "203.151.21.121", Port: 5090, Username: "u1", Enabled: true},
			2: {ID: 2, Domain: "203.151.21.121", Port: 5090, Username: "u2", Enabled: true},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

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
			1: {ID: 1, Domain: "example.local", Port: 5060, Username: "00025", Transport: "tcp"},
			2: {ID: 2, Domain: "example.local", Port: 5060, Username: "00026", Transport: "tcp"},
		},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])

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
			1: {ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"},
			2: {ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

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

func asteriskRewriteContactInvite(user, contactHost string, contactPort int, viaHost string, viaPort int) *gosip.Request {
	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: user,
		Host: contactHost,
		Port: contactPort,
	})
	req.AppendHeader(&gosip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            viaHost,
		Port:            viaPort,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: user, Host: contactHost},
	})
	req.AppendHeader(&gosip.ContactHeader{
		Address: gosip.Uri{User: "asterisk", Host: viaHost, Port: viaPort},
	})
	return req
}

func TestMatchTrunkFromInvite_AsteriskRewriteContactPicksOriginPort(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {
				ID:       1,
				Name:     "sipclient-agent-00025@sipagent.ttrs.or.th:5060",
				Domain:   "sipagent.ttrs.or.th",
				Port:     5060,
				Username: "00025",
			},
			2: {
				ID:       2,
				Name:     "sipclient-agent-00025@203.151.21.121:5160",
				Domain:   "203.151.21.121",
				Port:     5160,
				Username: "00025",
			},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

	req := asteriskRewriteContactInvite("00025", "192.168.80.3", 51782, "203.151.21.121", 5160)
	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		t.Fatalf("expected Asterisk rewrite-contact INVITE to match a trunk, ambiguous=%v rule=%s candidates=%v", result.Ambiguous, result.Rule, result.CandidateIDs)
	}
	if result.Trunk.ID != 2 {
		t.Fatalf("expected trunk 2 (Asterisk 22 :5160), got %d", result.Trunk.ID)
	}
	if result.Rule != "username_origin_via" && result.Rule != "username_origin_contact" && result.Rule != "username_origin_source" && result.Rule != "username_origin_source_relaxed" && result.Rule != "username_origin_via_relaxed" && result.Rule != "username_origin_contact_relaxed" {
		t.Fatalf("expected origin via/contact/source rule, got %s", result.Rule)
	}
	if !result.Owned {
		t.Fatalf("expected owned=true")
	}
	if result.Ambiguous {
		t.Fatalf("expected ambiguous=false after origin disambiguation")
	}
	if len(result.OwnedCandidates) != 2 {
		t.Fatalf("expected both 00025 trunks in ownedCandidates, got %v", result.OwnedCandidates)
	}
}

func TestMatchTrunkFromInvite_OriginHostDisambiguatesDuplicateUsername(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"},
			2: {ID: 2, Domain: "b.local", Port: 5070, Username: "00025", Transport: "tcp"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

	req := asteriskRewriteContactInvite("00025", "10.0.0.8", 51782, "b.local", 0)
	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		t.Fatalf("expected origin host to select a trunk, rule=%s", result.Rule)
	}
	if result.Trunk.ID != 2 {
		t.Fatalf("expected trunk 2, got %d", result.Trunk.ID)
	}
	if result.Rule != "username_origin_via" && result.Rule != "username_origin_via_relaxed" && result.Rule != "username_origin_contact" && result.Rule != "username_origin_contact_relaxed" && result.Rule != "username_origin_source" && result.Rule != "username_origin_source_relaxed" {
		t.Fatalf("expected origin host rule, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_RecentRegistrationDisambiguatesDuplicateUsername(t *testing.T) {
	t.Parallel()

	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp", LastRegisteredAt: &older},
			2: {ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp", LastRegisteredAt: &newer},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "192.168.80.3",
		Port: 51782,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "192.168.80.3"},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected no trunk from recency tie-break, got %d rule=%s", result.Trunk.ID, result.Rule)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true instead of recency selection")
	}
}

func TestMatchTrunkFromInvite_AgentNameDoesNotDisambiguateDuplicateUsername(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			1: {ID: 1, Name: "legacy-00025", Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"},
			2: {ID: 2, Name: "sipclient-agent-00025@b.local:5060", Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp"},
		},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, tm.trunks[1])
	setRegistrarIdentity(tm, tm.trunks[2])

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{
		User: "00025",
		Host: "192.168.80.3",
		Port: 51782,
	})
	req.AppendHeader(&gosip.ToHeader{
		Address: gosip.Uri{User: "00025", Host: "192.168.80.3"},
	})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected no trunk from agent-name tie-break, got %d rule=%s", result.Trunk.ID, result.Rule)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true instead of agent-name selection")
	}
}

func TestMatchTrunkFromInvite_LocalContactOriginSelectsHostnameTrunk(t *testing.T) {
	t.Parallel()

	desired := &Trunk{
		ID:        725,
		Name:      "sipclient-agent-00025@sipagent.ttrs.or.th:5060",
		Domain:    "sipagent.ttrs.or.th",
		Port:      5060,
		Username:  "00025",
		Transport: "tcp",
		Enabled:   true,
	}
	staleGateway := &Trunk{
		ID:        741,
		Name:      "sipclient-agent-00025@203.151.21.121:5090",
		Domain:    "203.151.21.121",
		Port:      5090,
		Username:  "00025",
		Transport: "tcp",
		Enabled:   true,
	}
	unowned := &Trunk{
		ID:        170,
		Name:      "legacy-00025",
		Domain:    "sipagent.ttrs.or.th",
		Port:      5060,
		Username:  "00025",
		Transport: "tcp",
		Enabled:   true,
	}
	disabled := &Trunk{
		ID:        726,
		Name:      "disabled-00025",
		Domain:    "sipagent.ttrs.or.th",
		Port:      5060,
		Username:  "00025",
		Transport: "tcp",
		Enabled:   false,
	}
	otherPBX := &Trunk{
		ID:        800,
		Name:      "sipclient-agent-00025@other.pbx:5060",
		Domain:    "other.pbx",
		Port:      5060,
		Username:  "00025",
		Transport: "tcp",
		Enabled:   true,
	}

	tm := &TrunkManager{
		publicIP:  "203.151.21.121",
		localPort: 5090,
		trunks: map[int64]*Trunk{
			desired.ID:      desired,
			staleGateway.ID: staleGateway,
			unowned.ID:      unowned,
			otherPBX.ID:     otherPBX,
		},
		ownedLeases: map[int64]bool{desired.ID: true, otherPBX.ID: true},
	}
	_ = disabled
	setRegistrarIdentity(tm, desired, "203.150.245.41")
	setRegistrarIdentity(tm, otherPBX, "198.51.100.10")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            "203.150.245.41",
		Port:            5060,
	})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(&gosip.ContactHeader{Address: gosip.Uri{User: "0000178810139", Host: "203.150.245.41", Port: 5060}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		t.Fatalf("expected hostname trunk match, ambiguous=%v rule=%s reason=%s candidates=%v eligible=%v", result.Ambiguous, result.Rule, result.Reason, result.CandidateIDs, result.EligibleIDs)
	}
	if result.Trunk.ID != desired.ID {
		t.Fatalf("expected trunk %d, got %d", desired.ID, result.Trunk.ID)
	}
	if result.Rule != "username_origin_source" && result.Rule != "username_origin_via" {
		t.Fatalf("expected source/via origin rule, got %s", result.Rule)
	}
	if !result.LocalRequestURI || !result.LocalToURI {
		t.Fatalf("expected local Gateway Contact classification, localRURI=%v localTo=%v", result.LocalRequestURI, result.LocalToURI)
	}
	if result.Rule == "ruri_user_domain" || result.Rule == "ruri_domain_port_fallback" {
		t.Fatalf("self-targeted Request-URI must not use registrar-domain rules, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_SourceTakesPrecedenceOverConflictingVia(t *testing.T) {
	t.Parallel()

	a := &Trunk{ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"}
	b := &Trunk{ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: a, 2: b},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, a, "203.150.245.41")
	setRegistrarIdentity(tm, b, "198.51.100.10")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "198.51.100.10", Port: 5060})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil || result.Trunk.ID != 1 {
		t.Fatalf("expected source-selected trunk 1, got %+v", result)
	}
	if result.Rule != "username_origin_source" {
		t.Fatalf("expected username_origin_source, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_NATSourcePortRewriteUsesRelaxedSource(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: trunk},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:45000")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil || result.Trunk.ID != 1 {
		t.Fatalf("expected relaxed source match, got %+v", result)
	}
	if result.Rule != "username_origin_source_relaxed" && result.Rule != "username_only_online" {
		t.Fatalf("expected relaxed source or unique eligible rule, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_SharedProxyEndpointRemainsAmbiguous(t *testing.T) {
	t.Parallel()

	a := &Trunk{ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"}
	b := &Trunk{ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: a, 2: b},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, a, "203.0.113.9")
	setRegistrarIdentity(tm, b, "203.0.113.9")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.0.113.9:5060")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expected shared proxy endpoint to stay ambiguous, got trunk %d", result.Trunk.ID)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true")
	}
}

func TestMatchTrunkFromInvite_MissingSourceUsesVia(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp"}
	other := &Trunk{ID: 2, Domain: "other.pbx", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: trunk, 2: other},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")
	setRegistrarIdentity(tm, other, "198.51.100.10")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.150.245.41", Port: 5060})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil || result.Trunk.ID != 1 {
		t.Fatalf("expected Via origin to select trunk 1 when source is missing, got %+v", result)
	}
	if result.Rule != "username_origin_via" && result.Rule != "username_origin_via_relaxed" && result.Rule != "username_origin_source" && result.Rule != "username_origin_source_relaxed" {
		t.Fatalf("expected via/source origin rule when SetSource is omitted, got %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_UnresolvedHostnameDoesNotMatchIPOrigin(t *testing.T) {
	t.Parallel()

	hostnameOnly := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp"}
	other := &Trunk{ID: 2, Domain: "other.pbx", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: hostnameOnly, 2: other},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, hostnameOnly)
	setRegistrarIdentity(tm, other)

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("unresolved hostname identity must not match INVITE IP origin, got trunk %d rule=%s", result.Trunk.ID, result.Rule)
	}
	if !result.Ambiguous {
		t.Fatalf("expected indistinguishable eligible candidates, got %+v", result)
	}
}

func TestMatchTrunkFromInvite_MultipleHostnamesSameAddressRemainAmbiguous(t *testing.T) {
	t.Parallel()

	a := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	b := &Trunk{ID: 2, Domain: "sipagent-alias.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: a, 2: b},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, a, "203.150.245.41")
	setRegistrarIdentity(tm, b, "203.150.245.41")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.SetTransport("tcp")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("shared resolved address must stay ambiguous, got trunk %d", result.Trunk.ID)
	}
	if !result.Ambiguous {
		t.Fatalf("expected ambiguous=true")
	}
}

func TestMatchTrunkFromInvite_DisabledIdentityIsNotEligible(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: false}
	identity := registrarIdentityFixture(trunk, "203.150.245.41")
	tm := &TrunkManager{
		publicIP:             "203.151.21.121",
		localPort:            5090,
		trunks:               map[int64]*Trunk{1: trunk},
		ownedLeases:          map[int64]bool{1: true},
		registrarIdentities:  map[int64]*registrarIdentity{1: identity},
		registrarGenerations: map[int64]uint64{1: identity.Generation},
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.SetTransport("tcp")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil || len(result.EligibleIDs) != 0 {
		t.Fatalf("disabled trunk must not be eligible, got trunk=%v eligible=%v", result.Trunk, result.EligibleIDs)
	}
}

func TestMatchTrunkFromInvite_ExpiredIdentityIsNotEligible(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: trunk},
		ownedLeases: map[int64]bool{1: true},
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")
	tm.registrarIdentities[1].ExpiresAt = time.Now().Add(-time.Minute)

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.SetTransport("tcp")
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("expired identity must not route trunk %d with rule %s", result.Trunk.ID, result.Rule)
	}
	if len(result.EligibleIDs) != 0 {
		t.Fatalf("expired identity must not be eligible, got %v", result.EligibleIDs)
	}
}

func TestMatchTrunkFromInvite_DirectRouteRequiresCurrentRegistration(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: trunk},
		ownedLeases: map[int64]bool{1: true},
	}
	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "sipagent.ttrs.or.th", Port: 5060})
	req.SetTransport("tcp")

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk != nil {
		t.Fatalf("unregistered direct-route trunk must not match, got rule %s", result.Rule)
	}
}

func TestMatchTrunkFromInvite_SourceRelaxedPrecedesConflictingViaExact(t *testing.T) {
	t.Parallel()

	a := &Trunk{ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	b := &Trunk{ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: a, 2: b},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, a, "203.150.245.41")
	setRegistrarIdentity(tm, b, "198.51.100.10")

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:45000")
	req.SetTransport("tcp")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "198.51.100.10", Port: 5060})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})

	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil || result.Trunk.ID != 1 {
		t.Fatalf("expected source-relaxed trunk 1, got trunk=%v rule=%s", result.Trunk, result.Rule)
	}
	if result.Rule != "username_origin_source_relaxed" {
		t.Fatalf("expected source-relaxed rule, got %s", result.Rule)
	}
}
