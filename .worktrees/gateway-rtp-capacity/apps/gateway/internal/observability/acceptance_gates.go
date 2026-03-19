package observability

type AcceptanceGates struct {
	SetupSuccessRateMin                float64
	SetupLatencyP95MsMax               float64
	RTPForwardLoopP95MsMax             float64
	CPUSustained5mMaxPercent           float64
	MemoryMaxPercent                   float64
	GoroutineLeakSlopeMax              float64
	WSQueueFullPer1000SessionMinutesMax float64
	ContinuousQualityAlarmWindowSecMax float64
}

func DefaultAcceptanceGates() AcceptanceGates {
	return AcceptanceGates{
		SetupSuccessRateMin:                  0.99,
		SetupLatencyP95MsMax:                 2500,
		RTPForwardLoopP95MsMax:               20,
		CPUSustained5mMaxPercent:             75,
		MemoryMaxPercent:                     80,
		GoroutineLeakSlopeMax:                0,
		WSQueueFullPer1000SessionMinutesMax:  1,
		ContinuousQualityAlarmWindowSecMax:   15,
	}
}
