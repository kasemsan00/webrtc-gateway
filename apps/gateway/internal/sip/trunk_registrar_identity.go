package sip

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gosip "github.com/emiago/sipgo/sip"
)

type registrarEndpoint struct {
	Host      string
	Port      int
	Transport string
}

type registrarIdentity struct {
	Generation uint64
	TrunkID    int64
	Username   string
	Hostname   string
	Port       int
	Transport  string
	Endpoints  []registrarEndpoint
	SuccessAt  time.Time
	ExpiresAt  time.Time
}

type registrarEndpointResolver interface {
	Resolve(domain string, port int, transport string) ([]registrarEndpoint, error)
}

type defaultRegistrarResolver struct{}

func (defaultRegistrarResolver) Resolve(domain string, port int, transport string) ([]registrarEndpoint, error) {
	return resolveRegistrarEndpoints(domain, port, transport)
}

func normalizeSIPHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return strings.TrimSuffix(host, ".")
}

func normalizeSIPPort(port int) int {
	if port <= 0 {
		return 5060
	}
	return port
}

func normalizeSIPTransport(transport string) string {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		return "tcp"
	}
	return transport
}

func parseHostPort(value string, defaultPort int) (string, int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}
	host, portStr, err := net.SplitHostPort(value)
	if err != nil {
		host = normalizeSIPHost(value)
		if host == "" {
			return "", 0, false
		}
		return host, normalizeSIPPort(defaultPort), true
	}
	port, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		port = defaultPort
	}
	host = normalizeSIPHost(host)
	if host == "" {
		return "", 0, false
	}
	return host, normalizeSIPPort(port), true
}

func normalizeRegistrarEndpoint(host string, port int, transport string) (registrarEndpoint, bool) {
	host = normalizeSIPHost(host)
	if host == "" {
		return registrarEndpoint{}, false
	}
	return registrarEndpoint{
		Host:      host,
		Port:      normalizeSIPPort(port),
		Transport: normalizeSIPTransport(transport),
	}, true
}

func registrarEndpointKey(ep registrarEndpoint) string {
	return ep.Transport + "|" + ep.Host + "|" + strconv.Itoa(ep.Port)
}

func mergeRegistrarEndpoints(dst []registrarEndpoint, extra ...registrarEndpoint) []registrarEndpoint {
	seen := make(map[string]struct{}, len(dst)+len(extra))
	out := make([]registrarEndpoint, 0, len(dst)+len(extra))
	for _, ep := range append(dst, extra...) {
		if ep.Host == "" {
			continue
		}
		ep.Host = normalizeSIPHost(ep.Host)
		ep.Port = normalizeSIPPort(ep.Port)
		ep.Transport = normalizeSIPTransport(ep.Transport)
		if ep.Host == "" {
			continue
		}
		key := registrarEndpointKey(ep)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ep)
	}
	return out
}

func endpointsEqualExact(a, b registrarEndpoint) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Transport == b.Transport
}

func endpointsEqualRelaxed(a, b registrarEndpoint) bool {
	return a.Host == b.Host && a.Transport == b.Transport
}

func resolveRegistrarEndpoints(domain string, port int, transport string) ([]registrarEndpoint, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("registrar domain is required")
	}
	transport = normalizeSIPTransport(transport)
	destination, err := resolveSIPDestination(domain, port, transport)
	if err != nil {
		return nil, err
	}
	host, destPort, ok := parseHostPort(destination, normalizeSIPPort(port))
	if !ok {
		return nil, fmt.Errorf("invalid registrar destination %q", destination)
	}
	if port > 0 {
		destPort = normalizeSIPPort(port)
	}

	if ip := net.ParseIP(host); ip != nil {
		ep, ok := normalizeRegistrarEndpoint(ip.String(), destPort, transport)
		if !ok {
			return nil, fmt.Errorf("invalid registrar IP %q", host)
		}
		return []registrarEndpoint{ep}, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		configured, ok := normalizeRegistrarEndpoint(host, destPort, transport)
		if !ok {
			return nil, err
		}
		return []registrarEndpoint{configured}, err
	}
	endpoints := make([]registrarEndpoint, 0, len(ips)+1)
	if configured, ok := normalizeRegistrarEndpoint(host, destPort, transport); ok {
		endpoints = append(endpoints, configured)
	}
	for _, ip := range ips {
		if ep, ok := normalizeRegistrarEndpoint(ip.String(), destPort, transport); ok {
			endpoints = append(endpoints, ep)
		}
	}
	return mergeRegistrarEndpoints(endpoints), nil
}

func sipResponseSourceEndpoint(res *gosip.Response, transport string) (registrarEndpoint, bool) {
	if res == nil {
		return registrarEndpoint{}, false
	}
	host, port, ok := parseHostPort(res.Source(), 5060)
	if !ok {
		return registrarEndpoint{}, false
	}
	return normalizeRegistrarEndpoint(host, port, transport)
}

func (id *registrarIdentity) clone() *registrarIdentity {
	if id == nil {
		return nil
	}
	copied := *id
	if len(id.Endpoints) > 0 {
		copied.Endpoints = append([]registrarEndpoint(nil), id.Endpoints...)
	}
	return &copied
}

