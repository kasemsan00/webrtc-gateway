package sip

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosip "github.com/emiago/sipgo/sip"
)

func TestNormalizeRegistrarEndpoint(t *testing.T) {
	t.Parallel()

	ep, ok := normalizeRegistrarEndpoint("SipAgent.TTRS.or.th.", 0, "TCP")
	if !ok {
		t.Fatal("expected hostname to normalize")
	}
	if ep.Host != "sipagent.ttrs.or.th" || ep.Port != 5060 || ep.Transport != "tcp" {
		t.Fatalf("unexpected endpoint %+v", ep)
	}

	ep, ok = normalizeRegistrarEndpoint("203.150.245.41", 5060, "udp")
	if !ok || ep.Host != "203.150.245.41" || ep.Transport != "udp" {
		t.Fatalf("unexpected IPv4 endpoint %+v", ep)
	}

	ep, ok = normalizeRegistrarEndpoint("[2001:db8::1]", 5060, "tcp")
	if !ok || ep.Host != "2001:db8::1" {
		t.Fatalf("unexpected IPv6 endpoint %+v ok=%v", ep, ok)
	}

	ep, ok = normalizeRegistrarEndpoint("sipagent.ttrs.or.th", 5071, "tcp")
	if !ok || ep.Port != 5071 {
		t.Fatalf("expected explicit port to be preserved, got %+v", ep)
	}

	if _, ok := normalizeRegistrarEndpoint("   ", 5060, "tcp"); ok {
		t.Fatal("empty host must be rejected")
	}
}

func TestParseHostPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value       string
		defaultPort int
		wantHost    string
		wantPort    int
		wantOK      bool
	}{
		{value: "203.150.245.41:5060", defaultPort: 5060, wantHost: "203.150.245.41", wantPort: 5060, wantOK: true},
		{value: "[2001:db8::1]:5060", defaultPort: 5060, wantHost: "2001:db8::1", wantPort: 5060, wantOK: true},
		{value: "sipagent.ttrs.or.th", defaultPort: 5060, wantHost: "sipagent.ttrs.or.th", wantPort: 5060, wantOK: true},
		{value: "   ", defaultPort: 5060, wantOK: false},
	}

	for _, tc := range tests {
		host, port, ok := parseHostPort(tc.value, tc.defaultPort)
		if ok != tc.wantOK {
			t.Fatalf("parseHostPort(%q): ok=%v want=%v", tc.value, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		if host != tc.wantHost || port != tc.wantPort {
			t.Fatalf("parseHostPort(%q): got %q:%d want %q:%d", tc.value, host, port, tc.wantHost, tc.wantPort)
		}
	}
}

func TestEndpointEquivalenceExactAndRelaxed(t *testing.T) {
	t.Parallel()

	exactA, _ := normalizeRegistrarEndpoint("203.150.245.41", 5060, "tcp")
	exactB, _ := normalizeRegistrarEndpoint("203.150.245.41", 5070, "tcp")
	relaxedMatch, _ := normalizeRegistrarEndpoint("203.150.245.41", 45000, "tcp")

	if !endpointsEqualExact(exactA, exactA) {
		t.Fatal("expected exact self-match")
	}
	if endpointsEqualExact(exactA, exactB) {
		t.Fatal("different ports must not match exactly")
	}
	if !endpointsEqualRelaxed(exactA, relaxedMatch) {
		t.Fatal("expected relaxed host+transport match across port rewrite")
	}
	if endpointsEqualRelaxed(exactA, registrarEndpoint{Host: "203.150.245.41", Port: 5060, Transport: "udp"}) {
		t.Fatal("relaxed match must still require transport equality")
	}
}

func TestSIPResponseSourceEndpoint(t *testing.T) {
	t.Parallel()

	res := gosip.NewResponse(gosip.StatusOK, "OK")
	res.SetSource("203.150.245.41:45000")

	ep, ok := sipResponseSourceEndpoint(res, "tcp")
	if !ok {
		t.Fatal("expected response source endpoint")
	}
	if ep.Host != "203.150.245.41" || ep.Port != 45000 || ep.Transport != "tcp" {
		t.Fatalf("unexpected response source endpoint %+v", ep)
	}
}

func TestMergeRegistrarEndpointsDeduplicates(t *testing.T) {
	t.Parallel()

	a, _ := normalizeRegistrarEndpoint("203.150.245.41", 5060, "tcp")
	b, _ := normalizeRegistrarEndpoint("203.150.245.41", 5060, "TCP")
	c, _ := normalizeRegistrarEndpoint("sipagent.ttrs.or.th", 5060, "tcp")
	merged := mergeRegistrarEndpoints(nil, a, b, c)
	if len(merged) != 2 {
		t.Fatalf("expected 2 unique endpoints, got %#v", merged)
	}
}

func TestResolveRegistrarEndpointsIPLiteral(t *testing.T) {
	t.Parallel()

	endpoints, err := resolveRegistrarEndpoints("203.150.245.41", 5060, "tcp")
	if err != nil {
		t.Fatalf("resolve IP literal: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Host != "203.150.245.41" || endpoints[0].Port != 5060 {
		t.Fatalf("unexpected endpoints %#v", endpoints)
	}
}

func registrarIdentityFixture(trunk *Trunk, extraHosts ...string) *registrarIdentity {
	endpoints := make([]registrarEndpoint, 0, len(extraHosts)+1)
	if configured, ok := normalizeRegistrarEndpoint(trunk.Domain, trunk.Port, trunk.Transport); ok {
		endpoints = append(endpoints, configured)
	}
	for _, host := range extraHosts {
		if endpoint, ok := normalizeRegistrarEndpoint(host, trunk.Port, trunk.Transport); ok {
			endpoints = append(endpoints, endpoint)
		}
	}
	now := time.Now()
	return &registrarIdentity{
		TrunkID:   trunk.ID,
		Username:  strings.TrimSpace(trunk.Username),
		Hostname:  normalizeSIPHost(trunk.Domain),
		Port:      normalizeSIPPort(trunk.Port),
		Transport: normalizeSIPTransport(trunk.Transport),
		Endpoints: mergeRegistrarEndpoints(endpoints),
		SuccessAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}

type countingResolver struct {
	calls atomic.Int32
}

func (c *countingResolver) Resolve(domain string, port int, transport string) ([]registrarEndpoint, error) {
	c.calls.Add(1)
	ep, _ := normalizeRegistrarEndpoint("203.150.245.41", port, transport)
	configured, _ := normalizeRegistrarEndpoint(domain, port, transport)
	return mergeRegistrarEndpoints(nil, configured, ep), nil
}

func TestInviteMatchingDoesNotCallResolver(t *testing.T) {
	t.Parallel()

	resolver := &countingResolver{}
	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: trunk},
		ownedLeases: map[int64]bool{1: true},
		resolver:    resolver,
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")

	req := asteriskRewriteContactInvite("00025", "203.151.21.121", 5090, "203.150.245.41", 5060)
	req.SetSource("203.150.245.41:5060")
	tm.MatchTrunkFromInviteDetailed(req)
	if resolver.calls.Load() != 0 {
		t.Fatalf("INVITE matching must not resolve DNS, calls=%d", resolver.calls.Load())
	}

	tm.publishRegistrarIdentityFromRegister(trunk, 0, nil, 3600)
	if resolver.calls.Load() == 0 {
		t.Fatal("registration publication should resolve endpoints")
	}
}

func TestRegistrarIdentityGenerationGuards(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	identity := &registrarIdentity{Username: "00025", Hostname: "sipagent.ttrs.or.th", Port: 5060, Transport: "tcp"}
	if !tm.tryPublishRegistrarIdentity(7, 0, identity) {
		t.Fatal("expected first publish to succeed")
	}
	tm.invalidateRegistrarIdentity(7)
	if tm.tryPublishRegistrarIdentity(7, 0, identity) {
		t.Fatal("stale generation must not overwrite after invalidate")
	}
	if tm.registrarIdentities[7] != nil {
		t.Fatal("identity should be cleared after invalidate")
	}
	gen := tm.currentRegistrarGeneration(7)
	if !tm.tryPublishRegistrarIdentity(7, gen, identity) {
		t.Fatal("current generation should publish")
	}
}

func TestRegistrarIdentityConcurrentMatchAndInvalidate(t *testing.T) {
	trunk := &Trunk{ID: 1, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:             "203.151.21.121",
		localPort:            5090,
		trunks:               map[int64]*Trunk{1: trunk},
		ownedLeases:          map[int64]bool{1: true},
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	initialGeneration := tm.beginRegistrarOperation(1)
	if !tm.tryPublishRegistrarIdentity(1, initialGeneration, registrarIdentityFixture(trunk, "203.150.245.41")) {
		t.Fatal("expected initial identity publication")
	}

	req := asteriskRewriteContactInvite("00025", "203.151.21.121", 5090, "203.150.245.41", 5060)
	req.SetSource("203.150.245.41:5060")
	req.SetTransport("tcp")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tm.MatchTrunkFromInviteDetailed(req)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		tm.invalidateRegistrarIdentity(1)
		generation := tm.beginRegistrarOperation(1)
		_ = tm.tryPublishRegistrarIdentity(1, generation, registrarIdentityFixture(trunk, "203.150.245.41"))
	}()
	wg.Wait()
}

func TestRegistrarIdentityDNSReplacementKeepsCurrentSnapshot(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 9, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	firstGeneration := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, firstGeneration, registrarIdentityFixture(trunk, "203.150.245.40")) {
		t.Fatal("expected first publish")
	}

	secondGeneration := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, secondGeneration, registrarIdentityFixture(trunk, "203.150.245.41")) {
		t.Fatal("new successful generation should replace current snapshot")
	}
	got := tm.registrarIdentities[trunk.ID]
	if got == nil || got.Generation != secondGeneration || !got.matchesExact(registrarEndpoint{Host: "203.150.245.41", Port: 5060, Transport: "tcp"}) {
		t.Fatalf("expected DNS replacement snapshot, got %+v", got)
	}
	if got.matchesExact(registrarEndpoint{Host: "203.150.245.40", Port: 5060, Transport: "tcp"}) {
		t.Fatal("replacement snapshot must not retain superseded DNS address")
	}
}

