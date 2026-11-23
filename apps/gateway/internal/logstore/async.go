package logstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"webrtc-sip-gateway/internal/telemetry"
)

const (
	defaultPayloadQueueCapacity   = 2048
	defaultSessionQueueCapacity   = 2048
	defaultMiscQueueCapacity      = 2048
	defaultPayloadWorkerCount     = 2
	defaultEndedHistoryMaxEntries = 4096
	defaultEndedHistoryTTL        = 5 * time.Minute
	defaultShutdownDrainTimeout   = 5 * time.Second
)

// AsyncLogStore is an optional interface implemented by LogStore
// implementations that provide non-blocking background persistence for call
// payloads, session snapshots, SIP dialogs, and session directory records.
// Kept separate from LogStore so lightweight test stubs remain source-compatible.
type AsyncLogStore interface {
	StorePayloadAndLogEventAsync(payload *PayloadRecord, event *Event)
	UpsertSessionAsync(sess *SessionRecord)
	UpsertDialogAsync(dialog *DialogRecord)
	UpsertSessionDirectoryAsync(sessionID, ownerInstanceID, wsURL string, ttlSeconds int)
}

var (
	_ AsyncLogStore = (*logStore)(nil)
	_ AsyncLogStore = (*noopStore)(nil)
)

type payloadEventJob struct {
	payload *PayloadRecord
	event   *Event
}

type asyncWriteKind int

const (
	asyncWriteDialog asyncWriteKind = iota + 1
	asyncWriteSessionDirectory
)

type asyncWriteJob struct {
	kind            asyncWriteKind
	dialog          *DialogRecord
	sessionID       string
	ownerInstanceID string
	wsURL           string
	ttlSeconds      int
}

// No-op AsyncLogStore implementation.
func (n *noopStore) StorePayloadAndLogEventAsync(payload *PayloadRecord, event *Event) {}
func (n *noopStore) UpsertSessionAsync(sess *SessionRecord)                            {}
func (n *noopStore) UpsertDialogAsync(dialog *DialogRecord)                            {}
func (n *noopStore) UpsertSessionDirectoryAsync(sessionID, ownerInstanceID, wsURL string, ttlSeconds int) {
}

// StorePayloadAndLogEventAsync inserts payload in a bounded background worker,
// sets event.PayloadID on success, and queues event onto the async event batch
// pipeline without blocking the caller.
func (s *logStore) StorePayloadAndLogEventAsync(payload *PayloadRecord, event *Event) {
	if s.pool == nil && s.storePayloadHook == nil {
		return
	}
	if payload == nil {
		if event != nil {
			s.LogEvent(event)
		}
		return
	}
	if s.payloadQueue == nil {
		if event != nil {
			s.LogEvent(event)
		}
		return
	}

	job := payloadEventJob{
		payload: clonePayloadRecord(payload),
		event:   cloneEvent(event),
	}

	select {
	case s.payloadQueue <- job:
		s.asyncAccepted.Add(1)
	default:
		s.asyncDropped.Add(1)
		telemetry.LogPersistenceDrop(context.Background())
		fmt.Printf("⚠️ Payload async queue full, dropping payload: %s/%s\n", payload.SessionID, payload.Kind)
		if job.event != nil {
			// Still record the timeline event even if payload persistence was shed.
			s.LogEvent(job.event)
		}
	}
}

// UpsertSessionAsync enqueues a call session snapshot with per-session_id
// coalescing and terminal-state protection so an older non-ended snapshot can
// never overwrite an ended snapshot.
func (s *logStore) UpsertSessionAsync(sess *SessionRecord) {
	if s.pool == nil && s.upsertSessionHook == nil {
		return
	}
	if sess == nil || strings.TrimSpace(sess.SessionID) == "" || s.sessionCoalescer == nil {
		return
	}

	if s.sessionCoalescer.enqueue(cloneSessionRecord(sess)) {
		s.asyncAccepted.Add(1)
		return
	}

	s.asyncDropped.Add(1)
	telemetry.LogPersistenceDrop(context.Background())
	fmt.Printf("⚠️ Session async queue full, dropping snapshot for session: %s\n", sess.SessionID)
}

