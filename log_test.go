package script_engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingLogger captures log calls for assertions in tests. It is
// concurrency-safe and is reusable across subtests.
type recordingLogger struct {
	mu      sync.Mutex
	records []logRecord
}

type logRecord struct {
	level string
	msg   string
	args  []any
}

func (r *recordingLogger) Debug(_ context.Context, msg string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, logRecord{"DEBUG", msg, args})
}

func (r *recordingLogger) Info(_ context.Context, msg string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, logRecord{"INFO", msg, args})
}

func (r *recordingLogger) Warn(_ context.Context, msg string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, logRecord{"WARN", msg, args})
}

func (r *recordingLogger) Error(_ context.Context, msg string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, logRecord{"ERROR", msg, args})
}

func (r *recordingLogger) snapshot() []logRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]logRecord, len(r.records))
	copy(out, r.records)
	return out
}

// Compile-time assertion: recordingLogger implements Logger.
var _ Logger = (*recordingLogger)(nil)

// withRestoredLogger runs fn while the package-level logger is temporarily
// swapped for `tmp`; the original logger is always restored when fn returns.
func withRestoredLogger(t *testing.T, tmp Logger, fn func()) {
	t.Helper()
	prev := GetLogger()
	SetLogger(tmp)
	defer SetLogger(prev)
	fn()
}

// TestNopLogger_Default verifies that the package-level default logger is a
// nopLogger, so importing the package produces no log side effects.
func TestNopLogger_Default(t *testing.T) {
	// The package-level default is nopLogger (silent). GetLogger must return
	// something that satisfies Logger and discards calls without panicking.
	l := GetLogger()
	require.NotNil(t, l)

	// Calling any method should be a no-op (no panic, no output).
	assert.NotPanics(t, func() {
		l.Debug(nil, "debug")
		l.Info(nil, "info")
		l.Warn(nil, "warn")
		l.Error(nil, "error", "k", "v")
	})
}

// TestSetLogger_InjectionAndReset verifies the SetLogger / GetLogger
// round-trip and that passing nil resets to the nopLogger.
func TestSetLogger_InjectionAndReset(t *testing.T) {
	original := GetLogger()

	rec := &recordingLogger{}
	withRestoredLogger(t, rec, func() {
		// Inside the block the injected logger is active.
		got := GetLogger()
		// Compare by pointer identity; assert.Same cannot be used because got
		// is an interface, not a direct pointer-typed value.
		assert.True(t, got == rec, "GetLogger must return the injected instance")

		got.Error(nil, "boom", "code", 500)
		got.Warn(nil, "watch out")

		records := rec.snapshot()
		require.Len(t, records, 2)
		assert.Equal(t, "ERROR", records[0].level)
		assert.Equal(t, "boom", records[0].msg)
		assert.Equal(t, []any{"code", 500}, records[0].args)
		assert.Equal(t, "WARN", records[1].level)
		assert.Equal(t, "watch out", records[1].msg)
	})

	// After the block the previous logger is back.
	assert.True(t, GetLogger() == original, "original logger must be restored")

	// Passing nil resets to nopLogger.
	SetLogger(nil)
	defer SetLogger(original)
	_, ok := GetLogger().(nopLogger)
	assert.True(t, ok, "SetLogger(nil) must revert to nopLogger")
}

// TestSlogLogger_OutputFormat verifies that SlogLogger forwards to the
// underlying *slog.Logger and emits the expected fields.
func TestSlogLogger_OutputFormat(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := SlogLogger{L: slog.New(h)}

	ctx := context.Background()
	logger.Debug(ctx, "debug msg", "key", "value")
	logger.Info(ctx, "info msg")
	logger.Warn(ctx, "warn msg", "n", 42)
	logger.Error(ctx, "error msg", "err", "fail")

	output := buf.String()
	// slog text handler emits lowercase levels (level=debug, etc.).
	for _, want := range []string{"level=DEBUG", "level=INFO", "level=WARN", "level=ERROR"} {
		assert.True(t, strings.Contains(strings.ToLower(output), strings.ToLower(want)),
			"expected %q in output, got:\n%s", want, output)
	}
	// Verify message body and structured args. slog quotes values that
	// contain spaces.
	assert.Contains(t, output, `msg="debug msg"`)
	assert.Contains(t, output, "key=value")
	assert.Contains(t, output, "n=42")
	assert.Contains(t, output, "err=fail")
}

// TestSlogLogger_NilContext verifies that passing a nil context does not
// panic; SlogLogger replaces it with context.Background().
func TestSlogLogger_NilContext(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := SlogLogger{L: slog.New(h)}

	assert.NotPanics(t, func() {
		logger.Debug(nil, "ok")
		logger.Info(nil, "ok")
		logger.Warn(nil, "ok")
		logger.Error(nil, "ok")
	})
	assert.Contains(t, buf.String(), "msg=ok")
}

// TestNewSlogLogger_Defaults verifies that NewSlogLogger returns a usable
// non-nil Logger that satisfies the SlogLogger concrete type and can log
// without panicking.
func TestNewSlogLogger_Defaults(t *testing.T) {
	l := NewSlogLogger()
	require.NotNil(t, l)
	sl, ok := l.(SlogLogger)
	require.True(t, ok, "NewSlogLogger must return a SlogLogger")
	require.NotNil(t, sl.L)

	// Smoke test: a single Error call should not panic (it writes to stderr).
	assert.NotPanics(t, func() {
		l.Error(context.Background(), "smoke", "k", 1)
	})
}

// TestSetLogger_Concurrent verifies that SetLogger / GetLogger are safe to
// call concurrently (they use a RWMutex internally).
func TestSetLogger_Concurrent(t *testing.T) {
	prev := GetLogger()
	defer SetLogger(prev)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Half the goroutines swap loggers, the other half read. The goal is to
	// trigger the race detector if any synchronization is missing.
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				SetLogger(&recordingLogger{})
			} else {
				_ = GetLogger()
			}
		}()
	}
	wg.Wait()

	// After the storm, reset should still work cleanly.
	SetLogger(nil)
	_, ok := GetLogger().(nopLogger)
	assert.True(t, ok)
}
