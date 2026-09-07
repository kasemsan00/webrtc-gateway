package sip

import (
	"sort"
	"strconv"
	"strings"
	"time"

	gosip "github.com/emiago/sipgo/sip"
)

const (
	originProvenanceSource  = "source"
	originProvenanceVia     = "via"
	originProvenanceContact = "contact"
)

type inviteOriginEvidence struct {
	endpoint   registrarEndpoint
	provenance string
}

func collectCandidateIDs(trunks []*Trunk) []int64 {
	ids := make([]int64, 0, len(trunks))
	for _, t := range trunks {
		if t != nil {
			ids = append(ids, t.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func filterTrunks(trunks []*Trunk, predicate func(*Trunk) bool) []*Trunk {
	matches := make([]*Trunk, 0)
	for _, t := range trunks {
		if predicate(t) {
			matches = append(matches, t)
		}
	}
	return matches
}

func normalizeInviteURI(uri gosip.Uri) (host, user string, port int) {
	host = normalizeSIPHost(uri.Host)
	user = strings.TrimSpace(uri.User)
	port = normalizeSIPPort(uri.Port)
	return host, user, port
}

func selectSingleMatch(matches []*Trunk, rule string) TrunkInviteMatchResult {
	result := TrunkInviteMatchResult{
		Rule:         rule,
		CandidateIDs: collectCandidateIDs(matches),
	}
	if len(matches) == 1 {
		result.Trunk = matches[0]
		return result
	}
	if len(matches) > 1 {
		result.Ambiguous = true
		result.Reason = "multiple_candidates"
	}
	return result
}

func cloneTrunk(trunk *Trunk) *Trunk {
	if trunk == nil {
		return nil
	}
	copied := *trunk
	return &copied
}

func isLocalGatewayContact(publicIP string, localPort int, host string, port int) bool {
	publicIP = normalizeSIPHost(publicIP)
	if publicIP == "" || host == "" {
		return false
	}
	return host == publicIP && normalizeSIPPort(port) == normalizeSIPPort(localPort)
}

func extractInviteOrigins(req *gosip.Request) []inviteOriginEvidence {
	if req == nil {
		return nil
	}
	origins := make([]inviteOriginEvidence, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(host string, port int, transport, provenance string) {
		ep, ok := normalizeRegistrarEndpoint(host, port, transport)
		if !ok {
			return
		}
		key := provenance + "|" + registrarEndpointKey(ep)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		origins = append(origins, inviteOriginEvidence{endpoint: ep, provenance: provenance})
	}

	viaTransport := "tcp"
	via := req.Via()
	if via != nil && strings.TrimSpace(via.Transport) != "" {
		viaTransport = via.Transport
	}
	sourceTransport := strings.TrimSpace(req.Transport())
	if sourceTransport == "" {
		sourceTransport = viaTransport
	}

	if host, port, ok := parseHostPort(strings.TrimSpace(req.Source()), 5060); ok {
		add(host, port, sourceTransport, originProvenanceSource)
	}
	if via != nil {
		add(via.Host, via.Port, via.Transport, originProvenanceVia)
	}
	if contact := req.Contact(); contact != nil {
		host, _, port := normalizeInviteURI(contact.Address)
		add(host, port, viaTransport, originProvenanceContact)
	}
	return origins
}

func summarizeOrigins(origins []inviteOriginEvidence) []string {
	out := make([]string, 0, len(origins))
	for _, origin := range origins {
		out = append(out, origin.provenance+":"+origin.endpoint.Transport+":"+origin.endpoint.Host+":"+strconv.Itoa(origin.endpoint.Port))
	}
	return out
}

type inviteMatchState struct {
	publicIP   string
	localPort  int
	trunks     []*Trunk
	owned      map[int64]bool
	identities map[int64]*registrarIdentity
}

func (tm *TrunkManager) snapshotInviteMatchState() inviteMatchState {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	state := inviteMatchState{
		publicIP:   tm.publicIP,
		localPort:  tm.localPort,
		trunks:     make([]*Trunk, 0, len(tm.trunks)),
		owned:      make(map[int64]bool, len(tm.ownedLeases)),
		identities: make(map[int64]*registrarIdentity, len(tm.registrarIdentities)),
	}
	for id, owned := range tm.ownedLeases {
		if owned {
			state.owned[id] = true
		}
	}
	for _, trunk := range tm.trunks {
		state.trunks = append(state.trunks, cloneTrunk(trunk))
	}
	sort.Slice(state.trunks, func(i, j int) bool { return state.trunks[i].ID < state.trunks[j].ID })
	for id, identity := range tm.registrarIdentities {
		state.identities[id] = identity.clone()
	}
	return state
}

func (state inviteMatchState) identityFor(id int64) *registrarIdentity {
	return state.identities[id]
}

func (state inviteMatchState) isOwned(id int64) bool {
	return state.owned[id]
}

func finalizeSelected(result TrunkInviteMatchResult, state inviteMatchState, sipUser string, ownedIDs, eligibleIDs []int64, localRURI, localTo bool, origins []inviteOriginEvidence) TrunkInviteMatchResult {
	result.SIPUser = sipUser
	result.OwnedCandidates = ownedIDs
	result.EligibleIDs = eligibleIDs
	result.LocalRequestURI = localRURI
	result.LocalToURI = localTo
	result.Origins = summarizeOrigins(origins)
	if result.Trunk != nil {
		result.Owned = state.isOwned(result.Trunk.ID)
		if identity := state.identityFor(result.Trunk.ID); identity != nil {
			result.RegistrarEvidence = identity.Hostname + ":" + strconv.Itoa(identity.Port)
		}
	}
	return result
}

func (state inviteMatchState) byUserDomainPort(host, user string, port int) []*Trunk {
	if host == "" || user == "" {
		return nil
	}
	return filterTrunks(state.trunks, func(trunk *Trunk) bool {
		return normalizeSIPHost(trunk.Domain) == host &&
			normalizeSIPPort(trunk.Port) == normalizeSIPPort(port) &&
			strings.TrimSpace(trunk.Username) == user
	})
}

func (state inviteMatchState) byUserDomain(host, user string) []*Trunk {
	if host == "" || user == "" {
		return nil
	}
	return filterTrunks(state.trunks, func(trunk *Trunk) bool {
		return normalizeSIPHost(trunk.Domain) == host && strings.TrimSpace(trunk.Username) == user
	})
}

func (state inviteMatchState) byDomainPort(host string, port int) []*Trunk {
	if host == "" {
		return nil
	}
	return filterTrunks(state.trunks, func(trunk *Trunk) bool {
		return normalizeSIPHost(trunk.Domain) == host && normalizeSIPPort(trunk.Port) == normalizeSIPPort(port)
	})
}

func (state inviteMatchState) isEligible(trunk *Trunk, now time.Time) bool {
	if trunk == nil || !trunk.Enabled || !state.isOwned(trunk.ID) {
		return false
	}
	return state.identityFor(trunk.ID).isCurrent(now)
}

func (state inviteMatchState) filterEligible(trunks []*Trunk, now time.Time) []*Trunk {
	return filterTrunks(trunks, func(trunk *Trunk) bool {
		return state.isEligible(trunk, now)
	})
}

func (state inviteMatchState) eligibleByUsername(user string, now time.Time) []*Trunk {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil
	}
	return filterTrunks(state.trunks, func(trunk *Trunk) bool {
		return state.isEligible(trunk, now) && strings.TrimSpace(trunk.Username) == user
	})
}

func (state inviteMatchState) eligibleByUsernameAndDomainPort(user, host string, port int, now time.Time) []*Trunk {
	if host == "" {
		return nil
	}
	candidates := state.filterEligible(state.byDomainPort(host, port), now)
	user = strings.TrimSpace(user)
	if user == "" {
		return candidates
	}
	return filterTrunks(candidates, func(trunk *Trunk) bool {
		return strings.TrimSpace(trunk.Username) == user
	})
}

func (state inviteMatchState) ownedByUsername(user string) []*Trunk {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil
	}
	return filterTrunks(state.trunks, func(trunk *Trunk) bool {
		return trunk != nil && state.isOwned(trunk.ID) && strings.TrimSpace(trunk.Username) == user
	})
}

func applyOwnershipSafety(result TrunkInviteMatchResult, state inviteMatchState) TrunkInviteMatchResult {
	if result.Trunk == nil || result.Ambiguous {
		return result
	}
	result.Owned = state.isOwned(result.Trunk.ID)
	return result
}

func selectByOrigin(candidates []*Trunk, state inviteMatchState, origins []inviteOriginEvidence) TrunkInviteMatchResult {
	if len(candidates) == 0 {
		return TrunkInviteMatchResult{Rule: "no_match", Reason: "no_eligible_candidates"}
	}
	if len(candidates) == 1 {
		return selectSingleMatch(candidates, "username_only_online")
	}
	if len(origins) == 0 {
		result := selectSingleMatch(candidates, "username_only_online")
		if result.Ambiguous {
			result.Reason = "missing_origin_multiple_eligible"
		}
		return result
	}

	filterExact := func(pool []*Trunk, origin registrarEndpoint) []*Trunk {
		return filterTrunks(pool, func(trunk *Trunk) bool {
			identity := state.identityFor(trunk.ID)
			return identity != nil && identity.matchesExact(origin)
		})
	}
	filterRelaxed := func(pool []*Trunk, origin registrarEndpoint) []*Trunk {
		return filterTrunks(pool, func(trunk *Trunk) bool {
			identity := state.identityFor(trunk.ID)
			return identity != nil && identity.matchesRelaxed(origin)
		})
	}

	remaining := candidates
	sourceRule := ""
	for _, origin := range origins {
		if origin.provenance != originProvenanceSource {
			continue
		}
		exact := filterExact(candidates, origin.endpoint)
		if len(exact) == 1 {
			return selectSingleMatch(exact, "username_origin_source")
		}
		if len(exact) > 1 {
			remaining = exact
			sourceRule = "username_origin_source"
			break
		}
		relaxed := filterRelaxed(candidates, origin.endpoint)
		if len(relaxed) == 1 {
			return selectSingleMatch(relaxed, "username_origin_source_relaxed")
		}
		if len(relaxed) > 1 {
			remaining = relaxed
			sourceRule = "username_origin_source_relaxed"
		}
		break
	}

	for _, origin := range origins {
		if origin.provenance == originProvenanceSource {
			continue
		}
		exact := filterExact(remaining, origin.endpoint)
		if len(exact) == 1 {
			rule := "username_origin_via"
			if origin.provenance == originProvenanceContact {
				rule = "username_origin_contact"
			}
			return selectSingleMatch(exact, rule)
		}
	}

	for _, origin := range origins {
		if origin.provenance == originProvenanceSource {
			continue
		}
		relaxed := filterRelaxed(remaining, origin.endpoint)
		if len(relaxed) == 1 {
			rule := "username_origin_via_relaxed"
			if origin.provenance == originProvenanceContact {
				rule = "username_origin_contact_relaxed"
			}
			return selectSingleMatch(relaxed, rule)
		}
	}

	if sourceRule != "" {
		return TrunkInviteMatchResult{
			Rule:         sourceRule,
			Ambiguous:    true,
			CandidateIDs: collectCandidateIDs(remaining),
			Reason:       "multiple_source_matches",
		}
	}

	result := selectSingleMatch(candidates, "username_only_online")
	if result.Ambiguous {
		result.Reason = "indistinguishable_eligible_candidates"
	}
	return result
}

// MatchTrunkFromInviteDetailed matches an incoming INVITE to a trunk with deterministic priority.
func (tm *TrunkManager) MatchTrunkFromInviteDetailed(req *gosip.Request) TrunkInviteMatchResult {
	state := tm.snapshotInviteMatchState()

	ruriHost, ruriUser, ruriPort := normalizeInviteURI(req.Recipient)
	toHost := ""
	toUser := ""
	toPort := 5060
	if to := req.To(); to != nil {
		toHost, toUser, toPort = normalizeInviteURI(to.Address)
	}
	sipUser := strings.TrimSpace(ruriUser)
	if sipUser == "" {
		sipUser = strings.TrimSpace(toUser)
	}

	localRURI := isLocalGatewayContact(state.publicIP, state.localPort, ruriHost, ruriPort)
	localTo := isLocalGatewayContact(state.publicIP, state.localPort, toHost, toPort)
	matchTime := time.Now()
	origins := extractInviteOrigins(req)
	eligible := state.eligibleByUsername(sipUser, matchTime)
	owned := state.ownedByUsername(sipUser)
	ownedIDs := collectCandidateIDs(owned)
	eligibleIDs := collectCandidateIDs(eligible)

	if !localRURI {
		result := applyOwnershipSafety(selectSingleMatch(state.filterEligible(state.byUserDomainPort(ruriHost, ruriUser, ruriPort), matchTime), "ruri_user_domain_port"), state)
		if result.Trunk != nil || result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
		result = applyOwnershipSafety(selectSingleMatch(state.filterEligible(state.byUserDomain(ruriHost, ruriUser), matchTime), "ruri_user_domain"), state)
		if result.Trunk != nil && !result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
		if result.Ambiguous {
			originResult := selectByOrigin(eligible, state, origins)
			if originResult.Trunk != nil || originResult.Ambiguous {
				return finalizeSelected(originResult, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
			}
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
	}

	if !localTo {
		result := applyOwnershipSafety(selectSingleMatch(state.filterEligible(state.byUserDomainPort(toHost, toUser, toPort), matchTime), "to_user_domain_port"), state)
		if result.Trunk != nil || result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
		result = applyOwnershipSafety(selectSingleMatch(state.filterEligible(state.byUserDomain(toHost, toUser), matchTime), "to_user_domain"), state)
		if result.Trunk != nil && !result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
	}

	if !localRURI {
		result := applyOwnershipSafety(selectSingleMatch(state.eligibleByUsernameAndDomainPort(sipUser, ruriHost, ruriPort, matchTime), "ruri_domain_port_fallback"), state)
		if result.Trunk != nil || result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
	}
	if !localTo {
		result := applyOwnershipSafety(selectSingleMatch(state.eligibleByUsernameAndDomainPort(sipUser, toHost, toPort, matchTime), "to_domain_port_fallback"), state)
		if result.Trunk != nil || result.Ambiguous {
			return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
		}
	}

	result := selectByOrigin(eligible, state, origins)
	if result.Rule == "" {
		result.Rule = "no_match"
		result.Reason = "no_eligible_owned_registration"
	}
	return finalizeSelected(result, state, sipUser, ownedIDs, eligibleIDs, localRURI, localTo, origins)
}

// MatchTrunkFromInvite matches an incoming INVITE to a trunk.
func (tm *TrunkManager) MatchTrunkFromInvite(req *gosip.Request) (*Trunk, bool) {
	result := tm.MatchTrunkFromInviteDetailed(req)
	if result.Trunk == nil {
		return nil, false
	}
	return result.Trunk, result.Owned
}
