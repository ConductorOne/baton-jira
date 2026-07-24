package client

import (
	"context"
	"errors"
	"fmt"
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

func connResetError() error {
	return &url.Error{
		Op:  "Get",
		URL: "https://example.atlassian.net/rest/api/2/user/viewissue/search",
		Err: &net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: os.NewSyscallError("read", syscall.ECONNRESET),
		},
	}
}

func TestIsTransientNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection reset by peer",
			err:      connResetError(),
			expected: true,
		},
		{
			name:     "unexpected EOF",
			err:      fmt.Errorf("no response returned: %w", io.ErrUnexpectedEOF),
			expected: true,
		},
		{
			name:     "broken pipe",
			err:      &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)},
			expected: true,
		},
		{
			name:     "context canceled",
			err:      &url.Error{Op: "Get", URL: "https://example.com", Err: context.Canceled},
			expected: false,
		},
		{
			name:     "context deadline exceeded",
			err:      &url.Error{Op: "Get", URL: "https://example.com", Err: context.DeadlineExceeded},
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("boom"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientNetworkError(tt.err); got != tt.expected {
				t.Errorf("isTransientNetworkError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestWrapErrorTransientNetworkErrorIsUnavailable(t *testing.T) {
	err := WrapError(connResetError(), "failed to get participate grants", nil)

	if status.Code(err) != codes.Unavailable {
		t.Errorf("expected codes.Unavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to get participate grants") {
		t.Errorf("expected message to contain context, got %q", err.Error())
	}
	// The original error must survive in the chain.
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("expected original ECONNRESET to remain in the error chain, got %v", err)
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

	c, err := New("user", "token", srv.URL)
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

func TestWrapErrorNonTransientStaysUnknown(t *testing.T) {
	err := WrapError(errors.New("boom"), "failed to get participate grants", nil)

	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		t.Errorf("expected plain error, got %v", err)
	}
}
