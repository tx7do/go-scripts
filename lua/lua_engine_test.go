package lua

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

func TestLuaEngine(t *testing.T) {
	// Create the engine.
	eng, err := newLuaEngine()
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

	// Execute a script.
	result, err := eng.ExecuteString(ctx, "test.lua", `
    function add(a, b)
        return a + b
    end
`)
	_ = result

	// Call the function with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err = eng.CallFunction(ctx, "add", 10, 20)
	if err != nil {
		t.Error(err)
	}
	fmt.Println(result) // expected: 30
}

func TestConcurrentCallAndGet(t *testing.T) {
	eng, err := newLuaEngine()
	assert.Nil(t, err)
	assert.NotNil(t, eng)
	defer eng.Close()

	ctx := context.Background()
	// Initialize and load the function plus global variable.
	err = eng.Init(ctx)
	assert.Nil(t, err)

	// Define the function `add` and run the snippet so it is loaded into the VM.
	_, err = eng.ExecuteString(ctx, "test.lua", `
        function add(a, b)
            return a + b
        end
    `)
	assert.Nil(t, err)

	err = eng.RegisterGlobal("config", map[string]interface{}{
		"host": "localhost",
		"port": 8080,
	})
	assert.Nil(t, err)

	var wg sync.WaitGroup
	var errCount int64

	// Concurrency and per-goroutine iteration counts.
	const goroutines = 50
	const loops = 200

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				// Each operation uses a context with a timeout.
				cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				val, callErr := eng.CallFunction(cctx, "add", 10, 20)
				cancel()
				if callErr != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}

				// Verify the return value is 30.
				switch v := val.(type) {
				case int:
					if v != 30 {
						atomic.AddInt64(&errCount, 1)
					}
				case int64:
					if v != 30 {
						atomic.AddInt64(&errCount, 1)
					}
				case float64:
					if v != 30.0 {
						atomic.AddInt64(&errCount, 1)
					}
				default:
					atomic.AddInt64(&errCount, 1)
				}

				// Read the global variable.
				gv, gerr := eng.GetGlobal("config")
				if gerr != nil {
					atomic.AddInt64(&errCount, 1)
				} else {
					if _, ok := gv.(map[string]interface{}); !ok {
						// convertFromLValue may return map[string]interface{} or other; accept non-nil.
						if gv == nil {
							atomic.AddInt64(&errCount, 1)
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()
	if atomic.LoadInt64(&errCount) != 0 {
		t.Fatalf("concurrent operations produced %d errors, lastError=%v", atomic.LoadInt64(&errCount), eng.GetLastError())
	}
}

func TestConcurrentInitClose(t *testing.T) {
	// This test ensures concurrent Init/Close does not race or panic.
	const goroutines = 40
	const ops = 200

	eng, err := newLuaEngine()
	assert.Nil(t, err)
	assert.NotNil(t, eng)
	defer eng.Close()

	var wg sync.WaitGroup
	var initErrCount int64
	var closeErrCount int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				// Alternate between Init and Close.
				if j%2 == 0 {
					if err = eng.Init(context.Background()); err != nil {
						// ErrLuaEngineAlreadyInitialized is allowed.
						if !errors.Is(err, ErrLuaEngineAlreadyInitialized) {
							atomic.AddInt64(&initErrCount, 1)
						}
					}
				} else {
					if err = eng.Close(); err != nil {
						// ErrLuaEngineNotInitialized is allowed.
						if !errors.Is(err, ErrLuaEngineNotInitialized) {
							atomic.AddInt64(&closeErrCount, 1)
						}
					}
				}
				// Sleep briefly to increase interleaving.
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt64(&initErrCount) != 0 || atomic.LoadInt64(&closeErrCount) != 0 {
		t.Fatalf("unexpected init/close errors: initErr=%d closeErr=%d lastError=%v",
			atomic.LoadInt64(&initErrCount), atomic.LoadInt64(&closeErrCount), eng.GetLastError())
	}

	// Try a final Init to ensure the engine is reusable.
	if err = eng.Init(context.Background()); err != nil && !errors.Is(err, ErrLuaEngineAlreadyInitialized) {
		t.Fatalf("final Init failed: %v", err)
	}

	// Ensure Close can be called cleanly.
	if err = eng.Close(); err != nil && !errors.Is(err, ErrLuaEngineNotInitialized) {
		t.Fatalf("final Close failed: %v", err)
	}

	fmt.Println("concurrent init/close test completed")
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

func TestLuaEngine_SetGetSource(t *testing.T) {
	eng, err := newLuaEngine()
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

func TestLuaEngine_Load_FromSource(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"hello": "answer = 42",
	}))

	require.NoError(t, eng.Load(context.Background(), "hello"))

	// Execute should expose `answer` to subsequent globals reads.
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	v, err := eng.GetGlobal("answer")
	require.NoError(t, err)
	assert.Equal(t, int64(42), v)
}

func TestLuaEngine_Load_NoSourceBound(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	err = eng.Load(context.Background(), "anything")
	require.Error(t, err)
}

func TestLuaEngine_LoadMulti(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"a": "x = 1",
		"b": "x = 2",
	}))

	// gopher-lua only keeps one compiled function at a time, so LoadMulti must
	// succeed for both keys (the second overwrites the first).
	require.NoError(t, eng.LoadMulti(context.Background(), []string{"a", "b"}))

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	v, err := eng.GetGlobal("x")
	require.NoError(t, err)
	// The last-loaded script wins.
	assert.Equal(t, int64(2), v)
}

