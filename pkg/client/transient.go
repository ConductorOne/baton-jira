package client

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// isTransientNetworkError reports whether err is a network-level failure that
// is safe to retry: connection resets, broken pipes, unexpected EOFs, and
// timeouts. Context cancellation is never transient — the caller is going away.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
