package api

import (
	"strings"
	"sync"
	"time"
)

const (
	// bridgedChatEchoTTL is how long a direct relay suppresses the PBX copy
	// of the same body. Asterisk forwards an in-dialog MESSAGE within a few
	// milliseconds; two seconds covers that echo without hiding a later resend.
	bridgedChatEchoTTL = 2 * time.Second
	// bridgedChatEchoMaxSize caps remembered (session, body) keys so a busy
	// process cannot grow the map without bound. Expired keys are dropped first.
	bridgedChatEchoMaxSize = 256
)

type bridgedChatEchoKey struct {
	sessionID string
	body      string
}

type bridgedChatEchoEntry struct {
	expires time.Time
	seq     uint64
	gen     uint64
}

// bridgedChatEchoGuard records chat bodies relayed to a bridged session so
// NotifySIPMessageOnDialog can drop Asterisk's forwarded copy.
type bridgedChatEchoGuard struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	now     func() time.Time
	seq     uint64
	entries map[bridgedChatEchoKey]bridgedChatEchoEntry
}

func newBridgedChatEchoGuard() bridgedChatEchoGuard {
	return bridgedChatEchoGuard{
		ttl:     bridgedChatEchoTTL,
		maxSize: bridgedChatEchoMaxSize,
		entries: make(map[bridgedChatEchoKey]bridgedChatEchoEntry),
	}
}

func (g *bridgedChatEchoGuard) window() time.Duration {
	if g.ttl > 0 {
		return g.ttl
	}
	return bridgedChatEchoTTL
}

func (g *bridgedChatEchoGuard) limit() int {
	if g.maxSize > 0 {
		return g.maxSize
	}
	return bridgedChatEchoMaxSize
}

func (g *bridgedChatEchoGuard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *bridgedChatEchoGuard) ensureLocked() {
	if g.entries == nil {
		g.entries = make(map[bridgedChatEchoKey]bridgedChatEchoEntry)
	}
}

// normalizeBridgedChatBody canonicalizes a chat body for echo comparison.
// SIP forwards often differ only by surrounding whitespace or CR/LF.
func normalizeBridgedChatBody(body string) string {
	body = strings.TrimSpace(body)
	if strings.Contains(body, "\r") {
		body = strings.ReplaceAll(body, "\r\n", "\n")
		body = strings.ReplaceAll(body, "\r", "\n")
		body = strings.TrimSpace(body)
	}
	return body
}

// remember records a relayed body and returns a generation. forget is a no-op
// once a later remember refreshes the same key.
func (g *bridgedChatEchoGuard) remember(sessionID, body string) uint64 {
	sessionID = strings.TrimSpace(sessionID)
	body = normalizeBridgedChatBody(body)
	if g == nil || sessionID == "" || body == "" {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureLocked()
	now := g.clock()
	g.expireLocked(now)
	g.seq++
	key := bridgedChatEchoKey{sessionID: sessionID, body: body}
	g.entries[key] = bridgedChatEchoEntry{
		expires: now.Add(g.window()),
		seq:     g.seq,
		gen:     g.seq,
	}
	g.evictLocked()
	return g.seq
}

// forget drops a key only when gen is still the latest remember for it.
func (g *bridgedChatEchoGuard) forget(sessionID, body string, gen uint64) {
	sessionID = strings.TrimSpace(sessionID)
	body = normalizeBridgedChatBody(body)
	if g == nil || gen == 0 || sessionID == "" || body == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.entries) == 0 {
		return
	}
	key := bridgedChatEchoKey{sessionID: sessionID, body: body}
	ent, ok := g.entries[key]
	if !ok || ent.gen != gen {
		return
	}
	delete(g.entries, key)
}

// suppress reports whether body was relayed to sessionID and the window is
// still open. The entry stays until it expires so repeated PBX copies drop too.
func (g *bridgedChatEchoGuard) suppress(sessionID, body string) bool {
	sessionID = strings.TrimSpace(sessionID)
	body = normalizeBridgedChatBody(body)
	if g == nil || sessionID == "" || body == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.entries) == 0 {
		return false
	}
	now := g.clock()
	g.expireLocked(now)
	ent, ok := g.entries[bridgedChatEchoKey{sessionID: sessionID, body: body}]
	return ok && now.Before(ent.expires)
}

func (g *bridgedChatEchoGuard) expireLocked(now time.Time) {
	for key, ent := range g.entries {
		if !now.Before(ent.expires) {
			delete(g.entries, key)
		}
	}
}

func (g *bridgedChatEchoGuard) evictLocked() {
	limit := g.limit()
	for len(g.entries) > limit {
		var oldestKey bridgedChatEchoKey
		var oldest uint64
		first := true
		for key, ent := range g.entries {
			if first || ent.seq < oldest {
				oldestKey = key
				oldest = ent.seq
				first = false
			}
		}
		if first {
			return
		}
		delete(g.entries, oldestKey)
	}
}

func (s *Server) noteBridgedChatRelay(sessionID, body string) uint64 {
	if s == nil {
		return 0
	}
	return s.bridgedChatEcho.remember(sessionID, body)
}

func (s *Server) forgetBridgedChatRelay(sessionID, body string, gen uint64) {
	if s == nil {
		return
	}
	s.bridgedChatEcho.forget(sessionID, body, gen)
}

func (s *Server) dropBridgedChatPBXCopy(sessionID, body string) bool {
	if s == nil {
		return false
	}
	return s.bridgedChatEcho.suppress(sessionID, body)
}

func (s *Server) sessionHasChatTarget(sessionID string) bool {
	targets, _, _ := s.selectSIPMessageTargets(sessionID)
	return len(targets) > 0
}
