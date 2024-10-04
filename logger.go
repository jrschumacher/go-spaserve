package spaserve

import (
	"context"
	"log/slog"
)

type servespaLogger struct {
	logger *slog.Logger
}

// newLogger creates a new logger function with the given context and logger.
func newLogger(logger *slog.Logger) *servespaLogger {
	return &servespaLogger{logger: logger}
}

func (l servespaLogger) logContext(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	if l.logger == nil {
		return
	}
	l.logger.LogAttrs(ctx, level, msg, attrs...)
}
