package tcl

// Concurrency / race stress tests. The shared singleton engine (see
// getSharedEngine) is exercised from several goroutines at once — execution is
// serialized internally by execMu. Deliberately modest goroutine counts:
// Windows tcl interpreters are a scarce resource (creating many deadlocks).

import (
	"context"
	"sync"
	"testing"
)

// TestTCLRace_ExecuteAndCallMixed: concurrent expression runs and
// scripted-proc calls with exact results on the shared engine.
func TestTCLRace_ExecuteAndCallMixed(t *testing.T) {
	eng := getSharedEngine(t)

	if _, err := eng.ExecuteString(context.Background(), "def", `
proc add {a b} {
    return [expr {$a + $b}]
}
`); err != nil {
		t.Fatal(err)
	}

	const goroutines, runs = 4, 10

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < goroutines; g++ { // exact-value expression runs
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < runs; i++ {
				res, err := eng.ExecuteString(context.Background(), "expr", "expr {1 + 2}")
				if err != nil {
					errs <- err
					return
				}
				if res != int64(3) {
					errs <- &tclResultError{op: "ExecuteString", got: res}
					return
				}
			}
		}()
	}
	for g := 0; g < goroutines; g++ { // proc calls
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < runs; i++ {
				a := int64(id*runs + i)
				res, err := eng.CallFunction(context.Background(), "add", a, a)
				if err != nil {
					errs <- err
					return
				}
				if res != 2*a {
					errs <- &tclResultError{op: "CallFunction", got: res}
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("mixed concurrent operations failed: %v", err)
	}
}

// tclResultError reports a result mismatch.
type tclResultError struct {
	op  string
	got any
}

func (e *tclResultError) Error() string {
	return e.op + ": unexpected result"
}
