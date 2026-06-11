package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPNSBaseURLSelectsEnvironment(t *testing.T) {
	if got := apnsBaseURL("sandbox"); got != apnsSandboxBaseURL {
		t.Fatalf("expected sandbox URL, got %q", got)
	}
	if got := apnsBaseURL("production"); got != apnsProductionBaseURL {
		t.Fatalf("expected production URL, got %q", got)
	}
}

func TestNormalizeAPNSTopicDefaultsToVoIPTopic(t *testing.T) {
	if got := normalizeAPNSTopic("th.or.nectec.kasemsan.softphone", ""); got != "th.or.nectec.kasemsan.softphone.voip" {
		t.Fatalf("unexpected topic: %q", got)
	}
	if got := normalizeAPNSTopic("ignored", "custom.topic.voip"); got != "custom.topic.voip" {
		t.Fatalf("unexpected custom topic: %q", got)
	}
}

func TestBuildAPNSVoIPPayloadIncludesIncomingMetadata(t *testing.T) {
	data := buildIncomingCallPushData("sess-1", "1001", "1002", "ios_pushkit", time.Unix(0, 0).UTC(), true)
	payload := buildAPNSVoIPPayload(data)

	aps, ok := payload["aps"].(map[string]interface{})
	if !ok || aps["content-available"] != 1 {
		t.Fatalf("expected content-available aps payload, got %#v", payload["aps"])
	}
	if payload["type"] != "incoming_call" || payload["sessionId"] != "sess-1" || payload["hasVideo"] != "true" {
		t.Fatalf("unexpected APNs payload: %#v", payload)
	}
}

func TestAPNSRequestHeaders(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sender := &APNSSender{
		keyID:      "KEY123",
		teamID:     "TEAM123",
		topic:      "th.or.nectec.kasemsan.softphone.voip",
		privateKey: key,
		baseURL:    "https://example.invalid",
	}

	req, err := sender.buildRequest(context.Background(), "abcdef", []byte(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if req.URL.String() != "https://example.invalid/3/device/abcdef" {
		t.Fatalf("unexpected URL: %s", req.URL.String())
	}
	if req.Header.Get("apns-push-type") != "voip" || req.Header.Get("apns-topic") != sender.topic || req.Header.Get("apns-priority") != "10" {
		t.Fatalf("unexpected APNs headers: %#v", req.Header)
	}
	if !strings.HasPrefix(req.Header.Get("authorization"), "bearer ") {
		t.Fatalf("missing bearer auth header")
	}
}

func TestAPNSCertificateAuthLoadsCombinedPEM(t *testing.T) {
	certFile := writeCombinedCertificatePEM(t)

	sender, err := NewAPNSSender(APNSConfig{
		Environment: "sandbox",
		AuthMode:    "certificate",
		CertFile:    certFile,
		Topic:       "th.or.nectec.kasemsan.softphone.voip",
	})
	if err != nil {
		t.Fatalf("new APNs certificate sender: %v", err)
	}
	if sender.authMode != apnsAuthModeCertificate {
		t.Fatalf("expected certificate auth mode, got %q", sender.authMode)
	}
	if sender.baseURL != apnsSandboxBaseURL {
		t.Fatalf("expected sandbox URL, got %q", sender.baseURL)
	}
	transport, ok := sender.httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("expected one TLS client certificate, got %#v", sender.httpClient.Transport)
	}
}

func TestAPNSCertificateRequestHeadersOmitBearerAuth(t *testing.T) {
	sender := &APNSSender{
		authMode: apnsAuthModeCertificate,
		topic:    "th.or.nectec.kasemsan.softphone.voip",
		baseURL:  "https://example.invalid",
	}

	req, err := sender.buildRequest(context.Background(), "abcdef", []byte(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if req.Header.Get("authorization") != "" {
		t.Fatalf("expected no bearer authorization header, got %q", req.Header.Get("authorization"))
	}
	if req.Header.Get("apns-push-type") != "voip" ||
		req.Header.Get("apns-topic") != sender.topic ||
		req.Header.Get("apns-priority") != "10" ||
		req.Header.Get("apns-expiration") == "" ||
		req.Header.Get("content-type") != "application/json" {
		t.Fatalf("unexpected APNs headers: %#v", req.Header)
	}
}

func TestNewAPNSSenderRejectsUnsupportedAuthMode(t *testing.T) {
	_, err := NewAPNSSender(APNSConfig{
		AuthMode: "pem",
		Topic:    "th.or.nectec.kasemsan.softphone.voip",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported auth mode") {
		t.Fatalf("expected unsupported auth mode error, got %v", err)
	}
}

func TestNewAPNSSenderCertificateAuthRequiresCertFile(t *testing.T) {
	_, err := NewAPNSSender(APNSConfig{
		AuthMode: "certificate",
		Topic:    "th.or.nectec.kasemsan.softphone.voip",
	})
	if err == nil || !strings.Contains(err.Error(), "certificate file is required") {
		t.Fatalf("expected missing certificate file error, got %v", err)
	}
}

func writeCombinedCertificatePEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "th.or.nectec.kasemsan.softphone.voip",
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	path := filepath.Join(t.TempDir(), "pushkit-combined.pem")
	if err := os.WriteFile(path, append(certPEM, keyPEM...), 0600); err != nil {
		t.Fatalf("write certificate pem: %v", err)
	}
	return path
}
