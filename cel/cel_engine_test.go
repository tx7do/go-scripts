package cel

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

// newTestEngine creates and initializes a CEL engine for testing.
func newTestEngine(t *testing.T) *engine {
	t.Helper()
	eng, err := newCELEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

////////////////////////////////////////////////////////////////////////////////
// Lifecycle tests
////////////////////////////////////////////////////////////////////////////////

func TestGetType(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)
	assert.Equal(t, scriptEngine.CELType, eng.GetType())
}

func TestInit(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
	_ = eng.Close()
}

func TestInit_AlreadyInitialized(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.Init(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELEngineAlreadyInitialized)
}

func TestClose(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)
	require.NoError(t, eng.Init(context.Background()))
	require.NoError(t, eng.Close())
	assert.False(t, eng.IsInitialized())
}

func TestClose_NotInitialized(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)
	err = eng.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

func TestIsInitialized(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)
	assert.False(t, eng.IsInitialized())
	require.NoError(t, eng.Init(context.Background()))
	assert.True(t, eng.IsInitialized())
	require.NoError(t, eng.Close())
	assert.False(t, eng.IsInitialized())
}

func TestOperations_NotInitialized(t *testing.T) {
	eng, err := newCELEngine()
	require.NoError(t, err)

	assert.ErrorIs(t, eng.Load(context.Background(), "k"), ErrCELEngineNotInitialized)
	_, err = eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
	_, err = eng.ExecuteString(context.Background(), "n", "1 + 1")
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
	assert.ErrorIs(t, eng.RegisterGlobal("x", 1), ErrCELEngineNotInitialized)
	_, err = eng.GetGlobal("x")
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
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
	err := eng.Load(context.Background(), "expr")
	assert.Error(t, err)
}

func TestExecuteFromKeyNoSource(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteFromKey(context.Background(), "expr")
	assert.Error(t, err)
}

////////////////////////////////////////////////////////////////////////////////
// Script execution tests
////////////////////////////////////////////////////////////////////////////////

func TestExecuteString_SimpleArith(t *testing.T) {
	eng := newTestEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", "1 + 2")
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

func TestExecuteString_StringConcat(t *testing.T) {
	eng := newTestEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", `"hello" + " " + "world"`)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestExecuteString_Bool(t *testing.T) {
	eng := newTestEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", "true && false")
	require.NoError(t, err)
	assert.Equal(t, false, result)
}

func TestExecuteString_Comparison(t *testing.T) {
	eng := newTestEngine(t)
	result, err := eng.ExecuteString(context.Background(), "test", "10 > 5")
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestExecuteString_InvalidExpr(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.ExecuteString(context.Background(), "test", "not a valid !!! expression")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELCompileFailed)
}

func TestExecuteString_UndeclaredVar(t *testing.T) {
	eng := newTestEngine(t)
	// CEL is strictly typed: undeclared variables fail at compile time.
	_, err := eng.ExecuteString(context.Background(), "test", "unknown_var + 1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELCompileFailed)
}

func TestExecute_NoExpressionLoaded(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.Execute(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELNoExpressionLoaded)
}

func TestLoadString_ThenExecute(t *testing.T) {
	eng := newTestEngine(t)

	require.NoError(t, eng.LoadString(context.Background(), "a", "10 + 20"))
	require.NoError(t, eng.LoadString(context.Background(), "b", "40 + 50"))

	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(90), result)
}

////////////////////////////////////////////////////////////////////////////////
// Global variable tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterGlobal_Int(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(42)))

	result, err := eng.ExecuteString(context.Background(), "test", "x + 8")
	require.NoError(t, err)
	assert.Equal(t, int64(50), result)
}

func TestRegisterGlobal_String(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("name", "Alice"))

	result, err := eng.ExecuteString(context.Background(), "test", `"Hello " + name`)
	require.NoError(t, err)
	assert.Equal(t, "Hello Alice", result)
}

func TestRegisterGlobal_Bool(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("flag", true))

	result, err := eng.ExecuteString(context.Background(), "test", "flag || false")
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestRegisterGlobal_Overwrite(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(10)))
	require.NoError(t, eng.RegisterGlobal("x", int64(99)))

	result, err := eng.ExecuteString(context.Background(), "test", "x")
	require.NoError(t, err)
	assert.Equal(t, int64(99), result)
}

func TestRegisterGlobal_DoubleType(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("pi", 3.14))

	result, err := eng.ExecuteString(context.Background(), "test", "pi * 2.0")
	require.NoError(t, err)
	assert.Equal(t, 6.28, result)
}

func TestRegisterGlobal_UintType(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("u", uint64(100)))

	result, err := eng.ExecuteString(context.Background(), "test", "u + uint(5)")
	require.NoError(t, err)
	assert.Equal(t, uint64(105), result)
}