func TestRegistrarOperationGenerationPreservesSuccessfulIdentityOnFailure(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 10, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	firstGeneration := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, firstGeneration, registrarIdentityFixture(trunk, "203.150.245.40")) {
		t.Fatal("expected initial successful identity")
	}

	failedRefreshGeneration := tm.beginRegistrarOperation(trunk.ID)
	if failedRefreshGeneration <= firstGeneration {
		t.Fatal("refresh operation must advance generation")
	}
	got := tm.registrarIdentities[trunk.ID]
	if got == nil || got.Generation != firstGeneration || !got.isCurrent(time.Now()) {
		t.Fatalf("failed refresh must retain current successful identity, got %+v", got)
	}

	nextGeneration := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, nextGeneration, registrarIdentityFixture(trunk, "203.150.245.41")) {
		t.Fatal("next successful refresh should publish")
	}
	if tm.tryPublishRegistrarIdentity(trunk.ID, failedRefreshGeneration, registrarIdentityFixture(trunk, "203.150.245.42")) {
		t.Fatal("late completion from failed/stale refresh generation must not publish")
	}
}

func TestRegistrarOperationGenerationRejectsLateRegisterAfterReconnect(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 11, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	oldConnectionGeneration := tm.beginRegistrarOperation(trunk.ID)
	tm.invalidateRegistrarIdentity(trunk.ID)
	newConnectionGeneration := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, newConnectionGeneration, registrarIdentityFixture(trunk, "203.150.245.41")) {
		t.Fatal("new connection should publish its identity")
	}
	if tm.tryPublishRegistrarIdentity(trunk.ID, oldConnectionGeneration, registrarIdentityFixture(trunk, "203.150.245.40")) {
		t.Fatal("old connection completion must not overwrite reconnect identity")
	}
}

