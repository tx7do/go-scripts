package gpython

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

// newTestEngine creates and initializes a GPython engine for testing.
func newTestEngine(t *testing.T) *engine {
	t.Helper()
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

////////////////////////////////////////////////////////////////////////////////
// Lifecycle tests
////////////////////////////////////////////////////////////////////////////////

func TestGetType(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	assert.Equal(t, scriptEngine.GPythonType, eng.GetType())
}

func TestInit(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
	_ = eng.Close()
}

func TestInit_AlreadyInitialized(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.Init(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonEngineAlreadyInitialized)
}

func TestClose(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	require.NoError(t, eng.Close())
	assert.False(t, eng.IsInitialized())
}

func TestClose_NotInitialized(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	err = eng.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

func TestIsInitialized(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	assert.False(t, eng.IsInitialized())
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
	require.NoError(t, eng.Close())
	assert.False(t, eng.IsInitialized())
}

func TestOperations_NotInitialized(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)

	assert.ErrorIs(t, eng.Load(context.Background(), "k"), ErrGPythonEngineNotInitialized)
	_, err = eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
	_, err = eng.ExecuteString(context.Background(), "n", "1 + 1")
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
	assert.ErrorIs(t, eng.RegisterGlobal("x", 1), ErrGPythonEngineNotInitialized)
	_, err = eng.GetGlobal("x")
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Source tests
////////////////////////////////////////////////////////////////////////////////

func TestSetGetSource(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	eng.SetSource(src)
	assert.Equal(t, src, eng.GetSource())

	eng.SetSource(nil)
	assert.Nil(t, eng.GetSource())
}

func TestLoadNoSource(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.Load(context.Background(), "script.py")
	assert.Error(t, err)
}

func TestExecuteFromKeyNoSource(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteFromKey(context.Background(), "script.py")
	assert.Error(t, err)
}

////////////////////////////////////////////////////////////////////////////////
// Script execution tests
////////////////////////////////////////////////////////////////////////////////

func TestExecuteString_SimpleArith(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "x = 1 + 2\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

func TestExecuteString_StringConcat(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `s = "hello" + " " + "world"
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("s")
	require.NoError(t, err)
	assert.Equal(t, "hello world", val)
}

func TestExecuteString_Bool(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "b = True and False\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, false, val)
}

func TestExecuteString_FunctionDef(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
def add(a, b):
    return a + b
`)
	require.NoError(t, err)

	val, err := eng.CallFunction(context.Background(), "add", int64(3), int64(4))
	require.NoError(t, err)
	assert.Equal(t, int64(7), val)
}

func TestExecuteString_InvalidCode(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "def broken(\n")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonCompileFailed)
}

func TestExecute_NoScriptLoaded(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.Execute(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonNoScriptLoaded)
}

func TestLoadString_ThenExecute(t *testing.T) {
	eng := newTestEngine(t)

	require.NoError(t, eng.LoadString(context.Background(), "a", "a = 10\n"))
	require.NoError(t, eng.LoadString(context.Background(), "b", "b = a + 20\n"))

	_, err := eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

func TestExecuteString_RuntimeError(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
x = [1, 2, 3]
y = x[10]
`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonRunFailed)
}

func TestExecuteString_MultiStatements(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
x = 10
y = 20
z = x + y
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("z")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

////////////////////////////////////////////////////////////////////////////////
// Global variable tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterGlobal_Int(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(42)))

	_, err := eng.ExecuteString(context.Background(), "test", "y = x + 8\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("y")
	require.NoError(t, err)
	assert.Equal(t, int64(50), val)
}

func TestRegisterGlobal_String(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("name", "Alice"))

	_, err := eng.ExecuteString(context.Background(), "test", `greeting = "Hello, " + name
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("greeting")
	require.NoError(t, err)
	assert.Equal(t, "Hello, Alice", val)
}

func TestRegisterGlobal_Bool(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("flag", true))

	_, err := eng.ExecuteString(context.Background(), "test", "result = flag and True\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, true, val)
}

func TestRegisterGlobal_Overwrite(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(10)))
	require.NoError(t, eng.RegisterGlobal("x", int64(99)))

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(99), val)
}

func TestRegisterGlobal_Float(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("pi", 3.14))

	_, err := eng.ExecuteString(context.Background(), "test", "twice = pi * 2.0\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("twice")
	require.NoError(t, err)
	assert.Equal(t, 6.28, val)
}

func TestGetGlobal(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(42)))

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestGetGlobal_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetGlobal("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonGlobalNotFound)
}

func TestRegisterGlobal_NotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	err := eng.RegisterGlobal("x", 1)
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

