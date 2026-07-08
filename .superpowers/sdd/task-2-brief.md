### Task 2: Gateway — SSE subscribe/broadcast helpers for ws-client stream

**Files:**
- Modify: `apps/gateway/internal/api/server.go:40-50` (fields), `apps/gateway/internal/api/server.go:185-190` (NewServer init)
- Modify: `apps/gateway/internal/api/ws_util.go:224` (append helpers)

**Interfaces:**
- Consumes: nothing new
- Produces: `s.subscribeWSClientStream() (int, chan []byte)`, `s.unsubscribeWSClientStream(id int)`, `s.broadcastWSClientStream(payload []byte)`

- [ ] **Step 1: Add stream fields to Server**

In `apps/gateway/internal/api/server.go`, after `sessionStreamSeq` (line 45):

```go
	wsClientStreams   map[int]chan []byte
	wsClientStreamSeq int
```

- [ ] **Step 2: Init in NewServer**

In `apps/gateway/internal/api/server.go`, in `NewServer` (after `sessionStreams: make(...)`):

```go
		wsClientStreams:   make(map[int]chan []byte),
```

- [ ] **Step 3: Add subscribe/broadcast helpers**

Append to `apps/gateway/internal/api/ws_util.go` (after `broadcastSessionStream`):

```go
func (s *Server) subscribeWSClientStream() (int, chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wsClientStreamSeq++
	id := s.wsClientStreamSeq
	ch := make(chan []byte, 32)
	s.wsClientStreams[id] = ch
	return id, ch
}

func (s *Server) unsubscribeWSClientStream(id int) {
	s.mu.Lock()
	delete(s.wsClientStreams, id)
	s.mu.Unlock()
}

func (s *Server) broadcastWSClientStream(payload []byte) {
	s.mu.RLock()
	streams := make([]chan []byte, 0, len(s.wsClientStreams))
	for _, ch := range s.wsClientStreams {
		streams = append(streams, ch)
	}
	s.mu.RUnlock()

	for _, ch := range streams {
		select {
		case ch <- payload:
		default:
		}
	}
}
```

- [ ] **Step 4: Run gateway tests**

Run: `cd apps/gateway && go test ./internal/api -run "TestHandleSSEStreams" -v`

Expected: PASS (no regression; new helpers are unused yet)

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/server.go apps/gateway/internal/api/ws_util.go
git commit -m "feat(gateway): add ws-client SSE stream subscribe/broadcast helpers"
```
