package sipclientauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientRegisterMobileSuccess(t *testing.T) {
	t.Parallel()

	var gotToken, gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("expected multipart form: %v", err)
		}
		gotToken = r.FormValue("token")
		gotType = r.FormValue("type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"OK",
			"message":"Register successfully",
			"data":{
				"name":"Android Reviewer",
				"ext":"1429900148716",
				"secret":"HmnKqtYQSTyeilvLOQYf",
				"domain":"sipclient.ttrs.or.th",
				"domain_video":"sipclient-video.ttrs.or.th",
				"domain_caption":"sipclient-caption.ttrs.or.th",
				"websocket":"wss://sipclient.ttrs.or.th:4443/ws"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{RegisterURL: server.URL, Timeout: time.Second})
	account, err := client.RegisterMobile(context.Background(), "access-token-1")
	if err != nil {
		t.Fatalf("RegisterMobile failed: %v", err)
	}
	if gotToken != "access-token-1" || gotType != "mobile" {
		t.Fatalf("unexpected form values token=%q type=%q", gotToken, gotType)
	}
	if account.Name != "Android Reviewer" ||
		account.Extension != "1429900148716" ||
		account.Secret != "HmnKqtYQSTyeilvLOQYf" ||
		account.Domain != "sipclient.ttrs.or.th" {
		t.Fatalf("unexpected account: %+v", account)
	}
}

func TestClientRegisterMobileRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "non ok status",
			body: `{"status":"ERROR","message":"nope","data":{"ext":"1001","secret":"s","domain":"sip.example.com"}}`,
		},
		{
			name: "missing domain",
			body: `{"status":"OK","data":{"ext":"1001","secret":"s"}}`,
		},
		{
			name: "missing ext",
			body: `{"status":"OK","data":{"domain":"sip.example.com","secret":"s"}}`,
		},
		{
			name: "missing secret",
			body: `{"status":"OK","data":{"domain":"sip.example.com","ext":"1001"}}`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewClient(Config{RegisterURL: server.URL, Timeout: time.Second})
			if _, err := client.RegisterMobile(context.Background(), "access-token-1"); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestClientRegisterMobileTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(Config{RegisterURL: server.URL, Timeout: time.Millisecond})
	if _, err := client.RegisterMobile(context.Background(), "access-token-1"); err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestClientRegisterMobileRejectsEmptyConfig(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{RegisterURL: "   ", Timeout: time.Second})
	if _, err := client.RegisterMobile(context.Background(), "access-token-1"); err == nil {
		t.Fatalf("expected missing register URL error")
	}
}

func TestClientRegisterMobileEscapesPlainFormValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("expected multipart form: %v", err)
		}
		if r.FormValue("token") != "token+with&special=value" {
			t.Fatalf("unexpected token value %q", r.FormValue("token"))
		}
		if _, err := url.ParseRequestURI(r.RequestURI); err != nil {
			t.Fatalf("request URI should remain valid: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK","data":{"domain":"sip.example.com","ext":"1001","secret":"secret"}}`))
	}))
	defer server.Close()

	client := NewClient(Config{RegisterURL: server.URL, Timeout: time.Second})
	if _, err := client.RegisterMobile(context.Background(), "token+with&special=value"); err != nil {
		t.Fatalf("RegisterMobile failed: %v", err)
	}
}