func TestRegisterGlobal_FromScript(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
counter = 100
name = "test"
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("counter")
	require.NoError(t, err)
	assert.Equal(t, int64(100), val)

	val, err = eng.GetGlobal("name")
	require.NoError(t, err)
	assert.Equal(t, "test", val)
}

////////////////////////////////////////////////////////////////////////////////
// Function tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterFunction_Simple(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("double", func(x int64) int64 {
		return x * 2
	}))

	result, err := eng.CallFunction(context.Background(), "double", int64(21))
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestRegisterFunction_TwoArgs(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("add", func(a, b int64) int64 {
		return a + b
	}))

	result, err := eng.CallFunction(context.Background(), "add", int64(10), int64(20))
	require.NoError(t, err)
	assert.Equal(t, int64(30), result)
}

func TestRegisterFunction_String(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("greet", func(name string) string {
		return "Hello, " + name + "!"
	}))

	result, err := eng.CallFunction(context.Background(), "greet", "World")
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestRegisterFunction_Overwrite(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("calc", func(x int64) int64 {
		return x + 1
	}))
	result, err := eng.CallFunction(context.Background(), "calc", int64(5))
	require.NoError(t, err)
	assert.Equal(t, int64(6), result)

	require.NoError(t, eng.RegisterFunction("calc", func(x int64) int64 {
		return x * 10
	}))
	result, err = eng.CallFunction(context.Background(), "calc", int64(5))
	require.NoError(t, err)
	assert.Equal(t, int64(50), result)
}

func TestRegisterFunction_NotFunc(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.RegisterFunction("bad", "not a func")
	require.Error(t, err)
}

func TestRegisterFunction_NotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	err := eng.RegisterFunction("fn", func() {})
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

