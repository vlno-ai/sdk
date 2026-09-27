package api

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const testToken = "abcdefghijklmnopqrstuvwxyzabcdef"

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	c, err := New(endpoint, testToken, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var actual *Error
	if !errors.As(err, &actual) || actual.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
	return actual
}

func TestOriginValidation(t *testing.T) {
	for _, endpoint := range []string{
		"http://example.com", "ftp://localhost", "https://user:password@example.com",
		"https://example.com/path", "https://example.com?x=y", "https://example.com?",
		"https://example.com#fragment", "https://example.com#", "https://example.com/%2f",
		"//example.com", "https:///missing-host", "https://[::1%25lo0]", "http://localhost.example.com",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := New(endpoint, testToken, "", time.Second); err == nil {
				t.Fatal("accepted invalid origin")
			}
		})
	}
	for _, endpoint := range []string{"http://127.0.0.1:8787", "http://localhost/", "http://[::1]:8787", "https://worker.example.com"} {
		if _, err := New(endpoint, testToken, "", time.Second); err != nil {
			t.Fatalf("valid origin rejected: %v", err)
		}
	}
	for _, token := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 129), testToken + "\r\nx: y"} {
		_, err := New("http://localhost", token, "", time.Second)
		assertCode(t, err, "invalid_token")
	}
	_, err := New("http://localhost", testToken, "", -time.Second)
	assertCode(t, err, "invalid_timeout")
}

func TestRequestAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/worlds" || r.Header.Get("Authorization") != "Bearer "+testToken ||
			r.Header.Get("Idempotency-Key") != "creation-key" || r.Header.Get("Content-Type") != "application/json" || !r.Close {
			t.Error("wrong request metadata")
		}
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["image"] != "tiny-notes" {
			t.Error("wrong payload")
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"id":"world","state":"creating"}`)
	}))
	defer server.Close()
	got, err := newTestClient(t, server.URL).Request(context.Background(), "POST", "/v1/worlds", map[string]any{"image": "tiny-notes"}, "creation-key")
	if err != nil || got["state"] != "creating" {
		t.Fatalf("request failed: %v", err)
	}
}

func TestTLSVerificationAndPrivateCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) }))
	defer server.Close()
	_, err := newTestClient(t, server.URL).Request(context.Background(), "GET", "/v1/catalog", nil, "")
	if failure := assertCode(t, err, "transport_error"); failure.Reason != "tls_verification" || failure.Stage != "tls_handshake" {
		t.Fatal("TLS verification reason lost")
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(server.URL, testToken, ca, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Request(context.Background(), "GET", "/v1/catalog", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = New(server.URL, testToken, ca, time.Second)
	assertCode(t, err, "invalid_ca_file")
}

func TestNoRedirectOrEnvironmentProxy(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); io.WriteString(w, `{}`) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/catalog", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", target.URL)
	t.Setenv("HTTPS_PROXY", target.URL)
	t.Setenv("ALL_PROXY", target.URL)
	c := newTestClient(t, server.URL)
	if c.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
	_, err := c.Request(context.Background(), "POST", "/v1/worlds", map[string]any{}, "creation-key")
	if e := assertCode(t, err, "http_error"); e.Status != 307 {
		t.Fatal("wrong status")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("followed redirect or proxy")
	}
}

func TestLocalValidationPrecedesRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{}`) }))
	defer server.Close()
	c := newTestClient(t, server.URL)
	for _, path := range []string{"https://attacker.invalid/v1/catalog", "//attacker.invalid", "/v1/../catalog", "/v1/%2e%2e/catalog", "/v1/catalog?x=y", "/v1/catalog#x", "/v1//catalog", "/v1/\\catalog"} {
		_, err := c.Request(context.Background(), "GET", path, nil, "")
		assertCode(t, err, "invalid_route")
	}
	_, err := c.Request(context.Background(), "PUT", "/v1/worlds", nil, "")
	assertCode(t, err, "invalid_method")
	_, err = c.Request(context.Background(), "POST", "/v1/worlds", nil, "bad\nkey")
	assertCode(t, err, "invalid_idempotency_key")
	_, err = c.Request(context.Background(), "POST", "/v1/worlds", map[string]any{"x": strings.Repeat("x", MaxRequestBytes)}, "")
	assertCode(t, err, "request_too_large")
	_, err = c.Request(context.Background(), "POST", "/v1/worlds", make(chan int), "")
	assertCode(t, err, "invalid_request")
	if calls.Load() != 0 {
		t.Fatal("invalid input reached server")
	}
}

