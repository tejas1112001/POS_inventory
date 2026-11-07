package worker

import (
	"context"
	"log/slog"
)

// minLevel drops records below a level. River logs routine job counts at
// INFO every few seconds; only its warnings and errors reach our logs.
type minLevel struct {
	h   slog.Handler
	min slog.Level
}

func (m minLevel) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= m.min && m.h.Enabled(ctx, l)
}

func (m minLevel) Handle(ctx context.Context, r slog.Record) error { return m.h.Handle(ctx, r) }

func (m minLevel) WithAttrs(a []slog.Attr) slog.Handler {
	return minLevel{h: m.h.WithAttrs(a), min: m.min}
}

func (m minLevel) WithGroup(n string) slog.Handler { return minLevel{h: m.h.WithGroup(n), min: m.min} }
