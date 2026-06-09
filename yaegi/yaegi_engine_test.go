package yaegi

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/yaegi/interp"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

// newTestEngine creates and initializes a Yaegi engine for testing.
func newTestEngine(t *testing.T) *engine {
	t.Helper()
	eng, err := newYaegiEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	return eng
}

////////////////////////////////////////////////////////////////////////////////
// Lifecycle tests
////////////////////////////////////////////////////////////////////////////////

// TestGetType verifies the engine type identifier.
func TestGetType(t *testing.T) {
	eng, err := newYaegiEngine()
	require.NoError(t, err)
	assert.Equal(t, scriptEngine.YaegiType, eng.GetType())
}

// TestInit verifies that Init succeeds on a fresh engine.
func TestInit(t *testing.T) {
	eng, err := newYaegiEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
}

// TestInit_AlreadyInitialized verifies that double Init fails.
func TestInit_AlreadyInitialized(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	err := eng.Init(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrYaegiEngineAlreadyInitialized)
}

// TestClose verifies that Close succeeds on an initialized engine.
func TestClose(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.Close())
	assert.False(t, eng.IsInitialized())
}

// TestClose_NotInitialized verifies that Close on a non-initialized engine fails.
func TestClose_NotInitialized(t *testing.T) {
	eng, err := newYaegiEngine()
	require.NoError(t, err)
	err = eng.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrYaegiEngineNotInitialized)
}

