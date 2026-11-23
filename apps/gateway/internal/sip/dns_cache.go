package sip

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	defaultDNSCacheTTL        = 30 * time.Second
	dnsCacheMaxEntriesForPrune = 256
)

type ipCacheEntry struct {
	ips       []net.IP
	expiresAt time.Time
}

type srvCacheEntry struct {
	cname     string
	addrs     []*net.SRV
	expiresAt time.Time
}

type dnsCache struct {
	mu          sync.RWMutex
	ttl         time.Duration
	now         func() time.Time
	lookupIPFn  func(host string) ([]net.IP, error)
	lookupSRVFn func(service, proto, name string) (string, []*net.SRV, error)
	ipEntries   map[string]ipCacheEntry
	srvEntries  map[string]srvCacheEntry
}

func newDNSCache(ttl time.Duration) *dnsCache {
	if ttl <= 0 {
		ttl = defaultDNSCacheTTL
	}
	return &dnsCache{
		ttl:         ttl,
		now:         time.Now,
		lookupIPFn:  net.LookupIP,
		lookupSRVFn: net.LookupSRV,
		ipEntries:   make(map[string]ipCacheEntry),
		srvEntries:  make(map[string]srvCacheEntry),
	}
}

func (c *dnsCache) LookupIP(host string) ([]net.IP, error) {
	trimmed := strings.TrimSpace(host)
	if ip := net.ParseIP(trimmed); ip != nil {
		return []net.IP{cloneIP(ip)}, nil
	}
	key := strings.ToLower(trimmed)
	now := c.now()

	c.mu.RLock()
	entry, ok := c.ipEntries[key]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return cloneIPs(entry.ips), nil
	}

	ips, err := c.lookupIPFn(trimmed)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return ips, nil
	}

	c.mu.Lock()
	if len(c.ipEntries) >= dnsCacheMaxEntriesForPrune {
		for k, v := range c.ipEntries {
			if !now.Before(v.expiresAt) {
				delete(c.ipEntries, k)
			}
		}
	}
	c.ipEntries[key] = ipCacheEntry{
		ips:       cloneIPs(ips),
		expiresAt: c.now().Add(c.ttl),
	}
	c.mu.Unlock()

	return cloneIPs(ips), nil
}

func (c *dnsCache) LookupSRV(service, proto, name string) (string, []*net.SRV, error) {
	key := fmt.Sprintf("%s|%s|%s",
		strings.ToLower(strings.TrimSpace(service)),
		strings.ToLower(strings.TrimSpace(proto)),
		strings.ToLower(strings.TrimSpace(name)),
	)
	now := c.now()

	c.mu.RLock()
	entry, ok := c.srvEntries[key]
	c.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.cname, cloneSRVs(entry.addrs), nil
	}

	cname, addrs, err := c.lookupSRVFn(service, proto, name)
	if err != nil {
		return "", nil, err
	}
	if len(addrs) == 0 {
		return cname, addrs, nil
	}

	c.mu.Lock()
	if len(c.srvEntries) >= dnsCacheMaxEntriesForPrune {
		for k, v := range c.srvEntries {
			if !now.Before(v.expiresAt) {
				delete(c.srvEntries, k)
			}
		}
	}
	c.srvEntries[key] = srvCacheEntry{
		cname:     cname,
		addrs:     cloneSRVs(addrs),
		expiresAt: c.now().Add(c.ttl),
	}
	c.mu.Unlock()

	return cname, cloneSRVs(addrs), nil
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	cp := make(net.IP, len(ip))
	copy(cp, ip)
	return cp
}

func cloneIPs(ips []net.IP) []net.IP {
	if ips == nil {
		return nil
	}
	out := make([]net.IP, len(ips))
	for i, ip := range ips {
		out[i] = cloneIP(ip)
	}
	return out
}

func cloneSRVs(addrs []*net.SRV) []*net.SRV {
	if addrs == nil {
		return nil
	}
	out := make([]*net.SRV, len(addrs))
	for i, a := range addrs {
		if a == nil {
			continue
		}
		cp := *a
		out[i] = &cp
	}
	return out
}

var defaultDNSCache = newDNSCache(defaultDNSCacheTTL)

func cachedLookupIP(host string) ([]net.IP, error) {
	return defaultDNSCache.LookupIP(host)
}

func cachedLookupSRV(service, proto, name string) (string, []*net.SRV, error) {
	return defaultDNSCache.LookupSRV(service, proto, name)
}