// UpsertDialogAsync enqueues a SIP dialog record for non-blocking background persistence.
func (s *logStore) UpsertDialogAsync(dialog *DialogRecord) {
	if s.pool == nil && s.upsertDialogHook == nil {
		return
	}
	if dialog == nil || s.miscQueue == nil {
		return
	}

	job := asyncWriteJob{
		kind:   asyncWriteDialog,
		dialog: cloneDialogRecord(dialog),
	}
	select {
	case s.miscQueue <- job:
		s.asyncAccepted.Add(1)
	default:
		s.asyncDropped.Add(1)
		telemetry.LogPersistenceDrop(context.Background())
		fmt.Printf("⚠️ Async write queue full, dropping dialog for session: %s\n", dialog.SessionID)
	}
}

// UpsertSessionDirectoryAsync enqueues a session directory upsert for
// non-blocking background persistence.
func (s *logStore) UpsertSessionDirectoryAsync(sessionID, ownerInstanceID, wsURL string, ttlSeconds int) {
	if s.pool == nil && s.upsertSessionDirHook == nil {
		return
	}
	if strings.TrimSpace(sessionID) == "" || s.miscQueue == nil {
		return
	}

	job := asyncWriteJob{
		kind:            asyncWriteSessionDirectory,
		sessionID:       sessionID,
		ownerInstanceID: ownerInstanceID,
		wsURL:           wsURL,
		ttlSeconds:      ttlSeconds,
	}
	select {
	case s.miscQueue <- job:
		s.asyncAccepted.Add(1)
	default:
		s.asyncDropped.Add(1)
		telemetry.LogPersistenceDrop(context.Background())
		fmt.Printf("⚠️ Async write queue full, dropping session directory update: %s\n", sessionID)
	}
}

func (s *logStore) ensureAsyncQueues() {
	if s.eventQueue == nil {
		s.eventQueue = make(chan *Event, 10000)
	}
	if s.statsQueue == nil {
		s.statsQueue = make(chan *StatsRecord, 1000)
	}
	if s.payloadQueue == nil {
		s.payloadQueue = make(chan payloadEventJob, defaultPayloadQueueCapacity)
	}
	if s.sessionCoalescer == nil {
		s.sessionCoalescer = newSessionCoalescer(defaultSessionQueueCapacity)
	}
	if s.miscQueue == nil {
		s.miscQueue = make(chan asyncWriteJob, defaultMiscQueueCapacity)
	}
	if s.asyncStopCh == nil {
		s.asyncStopCh = make(chan struct{})
	}
	if s.stopCh == nil {
		s.stopCh = make(chan struct{})
	}
}

func (s *logStore) startAsyncWorkers() {
	s.ensureAsyncQueues()
	for i := 0; i < defaultPayloadWorkerCount; i++ {
		s.asyncWg.Add(1)
		go s.payloadAsyncWorker()
	}
	s.asyncWg.Add(2)
	go s.sessionAsyncWorker()
	go s.miscAsyncWorker()
}

func (s *logStore) payloadAsyncWorker() {
	defer s.asyncWg.Done()

	process := func(ctx context.Context, job payloadEventJob) {
		opCtx, cancel := withDefaultTimeout(ctx, defaultDBOpTimeout)
		payloadID, err := s.execStorePayload(opCtx, job.payload)
		cancel()
		if err != nil {
			fmt.Printf("⚠️ Async payload store failed (%s/%s): %v\n", job.payload.SessionID, job.payload.Kind, err)
		} else {
			s.lastSuccessNS.Store(time.Now().UnixNano())
			if job.event != nil {
				id := payloadID
				job.event.PayloadID = &id
			}
		}
		if job.event != nil {
			s.LogEvent(job.event)
		}
	}

	for {
		select {
		case job := <-s.payloadQueue:
			process(context.Background(), job)
		case <-s.asyncStopCh:
			drainCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownDrainTimeout)
			defer cancel()
			for {
				select {
				case job := <-s.payloadQueue:
					if drainCtx.Err() != nil {
						if job.event != nil {
							s.LogEvent(job.event)
						}
						continue
					}
					process(drainCtx, job)
				default:
					return
				}
			}
		}
	}
}

func (s *logStore) sessionAsyncWorker() {
	defer s.asyncWg.Done()

	process := func(ctx context.Context, sess *SessionRecord) {
		opCtx, cancel := withDefaultTimeout(ctx, defaultDBOpTimeout)
		err := s.execUpsertSession(opCtx, sess)
		cancel()
		if err != nil {
			fmt.Printf("⚠️ Async session upsert failed (%s): %v\n", sess.SessionID, err)
			return
		}
		s.lastSuccessNS.Store(time.Now().UnixNano())
	}

	drainAll := func(ctx context.Context) {
		for {
			if ctx.Err() != nil {
				return
			}
			sess, ok := s.sessionCoalescer.pop()
			if !ok {
				return
			}
			process(ctx, sess)
		}
	}

	for {
		select {
		case <-s.sessionCoalescer.notifyCh:
			drainAll(context.Background())
		case <-s.asyncStopCh:
			drainCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownDrainTimeout)
			drainAll(drainCtx)
			cancel()
			return
		}
	}
}

