// Package api implements the bounded, authenticated worker control protocol.
package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	MaxRequestBytes  = 64 * 1024
	MaxResponseBytes = 4 * 1024 * 1024
)

var (
	tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)
	keyPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	routePattern = regexp.MustCompile(`^/v1/[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)
	codePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Error contains only bounded protocol metadata, never response text or URLs.
// Outcome "unknown" means a write may have reached the worker; callers must not
// automatically repeat it. Reuse the original idempotency key when reconciling.
type Error struct {
	Code    string
	Status  int
	Outcome string
	Reason  string
	Stage   string
	cause   error
}

func (e *Error) Error() string {
	code := e.Code
	if !codePattern.MatchString(code) {
		code = "api_error"
	}
	if e.Status >= 100 && e.Status <= 599 {
		return fmt.Sprintf("%s (HTTP %d)", code, e.Status)
	}
	return code
}

// Unwrap preserves errors.Is(err, context.Canceled/DeadlineExceeded) without
// exposing a transport error containing endpoint or TLS details.
func (e *Error) Unwrap() error { return e.cause }

type Client struct {
	origin string
	token  string
	http   *http.Client
}

// New accepts an origin only. Plain HTTP is permitted solely for literal
// loopback addresses and localhost. An optional PEM CA augments system roots.
func New(endpoint, token, caFile string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || u.Opaque != "" || u.User != nil || u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") ||
		u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" ||
		strings.Contains(endpoint, "#") || strings.Contains(u.Host, "%") || u.Hostname() == "" {
		return nil, &Error{Code: "invalid_endpoint"}
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, &Error{Code: "https_required"}
		}
	}
	if !tokenPattern.MatchString(token) {
		return nil, &Error{Code: "invalid_token"}
	}
	if timeout < 0 {
		return nil, &Error{Code: "invalid_timeout"}
	}
	if timeout == 0 {
		timeout = 200 * time.Second
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, &Error{Code: "invalid_ca_file"}
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, &Error{Code: "invalid_ca_file"}
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSClientConfig:        tlsConfig,
		TLSHandshakeTimeout:    10 * time.Second,
		MaxResponseHeaderBytes: MaxRequestBytes,
		DisableCompression:     true,
		// Fresh HTTP/1 connections and non-replayable bodies prevent net/http
		// retrying writes, including POSTs carrying an Idempotency-Key.
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &Client{
		origin: u.Scheme + "://" + u.Host,
		token:  token,
		http: &http.Client{
			Timeout:       timeout,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Request makes exactly one application-level attempt. Routes are strict local
// API paths: schemes, authority changes, traversal and escapes fail
// before credentials are attached or a connection is opened.
func (c *Client) Request(ctx context.Context, method, path string, payload any, idempotencyKey string) (map[string]any, error) {
	return c.request(ctx, method, path, payload, idempotencyKey, false, "")
}

// MCP makes one non-retried request to the scoped JSON-response MCP endpoint.
func (c *Client) MCP(ctx context.Context, path string, payload any, version string) (map[string]any, error) {
	if !regexp.MustCompile(`^/v1/(worlds|world-agent)/[a-f0-9]{32}/mcp$`).MatchString(path) {
		return nil, &Error{Code: "invalid_route"}
	}
	if version != "" && version != "2025-11-25" && version != "2025-06-18" && version != "2025-03-26" {
		return nil, &Error{Code: "invalid_mcp_version"}
	}
	return c.request(ctx, http.MethodPost, path, payload, "", true, version)
}

func (c *Client) request(ctx context.Context, method, path string, payload any, idempotencyKey string, mcp bool, version string) (map[string]any, error) {
	if !routePattern.MatchString(path) && !(method == http.MethodGet && regexp.MustCompile(`^/v1/world-runs/cw_[a-f0-9]{32}/cases/(0|[1-9]|1[0-5])/transcript\?after=(0|[1-9][0-9]{0,15})$`).MatchString(path)) {
		return nil, &Error{Code: "invalid_route"}
	}
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodDelete {
		return nil, &Error{Code: "invalid_method"}
	}
	if idempotencyKey != "" && !keyPattern.MatchString(idempotencyKey) {
		return nil, &Error{Code: "invalid_idempotency_key"}
	}
	var body io.Reader
	var size int
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, &Error{Code: "invalid_request"}
		}
		if len(encoded) > MaxRequestBytes {
			return nil, &Error{Code: "request_too_large"}
		}
		size = len(encoded)
		// Hide bytes.Reader from NewRequest so it cannot construct GetBody.
		body = io.NopCloser(bytes.NewReader(encoded))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, body)
	if err != nil {
		return nil, &Error{Code: "invalid_request"}
	}
	req.ContentLength = int64(size)
	req.GetBody = nil
	req.Close = true
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if mcp {
		req.Header.Set("Accept", "application/json, text/event-stream")
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	// Record only a bounded phase name. In particular, trace callbacks never
	// retain DNS names, addresses, certificates, headers, or raw error text.
	var phase atomic.Uint32
	advance := func(next uint32) {
		for previous := phase.Load(); previous < next; previous = phase.Load() {
			if phase.CompareAndSwap(previous, next) {
				return
			}
		}
	}
	trace := &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { advance(1) },
		ConnectStart:         func(string, string) { advance(2) },
		TLSHandshakeStart:    func() { advance(3) },
		GotConn:              func(httptrace.GotConnInfo) { advance(4) },
		GotFirstResponseByte: func() { advance(5) },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	failed := func(err error) *Error {
		failure := transportError(err)
		failure.Stage = [...]string{"request", "dns", "connect", "tls_handshake", "request", "response"}[phase.Load()]
		return failure
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, failed(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, failed(err)
	}
	if len(data) > MaxResponseBytes {
		return nil, &Error{Code: "response_too_large", Status: response.StatusCode, Outcome: "unknown"}
	}
	if mcp && response.StatusCode == http.StatusAccepted && len(data) == 0 {
		return nil, nil
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	parseErr := decoder.Decode(&result)
	if parseErr == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			parseErr = errors.New("trailing JSON")
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := &Error{Code: "http_error", Status: response.StatusCode}
		if details, ok := result["error"].(map[string]any); parseErr == nil && ok {
			if code, ok := details["code"].(string); ok && codePattern.MatchString(code) && !strings.Contains(code, c.token) {
				failure.Code = code
			}
			if details["outcome"] == "unknown" {
				failure.Outcome = "unknown"
			}
		}
		return nil, failure
	}
	if parseErr != nil || result == nil {
		return nil, &Error{Code: "invalid_response", Status: response.StatusCode, Outcome: "unknown"}
	}
	return result, nil
}

func transportError(err error) *Error {
	failure := &Error{Code: "transport_error", Outcome: "unknown", Reason: "network_error"}
	var verification *tls.CertificateVerificationError
	var timeout net.Error
	if errors.Is(err, context.Canceled) {
		failure.cause = context.Canceled
		failure.Reason = "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		failure.cause = context.DeadlineExceeded
		failure.Reason = "timeout"
	} else if errors.As(err, &verification) {
		failure.Reason = "tls_verification"
	} else if errors.As(err, &timeout) && timeout.Timeout() {
		failure.Reason = "timeout"
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		failure.Reason = "unexpected_eof"
	} else if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		failure.Reason = "connection_closed"
	} else if errors.Is(err, syscall.ECONNREFUSED) {
		failure.Reason = "connection_refused"
	} else if errors.Is(err, syscall.ECONNRESET) {
		failure.Reason = "connection_reset"
	}
	return failure
}
