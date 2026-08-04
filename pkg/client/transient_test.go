package client

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	jira "github.com/conductorone/go-jira/v2/cloud"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestConnectionResetIsUnavailable exercises the full path the CXP-762
// incident took: the server kills the TCP connection before responding, the
// uhttp transport under the Jira client classifies the failure as retryable,
// and WrapError preserves that classification through its wrap.
func TestConnectionResetIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		// SetLinger(0) makes Close send a RST instead of a FIN, so the
		// client observes "connection reset by peer" mid-request.
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		conn.Close()
	}))
	defer srv.Close()

	c, err := New(context.Background(), "user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, resp, err := c.Jira().User.FindUsersWithBrowsePermission(context.Background(), ".", jira.WithProjectKey("TEST"))
	if err == nil {
		t.Fatal("expected an error from the reset connection")
	}

	var statusCode *int
	if resp != nil {
		statusCode = &resp.StatusCode
	}
	wrapped := WrapError(err, "failed to get participate grants", statusCode)
	if status.Code(wrapped) != codes.Unavailable {
		t.Errorf("expected codes.Unavailable for connection reset, got %v", wrapped)
	}
	if !strings.Contains(wrapped.Error(), "failed to get participate grants") {
		t.Errorf("expected message to contain context, got %q", wrapped.Error())
	}
}

// TestWrapErrorUncoveredTransientErrorsAreUnavailable covers the transient
// cases uhttp's transport does not classify: bare io.EOF (server closed the
// connection cleanly before responding), broken pipes, and aborted
// connections (Errno.Temporary() is false for EPIPE/ECONNABORTED).
func TestWrapErrorUncoveredTransientErrorsAreUnavailable(t *testing.T) {
	sentinels := []error{io.EOF, syscall.EPIPE, syscall.ECONNABORTED}
	for _, sentinel := range sentinels {
		wrapped := WrapError(
			&url.Error{Op: "Get", URL: "https://example.com", Err: &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", sentinel)}},
			"failed to get participate grants", nil,
		)
		if status.Code(wrapped) != codes.Unavailable {
			t.Errorf("expected codes.Unavailable for %v, got %v", sentinel, wrapped)
		}
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("expected %v to remain in the error chain, got %v", sentinel, wrapped)
		}
	}
}

// TestNewJiraHTTPClientTransportChain guards the in-place transport wrap:
// the client must keep uhttp's client-level timeout AND still send basic
// auth through the wrapped transport chain.
func TestNewJiraHTTPClientTransportChain(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewJiraHTTPClient(context.Background(), "user", "token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if c.Timeout <= 0 {
		t.Errorf("expected the uhttp client timeout to be preserved, got %v", c.Timeout)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("request through wrapped transport failed: %v", err)
	}
	defer resp.Body.Close()

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:token"))
	if gotAuth != want {
		t.Errorf("expected basic auth header %q from the wrapped transport, got %q", want, gotAuth)
	}
}

// TestWrapErrorTruncated2xxIsUnavailable reproduces a mid-body failure on a
// 200 response: go-jira returns a non-nil response (so a status code IS
// available) together with a decode error, which must still classify as
// retryable.
func TestWrapErrorTruncated2xxIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Advertise more bytes than we send, then return: the connection dies
		// mid-body and the client sees an unexpected EOF while decoding.
		w.Header().Set("Content-Length", "5000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"accountId":"trunc`))
	}))
	defer srv.Close()

	c, err := New(context.Background(), "user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, resp, err := c.Jira().User.FindUsersWithBrowsePermission(context.Background(), ".", jira.WithProjectKey("TEST"))
	if err == nil {
		t.Fatal("expected an error from the truncated response")
	}
	var statusCode *int
	if resp != nil {
		statusCode = &resp.StatusCode
	}
	if statusCode == nil || *statusCode != http.StatusOK {
		t.Fatalf("test setup: expected a non-nil 200 response, got %v — the scenario under test requires it", statusCode)
	}

	wrapped := WrapError(err, "failed to get participate grants", statusCode)
	if status.Code(wrapped) != codes.Unavailable {
		t.Errorf("expected codes.Unavailable for mid-body failure on 2xx, got %v", wrapped)
	}
}

func TestWrapErrorRetryableStatusCodes(t *testing.T) {
	retryable := []int{
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusGatewayTimeout,
	}
	for _, code := range retryable {
		wrapped := WrapError(errors.New("boom"), "request failed", &code)
		if status.Code(wrapped) != codes.Unavailable {
			t.Errorf("expected codes.Unavailable for HTTP %d, got %v", code, wrapped)
		}
	}
}

func TestWrapErrorNonTransientStaysUnknown(t *testing.T) {
	err := WrapError(errors.New("boom"), "failed to get participate grants", nil)

	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		t.Errorf("expected plain error, got %v", err)
	}
}
