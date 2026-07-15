package session

import "fmt"

// BindH264AUParameterSetSeeder registers the live SIP→WebRTC access-unit
// normalizer so cached SPS/PPS can be seeded while a switch gate is active.
func (s *Session) BindH264AUParameterSetSeeder(seeder func(sps, pps []byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.h264AUParameterSetSeeder = seeder
}

// ArmSwitchSPSPPSInject enables uplink SPS/PPS injection for the next count IDRs.
func (s *Session) ArmSwitchSPSPPSInject(count int) {
	if count <= 0 {
		return
	}
	s.mu.Lock()
	s.SwitchSPSPPSInjectRemaining = count
	s.mu.Unlock()
	fmt.Printf("[%s] switch_sps_pps_inject_armed remaining=%d\n", s.ID, count)
}

// ArmSwitchSPSPPSInjectIfAuthoritative arms injection only for the current switch token.
func (s *Session) ArmSwitchSPSPPSInjectIfAuthoritative(generation int, mediaEpoch uint64, count int) bool {
	if count <= 0 {
		return s.IsSwitchVideoAuthority(generation, mediaEpoch)
	}
	s.mu.Lock()
	if s.MediaEpoch != mediaEpoch || s.SwitchGeneration != generation {
		s.mu.Unlock()
		return false
	}
	s.SwitchSPSPPSInjectRemaining = count
	s.mu.Unlock()
	fmt.Printf("[%s] switch_sps_pps_inject_armed generation=%d remaining=%d\n", s.ID, generation, count)
	return true
}

func (s *Session) maybeSeedH264AUNormalizerFromSIPCacheLocked() (func(sps, pps []byte), []byte, []byte) {
	if !s.SwitchVideoGateActive || len(s.SIPCachedSPS) == 0 || len(s.SIPCachedPPS) == 0 || s.h264AUParameterSetSeeder == nil {
		return nil, nil, nil
	}
	sps := append([]byte(nil), s.SIPCachedSPS...)
	pps := append([]byte(nil), s.SIPCachedPPS...)
	return s.h264AUParameterSetSeeder, sps, pps
}
