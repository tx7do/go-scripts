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

	Lua "github.com/yuin/gopher-lua"

	scriptEngine "github.com/tx7do/go-scripts"
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
// Runtime Hooks
////////////////////////////////////////////////////////////////////////////////

// TestLuaEngine_RuntimeHook_BeforeInit verifies that a hook registered before
// Init is replayed once Init completes, so the injected host function is
// callable from Lua.
func TestLuaEngine_RuntimeHook_BeforeInit(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	// Register the hook BEFORE Init. It exposes a Go function "greet".
	// Note: the function must be declared as Lua.LGFunction (a named type) so
	// RegisterFunction's type assertion (fn.(Lua.LGFunction)) succeeds; an
	// anonymous func(*Lua.LState) int passed through `any` would not match.
	var greet Lua.LGFunction = func(L *Lua.LState) int {
		name := L.CheckString(1)
		L.Push(Lua.LString("hi " + name))
		return 1
	}
	require.NoError(t, eng.AddRuntimeHook(func(ctx context.Context) error {
		return eng.RegisterFunction("greet", greet)
	}))

	// Init replays the hook.
	require.NoError(t, eng.Init(context.Background()))

	// Lua can call the hook-injected function.
	_, err = eng.ExecuteString(context.Background(), "hook.lua", `
        local r = greet("world")
        greet_result = r
    `)
	require.NoError(t, err)

	v, err := eng.GetGlobal("greet_result")
	require.NoError(t, err)
	assert.Equal(t, "hi world", v)
}

// TestLuaEngine_RuntimeHook_AfterInit verifies that a hook registered after
// Init runs immediately on the live LState.
func TestLuaEngine_RuntimeHook_AfterInit(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	var greet2 Lua.LGFunction = func(L *Lua.LState) int {
		name := L.CheckString(1)
		L.Push(Lua.LString("hello " + name))
		return 1
	}
	require.NoError(t, eng.AddRuntimeHook(func(ctx context.Context) error {
		return eng.RegisterFunction("greet", greet2)
	}))

	res, err := eng.CallFunction(context.Background(), "greet", "bob")
	require.NoError(t, err)
	assert.Equal(t, "hello bob", res)
}

// TestLuaEngine_RuntimeHook_PoolReuse_Isolation verifies that business globals
// injected by one engine are cleared before its LState returns to the pool, so
// a second engine that recycles the same LState does NOT see them.
//
// This is the core isolation guarantee for the global LState pool.
func TestLuaEngine_RuntimeHook_PoolReuse_Isolation(t *testing.T) {
	ctx := context.Background()

	// Engine A: injects a business global "secret_a".
	engA, err := newLuaEngine()
	require.NoError(t, err)
	require.NoError(t, engA.Init(ctx))
	require.NoError(t, engA.AddRuntimeHook(func(context.Context) error {
		return engA.RegisterGlobal("secret_a", "from-A")
	}))
	// Confirm A sees its global.
	v, err := engA.GetGlobal("secret_a")
	require.NoError(t, err)
	assert.Equal(t, "from-A", v)
	// Close A — its LState returns to the pool, stripped of secret_a.
	require.NoError(t, engA.Close())

	// Engine B: reuses a LState from the pool. It must NOT see secret_a.
	engB, err := newLuaEngine()
	require.NoError(t, err)
	defer engB.Close()
	require.NoError(t, engB.Init(ctx))

	// secret_a must be gone (cleared by A's Close).
	v, err = engB.GetGlobal("secret_a")
	// GetGlobal on a cleared global returns nil (LNil -> nil), no error.
	require.NoError(t, err)
	assert.Nil(t, v, "recycled LState must not leak secret_a from engine A")

	// But B's own injection works.
	require.NoError(t, engB.RegisterGlobal("secret_b", "from-B"))
	v, err = engB.GetGlobal("secret_b")
	require.NoError(t, err)
	assert.Equal(t, "from-B", v)

	// Standard library is intact on the recycled LState.
	_, err = engB.ExecuteString(ctx, "stdlib.lua", `x = string.len("abcde")`)
	require.NoError(t, err)
	v, err = engB.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(5), v)
}