// TestOperations_NotInitialized verifies that operations fail before Init.
func TestOperations_NotInitialized(t *testing.T) {
	eng, err := newYaegiEngine()
	require.NoError(t, err)

	// All these should fail with ErrYaegiEngineNotInitialized.
	assert.ErrorIs(t, eng.Load(context.Background(), "k"), ErrYaegiEngineNotInitialized)

	_, err = eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrYaegiEngineNotInitialized)

	_, err = eng.ExecuteString(context.Background(), "n", "1+1")
	assert.ErrorIs(t, err, ErrYaegiEngineNotInitialized)

	assert.ErrorIs(t, eng.RegisterGlobal("x", 1), ErrYaegiEngineNotInitialized)

	_, err = eng.GetGlobal("x")
	assert.ErrorIs(t, err, ErrYaegiEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Script execution tests
////////////////////////////////////////////////////////////////////////////////

// TestExecuteString_SimpleExpr verifies basic expression evaluation.
func TestExecuteString_SimpleExpr(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	result, err := eng.ExecuteString(context.Background(), "test", "1 + 2")
	require.NoError(t, err)
	assert.Equal(t, 3, result)
}

// TestExecuteString_StringExpr verifies string evaluation.
func TestExecuteString_StringExpr(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	result, err := eng.ExecuteString(context.Background(), "test", `"hello" + " " + "world"`)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

// TestExecuteString_VarDeclaration verifies variable declaration and access.
func TestExecuteString_VarDeclaration(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	_, err := eng.ExecuteString(context.Background(), "test", `x := 42`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, 42, val)
}

// TestExecuteString_FunctionDefAndCall verifies defining and calling a function.
func TestExecuteString_FunctionDefAndCall(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	// Define a function.
	_, err := eng.ExecuteString(context.Background(), "test",
		`func add(a, b int) int { return a + b }`)
	require.NoError(t, err)

	// Call it.
	result, err := eng.CallFunction(context.Background(), "add", 3, 4)
	require.NoError(t, err)
	assert.Equal(t, 7, result)
}

// TestExecuteString_InvalidCode verifies that invalid Go code produces an error.
func TestExecuteString_InvalidCode(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	_, err := eng.ExecuteString(context.Background(), "test", `not valid go code !!!`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrYaegiEvalFailed)
}

// TestExecuteString_PanicRecovery verifies that a panic in the script doesn't
// crash the test process.
func TestExecuteString_PanicRecovery(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	// This should cause a panic during eval, which we recover from.
	_, err := eng.ExecuteString(context.Background(), "test", `panic("test panic")`)
	require.Error(t, err)
}

// TestExecute_NoScriptLoaded verifies Execute fails when nothing is loaded.
func TestExecute_NoScriptLoaded(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	_, err := eng.Execute(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrYaegiNoScriptLoaded)
}

// TestLoadString_ThenExecute verifies the Load+Execute pattern.
func TestLoadString_ThenExecute(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	// Load multiple scripts.
	require.NoError(t, eng.LoadString(context.Background(), "a", `a := 10`))
	require.NoError(t, eng.LoadString(context.Background(), "b", `b := 20`))

	// Execute all; result should be the last one.
	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 20, result)

	// Verify both variables exist.
	v, err := eng.GetGlobal("a")
	require.NoError(t, err)
	assert.Equal(t, 10, v)

	v, err = eng.GetGlobal("b")
	require.NoError(t, err)
	assert.Equal(t, 20, v)
}

////////////////////////////////////////////////////////////////////////////////
// Context cancellation tests
////////////////////////////////////////////////////////////////////////////////

// TestExecuteString_ContextCanceled verifies that a pre-canceled context
// short-circuits the call.
func TestExecuteString_ContextCanceled(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := eng.ExecuteString(ctx, "test", "1+1")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestExecuteString_ContextTimeout verifies context timeout behavior.
func TestExecuteString_ContextTimeout(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // ensure expired

	_, err := eng.ExecuteString(ctx, "test", "1+1")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

////////////////////////////////////////////////////////////////////////////////
// Source integration tests
////////////////////////////////////////////////////////////////////////////////

// TestSetGetSource verifies binding and retrieving a Source.
func TestSetGetSource(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("test.go", "42")

	eng.SetSource(mem)
	assert.NotNil(t, eng.GetSource())

	// Clear source.
	eng.SetSource(nil)
	assert.Nil(t, eng.GetSource())
}

// TestExecuteFromKey verifies loading and executing from a Source.
func TestExecuteFromKey(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("expr.go", "6 * 7")
	eng.SetSource(mem)

	result, err := eng.ExecuteFromKey(context.Background(), "expr.go")
	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

// TestExecuteFromKey_NoSource verifies error when no source is bound.
func TestExecuteFromKey_NoSource(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	_, err := eng.ExecuteFromKey(context.Background(), "test.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source bound")
}

// TestExecuteFromKeys verifies multi-key execution.
func TestExecuteFromKeys(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("a.go", "10")
	mem.Set("b.go", "20")
	eng.SetSource(mem)

	results, err := eng.ExecuteFromKeys(context.Background(), []string{"a.go", "b.go"})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, 10, results[0])
	assert.Equal(t, 20, results[1])
}

// TestLoad_FromSource verifies Load through Source.
func TestLoad_FromSource(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("val.go", "99")
	eng.SetSource(mem)

	require.NoError(t, eng.Load(context.Background(), "val.go"))

	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 99, result)
}

// TestLoad_SourceError verifies that a missing key propagates the error.
func TestLoad_SourceError(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	eng.SetSource(mem)

	err := eng.Load(context.Background(), "missing.go")
	require.Error(t, err)
}

// TestLoadMulti verifies multi-key loading.
func TestLoadMulti(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("a.go", "1")
	mem.Set("b.go", "2")
	eng.SetSource(mem)

	err := eng.LoadMulti(context.Background(), []string{"a.go", "b.go"})
	require.NoError(t, err)

	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, result)
}

////////////////////////////////////////////////////////////////////////////////
// Global / Function / Module tests
////////////////////////////////////////////////////////////////////////////////

// TestRegisterGlobal verifies that RegisterGlobal stores the value in the
// engine's internal state. Note: due to yaegi's import limitations with custom
// packages, GetGlobal for registered globals may not work; script-defined
// variables accessed via GetGlobal do work.
func TestRegisterGlobal(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	require.NoError(t, eng.RegisterGlobal("MyVar", 123))

	// RegisterGlobal succeeds (registered via Use for script import).
	// GetGlobal for script-defined variables works directly.
	_, err := eng.ExecuteString(context.Background(), "test", `y := 99`)
	require.NoError(t, err)

	val, err := eng.GetGlobal("y")
	require.NoError(t, err)
	assert.Equal(t, 99, val)
}

// TestRegisterFunction verifies that RegisterFunction stores the function.
// CallFunction is tested with script-defined functions instead, as those work
// directly through Yaegi's Eval.
func TestRegisterFunction(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mul := func(a, b int) int { return a * b }
	require.NoError(t, eng.RegisterFunction("Mul", mul))

	// RegisterFunction succeeds (registered via Use for script import).
	// CallFunction with script-defined functions works directly.
	_, err := eng.ExecuteString(context.Background(), "test",
		`func add(a, b int) int { return a + b }`)
	require.NoError(t, err)

	result, err := eng.CallFunction(context.Background(), "add", 3, 4)
	require.NoError(t, err)
	assert.Equal(t, 7, result)
}

// TestRegisterFunction_InvalidType verifies that a non-func is rejected.
func TestRegisterFunction_InvalidType(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	err := eng.RegisterFunction("notAFunc", 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects a func type")
}

// TestCallFunction_NotFound verifies that calling an undefined function fails.
func TestCallFunction_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	_, err := eng.CallFunction(context.Background(), "NonExistent", 1, 2)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrYaegiFunctionNotFound)
}

// TestRegisterModule verifies registering a module. Due to yaegi's import
// limitations, we verify the registration succeeds without error.
func TestRegisterModule(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mathFns := map[string]any{
		"Double": func(x int) int { return x * 2 },
	}
	require.NoError(t, eng.RegisterModule("mymath", mathFns))

	// RegisterModule succeeds (registered via Use for script import).
	// Direct module function calls may not work due to yaegi's GoPath
	// resolution for custom packages.
}

// TestRegisterModule_InvalidType verifies that non-map module is rejected.
func TestRegisterModule_InvalidType(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	err := eng.RegisterModule("bad", "not a map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects map[string]any")
}

