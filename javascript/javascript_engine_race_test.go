package js

// Concurrency / race stress tests for the cancellation machinery: the
// interrupt-arming watcher goroutines (timer / ctx.Done vs done), the sticky
// goja interrupt shared by every run, ExecuteSync's quota path and Close, all
// under parallel load. Beyond race-detector fodder, the exact-result
// assertions turn the "late interrupt poisons the NEXT execution" hazard into
// a hard failure instead of a silent pass.

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	scriptEngine "github.com/tx7do/go-scripts"
)

const jsRunaway = "while(true){}"

func newStartedJSEngine(t *testing.T) *engine {
	t.Helper()
	eng, err := newJavascriptEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return eng
}

// TestJSRace_StickyInterruptNotPoisoned: after a deadline-interrupted runaway,
// the very next run must be clean — goja's interrupt is sticky, and a late or
// leftover interrupt would fail the follow-up run.
func TestJSRace_StickyInterruptNotPoisoned(t *testing.T) {
	eng := newStartedJSEngine(t)
	defer func() { _ = eng.Close() }()

	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := eng.ExecuteString(ctx, "runaway.js", jsRunaway)
		cancel()
		if !isJSQuotaError(err) {
			t.Fatalf("iter %d: runaway returned %v, want a deadline interrupt", i, err)
		}

		res, err := eng.ExecuteString(context.Background(), "ok.js", "6 * 7")
		if err != nil {
			t.Fatalf("iter %d: follow-up run poisoned by leftover interrupt: %v", i, err)
		}
		if res != int64(42) {
			t.Fatalf("iter %d: follow-up run returned %v, want 42", i, res)
		}
	}
}

// TestJSRace_CancelledRunsMixedWithNormal: deadline-budgeted runaway runs and
// exact-result normal runs interleave on one runtime. The normal runs must all
// produce exact results — an interrupt leaking across runs fails loudly here.
func TestJSRace_CancelledRunsMixedWithNormal(t *testing.T) {
	eng := newStartedJSEngine(t)
	defer func() { _ = eng.Close() }()

	if err := eng.RegisterFunction("add", func(a, b float64) float64 { return a + b }); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < 3; g++ { // deadline-budgeted runaways: must abort with an interrupt
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				_, err := eng.ExecuteString(ctx, "runaway.js", jsRunaway)
				cancel()
				if !isJSQuotaError(err) {
					errs <- err
					return
				}
			}
		}()
	}
	for g := 0; g < 3; g++ { // normal runs: exact results, zero tolerance for poison
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				res, err := eng.ExecuteString(context.Background(), "ok.js", "21 * 2")
				if err != nil {
					errs <- err
					return
				}
				if res != int64(42) {
					errs <- &jsResultError{op: "ExecuteString", got: res, want: int64(42)}
					return
				}
			}
		}()
	}
	for g := 0; g < 2; g++ { // host-function calls: exact results
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				a := float64(id*100 + i)
				res, err := eng.CallFunction(context.Background(), "add", a, 1)
				if err != nil {
					errs <- err
					return
				}
				// goja exports integral JS numbers as int64 — compare numerically.
				if jsToFloat(res) != a+1 {
					errs <- &jsResultError{op: "CallFunction", got: res, want: a + 1}
					return
				}
			}
		}(g)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("mixed cancelled/normal runs wedged")
	}
	close(errs)
	for err := range errs {
		t.Fatalf("mixed cancelled/normal runs failed: %v", err)
	}
}

// TestJSRace_CloseConcurrentWithCancelledRun: Close racing an in-flight
// cancel-context (deadline-less) run — the ctx.Done watcher path. Both the run
// and the Close must complete whichever wins.
func TestJSRace_CloseConcurrentWithCancelledRun(t *testing.T) {
	eng := newStartedJSEngine(t)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		_, err := eng.ExecuteString(runCtx, "runaway.js", jsRunaway)
		runErr <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel() // the run's only abort vector is the ctx watcher

	closed := make(chan error, 1)
	go func() { closed <- eng.Close() }()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close wedged behind cancelled run")
	}

	select {
	case err := <-runErr:
		switch {
		case err == nil,
			errors.Is(err, context.Canceled),
			errors.Is(err, ErrJavascriptEngineNotInitialized),
			errors.Is(err, ErrJavascriptRuntimeNotInitialized):
			// all legitimate outcomes depending on interleaving
		default:
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled run never returned after Close")
	}
}

// TestJSRace_ExecuteSyncQuotaConcurrentWithAsync: quota-budgeted runaway
// ExecuteSync runs (expected ErrQuotaExceeded) hammering the runtime while
// normal async runs demand exact results.
func TestJSRace_ExecuteSyncQuotaConcurrentWithAsync(t *testing.T) {
	eng := newStartedJSEngine(t)
	defer func() { _ = eng.Close() }()

	eng.SetQuota(scriptEngine.Quota{Timeout: 50 * time.Millisecond})

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < 2; g++ { // runaway sync runs under quota
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				// ExecuteSync runs the last-loaded programs: load the runaway
				// before each attempt (ExecuteSync errors with
				// ErrJavascriptNoProgramLoaded otherwise).
				if err := eng.LoadString(context.Background(), "runaway.js", jsRunaway); err != nil {
					errs <- err
					return
				}
				if _, err := eng.ExecuteSync(context.Background()); !isJSQuotaError(err) {
					errs <- err
					return
				}
			}
		}()
	}
	for g := 0; g < 2; g++ { // normal async runs: exact results
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				res, err := eng.ExecuteString(context.Background(), "ok.js", "21 * 2")
				if err != nil {
					errs <- err
					return
				}
				if res != int64(42) {
					errs <- &jsResultError{op: "ExecuteString", got: res, want: int64(42)}
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("quota sync + async mix wedged")
	}
	close(errs)
	for err := range errs {
		t.Fatalf("quota sync + async mix failed: %v", err)
	}
}

// jsResultError reports a result mismatch.
type jsResultError struct {
	op   string
	got  any
	want any
}

func (e *jsResultError) Error() string {
	return e.op + ": unexpected result"
}

// jsToFloat converts an exported JS number (int64 or float64) to float64.
func jsToFloat(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	default:
		return math.NaN()
	}
}
