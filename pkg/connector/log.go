package connector

import (
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isClientError returns true for gRPC codes that represent client errors (4xx equivalent).
func isClientError(err error) bool {
	code := status.Code(err)
	switch code {
	case codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Unimplemented,
		codes.Canceled,
		codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

// logError logs at Warn level for client errors, Error level for server errors.
func logError(l *zap.Logger, err error, msg string, fields ...zap.Field) {
	allFields := append([]zap.Field{zap.Error(err)}, fields...)
	if isClientError(err) {
		l.Warn(msg, allFields...)
	} else {
		l.Error(msg, allFields...)
	}
}
