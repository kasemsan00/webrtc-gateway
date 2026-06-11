package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestHandleREFERRespondsNotImplementedWithoutAffectingMedia(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-refer")
	req := sip.NewRequest(sip.REFER, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-refer"))
	tx := &midCallServerTxStub{}

	srv.handleREFER(req, tx)

	assertResponseCodes(t, tx, []int{501})
	if sess.GetState() != "active" {
		t.Fatalf("expected REFER policy not to affect active media state, got %s", sess.GetState())
	}
}

func TestHandlePRACKRespondsBadExtension(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-prack")
	req := sip.NewRequest(sip.PRACK, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-prack"))
	tx := &midCallServerTxStub{}

	srv.handlePRACK(req, tx)

	assertResponseCodes(t, tx, []int{420})
}

func TestHandleUPDATERejectsUnsupportedReliabilityAndSessionTimer(t *testing.T) {
	srv, _ := newMidCallTestServer(t, "call-update-extensions")

	t.Run("require 100rel", func(t *testing.T) {
		req := newMidCallUpdateRequest("call-update-extensions", "")
		req.AppendHeader(sip.NewHeader("Require", "100rel"))
		tx := &midCallServerTxStub{}

		srv.handleUPDATE(req, tx)

		assertResponseCodes(t, tx, []int{420})
	})

	t.Run("session timer", func(t *testing.T) {
		req := newMidCallUpdateRequest("call-update-extensions", "")
		req.AppendHeader(sip.NewHeader("Session-Expires", "90"))
		tx := &midCallServerTxStub{}

		srv.handleUPDATE(req, tx)

		assertResponseCodes(t, tx, []int{501})
	})
}
