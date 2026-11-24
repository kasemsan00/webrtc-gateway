package api

import (
	"testing"
	"time"

	"webrtc-sip-gateway/internal/session"
)

func waitForReports(t *testing.T, maker *screenStreamSIPMaker, want ...bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := maker.reported()
		if len(got) == len(want) {
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("reports to the sharing leg = %v, want %v", got, want)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("reports to the sharing leg = %v, want %v", maker.reported(), want)
}

func TestCanReceiveAuxVideoFollowsClientOptIn(t *testing.T) {
	rig := newStreamTestRig(t)
	if !rig.srv.CanReceiveAuxVideo(rig.sess.ID) {
		t.Fatal("an opted-in client can receive a second video")
	}
	rig.client.multiVideo = false
	if rig.srv.CanReceiveAuxVideo(rig.sess.ID) {
		t.Fatal("a client that did not opt in cannot")
	}
	rig.client.multiVideo = true
	rig.client.publicOnly = true
	if rig.srv.CanReceiveAuxVideo(rig.sess.ID) {
		t.Fatal("a public client cannot")
	}
	if rig.srv.CanReceiveAuxVideo("no-such-session") {
		t.Fatal("a session without a client cannot")
	}
}

func TestNotifyAuxVideoOfferedAsksClientToRenegotiate(t *testing.T) {
	rig := newStreamTestRig(t)

	rig.srv.NotifyAuxVideoOffered(rig.sess.ID)

	msg := readWSTestMessage(t, rig.client)
	if msg.Type != "renegotiate" || msg.Reason != "stream_added" || msg.Role != "screen" ||
		!msg.RequiresAnswer || msg.SDP != "" || msg.RenegotiationID == "" || msg.SessionID != rig.sess.ID {
		t.Fatalf("unexpected renegotiate message: %+v", msg)
	}
	pending, ok := rig.sess.GetPendingMidCallRenegotiation()
	if !ok || pending.ID != msg.RenegotiationID || pending.Source != session.MidCallRenegotiationSourceStreamAdded {
		t.Fatalf("pending renegotiation = %+v ok=%v, want the stream_added one", pending, ok)
	}
	if got := rig.maker.reported(); len(got) != 0 {
		t.Fatalf("nothing is reported before the client answers, got %v", got)
	}
}

func TestNotifyAuxVideoOfferedWithoutClientReportsUnsupported(t *testing.T) {
	rig := newStreamTestRig(t)
	delete(rig.srv.wsClients, rig.sess.ID)

	rig.srv.NotifyAuxVideoOffered(rig.sess.ID)

	waitForReports(t, rig.maker, false)
	assertNoWSTestMessage(t, rig.client)
}

func TestNotifyAuxVideoOfferedWhileBusyReportsUnsupported(t *testing.T) {
	rig := newStreamTestRig(t)
	if _, ok := rig.sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{Source: "sip_reinvite"}); !ok {
		t.Fatal("could not start a pending renegotiation")
	}

	rig.srv.NotifyAuxVideoOffered(rig.sess.ID)

	waitForReports(t, rig.maker, false)
	assertNoWSTestMessage(t, rig.client)
}

func (r *streamTestRig) offerStream(t *testing.T) WSMessage {
	t.Helper()
	r.srv.NotifyAuxVideoOffered(r.sess.ID)
	return readWSTestMessage(t, r.client)
}

func TestStreamAddedAnswerOutcomesAreReportedToSharingLeg(t *testing.T) {
	for _, tc := range []struct {
		name       string
		answer     string
		wantStatus string
		wantReason string
	}{
		{name: "client declines", answer: `"status":"failed","reason":"no_decoder"`, wantStatus: "failed", wantReason: "no_decoder"},
		{name: "client sends no offer", answer: `"status":"ok"`, wantStatus: "failed", wantReason: "sdp_required"},
		{name: "offer cannot be applied", answer: `"status":"ok","sdp":"v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n"`, wantStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newStreamTestRig(t)
			if _, err := rig.sess.OpenAuxVideo(nil, nil, 12004, 12005); err != nil {
				t.Fatalf("OpenAuxVideo: %v", err)
			}
			renegotiate := rig.offerStream(t)

			rig.srv.handleWSMessage(rig.client, []byte(`{"type":"renegotiate_answer","sessionId":"`+rig.sess.ID+
				`","renegotiationId":"`+renegotiate.RenegotiationID+`",`+tc.answer+`}`))

			result := readWSTestMessage(t, rig.client)
			if result.Type != "renegotiate_result" || result.Status != tc.wantStatus || result.RenegotiationID != renegotiate.RenegotiationID {
				t.Fatalf("unexpected result: %+v", result)
			}
			if tc.wantReason != "" && result.Reason != tc.wantReason {
				t.Fatalf("result reason = %q, want %q", result.Reason, tc.wantReason)
			}
			if _, pending := rig.sess.GetPendingMidCallRenegotiation(); pending {
				t.Fatal("the renegotiation claim must be released")
			}
			if rig.sess.AuxVideo() != nil {
				t.Fatal("the receiving auxiliary stream must be closed after failure")
			}
			waitForReports(t, rig.maker, false)
			if rig.sess.GetState() != session.StateActive {
				t.Fatalf("call state = %s, want active", rig.sess.GetState())
			}
		})
	}
}