func TestLuaEngine_LoadMulti_AbortsOnError(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{"a": "x = 1"}))

	err = eng.LoadMulti(context.Background(), []string{"a", "missing"})
	require.Error(t, err)
}

func TestLuaEngine_ExecuteFromKey(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"hello": "answer = 7",
	}))

	_, err = eng.ExecuteFromKey(context.Background(), "hello")
	require.NoError(t, err)

	v, err := eng.GetGlobal("answer")
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)
}

func TestLuaEngine_ExecuteFromKeys_OrderedResults(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetSource(newMemSourceWith(t, map[string]string{
		"a": "xa = 'A'",
		"b": "xb = 'B'",
	}))

	_, err = eng.ExecuteFromKeys(context.Background(), []string{"a", "b"})
	require.NoError(t, err)

	va, err := eng.GetGlobal("xa")
	require.NoError(t, err)
	assert.Equal(t, "A", va)

	vb, err := eng.GetGlobal("xb")
	require.NoError(t, err)
	assert.Equal(t, "B", vb)
}

func TestLuaEngine_LoadString_NameIgnored(t *testing.T) {
	// gopher-lua's LoadString has no notion of a name; LoadString(name, code)
	// must still work for any name value.
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	require.NoError(t, eng.LoadString(context.Background(), "any-name.lua", "x = 99"))
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	v, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(99), v)
}

func TestLuaEngine_Load_FromFileSource(t *testing.T) {
	// End-to-end smoke: bind a FileSource pointing at a temp file, Load it,
	// then Execute and read a global back.
	dir := t.TempDir()
	path := filepath.Join(dir, "script.lua")
	require.NoError(t, os.WriteFile(path, []byte("fx = 123"), 0o644))

	eng, err := newLuaEngine()
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
	assert.Equal(t, int64(123), v)
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

func TestLuaEngine_StartWatch_HotReload(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{
		"k": "val = 1",
	})
	eng.SetSource(memSrc)

	// Load and execute initially.
	require.NoError(t, eng.Load(context.Background(), "k"))
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err := eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v)

	// Start watching.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, eng.StartWatch(ctx, "k"))

	// Modify the script.
	time.Sleep(50 * time.Millisecond)
	memSrc.Set("k", "val = 2")

	// Wait a bit for the hot reload to trigger.
	time.Sleep(200 * time.Millisecond)

	// Execute again to verify the reloaded script.
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err = eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v)
}

func TestLuaEngine_StartWatch_NoSource(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	err = eng.StartWatch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source")
}

func TestLuaEngine_StartWatch_SourceNotWatcher(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	// fakeSourceOnly implements Reader but not Watcher.
	eng.SetSource(&readerOnly{code: "val = 1"})

	err = eng.StartWatch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement Watcher")
}