func (s *logStore) miscAsyncWorker() {
	defer s.asyncWg.Done()

	process := func(ctx context.Context, job asyncWriteJob) {
		opCtx, cancel := withDefaultTimeout(ctx, defaultDBOpTimeout)
		defer cancel()

		var err error
		switch job.kind {
		case asyncWriteDialog:
			err = s.execUpsertDialog(opCtx, job.dialog)
			if err != nil {
				fmt.Printf("⚠️ Async dialog upsert failed (%s): %v\n", job.dialog.SessionID, err)
				return
			}
		case asyncWriteSessionDirectory:
			err = s.execUpsertSessionDirectory(opCtx, job.sessionID, job.ownerInstanceID, job.wsURL, job.ttlSeconds)
			if err != nil {
				fmt.Printf("⚠️ Async session directory upsert failed (%s): %v\n", job.sessionID, err)
				return
			}
		default:
			return
		}
		s.lastSuccessNS.Store(time.Now().UnixNano())
	}

	for {
		select {
		case job := <-s.miscQueue:
			process(context.Background(), job)
		case <-s.asyncStopCh:
			drainCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownDrainTimeout)
			defer cancel()
			for {
				if drainCtx.Err() != nil {
					return
				}
				select {
				case job := <-s.miscQueue:
					process(drainCtx, job)
				default:
					return
				}
			}
		}
	}
}

func (s *logStore) execStorePayload(ctx context.Context, payload *PayloadRecord) (int64, error) {
	if s.storePayloadHook != nil {
		return s.storePayloadHook(ctx, payload)
	}
	return s.StorePayload(ctx, payload)
}

func (s *logStore) execUpsertSession(ctx context.Context, sess *SessionRecord) error {
	if s.upsertSessionHook != nil {
		return s.upsertSessionHook(ctx, sess)
	}
	return s.UpsertSession(ctx, sess)
}

func (s *logStore) execUpsertDialog(ctx context.Context, dialog *DialogRecord) error {
	if s.upsertDialogHook != nil {
		return s.upsertDialogHook(ctx, dialog)
	}
	return s.UpsertDialog(ctx, dialog)
}

func (s *logStore) execUpsertSessionDirectory(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error {
	if s.upsertSessionDirHook != nil {
		return s.upsertSessionDirHook(ctx, sessionID, ownerInstanceID, wsURL, ttlSeconds)
	}
	return s.UpsertSessionDirectory(ctx, sessionID, ownerInstanceID, wsURL, ttlSeconds)
}

// sessionCoalescer maintains FIFO ordering of pending session snapshots with
// per-session_id coalescing and terminal ("ended") state monotonicity.
type sessionCoalescer struct {
	mu       sync.Mutex
	capacity int
	order    []string
	pending  map[string]*SessionRecord
	endedAt  map[string]time.Time
	notifyCh chan struct{}
}

func newSessionCoalescer(capacity int) *sessionCoalescer {
	if capacity <= 0 {
		capacity = defaultSessionQueueCapacity
	}
	return &sessionCoalescer{
		capacity: capacity,
		order:    make([]string, 0, min(capacity, 256)),
		pending:  make(map[string]*SessionRecord),
		endedAt:  make(map[string]time.Time),
		notifyCh: make(chan struct{}, 1),
	}
}

func (c *sessionCoalescer) enqueue(incoming *SessionRecord) bool {
	if incoming == nil || incoming.SessionID == "" {
		return true
	}

	incomingTerminal := isTerminalSessionRecord(incoming)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	id := incoming.SessionID
	if !incomingTerminal {
		if _, alreadyEnded := c.endedAt[id]; alreadyEnded {
			// Terminal state already observed for this session; drop stale non-ended snapshot.
			return true
		}
		if existing, ok := c.pending[id]; ok {
			if isTerminalSessionRecord(existing) {
				return true
			}
			c.pending[id] = mergeSessionSnapshot(existing, incoming)
			return true
		}
	} else {
		c.recordEndedLocked(id, now)
		if existing, ok := c.pending[id]; ok {
			c.pending[id] = mergeTerminalSessionSnapshot(existing, incoming)
			return true
		}
	}

	if len(c.order) >= c.capacity {
		return false
	}

	c.order = append(c.order, id)
	c.pending[id] = incoming
	select {
	case c.notifyCh <- struct{}{}:
	default:
	}
	return true
}

func (c *sessionCoalescer) pop() (*SessionRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.order) == 0 {
		return nil, false
	}

	id := c.order[0]
	c.order[0] = ""
	c.order = c.order[1:]
	if len(c.order) == 0 {
		c.order = c.order[:0]
	}

	rec := c.pending[id]
	delete(c.pending, id)
	return rec, rec != nil
}

