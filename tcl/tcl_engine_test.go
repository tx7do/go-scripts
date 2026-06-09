package tcl

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

// On Windows, modernc.org/tcl is experimental. Creating more than ~28
// interpreters in a single process causes XTclInitNotifier to deadlock.
// To work around this, we use a single shared engine for all tests that
// require an initialized engine, resetting its state between tests.

var (
	sharedEng     *engine
	sharedEngOnce sync.Once
	sharedEngMu   sync.Mutex
)

// getSharedEngine returns a singleton engine for testing.
func getSharedEngine(t *testing.T) *engine {
	t.Helper()
	sharedEngOnce.Do(func() {
		eng, err := newTCLEngine()
		require.NoError(t, err)
		require.NoError(t, eng.Init(context.Background()))
		sharedEng = eng
	})
	// Reset interpreter state before each test.
	sharedEngMu.Lock()
	defer sharedEngMu.Unlock()
	// Clear all variables and procs by running a reset script.
	_, _ = sharedEng.interp.Eval(`
catch { info vars } vars
foreach v $vars {
    catch { unset $v }
}
catch { info procs } procs
foreach p $procs {
    catch { rename $p "" }
}
`)
	sharedEng.ClearError()
	return sharedEng
}

// localEngine creates a fresh engine for tests that specifically test
// lifecycle (Init/Close).
func localEngine(t *testing.T) *engine {
	t.Helper()
	eng, err := newTCLEngine()
	require.NoError(t, err)
	return eng
}

////////////////////////////////////////////////////////////////////////////////
// Lifecycle tests
////////////////////////////////////////////////////////////////////////////////

func TestGetType(t *testing.T) {
	eng := localEngine(t)
	assert.Equal(t, scriptEngine.TclType, eng.GetType())
}

func TestInit(t *testing.T) {
	eng := localEngine(t)
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
}

func TestInit_AlreadyInitialized(t *testing.T) {
	eng := getSharedEngine(t)
	err := eng.Init(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLEngineAlreadyInitialized)
}