func (id *registrarIdentity) isCurrent(now time.Time) bool {
	return id != nil && !id.ExpiresAt.IsZero() && id.ExpiresAt.After(now)
}

func (id *registrarIdentity) matchesExact(origin registrarEndpoint) bool {
	if id == nil {
		return false
	}
	for _, ep := range id.Endpoints {
		if endpointsEqualExact(ep, origin) {
			return true
		}
	}
	return false
}

func (id *registrarIdentity) matchesRelaxed(origin registrarEndpoint) bool {
	if id == nil {
		return false
	}
	for _, ep := range id.Endpoints {
		if endpointsEqualRelaxed(ep, origin) {
			return true
		}
	}
	return false
}

func trunkRegistrarIdentityChanged(previous, updated *Trunk) bool {
	if previous == nil || updated == nil {
		return false
	}
	return strings.TrimSpace(previous.Username) != strings.TrimSpace(updated.Username) ||
		normalizeSIPHost(previous.Domain) != normalizeSIPHost(updated.Domain) ||
		normalizeSIPPort(previous.Port) != normalizeSIPPort(updated.Port) ||
		normalizeSIPTransport(previous.Transport) != normalizeSIPTransport(updated.Transport)
}

func (tm *TrunkManager) registrarResolver() registrarEndpointResolver {
	if tm != nil && tm.resolver != nil {
		return tm.resolver
	}
	return defaultRegistrarResolver{}
}

func (tm *TrunkManager) beginRegistrarOperation(trunkID int64) uint64 {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.registrarGenerations == nil {
		tm.registrarGenerations = make(map[int64]uint64)
	}
	tm.registrarGenerations[trunkID]++
	return tm.registrarGenerations[trunkID]
}

func (tm *TrunkManager) currentRegistrarGeneration(trunkID int64) uint64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.registrarGenerations == nil {
		return 0
	}
	return tm.registrarGenerations[trunkID]
}

func (tm *TrunkManager) invalidateRegistrarIdentityLocked(trunkID int64) {
	if tm.registrarGenerations == nil {
		tm.registrarGenerations = make(map[int64]uint64)
	}
	tm.registrarGenerations[trunkID]++
	delete(tm.registrarIdentities, trunkID)
}

func (tm *TrunkManager) invalidateRegistrarIdentity(trunkID int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.invalidateRegistrarIdentityLocked(trunkID)
}

func (tm *TrunkManager) tryPublishRegistrarIdentity(trunkID int64, generation uint64, identity *registrarIdentity) bool {
	if identity == nil {
		return false
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.registrarGenerations == nil {
		tm.registrarGenerations = make(map[int64]uint64)
	}
	if tm.registrarIdentities == nil {
		tm.registrarIdentities = make(map[int64]*registrarIdentity)
	}
	if tm.registrarGenerations[trunkID] != generation {
		return false
	}
	published := identity.clone()
	published.Generation = generation
	published.TrunkID = trunkID
	tm.registrarIdentities[trunkID] = published
	return true
}

func (tm *TrunkManager) pruneRegistrarIdentitiesLocked() {
	if tm.registrarGenerations == nil {
		tm.registrarGenerations = make(map[int64]uint64)
	}
	for id := range tm.registrarIdentities {
		if _, ok := tm.trunks[id]; !ok {
			delete(tm.registrarIdentities, id)
			tm.registrarGenerations[id]++
		}
	}
}

func (tm *TrunkManager) publishRegistrarIdentityFromRegister(trunk *Trunk, generation uint64, res *gosip.Response, expiresSeconds int) {
	if trunk == nil {
		return
	}
	endpoints, err := tm.registrarResolver().Resolve(trunk.Domain, trunk.Port, trunk.Transport)
	if err != nil && len(endpoints) == 0 {
		fmt.Printf("⚠️ [TrunkManager] Trunk %d registrar endpoint resolve failed: %v\n", trunk.ID, err)
	}
	if extra, ok := sipResponseSourceEndpoint(res, trunk.Transport); ok {
		endpoints = mergeRegistrarEndpoints(endpoints, extra)
	}
	if configured, ok := normalizeRegistrarEndpoint(trunk.Domain, trunk.Port, trunk.Transport); ok {
		endpoints = mergeRegistrarEndpoints(endpoints, configured)
	}
	now := time.Now()
	expires := time.Duration(expiresSeconds) * time.Second
	if expires <= 0 {
		expires = time.Hour
	}
	identity := &registrarIdentity{
		Generation: generation,
		TrunkID:    trunk.ID,
		Username:   strings.TrimSpace(trunk.Username),
		Hostname:   normalizeSIPHost(trunk.Domain),
		Port:       normalizeSIPPort(trunk.Port),
		Transport:  normalizeSIPTransport(trunk.Transport),
		Endpoints:  endpoints,
		SuccessAt:  now,
		ExpiresAt:  now.Add(expires),
	}
	if !tm.tryPublishRegistrarIdentity(trunk.ID, generation, identity) {
		fmt.Printf("📞 [TrunkManager] Trunk %d skipped stale registrar identity publication (generation=%d)\n", trunk.ID, generation)
	}
}