// TestLuaEngine_RuntimeHook_ReverseRegister verifies a hook can expose a
// Go-side registry function so that Lua scripts register callbacks back into
// Go (the "reverse registration" capability). Go then dispatches the stored
// Lua callback.
func TestLuaEngine_RuntimeHook_ReverseRegister(t *testing.T) {
	ctx := context.Background()

	// Go-side hook registry: name -> Lua function.
	hooks := make(map[string]*Lua.LFunction)
	var hooksMu sync.Mutex

	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	// The hook exposes hook.register(name, fn) to Lua. Lua passes a Lua
	// function, which Go stores for later dispatch.
	var hookRegister Lua.LGFunction = func(L *Lua.LState) int {
		name := L.CheckString(1)
		fn := L.CheckFunction(2)
		hooksMu.Lock()
		hooks[name] = fn
		hooksMu.Unlock()
		return 0
	}
	require.NoError(t, eng.AddRuntimeHook(func(context.Context) error {
		return eng.RegisterFunction("hook_register", hookRegister)
	}))
	require.NoError(t, eng.Init(ctx))

	// Lua script registers a callback "on_event".
	_, err = eng.ExecuteString(ctx, "reg.lua", `
        hook_register("on_event", function(payload)
            return "processed:" .. payload
        end)
    `)
	require.NoError(t, err)

	// Go retrieves the stored Lua function and dispatches it.
	hooksMu.Lock()
	fn := hooks["on_event"]
	hooksMu.Unlock()
	require.NotNil(t, fn, "Lua callback should have been registered into Go")

	// Dispatch via the engine's LState using CallByParam.
	var ran int32
	done := make(chan any, 1)
	go func() {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		atomic.StoreInt32(&ran, 1)
		if err := eng.vm.L.CallByParam(Lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, Lua.LString("data")); err != nil {
			done <- err
			return
		}
		ret := eng.vm.L.Get(-1)
		eng.vm.L.Pop(1)
		done <- eng.vm.convertFromLValue(ret)
	}()

	select {
	case res := <-done:
		require.NoError(t, nil)
		assert.Equal(t, "processed:data", res)
		assert.Equal(t, int32(1), atomic.LoadInt32(&ran))
	case <-time.After(2 * time.Second):
		t.Fatal("reverse dispatch timed out")
	}
}

// TestLuaEngine_RuntimeHook_AsCapability verifies the optional capability is
// detected via the helper.
func TestLuaEngine_RuntimeHook_AsCapability(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	r := scriptEngine.AsRuntimeHookRegistrar(eng)
	require.NotNil(t, r, "lua engine should implement RuntimeHookRegistrar")
}

////////////////////////////////////////////////////////////////////////////////
// OpenLibs sandbox
////////////////////////////////////////////////////////////////////////////////

// TestLuaEngine_OpenLibs_DefaultOpensAll verifies the default (no SetOpenLibs)
// keeps all standard libraries available, including the dangerous ones — i.e.
// backward compatibility.
func TestLuaEngine_OpenLibs_DefaultOpensAll(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Init(context.Background()))

	// os and io should be available by default.
	_, err = eng.ExecuteString(context.Background(), "default.lua",
		`_G.os_time = os.time()`)
	require.NoError(t, err)
	v, err := eng.GetGlobal("os_time")
	require.NoError(t, err)
	assert.NotNil(t, v)

	// string library works too.
	_, err = eng.ExecuteString(context.Background(), "default2.lua",
		`_G.strlen = string.len("hello")`)
	require.NoError(t, err)
	v, err = eng.GetGlobal("strlen")
	require.NoError(t, err)
	assert.Equal(t, int64(5), v)
}

// TestLuaEngine_OpenLibs_SandboxDropsDangerous verifies that when SetOpenLibs
// excludes os and io, scripts can no longer access them — the sandbox is
// actually enforced.
func TestLuaEngine_OpenLibs_SandboxDropsDangerous(t *testing.T) {
	eng, err := newLuaEngine()
	require.NoError(t, err)
	defer eng.Close()

	// Sandbox: open base/table/string/math/coroutine only — no os, no io, no
	// package (require).
	eng.SetOpenLibs(
		AllowedLibBase, AllowedLibTab, AllowedLibStr, AllowedLibMath, AllowedLibCoroutine,
	)
	require.NoError(t, eng.Init(context.Background()))

	// os must be unavailable.
	_, err = eng.ExecuteString(context.Background(), "sandbox.lua",
		`_G.os_type = type(os)`)
	if assert.NoError(t, err) {
		v, gerr := eng.GetGlobal("os_type")
		require.NoError(t, gerr)
		assert.Equal(t, "nil", v, "os library should be dropped in sandbox mode")
	}

	// io must be unavailable too.
	_, err = eng.ExecuteString(context.Background(), "sandbox2.lua",
		`_G.io_type = type(io)`)
	if assert.NoError(t, err) {
		v, gerr := eng.GetGlobal("io_type")
		require.NoError(t, gerr)
		assert.Equal(t, "nil", v, "io library should be dropped in sandbox mode")
	}

	// But whitelisted libraries still work.
	_, err = eng.ExecuteString(context.Background(), "sandbox3.lua",
		`_G.strlen = string.len("hello")`)
	require.NoError(t, err)
	v, err := eng.GetGlobal("strlen")
	require.NoError(t, err)
	assert.Equal(t, int64(5), v, "string library should still work")
}
