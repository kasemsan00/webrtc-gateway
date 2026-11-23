package sip

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDNSCacheLookupIP_CachesWithinTTLAndRefreshesAfterExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var mu sync.Mutex
	var calls atomic.Int32
	var failLookup atomic.Bool

	cache := newDNSCache(30 * time.Second)
	cache.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	cache.lookupIPFn = func(host string) ([]net.IP, error) {
		calls.Add(1)
		if failLookup.Load() {
			return nil, errors.New("temporary dns failure")
		}
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}

	// IP literal bypasses lookup entirely.
	literalIPs, err := cache.LookupIP("198.51.100.5")
	if err != nil || len(literalIPs) != 1 || literalIPs[0].String() != "198.51.100.5" {
		t.Fatalf("literal IP failed: ips=%v err=%v", literalIPs, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("literal IP invoked lookup %d times", calls.Load())
	}

	// Errors are not cached.
	failLookup.Store(true)
	if _, err := cache.LookupIP("sip.example.com"); err == nil {
		t.Fatal("expected lookup error")
	}
	failLookup.Store(false)

	// First real lookup populates cache.
	ips1, err := cache.LookupIP("sip.example.com")
	if err != nil || len(ips1) != 1 || ips1[0].String() != "203.0.113.10" {
		t.Fatalf("first lookup failed: ips=%v err=%v", ips1, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}

	// Mutating returned slice must not corrupt cached entry.
	ips1[0][len(ips1[0])-1] = 99

	// Cache hit within TTL (case-insensitive).
	mu.Lock()
	now = now.Add(29 * time.Second)
	mu.Unlock()
	ips2, err := cache.LookupIP("SIP.EXAMPLE.COM")
	if err != nil || len(ips2) != 1 || ips2[0].String() != "203.0.113.10" {
		t.Fatalf("cached lookup failed or corrupted: ips=%v err=%v", ips2, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected cache hit, got calls=%d", calls.Load())
	}

	// After TTL expires, cache refreshes.
	mu.Lock()
	now = now.Add(2 * time.Second)
	mu.Unlock()
	if _, err := cache.LookupIP("sip.example.com"); err != nil {
		t.Fatalf("post-expiry lookup failed: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected refresh after expiry, got calls=%d", calls.Load())
	}
}

func TestDNSCacheLookupSRV_CachesWithinTTLAndRefreshesAfterExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var mu sync.Mutex
	var calls atomic.Int32
	var failLookup atomic.Bool

	cache := newDNSCache(30 * time.Second)
	cache.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	cache.lookupSRVFn = func(service, proto, name string) (string, []*net.SRV, error) {
		calls.Add(1)
		if failLookup.Load() {
			return "", nil, errors.New("srv lookup failed")
		}
		return "_sip._tcp.example.com.", []*net.SRV{
			{Target: "pbx.example.com.", Port: 5061, Priority: 10, Weight: 5},
		}, nil
	}

	// Error is not cached.
	failLookup.Store(true)
	if _, _, err := cache.LookupSRV("sip", "tcp", "example.com"); err == nil {
		t.Fatal("expected SRV error")
	}
	failLookup.Store(false)

	cname, addrs, err := cache.LookupSRV("sip", "tcp", "example.com")
	if err != nil || cname != "_sip._tcp.example.com." || len(addrs) != 1 || addrs[0].Port != 5061 {
		t.Fatalf("first SRV lookup unexpected: cname=%q addrs=%+v err=%v", cname, addrs, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}

	// Defensive copy check.
	addrs[0].Port = 9999

	// Hit within TTL.
	mu.Lock()
	now = now.Add(15 * time.Second)
	mu.Unlock()
	_, addrs2, err := cache.LookupSRV("SIP", "TCP", "EXAMPLE.COM")
	if err != nil || len(addrs2) != 1 || addrs2[0].Port != 5061 {
		t.Fatalf("cached SRV unexpected: addrs=%+v err=%v", addrs2, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected SRV cache hit, got calls=%d", calls.Load())
	}

	// Refresh after TTL.
	mu.Lock()
	now = now.Add(20 * time.Second)
	mu.Unlock()
	if _, _, err := cache.LookupSRV("sip", "tcp", "example.com"); err != nil {
		t.Fatalf("post-expiry SRV lookup failed: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected SRV refresh after expiry, got calls=%d", calls.Load())
	}
}
