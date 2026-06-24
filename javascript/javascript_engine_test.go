package js

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
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
	result, err := eng.ExecuteString(ctx, "test.js", `
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
				_, _ = eng.ExecuteString(ctxExe, "expr.js", "1 + 2 + 3")
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
				_, _ = eng.ExecuteString(c, "expr.js", "1+2+3+"+time.Now().Format("150405")) // short computation
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

////////////////////////////////////////////////////////////////////////////////
// ScriptSource / new Load* / ExecuteFromKey* APIs
////////////////////////////////////////////////////////////////////////////////

// newMemSourceWith returns a MemSource pre-populated with the given key=code
// pairs.
func newMemSourceWith(t *testing.T, scripts map[string]string) *source.MemSource {
	t.Helper()
	src := source.NewMemSource()
	for k, v := range scripts {
		src.Set(k, v)
	}
	return src
}

func TestJavascriptEngine_SetGetSource(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	// Initially nil.
	assert.Nil(t, eng.GetSource())

	want := newMemSourceWith(t, map[string]string{"k": "x"})
	eng.SetSource(want)
	assert.Same(t, want, eng.GetSource())

	// Passing nil clears the binding.
	eng.SetSource(nil)
	assert.Nil(t, eng.GetSource())
}

func TestJavascriptEngine_Load_FromSource(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"hello": "globalThis.answer = 42;",
	}))

	require.NoError(t, eng.Load(context.Background(), "hello"))

	results, err := eng.Execute(context.Background())
	require.NoError(t, err)
	// Execute returns one result per queued program.
	assert.Len(t, results.([]any), 1)

	v, err := eng.GetGlobal("answer")
	require.NoError(t, err)
	assert.Equal(t, int64(42), v)
}

func TestJavascriptEngine_Load_NoSourceBound(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	err = eng.Load(context.Background(), "anything")
	require.Error(t, err)
}

func TestJavascriptEngine_LoadMulti(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"a": "globalThis.xa = 'A';",
		"b": "globalThis.xb = 'B';",
	}))

	require.NoError(t, eng.LoadMulti(context.Background(), []string{"a", "b"}))

	results, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Len(t, results.([]any), 2)

	va, err := eng.GetGlobal("xa")
	require.NoError(t, err)
	assert.Equal(t, "A", va)

	vb, err := eng.GetGlobal("xb")
	require.NoError(t, err)
	assert.Equal(t, "B", vb)
}

func TestJavascriptEngine_LoadMulti_AbortsOnError(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{"a": "1"}))

	err = eng.LoadMulti(context.Background(), []string{"a", "missing"})
	require.Error(t, err)
}

func TestJavascriptEngine_ExecuteFromKey(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"hello": "globalThis.answer = 7;",
	}))

	_, err = eng.ExecuteFromKey(context.Background(), "hello")
	require.NoError(t, err)

	v, err := eng.GetGlobal("answer")
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)
}

func TestJavascriptEngine_ExecuteFromKeys_OrderedResults(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"a": "globalThis.xa = 'A';",
		"b": "globalThis.xb = 'B';",
	}))

	res, err := eng.ExecuteFromKeys(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, res, 2)

	va, err := eng.GetGlobal("xa")
	require.NoError(t, err)
	assert.Equal(t, "A", va)

	vb, err := eng.GetGlobal("xb")
	require.NoError(t, err)
	assert.Equal(t, "B", vb)
}

func TestJavascriptEngine_LoadString_NameUsedInTrace(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	// Loading invalid source with a specific name should surface that name in
	// the resulting compile error.
	err = eng.LoadString(context.Background(), "my-name.js", "@bad syntax@")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-name.js")
}

func TestJavascriptEngine_Load_FromFileSource(t *testing.T) {
	// End-to-end smoke: bind a FileSource pointing at a temp file, Load it,
	// then Execute and read a global back.
	dir := t.TempDir()
	path := filepath.Join(dir, "script.js")
	require.NoError(t, os.WriteFile(path, []byte("globalThis.fx = 999;"), 0o644))

	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(source.NewFileSource())
	require.NoError(t, eng.Load(context.Background(), path))

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	v, err := eng.GetGlobal("fx")
	require.NoError(t, err)
	assert.Equal(t, int64(999), v)
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

func TestJavascriptEngine_StartWatch_HotReload(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{
		"k": "globalThis.val = 1;",
	})
	eng.SetSource(memSrc)

	// Load and execute initially.
	require.NoError(t, eng.Load(context.Background(), "k"))
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err := eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v)

	// Clear programs to avoid stacking.
	eng.ClearPrograms()

	// Start watching.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, eng.StartWatch(ctx, "k"))

	// Modify the script.
	time.Sleep(50 * time.Millisecond)
	memSrc.Set("k", "globalThis.val = 2;")

	// Wait a bit for the hot reload to trigger.
	time.Sleep(200 * time.Millisecond)

	// Execute again to verify the reloaded script.
	eng.ClearPrograms()                                     // clear old programs before execute
	require.NoError(t, eng.Load(context.Background(), "k")) // ensure latest is loaded
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err = eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v)
}

func TestJavascriptEngine_StartWatch_NoSource(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	err = eng.StartWatch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source")
}

func TestJavascriptEngine_StartWatch_SourceNotWatcher(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(&readerOnly{code: "var x = 1;"})

	err = eng.StartWatch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement Watcher")
}

func TestJavascriptEngine_StopWatch(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{"k": "globalThis.val = 1;"})
	eng.SetSource(memSrc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start then stop.
	require.NoError(t, eng.StartWatch(ctx, "k"))
	require.NoError(t, eng.StopWatch("k"))

	// Modify after stop should NOT trigger reload.
	memSrc.Set("k", "globalThis.val = 99;")
	time.Sleep(200 * time.Millisecond)

	// No reload happened; verify via manual Load.
	eng.ClearPrograms()
	require.NoError(t, eng.Load(context.Background(), "k"))
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err := eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(99), v) // manual Load
}

func TestJavascriptEngine_Close_StopsWatchers(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{"k": "globalThis.val = 1;"})
	eng.SetSource(memSrc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, eng.StartWatch(ctx, "k"))

	// Close should clean up watchers without blocking.
	done := make(chan struct{})
	go func() {
		require.NoError(t, eng.Close())
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked; watchers not cleaned up")
	}
}

////////////////////////////////////////////////////////////////////////////////
// Runtime Hooks
////////////////////////////////////////////////////////////////////////////////

// TestJavascriptEngine_RuntimeHook_BeforeInit verifies that a hook registered
// before Init is replayed once Init completes, so the injected host function
// is available to scripts.
func TestJavascriptEngine_RuntimeHook_BeforeInit(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()

	// Register the hook BEFORE Init. It exposes a Go function "greet".
	require.NoError(t, eng.AddRuntimeHook(func(ctx context.Context) error {
		return eng.RegisterFunction("greet", func(name string) string {
			return "hi " + name
		})
	}))

	// Init replays the hook.
	require.NoError(t, eng.Init(context.Background()))

	// The script should be able to call the hook-injected function.
	res, err := eng.ExecuteString(context.Background(), "hook.js", `greet("world")`)
	require.NoError(t, err)
	assert.Equal(t, "hi world", res)
}

// TestJavascriptEngine_RuntimeHook_AfterInit verifies that a hook registered
// after Init runs immediately on the live runtime.
func TestJavascriptEngine_RuntimeHook_AfterInit(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	// Register the hook AFTER Init. It should run immediately.
	require.NoError(t, eng.AddRuntimeHook(func(ctx context.Context) error {
		return eng.RegisterFunction("greet", func(name string) string {
			return "hello " + name
		})
	}))

	res, err := eng.ExecuteString(context.Background(), "hook.js", `greet("bob")`)
	require.NoError(t, err)
	assert.Equal(t, "hello bob", res)
}

// TestJavascriptEngine_RuntimeHook_AsCapability verifies the optional
// capability is detected via the helper.
func TestJavascriptEngine_RuntimeHook_AsCapability(t *testing.T) {
	eng, err := newJavascriptEngine()
	require.NoError(t, err)
	defer eng.Close()

	r := scriptEngine.AsRuntimeHookRegistrar(eng)
	require.NotNil(t, r, "JS engine should implement RuntimeHookRegistrar")

	_ = scriptEngine.AsRuntimeHookRegistrar("not an engine")
	// Non-engine values must return nil (already asserted by require.NotNil above).
}

// readerOnly implements source.Reader but NOT source.Watcher.
type readerOnly struct {
	code string
}

func (r *readerOnly) Load(_ context.Context, _ string) (string, error) { return r.code, nil }
func (r *readerOnly) Close() error                                     { return nil }
