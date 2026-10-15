package wazero

// Concurrency / race stress tests: full lifecycle churn (LoadString → Execute
// → CallFunction) plus real ctx cancellation (wazero honors ctx) sharing one
// engine, serialized internally by execMu.

import (
	"context"
	"sync"
	"testing"
)

// TestWazeroRace_LifecycleAndCancel: goroutines run the whole load/instantiate/
// call lifecycle concurrently while others exercise cancelled-context calls;
// after the churn the engine must still work normally.
func TestWazeroRace_LifecycleAndCancel(t *testing.T) {
	eng := newTestEngine(t)
	t.Cleanup(func() { _ = eng.Close() })

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < 3; g++ { // lifecycle churn with exact results
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if err := eng.LoadString(context.Background(), "add", string(addWasm)); err != nil {
					errs <- err
					return
				}
				if _, err := eng.Execute(context.Background()); err != nil {
					errs <- err
					return
				}
				x := uint64(id*10 + i)
				res, err := eng.CallFunction(context.Background(), "add", x, uint64(4))
				if err != nil {
					errs <- err
					return
				}
				if res != x+4 {
					errs <- &wazeroResultError{got: res}
					return
				}
			}
		}(g)
	}
	for g := 0; g < 2; g++ { // cancelled-ctx calls churn ctx plumbing; results
		wg.Add(1) // are not asserted — wazero only honors ctx at yield points,
		go func() {
			defer wg.Done() // so a fast function may well succeed
			for i := 0; i < 5; i++ {
				_ = eng.LoadString(cancelled, "add", string(addWasm))
				_, _ = eng.CallFunction(cancelled, "add", uint64(1), uint64(2))
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("lifecycle/cancel churn failed: %v", err)
	}

	// Engine still healthy after the churn.
	if err := eng.LoadString(context.Background(), "add", string(addWasm)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := eng.CallFunction(context.Background(), "add", uint64(3), uint64(4))
	if err != nil {
		t.Fatal(err)
	}
	if res != uint64(7) {
		t.Fatalf("final call returned %v, want 7", res)
	}
}

// wazeroResultError reports a result mismatch.
type wazeroResultError struct {
	got any
}

func (e *wazeroResultError) Error() string {
	return "unexpected result"
}