func TestRegistrarOperationFailureDoesNotPublishInitialIdentity(t *testing.T) {
	t.Parallel()

	tm := &TrunkManager{
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	_ = tm.beginRegistrarOperation(12)
	if tm.registrarIdentities[12] != nil {
		t.Fatal("failed initial REGISTER must not publish an identity")
	}
}

func TestRegistrarIdentityPruneInvalidatesLatePublication(t *testing.T) {
	t.Parallel()

	trunk := &Trunk{ID: 14, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		trunks:              map[int64]*Trunk{},
		registrarIdentities: map[int64]*registrarIdentity{trunk.ID: registrarIdentityFixture(trunk)},
	}
	tm.mu.Lock()
	tm.pruneRegistrarIdentitiesLocked()
	tm.mu.Unlock()

	if tm.registrarIdentities[trunk.ID] != nil {
		t.Fatal("prune must remove identity for unloaded trunk")
	}
	if tm.registrarGenerations[trunk.ID] != 1 {
		t.Fatalf("prune must advance generation, got %d", tm.registrarGenerations[trunk.ID])
	}
	if tm.tryPublishRegistrarIdentity(trunk.ID, 0, registrarIdentityFixture(trunk)) {
		t.Fatal("late publication from pre-prune generation must fail")
	}
}

func TestRegistrarIdentityConcurrentOperations(t *testing.T) {
	trunk := &Trunk{ID: 13, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:             "203.151.21.121",
		localPort:            5090,
		trunks:               map[int64]*Trunk{trunk.ID: trunk},
		ownedLeases:          map[int64]bool{trunk.ID: true},
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	req := asteriskRewriteContactInvite("00025", "203.151.21.121", 5090, "203.150.245.41", 5060)
	req.SetSource("203.150.245.41:5060")
	req.SetTransport("tcp")

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			generation := tm.beginRegistrarOperation(trunk.ID)
			if index%5 == 0 {
				tm.invalidateRegistrarIdentity(trunk.ID)
				return
			}
			_ = tm.tryPublishRegistrarIdentity(trunk.ID, generation, registrarIdentityFixture(trunk, "203.150.245.41"))
			_ = tm.MatchTrunkFromInviteDetailed(req)
		}(i)
	}
	wg.Wait()

	generation := tm.beginRegistrarOperation(trunk.ID)
	if !tm.tryPublishRegistrarIdentity(trunk.ID, generation, registrarIdentityFixture(trunk, "203.150.245.41")) {
		t.Fatal("latest operation should publish after concurrent lifecycle activity")
	}
}

