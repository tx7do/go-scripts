package lua

// Regression tests for the async-path cancellation semantics (upstream issue
// #69): Execute / ExecuteString / CallFunction used to return on ctx
// cancellation without waiting for the run goroutine, leaking that goroutine
// together with e.mu and the pooled LState; and they never wired ctx into the
// VM, so a runaway script was never interrupted and e.mu was never released
// (engine-wide deadlock). Both are exercised here.

import (
	"context"
	"testing"
	"time"
)

const runawayScript = "while true do end"

// waitForClose runs Close on a watchdog: a healthy engine closes promptly; a
// leaked run goroutine holding e.mu would block it forever.
func waitForClose(t *testing.T, e *engine) {
	t.Helper()
	closed := make(chan error, 1)
	go func() { closed <- e.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return within 5s — run goroutine still holds e.mu (leak/deadlock regression)")
	}
}

func newStartedEngine(t *testing.T) *engine {
	t.Helper()
	e, err := newLuaEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

// TestLuaAsyncCancel_ExecuteString covers the reporter's repro shape: a
// runaway script cancelled mid-run must not wedge the engine, and the engine
// must remain creatable-and-closable in a loop (the race report only showed up
// when several engines were created and closed in one process).
func TestLuaAsyncCancel_ExecuteString(t *testing.T) {
	for i := 0; i < 2; i++ {
		e := newStartedEngine(t)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := e.ExecuteString(ctx, "spin", runawayScript)
		cancel()
		if err != context.DeadlineExceeded {
			t.Fatalf("iter %d: expected context.DeadlineExceeded, got %v", i, err)
		}

		// e.mu must be free again: a normal run succeeds right after.
		if _, err := e.ExecuteString(context.Background(), "ok", "ok = 1"); err != nil {
			t.Fatalf("iter %d: engine not reusable after cancelled run: %v", i, err)
		}
		if v, err := e.GetGlobal("ok"); err != nil || v != int64(1) {
			t.Fatalf("iter %d: unexpected global after reuse: v=%v err=%v", i, v, err)
		}

		waitForClose(t, e)
	}
}

// TestLuaAsyncCancel_Execute covers the LoadString+Execute variant.
func TestLuaAsyncCancel_Execute(t *testing.T) {
	e := newStartedEngine(t)
	defer func() { waitForClose(t, e) }()

	if err := e.LoadString(context.Background(), "spin", runawayScript); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.Execute(ctx); err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

// TestLuaAsyncCancel_CallFunction covers the CallFunction variant with a
// runaway Lua function.
func TestLuaAsyncCancel_CallFunction(t *testing.T) {
	e := newStartedEngine(t)
	defer func() { waitForClose(t, e) }()

	if _, err := e.ExecuteString(context.Background(), "def", "function spin() while true do end end"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.CallFunction(ctx, "spin"); err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

// TestLuaAsyncCancel_AlreadyCancelledContext: an already-expired context must
// abort immediately and still leave the engine usable.
func TestLuaAsyncCancel_AlreadyCancelledContext(t *testing.T) {
	e := newStartedEngine(t)
	defer func() { waitForClose(t, e) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.ExecuteString(ctx, "spin", runawayScript); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, err := e.ExecuteString(context.Background(), "ok", "ok = 1"); err != nil {
		t.Fatalf("engine not reusable after cancelled run: %v", err)
	}
}

// TestLuaAsyncCancel_DeadlineLessContextKeepsFastPath: a deadline-less context
// is not wired into the VM, and a normal script completes as before.
func TestLuaAsyncCancel_DeadlineLessContextKeepsFastPath(t *testing.T) {
	e := newStartedEngine(t)
	defer func() { waitForClose(t, e) }()

	if _, err := e.ExecuteString(context.Background(), "ok", "ok = 42"); err != nil {
		t.Fatalf("background-context run failed: %v", err)
	}
	if v, err := e.GetGlobal("ok"); err != nil || v != int64(42) {
		t.Fatalf("unexpected global: v=%v err=%v", v, err)
	}
}