func TestResponseLimitsAndShape(t *testing.T) {
	for _, tc := range []struct{ name, response, code string }{
		{"large", strings.Repeat("x", MaxResponseBytes+1), "response_too_large"},
		{"array", `[]`, "invalid_response"},
		{"null", `null`, "invalid_response"},
		{"malformed", `{`, "invalid_response"},
		{"multiple", `{} {}`, "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.response) }))
			defer server.Close()
			_, err := newTestClient(t, server.URL).Request(context.Background(), "GET", "/v1/catalog", nil, "")
			assertCode(t, err, tc.code)
		})
	}
}

func TestResponsePreservesLargeIntegers(t *testing.T) {
	const body = `{"id":9007199254740993,"nested":{"value":9223372036854775807}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer server.Close()
	value, err := newTestClient(t, server.URL).Request(context.Background(), "GET", "/v1/catalog", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) != body {
		t.Fatalf("integer precision changed: %s (%v)", encoded, err)
	}
}

func TestSanitizedErrors(t *testing.T) {
	for _, tc := range []struct{ code, expected string }{
		{"world_busy", "world_busy"}, {"secret\nBearer " + testToken, "http_error"}, {testToken, "http_error"}, {"prefix_" + testToken, "http_error"}, {strings.Repeat("x", 65), "http_error"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": tc.code, "outcome": "unknown", "message": testToken}})
		}))
		_, err := newTestClient(t, server.URL).Request(context.Background(), "GET", "/v1/catalog", nil, "")
		server.Close()
		e := assertCode(t, err, tc.expected)
		if e.Status != 503 || e.Outcome != "unknown" || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "Bearer") {
			t.Fatal("unsanitized error")
		}
	}
}

func TestWriteNotRetriedAfterLostResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		if r.Method == "GET" {
			io.WriteString(w, `{}`)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	if _, err := c.Request(context.Background(), "GET", "/v1/catalog", nil, ""); err != nil {
		t.Fatal(err)
	}
	_, err := c.Request(context.Background(), "POST", "/v1/worlds", map[string]any{"image": "tiny-notes"}, "creation-key")
	failure := assertCode(t, err, "transport_error")
	if failure.Outcome != "unknown" {
		t.Fatal("lost response must remain unknown")
	}
	if failure.Reason != "connection_closed" || failure.Stage != "request" {
		t.Fatalf("unexpected lost-response reason: %s", failure.Reason)
	}
	if calls.Load() != 2 {
		t.Fatalf("request retried: %d calls", calls.Load())
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c, err := New(server.URL, testToken, "", 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Request(context.Background(), "GET", "/v1/catalog", nil, "")
	if failure := assertCode(t, err, "transport_error"); failure.Reason != "timeout" || failure.Stage != "request" {
		t.Fatal("timeout reason lost")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout cause lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Request(ctx, "GET", "/v1/catalog", nil, "")
	if assertCode(t, err, "transport_error").Reason != "cancelled" {
		t.Fatal("cancellation reason lost")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause lost")
	}
}

func TestPartialResponseHasResponseStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		io.WriteString(w, `{}`)
	}))
	defer server.Close()
	_, err := newTestClient(t, server.URL).Request(context.Background(), "GET", "/v1/catalog", nil, "")
	failure := assertCode(t, err, "transport_error")
	if failure.Reason != "unexpected_eof" || failure.Stage != "response" {
		t.Fatalf("incorrect transport phase: %s/%s", failure.Reason, failure.Stage)
	}
}

func TestTransportReasonsNeverIncludeRawErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{io.ErrUnexpectedEOF, "unexpected_eof"},
		{io.EOF, "connection_closed"},
		{syscall.ECONNREFUSED, "connection_refused"},
		{syscall.ECONNRESET, "connection_reset"},
		{errors.New("private transport detail " + testToken), "network_error"},
	} {
		failure := transportError(fmt.Errorf("private endpoint and token %s: %w", testToken, tc.err))
		if failure.Reason != tc.reason || strings.Contains(failure.Error(), testToken) || failure.Unwrap() != nil {
			t.Fatalf("unexpected sanitized transport metadata: %s, %s", failure.Code, failure.Reason)
		}
	}
}