func TestGetGlobal(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", 42))

	val, err := eng.GetGlobal("x")
	require.NoError(t, err)
	assert.Equal(t, 42, val)
}

func TestGetGlobal_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.GetGlobal("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELGlobalNotFound)
}

func TestRegisterGlobal_NotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	err := eng.RegisterGlobal("x", 1)
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Function tests
////////////////////////////////////////////////////////////////////////////////

func TestRegisterFunction_Simple(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("twice", func(x int64) int64 {
		return x * 2
	}))

	result, err := eng.ExecuteString(context.Background(), "test", "twice(21)")
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)
}

func TestRegisterFunction_TwoArgs(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("add", func(a, b int64) int64 {
		return a + b
	}))

	result, err := eng.ExecuteString(context.Background(), "test", "add(10, 20)")
	require.NoError(t, err)
	assert.Equal(t, int64(30), result)
}

func TestRegisterFunction_String(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("greet", func(name string) string {
		return "Hello, " + name + "!"
	}))

	result, err := eng.ExecuteString(context.Background(), "test", `greet("World")`)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestRegisterFunction_Overwrite(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("calc", func(x int64) int64 {
		return x + 1
	}))
	result, err := eng.ExecuteString(context.Background(), "test", "calc(5)")
	require.NoError(t, err)
	assert.Equal(t, int64(6), result)

	require.NoError(t, eng.RegisterFunction("calc", func(x int64) int64 {
		return x * 10
	}))
	result, err = eng.ExecuteString(context.Background(), "test", "calc(5)")
	require.NoError(t, err)
	assert.Equal(t, int64(50), result)
}

func TestRegisterFunction_NotFunc(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.RegisterFunction("bad", "not a func")
	require.Error(t, err)
}

func TestRegisterFunction_NotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	err := eng.RegisterFunction("fn", func() {})
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

func TestCallFunction(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterFunction("triple", func(x int64) int64 {
		return x * 3
	}))

	result, err := eng.CallFunction(context.Background(), "triple", int64(7))
	require.NoError(t, err)
	assert.Equal(t, int64(21), result)
}

func TestCallFunction_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.CallFunction(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCELFunctionNotFound)
}

func TestCallFunction_NotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	_, err := eng.CallFunction(context.Background(), "fn")
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

func TestRegisterFunction_WithGlobal(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("base", int64(100)))
	require.NoError(t, eng.RegisterFunction("offset", func(x int64) int64 {
		return x + 5
	}))

	result, err := eng.ExecuteString(context.Background(), "test", "offset(base)")
	require.NoError(t, err)
	assert.Equal(t, int64(105), result)
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

	result, err := eng.ExecuteString(context.Background(), "test", "config_timeout > 10")
	require.NoError(t, err)
	assert.Equal(t, true, result)

	result, err = eng.ExecuteString(context.Background(), "test", `config_name == "myapp"`)
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestRegisterModule_BadType(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.RegisterModule("bad", "not a map")
	assert.Error(t, err)
}

func TestRegisterModule_NotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	err := eng.RegisterModule("m", map[string]any{})
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Load / Source tests
////////////////////////////////////////////////////////////////////////////////