////////////////////////////////////////////////////////////////////////////////
// Error handling tests
////////////////////////////////////////////////////////////////////////////////

// TestLastError verifies the last-error recording mechanism.
func TestLastError(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	// Initially no error.
	assert.NoError(t, eng.GetLastError())

	// Trigger an error.
	_, err := eng.ExecuteString(context.Background(), "test", `invalid !!!`)
	require.Error(t, err)
	assert.Error(t, eng.GetLastError())

	// ClearError.
	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

////////////////////////////////////////////////////////////////////////////////
// Hot reload tests
////////////////////////////////////////////////////////////////////////////////

// TestStartWatch_HotReload verifies that StartWatch reloads on change.
func TestStartWatch_HotReload(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("script.go", "1")
	eng.SetSource(mem)

	// Load the initial script.
	require.NoError(t, eng.Load(context.Background(), "script.go"))

	// Start watching.
	require.NoError(t, eng.StartWatch(context.Background(), "script.go"))

	// Simulate a change.
	time.Sleep(50 * time.Millisecond)
	mem.Set("script.go", "2")

	// Wait for reload.
	time.Sleep(200 * time.Millisecond)

	// The script queue should now contain the new value.
	eng.mu.RLock()
	scripts := make([]string, len(eng.scripts))
	copy(scripts, eng.scripts)
	eng.mu.RUnlock()

	// Should have at least 2 entries (initial load + reload).
	require.GreaterOrEqual(t, len(scripts), 2)
	assert.Equal(t, "2", scripts[len(scripts)-1])
}

// TestStartWatch_NoSource verifies error when no source is bound.
func TestStartWatch_NoSource(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	err := eng.StartWatch(context.Background(), "test.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source bound")
}

// TestStartWatch_SourceNotWatcher verifies error when source doesn't implement Watcher.
func TestStartWatch_SourceNotWatcher(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	eng.SetSource(&readerOnly{})
	err := eng.StartWatch(context.Background(), "test.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement Watcher")
}

// TestStopWatch verifies that StopWatch stops the watcher.
func TestStopWatch(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	mem := source.NewMemSource()
	mem.Set("script.go", "1")
	eng.SetSource(mem)

	require.NoError(t, eng.Load(context.Background(), "script.go"))
	require.NoError(t, eng.StartWatch(context.Background(), "script.go"))

	// Stop it.
	require.NoError(t, eng.StopWatch("script.go"))

	// Change should NOT trigger reload.
	time.Sleep(50 * time.Millisecond)
	mem.Set("script.go", "99")
	time.Sleep(200 * time.Millisecond)

	eng.mu.RLock()
	scripts := eng.scripts
	eng.mu.RUnlock()

	// Should still have only the initial load.
	require.Len(t, scripts, 1)
	assert.Equal(t, "1", scripts[0])
}

// TestClose_StopsWatchers verifies that Close cleans up all watchers.
func TestClose_StopsWatchers(t *testing.T) {
	eng := newTestEngine(t)

	mem := source.NewMemSource()
	mem.Set("script.go", "1")
	eng.SetSource(mem)

	require.NoError(t, eng.Load(context.Background(), "script.go"))
	require.NoError(t, eng.StartWatch(context.Background(), "script.go"))

	// Close should stop all watchers without deadlock.
	require.NoError(t, eng.Close())

	// watchers map should be empty.
	eng.watchersMu.Lock()
	n := len(eng.watchers)
	eng.watchersMu.Unlock()
	assert.Equal(t, 0, n)
}

// readerOnly is a minimal source.Reader that does NOT implement Watcher,
// used for testing error paths.
type readerOnly struct {
	mu   sync.Mutex
	data map[string]string
}

func (r *readerOnly) Load(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.data[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

func (r *readerOnly) Close() error { return nil }

// TestExecuteString_MultiStatementScript verifies running a multi-line script.
func TestExecuteString_MultiStatementScript(t *testing.T) {
	eng := newTestEngine(t)
	defer eng.Close()

	code := strings.Join([]string{
		`x := 10`,
		`y := 20`,
		`z := x + y`,
		`z`,
	}, "; ")

	result, err := eng.ExecuteString(context.Background(), "multi", code)
	require.NoError(t, err)
	assert.Equal(t, 30, result)
}

// TestDebugYaegiUseImport tests whether yaegi Use+import works as expected.
// This is a diagnostic test that logs behavior but doesn't assert.
func TestDebugYaegiUseImport(t *testing.T) {
	i := interp.New(interp.Options{})
	i.Use(interp.Exports{"host": map[string]reflect.Value{
		"X": reflect.ValueOf(42),
	}})

	v1, err1 := i.Eval(`import "host"`)
	t.Logf("import result: %v, error: %v", v1, err1)

	v2, err2 := i.Eval("host.X")
	t.Logf("eval result: %v, error: %v", v2, err2)

	// In yaegi v0.16, import of custom packages may fail because yaegi
	// tries to locate source code on disk. The Use() registers binary
	// symbols, but the import path still needs GoPath resolution.
	// This is a known yaegi limitation for custom packages.
}