func TestStreamAddedTimeoutIsReportedToSharingLeg(t *testing.T) {
	rig := newStreamTestRig(t)
	if _, err := rig.sess.OpenAuxVideo(nil, nil, 12004, 12005); err != nil {
		t.Fatalf("OpenAuxVideo: %v", err)
	}
	started, ok := rig.sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		Source: session.MidCallRenegotiationSourceStreamAdded,
	})
	if !ok {
		t.Fatal("could not start the renegotiation")
	}

	rig.srv.scheduleMidCallRenegotiationTimeout(rig.sess.ID, started.ID, 20*time.Millisecond)

	result := readWSTestMessage(t, rig.client)
	if result.Type != "renegotiate_result" || result.Status != "timeout" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if rig.sess.AuxVideo() != nil {
		t.Fatal("the receiving auxiliary stream must be closed after timeout")
	}
	waitForReports(t, rig.maker, false)
}

func TestOtherRenegotiationTimeoutIsNotReportedAsScreenRefusal(t *testing.T) {
	rig := newStreamTestRig(t)
	started, ok := rig.sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{Source: "sip_reinvite"})
	if !ok {
		t.Fatal("could not start the renegotiation")
	}

	rig.srv.scheduleMidCallRenegotiationTimeout(rig.sess.ID, started.ID, 20*time.Millisecond)

	if result := readWSTestMessage(t, rig.client); result.Status != "timeout" {
		t.Fatalf("unexpected result: %+v", result)
	}
	time.Sleep(50 * time.Millisecond)
	if got := rig.maker.reported(); len(got) != 0 {
		t.Fatalf("a camera renegotiation timeout must not touch the screen stream, got %v", got)
	}
}

func TestResumeWithoutScreenSectionEndsTheStream(t *testing.T) {

	t.Run("nothing lost", func(t *testing.T) {
		rig := newStreamTestRig(t)
		rig.srv.handleAuxAfterResume(rig.sess)
		time.Sleep(30 * time.Millisecond)
		if _, closed, _ := rig.maker.counts(); closed != 0 || len(rig.maker.reported()) != 0 {
			t.Fatal("a resume without an open stream must not touch SIP")
		}
		assertNoWSTestMessage(t, rig.client)
	})
	t.Run("sharing session closes its SIP m-line", func(t *testing.T) {
		rig := newStreamTestRig(t)
		rig.sess.SetAuxResumeOutcomeForTest(session.AuxResumeOutcome{Lost: true, Sending: true})

		rig.srv.handleAuxAfterResume(rig.sess)

		msg := readWSTestMessage(t, rig.client)
		if msg.Type != "stream_state" || msg.Active == nil || *msg.Active || msg.Reason != "resume" {
			t.Fatalf("unexpected stream_state: %+v", msg)
		}
		if _, closed, reason := rig.maker.counts(); closed != 1 || reason != "resume" {
			t.Fatalf("CloseScreenStream calls = %d reason=%q, want 1 resume", closed, reason)
		}
	})
	t.Run("receiving session tells the sharing leg", func(t *testing.T) {
		rig := newStreamTestRig(t)
		rig.sess.SetAuxResumeOutcomeForTest(session.AuxResumeOutcome{Lost: true})

		rig.srv.handleAuxAfterResume(rig.sess)

		if msg := readWSTestMessage(t, rig.client); msg.Type != "stream_state" || msg.Reason != "resume" {
			t.Fatalf("unexpected stream_state: %+v", msg)
		}
		waitForReports(t, rig.maker, false)
		if _, closed, _ := rig.maker.counts(); closed != 0 {
			t.Fatal("the receiving leg does not close the sharer's m-line itself")
		}
	})
}
