package session

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/telemetry"
)

func TestPacketLoopsContainNoTelemetryCallsOrAttributes(t *testing.T) {
	paths := []string{"rtp_forward.go", "rtp_cache.go", "keyframe.go", "video_feedback.go", "video_recovery.go", "rtp_disorder.go"}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.SelectorExpr:
				if identifier, ok := value.X.(*ast.Ident); ok && identifier.Name == "telemetry" {
					t.Errorf("%s calls telemetry.%s", path, value.Sel.Name)
				}
			case *ast.BasicLit:
				lower := strings.ToLower(value.Value)
				for _, forbidden := range []string{"session.id", "call_id", "username", "device", "error.message", "url"} {
					if strings.Contains(lower, forbidden) {
						t.Errorf("%s contains high-cardinality telemetry attribute %q", path, forbidden)
					}
				}
			}
			return true
		})
	}
}

func TestPacketCacheAllocationsUnaffectedByTelemetryEnablement(t *testing.T) {
	measure := func() float64 {
		sess := &Session{VideoRTPHistorySize: 8}
		sess.initVideoRTPHistory()
		packet := make([]byte, 1200)
		return testing.AllocsPerRun(1000, func() { sess.CacheVideoRTPPacket(42, packet) })
	}
	disabled, err := telemetry.New(context.Background(), config.ObservabilityConfig{Enable: false}, "packet-test", "test")
	if err != nil {
		t.Fatal(err)
	}
	disabledAllocs := measure()
	if err := disabled.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer collector.Close()
	cfg := config.ObservabilityConfig{Enable: true, Endpoint: collector.URL, Protocol: "http/protobuf", ServiceName: "packet-test", Environment: "test", MetricsIntervalMS: 1000, TracesEnable: true, TraceSampleRatio: 1, MaxQueueSize: 4, MaxExportBatchSize: 2, ScheduleDelayMS: 10, ExportTimeoutMS: 50, ShutdownTimeoutMS: 100}
	enabled, err := telemetry.New(context.Background(), cfg, "packet-test", "test")
	if err != nil {
		t.Fatal(err)
	}
	enabledAllocs := measure()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = enabled.Shutdown(ctx)
	if enabledAllocs > disabledAllocs {
		t.Fatalf("telemetry changed packet-cache allocations: disabled=%.2f enabled=%.2f", disabledAllocs, enabledAllocs)
	}
}

func TestRollbackDisabledTelemetryPreservesCallAndPacketPaths(t *testing.T) {
	disabled, err := telemetry.New(context.Background(), config.ObservabilityConfig{Enable: false}, "rollback-test", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disabled.Shutdown(context.Background()) }()
	sess := &Session{ID: "rollback-session", State: StateNew, VideoRTPHistorySize: 8}
	sess.initVideoRTPHistory()
	sess.SetCallInfo("outbound", "from", "to", "call-id")
	sess.SetState(StateConnecting)
	sess.CacheVideoRTPPacket(1, make([]byte, 120))
	if got := sess.getCachedVideoRTPPacket(1); len(got) != 120 {
		t.Fatalf("packet cache unavailable after telemetry disable: %d", len(got))
	}
	sess.SetState(StateActive)
	sess.SetState(StateEnded)
	if sess.GetState() != StateEnded {
		t.Fatalf("call state after rollback = %s", sess.GetState())
	}
}

func BenchmarkRTPPacketCacheTelemetryGuard(b *testing.B) {
	sess := &Session{VideoRTPHistorySize: 512}
	sess.initVideoRTPHistory()
	packet := make([]byte, 1200)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		sess.CacheVideoRTPPacket(uint16(index), packet)
	}
}
