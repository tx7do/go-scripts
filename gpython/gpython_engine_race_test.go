package gpython

// Concurrency / race stress tests: ExecuteString, CallFunction, GetGlobal and
// registration churn share one engine (serialized internally by execMu) so
// `go test -race` sees the full shared-state surface.

import (
	"context"
	"sync"
	"testing"
)

// TestGPythonRace_ExecuteAndCallMixed: concurrent statement runs increment a
// shared global (serialization makes the final value exact), scripted-function
// calls return exact products, and global churn runs alongside.
func TestGPythonRace_ExecuteAndCallMixed(t *testing.T) {
	eng, err := newGPythonEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	if _, err := eng.ExecuteString(context.Background(), "init", "counter = 0\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ExecuteString(context.Background(), "def", "def multiply(a, b):\n    return a * b\n"); err != nil {
		t.Fatal(err)
	}

	const goroutines, runs = 5, 10

	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for g := 0; g < goroutines; g++ { // exact-count serialized increments
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < runs; i++ {
				if _, err := eng.ExecuteString(context.Background(), "inc", "counter = counter + 1\n"); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for g := 0; g < goroutines; g++ { // scripted-function calls
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < runs; i++ {
				a := int64(id*runs + i + 1)
				res, err := eng.CallFunction(context.Background(), "multiply", a, int64(2))
				if err != nil {
					errs <- err
					return
				}
				if res != 2*a {
					errs <- &gpythonResultError{op: "CallFunction", got: res}
					return
				}
			}
		}(g)
	}
	wg.Add(1)
	go func() { // global churn while the others run
		defer wg.Done()
		for i := 0; i < runs; i++ {
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

	v, err := eng.GetGlobal("counter")
	if err != nil {
		t.Fatal(err)
	}
	if v != int64(goroutines*runs) {
		t.Fatalf("counter = %v, want %d — runs were not serialized", v, goroutines*runs)
	}
}

// gpythonResultError reports a result mismatch.
type gpythonResultError struct {
	op  string
	got any
}

func (e *gpythonResultError) Error() string {
	return e.op + ": unexpected result"
}