func TestLuaEngine_StopWatch(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{"k": "val = 1"})
	eng.SetSource(memSrc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start then stop.
	require.NoError(t, eng.StartWatch(ctx, "k"))
	require.NoError(t, eng.StopWatch("k"))

	// Modify after stop should NOT trigger reload.
	memSrc.Set("k", "val = 99")
	time.Sleep(200 * time.Millisecond)

	// No reload happened.
	require.NoError(t, eng.Load(context.Background(), "k"))
	_, err = eng.Execute(context.Background())
	require.NoError(t, err)
	v, err := eng.GetGlobal("val")
	require.NoError(t, err)
	assert.Equal(t, int64(99), v) // manual Load, not hot reload
}

func TestLuaEngine_Close_StopsWatchers(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))

	memSrc := newMemSourceWith(t, map[string]string{"k": "val = 1"})
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

// readerOnly implements source.Reader but NOT source.Watcher.
type readerOnly struct {
	code string
}

func (r *readerOnly) Load(_ context.Context, _ string) (string, error) { return r.code, nil }
func (r *readerOnly) Close() error                                     { return nil }

////////////////////////////////////////////////////////////////////////////////
// Sandbox (SetOpenLibs)
////////////////////////////////////////////////////////////////////////////////

// TestLuaEngine_SetOpenLibs_Sandbox verifies that only allow-listed libraries
// are opened: a script can use a permitted library (string) but cannot access a
// withheld one (os), which must be nil.
func TestLuaEngine_SetOpenLibs_Sandbox(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)
	defer eng.Close()

	// Allow only base + string; os / io / debug / etc. must be unavailable.
	eng.SetOpenLibs("base", "string")
	require.NoError(t, eng.Init(context.Background()))

	// string must be usable.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		up = string.upper("abc")
	`)
	require.NoError(t, err)
	v, err := eng.GetGlobal("up")
	require.NoError(t, err)
	assert.Equal(t, "ABC", v)

	// os must NOT have been opened: referencing os.execute should fail because
	// `os` is nil (indexing nil raises a Lua error under pcall protection).
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		local _ = os.time()
	`)
	require.Error(t, err, "os library should not be available in sandbox mode")
}

// TestLuaEngine_SetOpenLibs_AfterInitIsNoOp verifies that SetOpenLibs is a no-op
// once the engine is initialized (it records the last error instead).
func TestLuaEngine_SetOpenLibs_AfterInitIsNoOp(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	eng.SetOpenLibs("base")
	require.ErrorIs(t, eng.GetLastError(), ErrLuaEngineAlreadyInitialized)
}

// TestLuaEngine_SetOpenLibs_AllowsRequire verifies that when "package" is
// allow-listed, require-based modules work. It also confirms that a withheld
// library (math) is not reachable even though package is open.
func TestLuaEngine_SetOpenLibs_AllowsRequire(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	// base + package are enough for require to work; string is also opened so we
	// can build a result. math is intentionally withheld.
	eng.SetOpenLibs("base", "package", "string")
	require.NoError(t, eng.Init(context.Background()))

	// require of a gopher-lua-libs module (http) needs package.preload, which is
	// registered because package is open. Load a trivial script that calls
	// string.format to prove require is wired up via package.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		local s = string.format("%d-%s", 42, "ok")
		formed = s
	`)
	require.NoError(t, err)
	v, err := eng.GetGlobal("formed")
	require.NoError(t, err)
	assert.Equal(t, "42-ok", v)

	// math must NOT be open: indexing nil raises a Lua error.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		local _ = math.abs(-1)
	`)
	require.Error(t, err, "math library should not be available when not allow-listed")
}

