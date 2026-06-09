package js

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestJavascriptEngine(t *testing.T) {
	// Create the engine.
	eng, err := newJavascriptEngine()
	assert.Nil(t, err)
	assert.NotNil(t, eng)
	defer eng.Close()

	// Initialize.
	ctx := context.Background()
	if err = eng.Init(ctx); err != nil {
		t.Fatal(err)
	}

	// Register a global variable.
	err = eng.RegisterGlobal("config", map[string]interface{}{
		"host": "localhost",
		"port": 8080,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Register a function.
	err = eng.RegisterFunction("log", func(msg string) {
		fmt.Println("JS Log:", msg)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Execute a script.
	result, err := eng.ExecuteString(ctx, `
    function add(a, b) {
        log('Adding ' + a + ' and ' + b);
        return a + b;
    }
    add(10, 20);
`)
	fmt.Println(result) // expected: 30

	// Call the function with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err = eng.CallFunction(ctx, "add", 100, 200)
	if err != nil {
		t.Error(err)
	}
	fmt.Println(result) // expected: 300
}

func TestConcurrentExecuteAndCallFunction(t *testing.T) {
	eng, err := newJavascriptEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err = eng.Init(ctx); err != nil {
		t.Fatal(err)
	}

	// Register a simple add function for CallFunction.
	if err = eng.RegisterFunction("add", func(a, b float64) float64 { return a + b }); err != nil {
		t.Fatal(err)
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Call ExecuteString and CallFunction concurrently.
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			// Each goroutine issues several calls.
			for j := 0; j < 20; j++ {
				// ExecuteString
				ctxExe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
				_, _ = eng.ExecuteString(ctxExe, "1 + 2 + 3")
				cancel()

				// CallFunction
				ctxCall, cancel2 := context.WithTimeout(ctx, 500*time.Millisecond)
				_, _ = eng.CallFunction(ctxCall, "add", 10, 20)
				cancel2()
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Finished successfully.
	case <-time.After(10 * time.Second):
		t.Fatal("timeout: concurrent execute/call did not finish")
	}
}

func TestConcurrentInitCloseAndExecute(t *testing.T) {
	eng, err := newJavascriptEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()

	// In the background, repeatedly Init / Register / Close.
	stopBg := make(chan struct{})
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		for i := 0; i < 50; i++ {
			_ = eng.Init(ctx)
			// Try to register a global; ignore errors (engine may be un/initialized).
			_ = eng.RegisterGlobal("g", map[string]any{"i": i})
			time.Sleep(5 * time.Millisecond)
			_ = eng.Close()
			time.Sleep(5 * time.Millisecond)
		}
		close(stopBg)
	}()

	// Execute short scripts concurrently. ErrJavascriptEngineNotInitialized may
	// occur around Init/Close transitions and is acceptable.
	const callers = 200
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(i int) {
			defer wg.Done()
			// Each caller performs many short invocations.
			for j := 0; j < 30; j++ {
				c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
				_, _ = eng.ExecuteString(c, "1+2+3+"+time.Now().Format("150405")) // short computation
				cancel()
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		<-stopBg
		bgWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Finished successfully.
	case <-time.After(20 * time.Second):
		t.Fatal("timeout: concurrent init/close and execute did not finish")
	}
}
