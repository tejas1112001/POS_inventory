package httpx

import (
	"context"
	"log/slog"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyLogger
	keyHost
)

// RequestID returns the request ID set by the RequestID middleware.
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(keyRequestID).(string)
	return s
}

// LoggerFrom returns the request-scoped logger, or the default logger.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(keyLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithLogger stores a request-scoped logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, keyLogger, l)
}

// HostFrom returns the classified host of the request.
func HostFrom(ctx context.Context) Host {
	h, _ := ctx.Value(keyHost).(Host)
	return h
}

func withHost(ctx context.Context, h Host) context.Context {
	return context.WithValue(ctx, keyHost, h)
}
