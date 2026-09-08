package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/telemetry"
)

func TestFakeCollectorFaultsDoNotBlockCallControlAndRecover(t *testing.T) {
	const (
		healthy int32 = iota
		slow
		refused
	)
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		switch mode.Load() {
		case slow:
			<-request.Context().Done()
			return
		case refused:
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
			writer.Header().Set("Content-Type", "application/x-protobuf")
			writer.WriteHeader(http.StatusOK)
		}
	}))

	cfg := config.ObservabilityConfig{
		Enable: true, Endpoint: server.URL, Protocol: "http/protobuf", ServiceName: "fault-test", Environment: "test",
		MetricsIntervalMS: 1000, TracesEnable: true, TraceSampleRatio: 1, MaxQueueSize: 4, MaxExportBatchSize: 1,
		ScheduleDelayMS: 5, ExportTimeoutMS: 50, ShutdownTimeoutMS: 100,
	}
	runtime, err := telemetry.New(context.Background(), cfg, "instance-fault", "test")
	if err != nil {
		t.Fatal(err)
	}
	emit := func(name string) {
		_, span := runtime.Tracer().Start(context.Background(), name)
		span.End()
	}
	wait := func(description string, condition func(telemetry.HealthSnapshot) bool) telemetry.HealthSnapshot {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			snapshot := runtime.Health()
			if condition(snapshot) {
				return snapshot
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s; health=%#v", description, runtime.Health())
		return telemetry.HealthSnapshot{}
	}

	mode.Store(healthy)
	emit("healthy")
	wait("healthy ingest", func(snapshot telemetry.HealthSnapshot) bool {
		return snapshot.State == "connected" && snapshot.Queue.Exported > 0
	})

	mode.Store(slow)
	beforeFailures := runtime.Health().Queue.Failed
	emit("slow")
	started := time.Now()
	call := &session.Session{ID: "call-under-export-fault", State: session.StateNew}
	call.SetCallInfo("outbound", "private-from", "private-to", "private-call-id")
	call.SetState(session.StateConnecting)
	call.SetState(session.StateActive)
	call.SetState(session.StateEnded)
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("call control blocked behind slow collector for %v", elapsed)
	}
	wait("slow failure", func(snapshot telemetry.HealthSnapshot) bool { return snapshot.Queue.Failed > beforeFailures })

	mode.Store(refused)
	beforeFailures = runtime.Health().Queue.Failed
	emit("refused")
	wait("collector refusal", func(snapshot telemetry.HealthSnapshot) bool { return snapshot.Queue.Failed > beforeFailures })

	mode.Store(healthy)
	lastFailure := runtime.Health().LastFailureAt
	emit("recovery")
	wait("collector recovery", func(snapshot telemetry.HealthSnapshot) bool {
		return snapshot.State == "connected" && snapshot.LastSuccessAt != nil && lastFailure != nil && snapshot.LastSuccessAt.After(*lastFailure)
	})

	server.Close()
	beforeFailures = runtime.Health().Queue.Failed
	emit("disconnect")
	wait("collector disconnect", func(snapshot telemetry.HealthSnapshot) bool { return snapshot.Queue.Failed > beforeFailures })

	beforeDropped := runtime.Health().Queue.Dropped
	started = time.Now()
	for index := 0; index < 64; index++ {
		emit("saturate")
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("queue saturation blocked producers for %v", elapsed)
	}
	wait("queue saturation", func(snapshot telemetry.HealthSnapshot) bool { return snapshot.Queue.Dropped > beforeDropped })

	shutdownStarted := time.Now()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runtime.Shutdown(shutdownCtx)
	if elapsed := time.Since(shutdownStarted); elapsed > 400*time.Millisecond {
		t.Fatalf("shutdown exceeded bounded budget: %v", elapsed)
	}
}