func TestLoadFromSource(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("expr1", "1 + 2")
	eng.SetSource(src)

	err := eng.Load(context.Background(), "expr1")
	require.NoError(t, err)

	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

func TestLoadMulti(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a", "10 + 20")
	src.Set("b", "30 + 40")
	eng.SetSource(src)

	err := eng.LoadMulti(context.Background(), []string{"a", "b"})
	require.NoError(t, err)

	result, err := eng.Execute(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(70), result)
}

func TestLoadMultiError(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a", "1 + 2")
	eng.SetSource(src)

	err := eng.LoadMulti(context.Background(), []string{"a", "missing"})
	assert.Error(t, err)
}

func TestExecuteFromKey(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("expr", "5 * 6")
	eng.SetSource(src)

	result, err := eng.ExecuteFromKey(context.Background(), "expr")
	require.NoError(t, err)
	assert.Equal(t, int64(30), result)
}

func TestExecuteFromKeys(t *testing.T) {
	eng := newTestEngine(t)
	src := source.NewMemSource()
	src.Set("a", "10")
	src.Set("b", "20")
	eng.SetSource(src)

	results, err := eng.ExecuteFromKeys(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, int64(10), results[0])
	assert.Equal(t, int64(20), results[1])
}

func TestLoadStringNotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	err := eng.LoadString(context.Background(), "expr", "1 + 1")
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

func TestExecuteStringNotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	_, err := eng.ExecuteString(context.Background(), "expr", "1 + 1")
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

func TestExecuteNotInitialized(t *testing.T) {
	eng, _ := newCELEngine()
	_, err := eng.Execute(context.Background())
	assert.ErrorIs(t, err, ErrCELEngineNotInitialized)
}

////////////////////////////////////////////////////////////////////////////////
// Error handling tests
////////////////////////////////////////////////////////////////////////////////

func TestLastError(t *testing.T) {
	eng := newTestEngine(t)
	assert.NoError(t, eng.GetLastError())

	_ = eng.LoadString(context.Background(), "bad", "!!! invalid")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

func TestClearError(t *testing.T) {
	eng := newTestEngine(t)
	_ = eng.LoadString(context.Background(), "bad", "!!! invalid")
	assert.Error(t, eng.GetLastError())

	eng.ClearError()
	assert.NoError(t, eng.GetLastError())
}

////////////////////////////////////////////////////////////////////////////////
// Factory tests
////////////////////////////////////////////////////////////////////////////////

func TestFactoryRegistration(t *testing.T) {
	factory, ok := scriptEngine.GetFactory(scriptEngine.CELType)
	assert.True(t, ok)
	assert.NotNil(t, factory)
}

func TestFactoryCreateAndUse(t *testing.T) {
	eng, err := scriptEngine.NewScriptEngine(scriptEngine.CELType)
	require.NoError(t, err)
	require.NotNil(t, eng)
	require.NoError(t, eng.Init(context.Background()))
	defer eng.Close()

	result, err := eng.ExecuteString(context.Background(), "test", "1 + 2")
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

////////////////////////////////////////////////////////////////////////////////
// Hot reload tests
////////////////////////////////////////////////////////////////////////////////

func TestStartWatch_NoSource(t *testing.T) {
	eng := newTestEngine(t)
	err := eng.StartWatch(context.Background(), "expr")
	assert.Error(t, err)
}

func TestStartWatch_SourceNotWatcher(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetSource(nonWatcherSource{})
	err := eng.StartWatch(context.Background(), "expr")
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
	src.Set("watch_expr", "1 + 2")
	eng.SetSource(src)

	err := eng.StartWatch(context.Background(), "watch_expr")
	require.NoError(t, err)

	// Trigger a change.
	src.Set("watch_expr", "10 + 20")
	time.Sleep(50 * time.Millisecond)

	err = eng.StopWatch("watch_expr")
	assert.NoError(t, err)
}

////////////////////////////////////////////////////////////////////////////////
// CEL built-in tests
////////////////////////////////////////////////////////////////////////////////

func TestCEL_BuiltIn_Macros(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("items", []int64{1, 2, 3, 4, 5}))

	result, err := eng.ExecuteString(context.Background(), "test", "items.size()")
	require.NoError(t, err)
	assert.Equal(t, int64(5), result)
}

func TestCEL_BuiltIn_Has(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("m", map[string]int64{"a": 1, "b": 2}))

	// CEL uses `in` operator for map key presence.
	result, err := eng.ExecuteString(context.Background(), "test", `"a" in m`)
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestCEL_BuiltIn_Timestamp(t *testing.T) {
	eng := newTestEngine(t)
	// CEL has built-in timestamp literals.
	result, err := eng.ExecuteString(context.Background(), "test",
		`timestamp("2024-01-01T00:00:00Z").getFullYear() >= 2024`)
	require.NoError(t, err)
	assert.Equal(t, true, result)
}

func TestCEL_Conditional(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("x", int64(10)))

	result, err := eng.ExecuteString(context.Background(), "test",
		"x > 5 ? \"big\" : \"small\"")
	require.NoError(t, err)
	assert.Equal(t, "big", result)
}

func TestCEL_ListComprehension(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("nums", []int64{1, 2, 3, 4, 5}))

	result, err := eng.ExecuteString(context.Background(), "test",
		"nums.filter(x, x > 2).size()")
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

func TestCEL_StringMethods(t *testing.T) {
	eng := newTestEngine(t)
	// CEL strings support concatenation and comparison as built-ins.
	result, err := eng.ExecuteString(context.Background(), "test",
		`"hello" + " " + "world"`)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

////////////////////////////////////////////////////////////////////////////////
// Concurrent safety tests
////////////////////////////////////////////////////////////////////////////////

func TestConcurrentExecute(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.LoadString(context.Background(), "expr", "1 + 2"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := eng.Execute(context.Background())
			assert.NoError(t, err)
			assert.NotNil(t, result)
		}()
	}
	wg.Wait()
}

func TestConcurrentRegisterAndExecute(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, eng.RegisterGlobal("counter", int64(0)))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = eng.ExecuteString(context.Background(), "expr", "counter + 1")
		}(i)
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