func TestCacheUpsertedTrunkInvalidatesIdentityWithMetadataReplacement(t *testing.T) {
	t.Parallel()

	previous := &Trunk{ID: 20, PublicID: "public-20", Domain: "old.example", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	updated := &Trunk{ID: 20, PublicID: "public-20", Domain: "new.example", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		trunks:               map[int64]*Trunk{previous.ID: previous},
		trunkByPublic:        map[string]int64{previous.PublicID: previous.ID},
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	generation := tm.beginRegistrarOperation(previous.ID)
	if !tm.tryPublishRegistrarIdentity(previous.ID, generation, registrarIdentityFixture(previous, "203.0.113.20")) {
		t.Fatal("expected initial registrar identity")
	}

	tm.cacheUpsertedTrunk(updated)

	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.trunks[updated.ID] != updated {
		t.Fatal("expected updated trunk metadata in cache")
	}
	if tm.registrarIdentities[updated.ID] != nil {
		t.Fatal("metadata replacement must invalidate registrar identity in the same critical section")
	}
	if tm.registrarGenerations[updated.ID] <= generation {
		t.Fatalf("expected generation to advance, got %d", tm.registrarGenerations[updated.ID])
	}
}

func TestReplaceLoadedTrunksInvalidatesChangedSameIDIdentity(t *testing.T) {
	t.Parallel()

	changedBefore := &Trunk{ID: 21, PublicID: "public-21", Domain: "old.example", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	changedAfter := &Trunk{ID: 21, PublicID: "public-21", Domain: "new.example", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	unchangedBefore := &Trunk{ID: 22, PublicID: "public-22", Domain: "same.example", Port: 5060, Username: "00026", Transport: "tcp", Enabled: true}
	unchangedAfter := *unchangedBefore
	tm := &TrunkManager{
		trunks: map[int64]*Trunk{
			changedBefore.ID:   changedBefore,
			unchangedBefore.ID: unchangedBefore,
		},
		trunkByPublic: map[string]int64{
			changedBefore.PublicID:   changedBefore.ID,
			unchangedBefore.PublicID: unchangedBefore.ID,
		},
		registrarIdentities:  make(map[int64]*registrarIdentity),
		registrarGenerations: make(map[int64]uint64),
	}
	changedGeneration := tm.beginRegistrarOperation(changedBefore.ID)
	unchangedGeneration := tm.beginRegistrarOperation(unchangedBefore.ID)
	if !tm.tryPublishRegistrarIdentity(changedBefore.ID, changedGeneration, registrarIdentityFixture(changedBefore, "203.0.113.21")) {
		t.Fatal("expected changed identity publication")
	}
	if !tm.tryPublishRegistrarIdentity(unchangedBefore.ID, unchangedGeneration, registrarIdentityFixture(unchangedBefore, "203.0.113.22")) {
		t.Fatal("expected unchanged identity publication")
	}

	tm.replaceLoadedTrunks(
		map[int64]*Trunk{changedAfter.ID: changedAfter, unchangedAfter.ID: &unchangedAfter},
		map[string]int64{changedAfter.PublicID: changedAfter.ID, unchangedAfter.PublicID: unchangedAfter.ID},
	)

	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.registrarIdentities[changedAfter.ID] != nil {
		t.Fatal("load refresh must invalidate identity when registrar fields change for the same trunk ID")
	}
	if tm.registrarGenerations[changedAfter.ID] <= changedGeneration {
		t.Fatalf("expected changed generation to advance, got %d", tm.registrarGenerations[changedAfter.ID])
	}
	if tm.registrarIdentities[unchangedAfter.ID] == nil {
		t.Fatal("load refresh must preserve identity when registrar fields are unchanged")
	}
	if tm.registrarGenerations[unchangedAfter.ID] != unchangedGeneration {
		t.Fatalf("unchanged generation advanced unexpectedly: got %d want %d", tm.registrarGenerations[unchangedAfter.ID], unchangedGeneration)
	}
}

func BenchmarkMatchTrunkFromInviteDetailed(b *testing.B) {
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      make(map[int64]*Trunk, 200),
		ownedLeases: make(map[int64]bool, 200),
	}
	for i := int64(1); i <= 200; i++ {
		trunk := &Trunk{ID: i, Domain: "sipagent.ttrs.or.th", Port: 5060, Username: strings.TrimSpace("user"), Transport: "tcp"}
		if i == 25 {
			trunk.Username = "00025"
		} else {
			trunk.Username = "u" + time.Unix(i, 0).Format("05")
		}
		tm.trunks[i] = trunk
		tm.ownedLeases[i] = true
		if trunk.Username == "00025" {
			setRegistrarIdentity(tm, trunk, "203.150.245.41")
		} else {
			setRegistrarIdentity(tm, trunk)
		}
	}
	req := asteriskRewriteContactInvite("00025", "203.151.21.121", 5090, "203.150.245.41", 5060)
	req.SetSource("203.150.245.41:5060")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tm.MatchTrunkFromInviteDetailed(req)
	}
}