func TestCallFunction_PythonDefined(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
def multiply(a, b):
    return a * b
`)
	require.NoError(t, err)

	result, err := eng.CallFunction(context.Background(), "multiply", int64(6), int64(7))
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestCallFunction_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.CallFunction(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGPythonFunctionNotFound)
}

func TestCallFunction_NotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	_, err := eng.CallFunction(context.Background(), "fn")
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Module tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterModule(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.RegisterModule("config", map[string]any{
		"timeout": int64(30),
		"name":    "myapp",
	})
	require.NoError(t, err)

	val, err := eng.GetGlobal("config_timeout")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)

	val, err = eng.GetGlobal("config_name")
	require.NoError(t, err)
	assert.Equal(t, "myapp", val)
}

func TestRegisterModule_BadType(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.RegisterModule("bad", "not a map")
	assert.Error(t, err)
}

func TestRegisterModule_NotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	err := eng.RegisterModule("m", map[string]any{})
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Load / Source tests
////////////////////////////////////////////////////////////////////////////////

func TestLoadFromSource(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("script1.py", "x = 1 + 2\n")
	eng.SetSource(src)

	err := eng.Load(context.Background(), "script1.py")
	require.NoError(t, err)

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

func TestLoadMulti(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a.py", "a = 10\n")
	src.Set("b.py", "b = a + 20\n")
	eng.SetSource(src)

	err := eng.LoadMulti(context.Background(), []string{"a.py", "b.py"})
	require.NoError(t, err)

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

func TestLoadMultiError(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a.py", "x = 1\n")
	eng.SetSource(src)

	err := eng.LoadMulti(context.Background(), []string{"a.py", "missing.py"})
	assert.Error(t, err)
}

func TestExecuteFromKey(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("expr.py", "x = 5 * 6\n")
	eng.SetSource(src)

	_, err := eng.ExecuteFromKey(context.Background(), "expr.py")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

func TestExecuteFromKeys(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a.py", "a = 10\n")
	src.Set("b.py", "b = 20\n")
	eng.SetSource(src)

	results, err := eng.ExecuteFromKeys(context.Background(), []string{"a.py", "b.py"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestLoadStringNotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	err := eng.LoadString(context.Background(), "script.py", "x = 1\n")
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

func TestExecuteStringNotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	_, err := eng.ExecuteString(context.Background(), "script.py", "x = 1\n")
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

func TestExecuteNotInitialized(t *testing.T) {
	eng, _ := newGPythonEngine()
	_, err := eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrGPythonEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Error handling tests
////////////////////////////////////////////////////////////////////////////////

func TestLastError(t *testing.T) {
	eng := newTestEngine(t)
	assert.NoError(t, eng.GetLastError())

	_ = eng.LoadString(context.Background(), "bad.py", "def broken(\n")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

func TestClearError(t *testing.T) {
	eng := newTestEngine(t)
	_ = eng.LoadString(context.Background(), "bad.py", "def broken(\n")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

////////////////////////////////////////////////////////////////////////////////
// Factory tests
////////////////////////////////////////////////////////////////////////////////

func TestFactoryRegistration(t *testing.T) {
	factory, ok := scriptEngine.GetFactory(scriptEngine.GPythonType)
	assert.True(t, ok)
	assert.NotNil(t, factory)
}

func TestFactoryCreateAndUse(t *testing.T) {
	eng, err := scriptEngine.NewScriptEngine(scriptEngine.GPythonType)
	require.NoError(t, err)
	require.NotNil(t, eng)
	require.NoError(t, eng.Init(context.Background()))
	defer eng.Close()

	_, err = eng.ExecuteString(context.Background(), "test", "x = 1 + 2\n")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

////////////////////////////////////////////////////////////////////////////////
// Python language feature tests
////////////////////////////////////////////////////////////////////////////////

func TestPython_List(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
items = [1, 2, 3, 4, 5]
total = sum(items)
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("total")
	require.NoError(t, err)
	assert.Equal(t, int64(15), val)
}

func TestPython_Dict(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
d = {"a": 1, "b": 2}
result = d["a"] + d["b"]
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

func TestPython_IfElse(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
x = 10
if x > 5:
    result = "big"
else:
    result = "small"
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, "big", val)
}

func TestPython_ForLoop(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
total = 0
for i in range(10):
    total = total + i
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("total")
	require.NoError(t, err)
	assert.Equal(t, int64(45), val)
}

func TestPython_Class(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
class Calculator:
    def __init__(self, x):
        self.x = x
    def multiply(self, y):
        return self.x * y

calc = Calculator(7)
result = calc.multiply(6)
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestPython_MathModule(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
import math
result = math.floor(3.7)
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

////////////////////////////////////////////////////////////////////////////////
// Hot reload tests
////////////////////////////////////////////////////////////////////////////////

func TestStartWatch_NoSource(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.StartWatch(context.Background(), "script.py")
	assert.Error(t, err)
}

func TestStartWatch_SourceNotWatcher(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetSource(nonWatcherSource{})
	err := eng.StartWatch(context.Background(), "script.py")
	assert.Error(t, err)
}

func TestStopWatch_NoOp(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.StopWatch("nonexistent")
	assert.NoError(t, err)
}

func TestStartStopWatch_WithMem(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("watch_script.py", "x = 1\n")
	eng.SetSource(src)

	err := eng.StartWatch(context.Background(), "watch_script.py")
	require.NoError(t, err)

	// Trigger a change.
	src.Set("watch_script.py", "x = 2\n")
	time.Sleep(50 * time.Millisecond)

	err = eng.StopWatch("watch_script.py")
	assert.NoError(t, err)
}

////////////////////////////////////////////////////////////////////////////////
// Concurrent safety tests
////////////////////////////////////////////////////////////////////////////////

func TestConcurrentExecute(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.LoadString(context.Background(), "expr", "x = 1 + 2\n"))

	var wg sync.WaitGroup
	// Only test read-like operations concurrently since gpython uses
	// execMu internally for serialization.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := eng.Execute(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////

// nonWatcherSource is a minimal Reader that does NOT implement Watcher.
type nonWatcherSource struct{}

func (nonWatcherSource) Load(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (nonWatcherSource) Close() error { return nil }

////////////////////////////////////////////////////////////////////////////////
// Runtime Hooks
////////////////////////////////////////////////////////////////////////////////

// TestGPythonEngine_RuntimeHook_BeforeInit verifies that a hook registered
// before Init is replayed once Init completes, so the injected host function
// is callable.
func TestGPythonEngine_RuntimeHook_BeforeInit(t *testing.T) {
	eng, err := newGPythonEngine()
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

	// gpython dispatches host functions directly, so we can verify end-to-end.
	res, err := eng.CallFunction(context.Background(), "greet", "world")
	require.NoError(t, err)
	assert.Equal(t, "hi world", res)
}

// TestGPythonEngine_RuntimeHook_AfterInit verifies that a hook registered after
// Init runs immediately on the live module.
func TestGPythonEngine_RuntimeHook_AfterInit(t *testing.T) {
	eng := newTestEngine(t)

	require.NoError(t, eng.AddRuntimeHook(func(ctx context.Context) error {
		return eng.RegisterFunction("greet", func(name string) string {
			return "hello " + name
		})
	}))

	res, err := eng.CallFunction(context.Background(), "greet", "bob")
	require.NoError(t, err)
	assert.Equal(t, "hello bob", res)
}

// TestGPythonEngine_RuntimeHook_AsCapability verifies the optional capability
// is detected via the helper.
func TestGPythonEngine_RuntimeHook_AsCapability(t *testing.T) {
	eng, err := newGPythonEngine()
	require.NoError(t, err)
	defer eng.Close()

	r := scriptEngine.AsRuntimeHookRegistrar(eng)
	require.NotNil(t, r, "gpython engine should implement RuntimeHookRegistrar")
}
