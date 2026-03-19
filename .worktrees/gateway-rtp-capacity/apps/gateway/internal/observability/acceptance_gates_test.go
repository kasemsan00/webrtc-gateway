package observability

import "testing"

func TestAcceptanceGates_DefaultThresholds(t *testing.T) {
	g := DefaultAcceptanceGates()
	if g.SetupSuccessRateMin != 0.99 {
		t.Fatalf("want 0.99 got %v", g.SetupSuccessRateMin)
	}
	if g.SetupLatencyP95MsMax != 2500 {
		t.Fatalf("want 2500 got %v", g.SetupLatencyP95MsMax)
	}
	if g.RTPForwardLoopP95MsMax != 20 {
		t.Fatalf("want 20 got %v", g.RTPForwardLoopP95MsMax)
	}
	if g.CPUSustained5mMaxPercent != 75 {
		t.Fatalf("want 75 got %v", g.CPUSustained5mMaxPercent)
	}
	if g.MemoryMaxPercent != 80 {
		t.Fatalf("want 80 got %v", g.MemoryMaxPercent)
	}
	if g.GoroutineLeakSlopeMax != 0 {
		t.Fatalf("want 0 got %v", g.GoroutineLeakSlopeMax)
	}
	if g.WSQueueFullPer1000SessionMinutesMax != 1 {
		t.Fatalf("want 1 got %v", g.WSQueueFullPer1000SessionMinutesMax)
	}
	if g.ContinuousQualityAlarmWindowSecMax != 15 {
		t.Fatalf("want 15 got %v", g.ContinuousQualityAlarmWindowSecMax)
	}
}
