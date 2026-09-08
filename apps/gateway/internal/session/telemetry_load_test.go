package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/telemetry"
)

type loadSample struct {
	name       string
	elapsed    time.Duration
	allocs     uint64
	goroutines int
}

func TestConcurrentCallAndMediaTelemetryRegimes(t *testing.T) {
	disabled := measureCallMediaLoad(t, "disabled", config.ObservabilityConfig{Enable: false})
	healthyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	}))
	defer healthyServer.Close()
	healthy := measureCallMediaLoad(t, "healthy", enabledLoadConfig(healthyServer.URL, 2048, 50))

	unavailableServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	unavailableURL := unavailableServer.URL
	unavailableServer.Close()
	unavailable := measureCallMediaLoad(t, "unavailable", enabledLoadConfig(unavailableURL, 2048, 50))

	block := make(chan struct{})
	saturatedServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block
	}))
	defer saturatedServer.Close()
	defer close(block)
	saturated := measureCallMediaLoad(t, "saturated", enabledLoadConfig(saturatedServer.URL, 1, 500))

	baseline := disabled.elapsed
	if baseline <= 0 {
		t.Fatal("disabled baseline produced no work")
	}
	for _, sample := range []loadSample{healthy, unavailable, saturated} {
		if sample.elapsed > 2*time.Second {
			t.Fatalf("%s call/media load took %v, exceeding 2s absolute budget", sample.name, sample.elapsed)
		}
		if sample.elapsed > 3*baseline+50*time.Millisecond {
			t.Fatalf("%s elapsed %v exceeded 3x disabled baseline %v", sample.name, sample.elapsed, baseline)
		}
		if sample.goroutines > 80 {
			t.Fatalf("%s leftover goroutines = %d", sample.name, sample.goroutines)
		}
		if sample.allocs > disabled.allocs*4+1_000_000 {
			t.Fatalf("%s allocs %d exceeded disabled baseline %d", sample.name, sample.allocs, disabled.allocs)
		}
	}
}

func enabledLoadConfig(endpoint string, queue, timeoutMS int) config.ObservabilityConfig {
	return config.ObservabilityConfig{
		Enable: true, Endpoint: endpoint, Protocol: "http/protobuf", ServiceName: "load-test", Environment: "test",
		MetricsIntervalMS: 1000, TracesEnable: true, TraceSampleRatio: 1, MaxQueueSize: queue, MaxExportBatchSize: 1,
		ScheduleDelayMS: 10, ExportTimeoutMS: timeoutMS, ShutdownTimeoutMS: 100,
	}
}

func measureCallMediaLoad(t *testing.T, name string, cfg config.ObservabilityConfig) loadSample {
	t.Helper()
	runtime.GC()
	before := runtime.NumGoroutine()
	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)
	tel, err := telemetry.New(context.Background(), cfg, "load-"+name, "test")
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 200)
	started := time.Now()
	var waitGroup sync.WaitGroup
	for index := 0; index < 16; index++ {
		waitGroup.Add(1)
		go func(id int) {
			defer waitGroup.Done()
			sess := &Session{ID: "load-session", State: StateNew, VideoRTPHistorySize: 8}
			sess.initVideoRTPHistory()
			sess.SetCallInfo("outbound", "from", "to", "call-id")
			sess.SetState(StateConnecting)
			for seq := 0; seq < 128; seq++ {
				sess.CacheVideoRTPPacket(uint16(seq), packet)
			}
			sess.SetState(StateActive)
			sess.SetState(StateEnded)
			_ = id
		}(index)
	}
	waitGroup.Wait()
	elapsed := time.Since(started)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = tel.Shutdown(shutdownCtx)
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)
	delta := runtime.NumGoroutine() - before
	if delta < 0 {
		delta = 0
	}
	return loadSample{name: name, elapsed: elapsed, allocs: endMem.TotalAlloc - startMem.TotalAlloc, goroutines: delta}
}