func (c *sessionCoalescer) recordEndedLocked(sessionID string, now time.Time) {
	if len(c.endedAt) >= defaultEndedHistoryMaxEntries {
		cutoff := now.Add(-defaultEndedHistoryTTL)
		for id, ts := range c.endedAt {
			if ts.Before(cutoff) {
				delete(c.endedAt, id)
			}
		}
		// Hard bound in case bursts exceed max entries within TTL.
		if len(c.endedAt) >= defaultEndedHistoryMaxEntries {
			clear(c.endedAt)
		}
	}
	c.endedAt[sessionID] = now
}

func isTerminalSessionRecord(sess *SessionRecord) bool {
	if sess == nil {
		return false
	}
	return sess.EndedAt != nil || strings.EqualFold(strings.TrimSpace(sess.FinalState), "ended")
}

func mergeSessionSnapshot(existing, incoming *SessionRecord) *SessionRecord {
	merged := *incoming
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = existing.CreatedAt
	}
	if merged.UpdatedAt.Before(existing.UpdatedAt) {
		merged.UpdatedAt = existing.UpdatedAt
	}
	if merged.Direction == "" {
		merged.Direction = existing.Direction
	}
	if merged.FromURI == "" {
		merged.FromURI = existing.FromURI
	}
	if merged.ToURI == "" {
		merged.ToURI = existing.ToURI
	}
	if merged.SIPCallID == "" {
		merged.SIPCallID = existing.SIPCallID
	}
	if merged.AuthMode == "" {
		merged.AuthMode = existing.AuthMode
	}
	if merged.TrunkID == nil && existing.TrunkID != nil {
		id := *existing.TrunkID
		merged.TrunkID = &id
	}
	if merged.TrunkName == "" {
		merged.TrunkName = existing.TrunkName
	}
	if merged.SIPUsername == "" {
		merged.SIPUsername = existing.SIPUsername
	}
	if merged.Meta == nil && existing.Meta != nil {
		merged.Meta = cloneMap(existing.Meta)
	}
	return &merged
}

func mergeTerminalSessionSnapshot(existing, incoming *SessionRecord) *SessionRecord {
	merged := mergeSessionSnapshot(existing, incoming)
	merged.FinalState = "ended"
	if existing.EndedAt != nil {
		t := *existing.EndedAt
		merged.EndedAt = &t
	} else if incoming.EndedAt != nil {
		t := *incoming.EndedAt
		merged.EndedAt = &t
	}
	switch {
	case incoming.EndReason != "" && !strings.EqualFold(incoming.EndReason, "ended"):
		merged.EndReason = incoming.EndReason
	case existing.EndReason != "":
		merged.EndReason = existing.EndReason
	default:
		merged.EndReason = incoming.EndReason
	}
	return merged
}

func cloneSessionRecord(sess *SessionRecord) *SessionRecord {
	if sess == nil {
		return nil
	}
	cp := *sess
	if sess.EndedAt != nil {
		t := *sess.EndedAt
		cp.EndedAt = &t
	}
	if sess.TrunkID != nil {
		id := *sess.TrunkID
		cp.TrunkID = &id
	}
	cp.Meta = cloneMap(sess.Meta)
	return &cp
}

func clonePayloadRecord(payload *PayloadRecord) *PayloadRecord {
	if payload == nil {
		return nil
	}
	cp := *payload
	cp.Parsed = cloneMap(payload.Parsed)
	return &cp
}

func cloneEvent(event *Event) *Event {
	if event == nil {
		return nil
	}
	cp := *event
	if event.PayloadID != nil {
		id := *event.PayloadID
		cp.PayloadID = &id
	}
	cp.Data = cloneMap(event.Data)
	return &cp
}

func cloneDialogRecord(dialog *DialogRecord) *DialogRecord {
	if dialog == nil {
		return nil
	}
	cp := *dialog
	if dialog.RouteSet != nil {
		cp.RouteSet = append([]string(nil), dialog.RouteSet...)
	}
	return &cp
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