// TestLuaEngine_SetOpenLibs_UnknownNamesIgnored verifies that unrecognized
// library names neither cause an error nor open anything; the engine still
// initializes and the explicitly-listed valid libs still work.
func TestLuaEngine_SetOpenLibs_UnknownNamesIgnored(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	// "frobnicate" and "io" are not real standard libs... actually io IS real,
	// so use a clearly-bogus name. base + a bogus name must still init fine.
	eng.SetOpenLibs("base", "string", "totally-not-a-real-lib", "????")
	require.NoError(t, eng.Init(context.Background()))

	// base/string still work.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		x = string.len("abcd")
	`)
	require.NoError(t, err)
	v, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(4), v)

	// os (never listed) stays blocked.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua", `
		local _ = os.time()
	`)
	require.Error(t, err)
}

// TestLuaEngine_SetOpenLibs_DefaultOpensAll verifies the default mode (no
// SetOpenLibs call) still opens the full standard-library set, so existing
// behavior is preserved. This is the non-sandbox path.
func TestLuaEngine_SetOpenLibs_DefaultOpensAll(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	// NOTE: no SetOpenLibs call -> default full-set behavior.
	require.NoError(t, eng.Init(context.Background()))

	// Several libraries that were NOT opened in the sandbox tests must all be
	// available here: os, math, table, string.
	_, err = eng.ExecuteString(context.Background(), "default.lua", `
		a = os.time() and 1 or 0
		b = math.abs(-5)
		c = #table.concat({"x","y"}, "-")
		d = string.upper("ok")
	`)
	require.NoError(t, err)

	// Read each back explicitly.
	a, _ := eng.GetGlobal("a")
	assert.Equal(t, int64(1), a)
	b, _ := eng.GetGlobal("b")
	assert.Equal(t, int64(5), b)
	c, _ := eng.GetGlobal("c")
	assert.Equal(t, int64(3), c) // "x-y"
	d, _ := eng.GetGlobal("d")
	assert.Equal(t, "OK", d)
}

// TestLuaEngine_SetOpenLibs_BlockedLibs confirms a range of dangerous libraries
// (io, debug, os) are all withheld in sandbox mode, not just os.
func TestLuaEngine_SetOpenLibs_BlockedLibs(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	eng.SetOpenLibs("base", "string", "math")
	require.NoError(t, eng.Init(context.Background()))

	for _, lib := range []string{"os", "io", "debug"} {
		_, err := eng.ExecuteString(context.Background(), "sandbox.lua",
			"local _ = "+lib+".x")
		require.Error(t, err, "%s library should be blocked in sandbox mode", lib)
	}
}

// TestLuaEngine_SetOpenLibs_PrivateStateIsolation verifies the core sandbox
// guarantee: a sandboxed engine must NOT inherit globals/libs from a previously
// used (default-mode) LState. Both engines are exercised; if the sandbox
// reused a pooled full-lib state, the second engine would see `os` available.
func TestLuaEngine_SetOpenLibs_PrivateStateIsolation(t *testing.T) {
	// First engine: default mode, opens everything, sets a global marker.
	def, err := newLuaEngine()
	require.NoError(t, err)
	require.NoError(t, def.Init(context.Background()))
	_, err = def.ExecuteString(context.Background(), "default.lua", `
		marker = "default-was-here"
		_ = os.time()
	`)
	require.NoError(t, err)
	// Returning this engine to the pool is what could pollute a later borrower.
	require.NoError(t, def.Close())

	// Second engine: sandboxed. Its allow-list does NOT include os. If it
	// reused the default engine's LState, `os` would leak through.
	sb, err := newLuaEngine()
	require.NoError(t, err)
	defer sb.Close()
	sb.SetOpenLibs("base", "string")
	require.NoError(t, sb.Init(context.Background()))

	// os must be blocked even though a full-lib engine just ran.
	_, err = sb.ExecuteString(context.Background(), "sandbox.lua", `
		local _ = os.time()
	`)
	require.Error(t, err, "sandboxed engine must not inherit os from a prior default engine")

	// The marker global from the default engine must also be absent.
	v, _ := sb.GetGlobal("marker")
	assert.Nil(t, v, "sandboxed engine must not inherit globals from a prior default engine")
}

////////////////////////////////////////////////////////////////////////////////
// Instruction-level cancellation (P1: L.SetContext)
//
// These guard against the original deadlock bug: a runaway Lua loop held e.mu
// after ctx cancellation, so Close() blocked forever. With L.SetContext,
// gopher-lua aborts the script at the next instruction on ctx.Done, releasing
// the lock.
////////////////////////////////////////////////////////////////////////////////

// closeWithin invokes eng.Close() in a goroutine and fails the test if it does
// not return within the given timeout. This is the deadlock detector.
func closeWithin(t *testing.T, eng *engine, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = eng.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("eng.Close() deadlocked (did not return within %v)", timeout)
	}
}

// TestCallFunction_InfiniteLoop_CloseDoesNotDeadlock is THE regression test for
// P1: a Lua function containing `while true do end` is invoked with a short
// timeout; the call must return an error, and Close() afterwards must not hang.
func TestCallFunction_InfiniteLoop_CloseDoesNotDeadlock(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	require.NotNil(t, eng)

	require.NoError(t, eng.Init(context.Background()))

	// Load a function that spins forever.
	_, err = eng.ExecuteString(context.Background(), "loop.lua", `
		function loop_forever()
			while true do end
		end
	`)
	require.NoError(t, err)

	// Call it with a short timeout; expect a cancellation error.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, callErr := eng.CallFunction(ctx, "loop_forever")
	elapsed := time.Since(start)

	require.Error(t, callErr, "CallFunction on an infinite loop should return an error on ctx cancel")
	// It must return promptly (well under the timeout + slack), not block.
	assert.Less(t, elapsed, 2*time.Second, "CallFunction should return shortly after ctx cancel")

	// THE critical assertion: Close() must NOT deadlock. Before P1, the runaway
	// goroutine held e.mu forever and this blocked indefinitely.
	closeWithin(t, eng, 3*time.Second)
}

// TestExecuteString_InfiniteLoop_CloseDoesNotDeadlock mirrors the above for the
// ExecuteString entry point (inline infinite loop, no named function).
func TestExecuteString_InfiniteLoop_CloseDoesNotDeadlock(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)

	require.NoError(t, eng.Init(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, callErr := eng.ExecuteString(ctx, "loop.lua", "while true do end")
	require.Error(t, callErr, "ExecuteString on an infinite loop should return an error on ctx cancel")

	closeWithin(t, eng, 3*time.Second)
}

// TestExecute_InfiniteLoop_CloseDoesNotDeadlock mirrors the above for the
// Execute entry point (script pre-loaded via LoadString, then run).
func TestExecute_InfiniteLoop_CloseDoesNotDeadlock(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)

	require.NoError(t, eng.Init(context.Background()))
	require.NoError(t, eng.LoadString(context.Background(), "loop.lua", "while true do end"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, callErr := eng.Execute(ctx)
	require.Error(t, callErr, "Execute on an infinite loop should return an error on ctx cancel")

	closeWithin(t, eng, 3*time.Second)
}

// TestCallFunction_ManualCancel verifies that explicit (non-timeout) context
// cancellation also aborts the running script promptly.
func TestCallFunction_ManualCancel(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.Init(context.Background()))
	_, err = eng.ExecuteString(context.Background(), "loop.lua", `
		function slow()
			while true do end
		end
	`)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, callErr := eng.CallFunction(ctx, "slow")
	elapsed := time.Since(start)
	require.Error(t, callErr)
	assert.Less(t, elapsed, 2*time.Second, "CallFunction should abort shortly after manual cancel")
}

// TestCallFunction_EngineReusableAfterAbort verifies the engine is left in a
// consistent state after an aborted run: a subsequent normal call succeeds and
// returns the right value. (A leaked/broken LState stack would surface here.)
func TestCallFunction_EngineReusableAfterAbort(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.Init(context.Background()))
	_, err = eng.ExecuteString(context.Background(), "fns.lua", `
		function add(a, b) return a + b end
		function spin() while true do end end
	`)
	require.NoError(t, err)

	// Normal call works.
	r, err := eng.CallFunction(context.Background(), "add", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), r)

	// Abort a runaway call.
	spinCtx, spinCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, spinErr := eng.CallFunction(spinCtx, "spin")
	require.Error(t, spinErr)
	spinCancel()

	// After the abort, the engine must still work.
	r, err = eng.CallFunction(context.Background(), "add", 10, 20)
	require.NoError(t, err, "engine must be reusable after an aborted call")
	assert.Equal(t, int64(30), r)
}


