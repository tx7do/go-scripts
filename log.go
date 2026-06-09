package script_engine

import (
	"context"
	"log/slog"
	"os"
	"sync"
)

// Logger is the minimal logging interface used by script_engine internals.
//
// It is deliberately small (4 methods, no WithAttrs/WithGroup) so that any
// project can adapt its own backend (the stdlib *slog.Logger, zap, zerolog,
// kratos log, ...) with a few lines of glue code and inject it via
// [SetLogger].
//
// The first argument is always a context, so backends that support it can
// extract trace ids / request-scoped attributes. Callers that have no context
// at hand may pass nil.
type Logger interface {
	// Debug logs a message at DEBUG level with optional structured key/value
	// pairs passed as args (alternating keys and values).
	Debug(ctx context.Context, msg string, args ...any)
	// Info logs a message at INFO level.
	Info(ctx context.Context, msg string, args ...any)
	// Warn logs a message at WARN level.
	Warn(ctx context.Context, msg string, args ...any)
	// Error logs a message at ERROR level.
	Error(ctx context.Context, msg string, args ...any)
}

// nopLogger discards every log record. It is the zero-cost default used until
// the caller injects a real Logger via [SetLogger].
type nopLogger struct{}

func (nopLogger) Debug(context.Context, string, ...any) {}
func (nopLogger) Info(context.Context, string, ...any)  {}
func (nopLogger) Warn(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, string, ...any) {}

// Compile-time assertion: nopLogger implements Logger.
var _ Logger = nopLogger{}

// SlogLogger adapts the stdlib [*slog.Logger] to the [Logger] interface.
//
// This is the reference implementation returned by [NewSlogLogger]. Callers
// that already own a configured *slog.Logger can wrap it directly:
//
//	scriptEngine.SetLogger(scriptEngine.SlogLogger{L: mySlogLogger})
type SlogLogger struct {
	// L is the underlying slog logger. It MUST be non-nil; [NewSlogLogger]
	// always returns a ready-to-use instance.
	L *slog.Logger
}

// Debug forwards to slog.Logger.DebugContext.
func (s SlogLogger) Debug(ctx context.Context, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.L.DebugContext(ctx, msg, args...)
}

// Info forwards to slog.Logger.InfoContext.
func (s SlogLogger) Info(ctx context.Context, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.L.InfoContext(ctx, msg, args...)
}

// Warn forwards to slog.Logger.WarnContext.
func (s SlogLogger) Warn(ctx context.Context, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.L.WarnContext(ctx, msg, args...)
}

// Error forwards to slog.Logger.ErrorContext.
func (s SlogLogger) Error(ctx context.Context, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.L.ErrorContext(ctx, msg, args...)
}

// Compile-time assertion: SlogLogger implements Logger.
var _ Logger = SlogLogger{}

// NewSlogLogger builds a [SlogLogger] backed by the stdlib slog with sensible
// defaults: a text handler writing to stderr at INFO level.
//
// Callers needing a different format / level / destination should either:
//   - build their own *slog.Logger and wrap it: SlogLogger{L: myLogger}
//   - or implement the [Logger] interface themselves and pass it to
//     [SetLogger].
func NewSlogLogger() Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	return SlogLogger{L: slog.New(h)}
}

// global logger state. Default is nopLogger so importing the package is free
// of side effects; callers opt in to logging via [SetLogger].
var (
	globalLoggerMu sync.RWMutex
	globalLogger   Logger = nopLogger{}
)

// SetLogger sets the package-level [Logger] used by all engines, pools and
// submodules. Pass nil to revert to the silent nopLogger.
//
// SetLogger is safe to call concurrently with log calls; callers that want to
// mutate the logger without races should hold their own coordination.
func SetLogger(l Logger) {
	globalLoggerMu.Lock()
	defer globalLoggerMu.Unlock()
	if l == nil {
		globalLogger = nopLogger{}
		return
	}
	globalLogger = l
}

// GetLogger returns the currently active package-level [Logger].
// It is the entry point submodules use to obtain the shared logger without
// taking a direct dependency on any logging framework.
func GetLogger() Logger {
	globalLoggerMu.RLock()
	defer globalLoggerMu.RUnlock()
	return globalLogger
}