func TestClose_NotInitialized(t *testing.T) {
	eng := localEngine(t)
	err := eng.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

func TestIsInitialized(t *testing.T) {
	eng := getSharedEngine(t)
	assert.True(t, eng.IsInitialized())
}

func TestOperations_NotInitialized(t *testing.T) {
	eng := localEngine(t)

	assert.ErrorIs(t, eng.Load(context.Background(), "k"), ErrTCLEngineNotInitialized)
	_, err := eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
	_, err = eng.ExecuteString(context.Background(), "n", "set x 1")
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
	assert.ErrorIs(t, eng.RegisterGlobal("x", 1), ErrTCLEngineNotInitialized)
	_, err = eng.GetGlobal("x")
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Source tests
////////////////////////////////////////////////////////////////////////////////

func TestSetGetSource(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	eng.SetSource(src)
	assert.Equal(t, src, eng.GetSource())

	eng.SetSource(nil)
	assert.Nil(t, eng.GetSource())
}

func TestLoadNoSource(t *testing.T) {
	eng := getSharedEngine(t)
	eng.SetSource(nil)
	err := eng.Load(context.Background(), "script.tcl")
	assert.Error(t, err)
}

func TestExecuteFromKeyNoSource(t *testing.T) {
	eng := getSharedEngine(t)
	eng.SetSource(nil)
	_, err := eng.ExecuteFromKey(context.Background(), "script.tcl")
	assert.Error(t, err)
}

////////////////////////////////////////////////////////////////////////////////
// Script execution tests
////////////////////////////////////////////////////////////////////////////////

func TestExecuteString_SimpleArith(t *testing.T) {
	eng := getSharedEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", "expr {1 + 2}")
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

func TestExecuteString_SetVar(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "set x 42")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestExecuteString_StringConcat(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `set s "hello world"`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("s")
	require.NoError(t, err)
	assert.Equal(t, "hello world", val)
}

func TestExecuteString_ProcDef(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
proc add {a b} {
    return [expr {$a + $b}]
}
`)
	require.NoError(t, err)

	result, err := eng.CallFunction(context.Background(), "add", int64(3), int64(4))
	require.NoError(t, err)
	assert.Equal(t, int64(7), result)
}

func TestExecuteString_InvalidCode(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "set ")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLEvalFailed)
}

func TestExecute_NoScriptLoaded(t *testing.T) {
	eng := getSharedEngine(t)
	// Clear loaded scripts.
	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()
	_, err := eng.Execute(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLNoScriptLoaded)
}

func TestLoadString_ThenExecute(t *testing.T) {
	eng := getSharedEngine(t)
	// Clear scripts first.
	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()

	require.NoError(t, eng.LoadString(context.Background(), "a", "set a 10"))
	require.NoError(t, eng.LoadString(context.Background(), "b", "set b [expr {$a + 20}]"))

	_, err := eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)

	// Clear scripts after.
	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()
}

func TestExecuteString_RuntimeError(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "error \"something bad\"")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLEvalFailed)
}

func TestExecuteString_MultiStatements(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set x 10
set y 20
set z [expr {$x + $y}]
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("z")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

func TestExecuteString_ReturnsResult(t *testing.T) {
	eng := getSharedEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", "expr {6 * 7}")
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

////////////////////////////////////////////////////////////////////////////////
// Global variable tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterGlobal_Int(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(42)))

	_, err := eng.ExecuteString(context.Background(), "test", "set y [expr {$x + 8}]")
	require.NoError(t, err)

	val, err := eng.GetGlobal("y")
	require.NoError(t, err)
	assert.Equal(t, int64(50), val)
}

func TestRegisterGlobal_String(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterGlobal("name", "Alice"))

	_, err := eng.ExecuteString(context.Background(), "test", `set greeting "Hello, $name"`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("greeting")
	require.NoError(t, err)
	assert.Equal(t, "Hello, Alice", val)
}

func TestRegisterGlobal_Overwrite(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(10)))
	require.NoError(t, eng.RegisterGlobal("x", int64(99)))

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(99), val)
}

func TestRegisterGlobal_Float(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterGlobal("pi", 3.14))

	val, err := eng.GetGlobal("pi")
	require.NoError(t, err)
	assert.Equal(t, 3.14, val)
}

func TestGetGlobal(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(42)))

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestGetGlobal_NotFound(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.GetGlobal("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLGlobalNotFound)
}

func TestRegisterGlobal_NotInitialized(t *testing.T) {
	eng := localEngine(t)
	err := eng.RegisterGlobal("x", 1)
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

func TestRegisterGlobal_FromScript(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set counter 100
set name "test"
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
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterFunction("double", func(x int64) int64 {
		return x * 2
	}))

	result, err := eng.CallFunction(context.Background(), "double", int64(21))
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestRegisterFunction_TwoArgs(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterFunction("add", func(a, b int64) int64 {
		return a + b
	}))

	result, err := eng.CallFunction(context.Background(), "add", int64(10), int64(20))
	require.NoError(t, err)
	assert.Equal(t, int64(30), result)
}

func TestRegisterFunction_String(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterFunction("greet", func(name string) string {
		return "Hello, " + name + "!"
	}))

	result, err := eng.CallFunction(context.Background(), "greet", "World")
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestRegisterFunction_Overwrite(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterFunction("calc", func(x int64) int64 {
		return x + 1
	}))
	result, err := eng.CallFunction(context.Background(), "calc", int64(5))
	require.NoError(t, err)
	assert.Equal(t, int64(6), result)

	require.NoError(t, eng.RegisterFunction("calc2", func(x int64) int64 {
		return x * 10
	}))
	result, err = eng.CallFunction(context.Background(), "calc2", int64(5))
	require.NoError(t, err)
	assert.Equal(t, int64(50), result)
}

func TestRegisterFunction_NotFunc(t *testing.T) {
	eng := getSharedEngine(t)
	err := eng.RegisterFunction("bad", "not a func")
	require.Error(t, err)
}

func TestRegisterFunction_NotInitialized(t *testing.T) {
	eng := localEngine(t)
	err := eng.RegisterFunction("fn", func() {})
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

func TestRegisterFunction_CallableFromScript(t *testing.T) {
	eng := getSharedEngine(t)
	require.NoError(t, eng.RegisterFunction("triple", func(x int64) int64 {
		return x * 3
	}))

	_, err := eng.ExecuteString(context.Background(), "test", "set result [triple 14]")
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestCallFunction_TCLDefined(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
proc multiply {a b} {
    return [expr {$a * $b}]
}
`)
	require.NoError(t, err)

	result, err := eng.CallFunction(context.Background(), "multiply", int64(6), int64(7))
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestCallFunction_NotFound(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.CallFunction(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTCLFunctionNotFound)
}

func TestCallFunction_NotInitialized(t *testing.T) {
	eng := localEngine(t)
	_, err := eng.CallFunction(context.Background(), "fn")
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Module tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterModule(t *testing.T) {
	eng := getSharedEngine(t)
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
	eng := getSharedEngine(t)
	err := eng.RegisterModule("bad", "not a map")
	assert.Error(t, err)
}

func TestRegisterModule_NotInitialized(t *testing.T) {
	eng := localEngine(t)
	err := eng.RegisterModule("m", map[string]any{})
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Load / Source tests
////////////////////////////////////////////////////////////////////////////////

func TestLoadFromSource(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("script1.tcl", "set x [expr {1 + 2}]")
	eng.SetSource(src)

	// Clear scripts first.
	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()

	err := eng.Load(context.Background(), "script1.tcl")
	require.NoError(t, err)

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)

	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()
}

func TestLoadMulti(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("a.tcl", "set a 10")
	src.Set("b.tcl", "set b [expr {$a + 20}]")
	eng.SetSource(src)

	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()

	err := eng.LoadMulti(context.Background(), []string{"a.tcl", "b.tcl"})
	require.NoError(t, err)

	_, err = eng.Execute(context.Background())
	require.NoError(t, err)

	val, err := eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)

	eng.mu.Lock()
	eng.scripts = nil
	eng.mu.Unlock()
}

func TestLoadMultiError(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("a.tcl", "set x 1")
	eng.SetSource(src)

	err := eng.LoadMulti(context.Background(), []string{"a.tcl", "missing.tcl"})
	assert.Error(t, err)
}

func TestExecuteFromKey(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("expr.tcl", "set x [expr {5 * 6}]")
	eng.SetSource(src)

	_, err := eng.ExecuteFromKey(context.Background(), "expr.tcl")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(30), val)
}

func TestExecuteFromKeys(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("a.tcl", "set a 10")
	src.Set("b.tcl", "set b 20")
	eng.SetSource(src)

	results, err := eng.ExecuteFromKeys(context.Background(), []string{"a.tcl", "b.tcl"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestLoadStringNotInitialized(t *testing.T) {
	eng := localEngine(t)
	err := eng.LoadString(context.Background(), "script.tcl", "set x 1")
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

func TestExecuteStringNotInitialized(t *testing.T) {
	eng := localEngine(t)
	_, err := eng.ExecuteString(context.Background(), "script.tcl", "set x 1")
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

func TestExecuteNotInitialized(t *testing.T) {
	eng := localEngine(t)
	_, err := eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrTCLEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Error handling tests
////////////////////////////////////////////////////////////////////////////////

func TestLastError(t *testing.T) {
	eng := getSharedEngine(t)
	// Clear any previous error.
	eng.ClearError()
	assert.NoError(t, eng.GetLastError())

	_, _ = eng.ExecuteString(context.Background(), "bad.tcl", "error \"oops\"")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

func TestClearError(t *testing.T) {
	eng := getSharedEngine(t)
	_, _ = eng.ExecuteString(context.Background(), "bad.tcl", "error \"oops\"")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

////////////////////////////////////////////////////////////////////////////////
// Factory tests
////////////////////////////////////////////////////////////////////////////////

func TestFactoryRegistration(t *testing.T) {
	factory, ok := scriptEngine.GetFactory(scriptEngine.TclType)
	assert.True(t, ok)
	assert.NotNil(t, factory)
}

func TestFactoryCreateAndUse(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "set x [expr {1 + 2}]")
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, int64(3), val)
}

////////////////////////////////////////////////////////////////////////////////
// TCL language feature tests
////////////////////////////////////////////////////////////////////////////////

func TestTCL_List(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set items {1 2 3 4 5}
set total 0
foreach i $items {
    set total [expr {$total + $i}]
}
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("total")
	require.NoError(t, err)
	assert.Equal(t, int64(15), val)
}

func TestTCL_IfElse(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set x 10
if {$x > 5} {
    set result "big"
} else {
    set result "small"
}
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("result")
	require.NoError(t, err)
	assert.Equal(t, "big", val)
}

func TestTCL_StringOps(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set s "Hello World"
set len [string length $s]
set upper [string toupper $s]
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("len")
	require.NoError(t, err)
	assert.Equal(t, int64(11), val)

	val, err = eng.GetGlobal("upper")
	require.NoError(t, err)
	assert.Equal(t, "HELLO WORLD", val)
}

func TestTCL_WhileLoop(t *testing.T) {
	eng := getSharedEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", `
set total 0
set i 0
while {$i < 10} {
    set total [expr {$total + $i}]
    incr i
}
`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("total")
	require.NoError(t, err)
	assert.Equal(t, int64(45), val)
}

////////////////////////////////////////////////////////////////////////////////
// Hot reload tests
////////////////////////////////////////////////////////////////////////////////

func TestStartWatch_NoSource(t *testing.T) {
	eng := getSharedEngine(t)
	eng.SetSource(nil)
	err := eng.StartWatch(context.Background(), "script.tcl")
	assert.Error(t, err)
}

func TestStartWatch_SourceNotWatcher(t *testing.T) {
	eng := getSharedEngine(t)
	eng.SetSource(nonWatcherSource{})
	err := eng.StartWatch(context.Background(), "script.tcl")
	assert.Error(t, err)
}

func TestStopWatch_NoOp(t *testing.T) {
	eng := getSharedEngine(t)
	err := eng.StopWatch("nonexistent")
	assert.NoError(t, err)
}

func TestStartStopWatch_WithMem(t *testing.T) {
	eng := getSharedEngine(t)
	src := source.NewMemSource()
	src.Set("watch_script.tcl", "set x 1")
	eng.SetSource(src)

	err := eng.StartWatch(context.Background(), "watch_script.tcl")
	require.NoError(t, err)

	src.Set("watch_script.tcl", "set x 2")
	time.Sleep(50 * time.Millisecond)

	err = eng.StopWatch("watch_script.tcl")
	assert.NoError(t, err)
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
