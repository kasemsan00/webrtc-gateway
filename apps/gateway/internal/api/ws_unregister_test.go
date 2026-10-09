package api

import (
	"errors"
	"testing"

	"webrtc-sip-gateway/internal/auth"
	"webrtc-sip-gateway/internal/sip"
)

func mobileLogoutFixture(t *testing.T) (*Server, *agentTrunkManagerStub, *WSClient) {
	t.Helper()
	name, _ := sip.BuildMobileTrunkName("mobile-user")
	tm := &agentTrunkManagerStub{owned: true, trunk: &sip.Trunk{ID: 100, Name: name, Enabled: true}}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()
	client.agentDeviceOnly = false
	client.authClaims = &auth.VerifiedClaims{Realm: auth.TokenRealmUser, Subject: "mobile-user"}
	client.mobileTrunkID = 100
	client.resolvedTrunkID = 100
	client.trunkResolved = true
	srv.wsConnections[client] = struct{}{}
	return srv, tm, client
}

func TestMobileUnregisterReleasesProvisionedTrunkAndAcknowledges(t *testing.T) {
	srv, tm, client := mobileLogoutFixture(t)
	// A supplied ID must never redirect unregister to somebody else's trunk.
	srv.handleWSUnregister(client, WSMessage{Type: "unregister", TrunkID: 999, RequestID: "logout-1"})
	messages := readAgentWSMessages(t, client)
	if tm.unregisterID != 100 || !tm.fcmCleared || tm.notifyUserID != nil || client.trunkResolved {
		t.Fatalf("logout did not release the provisioned trunk")
	}
	if len(messages) != 1 || messages[0].Type != "unregistered" || messages[0].RequestID != "logout-1" {
		t.Fatalf("bad ack: %+v", messages)
	}
	srv.handleWSUnregister(client, WSMessage{Type: "unregister", RequestID: "retry"})
	messages = readAgentWSMessages(t, client)
	if len(messages) != 1 || messages[0].Type != "unregistered" {
		t.Fatalf("retry must be idempotent: %+v", messages)
	}
}

func TestMobileUnregisterFailureRetainsRetryTargetAndNeverAcknowledgesSuccess(t *testing.T) {
	for _, failure := range []string{"sip", "push"} {
		t.Run(failure, func(t *testing.T) {
			srv, tm, client := mobileLogoutFixture(t)
			if failure == "sip" {
				tm.unregisterErr = errors.New("registrar unavailable")
			} else {
				tm.fcmClearErr = errors.New("database unavailable")
			}
			srv.handleWSUnregister(client, WSMessage{Type: "unregister", RequestID: "failed"})
			messages := readAgentWSMessages(t, client)
			if len(messages) != 1 || messages[0].Type != "error" || messages[0].Operation != "unregister" || messages[0].RequestID != "failed" {
				t.Fatalf("false success: %+v", messages)
			}
			if client.mobileTrunkID != 100 || !client.trunkResolved {
				t.Fatal("retry authority lost")
			}
			tm.unregisterErr = nil
			tm.fcmClearErr = nil
			srv.handleWSUnregister(client, WSMessage{Type: "unregister", RequestID: "retry"})
			if messages = readAgentWSMessages(t, client); len(messages) != 1 || messages[0].Type != "unregistered" {
				t.Fatalf("retry failed: %+v", messages)
			}
		})
	}
}

func TestMobileUnregisterRejectsWrongOwnerAndSharedRegistration(t *testing.T) {
	for _, scenario := range []string{"wrong_owner", "public", "unprovisioned", "sibling"} {
		t.Run(scenario, func(t *testing.T) {
			srv, tm, client := mobileLogoutFixture(t)
			switch scenario {
			case "wrong_owner":
				client.authClaims.Subject = "somebody-else"
			case "public":
				client.publicOnly = true
			case "unprovisioned":
				client.mobileTrunkID = 0
			case "sibling":
				srv.wsConnections[&WSClient{mobileTrunkID: 100}] = struct{}{}
			}
			srv.handleWSUnregister(client, WSMessage{Type: "unregister"})
			messages := readAgentWSMessages(t, client)
			if tm.unregisterCount != 0 || len(messages) != 1 || messages[0].Type != "error" {
				t.Fatalf("unsafe release: %+v", messages)
			}
		})
	}
}

func TestDeviceUnregisterFailurePreservesBindingForRetry(t *testing.T) {
	tm := &agentTrunkManagerStub{unregisterErr: errors.New("SIP failed")}
	srv := newAgentDeviceTestServer(t, tm)
	client := newAgentDeviceWSClient()
	srv.bindAgentClient(client, 100)
	srv.handleWSDeviceUnregister(client, WSMessage{Type: "unregister", RequestID: "device-logout"})
	messages := readAgentWSMessages(t, client)
	if len(messages) != 1 || messages[0].Type != "error" || messages[0].RequestID != "device-logout" || !client.trunkResolved {
		t.Fatalf("binding lost or false ack: %+v", messages)
	}
	tm.unregisterErr = nil
	srv.handleWSDeviceUnregister(client, WSMessage{Type: "unregister"})
	messages = readAgentWSMessages(t, client)
	if len(messages) != 1 || messages[0].Type != "unregistered" || client.trunkResolved {
		t.Fatalf("retry failed: %+v", messages)
	}
}

func TestMobileUnregisterCannotRaceProvisioning(t *testing.T) {
	srv, tm, client := mobileLogoutFixture(t)
	release, ok := srv.reserveMobileAccount("mobile-user")
	if !ok {
		t.Fatal("reservation failed")
	}
	srv.handleWSUnregister(client, WSMessage{Type: "unregister"})
	messages := readAgentWSMessages(t, client)
	if tm.unregisterCount != 0 || len(messages) != 1 || messages[0].Type != "error" {
		t.Fatalf("released a provisioning account: %+v", messages)
	}
	release()
	srv.handleWSUnregister(client, WSMessage{Type: "unregister"})
	messages = readAgentWSMessages(t, client)
	if len(messages) != 1 || messages[0].Type != "unregistered" {
		t.Fatalf("reservation leaked: %+v", messages)
	}
}
