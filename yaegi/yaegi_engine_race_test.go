package yaegi

// Concurrency / race stress tests. yaegi checks ctx only once before an Eval
// (it cannot interrupt an in-progress run), so the cancellation mix below
// exercises the pre-check path under parallel load; everything is serialized
// internally by execMu.

import (
	"context"
	"sync"
	"testing"
)

// TestYaegiRace_ExecuteAndCallMixed: exact-value runs, scripted-function
// calls and cancelled-context runs share one engine concurrently. Normal runs
// must keep exact results; already-cancelled contexts must fail fast.
func TestYaegiRace_ExecuteAndCallMixed(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	if _, err := eng.ExecuteString(context.Background(), "def", `func add(a, b int) int { return a + b }`); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < 3; g++ { // exact-value runs
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				res, err := eng.ExecuteString(context.Background(), "expr", "1 + 2")
				if err != nil {
					errs <- err
					return
				}
				if res != 3 {
					errs <- &yaegiResultError{op: "ExecuteString", got: res}
					return
				}
			}
		}()
	}
	for g := 0; g < 3; g++ { // scripted-function calls
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				a := id*10 + i
				res, err := eng.CallFunction(context.Background(), "add", a, a)
				if err != nil {
					errs <- err
					return
				}
				if res != 2*a {
					errs <- &yaegiResultError{op: "CallFunction", got: res}
					return
				}
			}
		}(g)
	}
	for g := 0; g < 2; g++ { // pre-cancelled contexts fail fast, engine unharmed
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if _, err := eng.ExecuteString(cancelled, "never", "1 + 2"); err == nil {
					errs <- context.Canceled
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("mixed concurrent operations failed: %v", err)
	}
}

// yaegiResultError reports a result mismatch.
type yaegiResultError struct {
	op  string
	got any
}

func (e *yaegiResultError) Error() string {
	return e.op + ": unexpected result"
}
