package cel

// Concurrency / race stress tests: ExecuteString, CallFunction and
// registration churn share one engine (serialized internally by execMu) so
// `go test -race` sees the full shared-state surface.

import (
	"context"
	"sync"
	"testing"
)

// TestCELRace_ExecuteAndCallMixed hammers one engine from three angles at
// once: exact-value expression runs, host-function calls, and global
// registration churn. All must coexist without errors or cross-talk.
func TestCELRace_ExecuteAndCallMixed(t *testing.T) {
	eng, err := newCELEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	if err := eng.RegisterFunction("triple", func(x int64) int64 { return x * 3 }); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < 3; g++ { // exact-value expression runs
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				res, err := eng.ExecuteString(context.Background(), "expr", "1 + 2")
				if err != nil {
					errs <- err
					return
				}
				if res != int64(3) {
					errs <- &celResultError{op: "ExecuteString", got: res}
					return
				}
			}
		}()
	}
	for g := 0; g < 3; g++ { // host-function calls
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				x := int64(id*100 + i)
				res, err := eng.CallFunction(context.Background(), "triple", x)
				if err != nil {
					errs <- err
					return
				}
				if res != 3*x {
					errs <- &celResultError{op: "CallFunction", got: res}
					return
				}
			}
		}(g)
	}
	wg.Add(1)
	go func() { // registration churn while the others run
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := eng.RegisterGlobal("churnKey", i); err != nil {
				errs <- err
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("mixed concurrent operations failed: %v", err)
	}
}

// celResultError reports a result mismatch.
type celResultError struct {
	op  string
	got any
}

func (e *celResultError) Error() string {
	return e.op + ": unexpected result"
}
