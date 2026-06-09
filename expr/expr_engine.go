// Package expr provides a script engine implementation using the Expr
// expression language (https://github.com/expr-lang/expr). Expr is a fast,
// type-safe expression evaluator that supports variables, functions, and a
// rich set of built-in operators.
//
// Construction:
//
//	eng, _ := expr.New(ctx)
//	eng.Init(ctx)
//	eng.RegisterGlobal("name", "world")
//	result, _ := eng.ExecuteString(ctx, "greet", `"Hello " + name`)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package expr

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.ExprType, func() (scriptEngine.Engine, error) {
		return newExprEngine()
	})
}

// compiledExpr holds a compiled Expr program.
type compiledExpr struct {
	name    string
	expr    string
	program *vm.Program
}

// engine is the Expr expression engine implementation.
//
// Lock ordering convention (same as other engines):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	programs []compiledExpr
	env      map[string]any // combined env: globals + functions + modules

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, programs, source, env
	execMu      sync.Mutex   // protects compilation and evaluation
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newExprEngine creates an Expr engine instance.
func newExprEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.ExprType
}

// Init initializes the engine.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrExprEngineAlreadyInitialized)
		return ErrExprEngineAlreadyInitialized
	}

	e.env = make(map[string]any)
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases underlying resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	e.stopAllWatchers()

	e.programs = nil
	e.env = nil
	e.initialized = false

	e.lastErrorMu.Lock()
	e.lastError = nil
	e.lastErrorMu.Unlock()

	return nil
}

// IsInitialized reports whether the engine has been initialized.
func (e *engine) IsInitialized() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.initialized
}

////////////////////////////////////////////////////////////////////////////////
// ScriptSource injection & access
////////////////////////////////////////////////////////////////////////////////

// SetSource binds a ScriptSource to the engine.
func (e *engine) SetSource(src source.Reader) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.source = src
}

// GetSource returns the currently bound ScriptSource, or nil if none has been set.
func (e *engine) GetSource() source.Reader {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

////////////////////////////////////////////////////////////////////////////////
// Script loading
////////////////////////////////////////////////////////////////////////////////

// Load loads an Expr expression from the bound Source using the given key.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("expr engine: no source bound")
		e.setLastError(err)
		return err
	}
	code, err := src.Load(ctx, key)
	if err != nil {
		e.setLastError(err)
		return err
	}
	return e.LoadString(ctx, key, code)
}

// LoadMulti loads multiple Expr expressions from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString compiles an inline Expr expression. `name` is used for diagnostics.
// The expression is compiled against the current environment (including all
// registered globals and functions) and appended to the queue.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.mu.RLock()
	env := e.snapshotEnv()
	e.mu.RUnlock()

	program, err := expr.Compile(code, expr.Env(env))
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrExprCompileFailed, name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.mu.Lock()
	e.programs = append(e.programs, compiledExpr{
		name:    name,
		expr:    code,
		program: program,
	})
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute evaluates all loaded Expr expressions in order and returns the result
// of the last one. Registered globals and functions are automatically passed
// as the evaluation environment.
func (e *engine) Execute(_ context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return nil, ErrExprEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.programs) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrExprNoExpressionLoaded)
		return nil, ErrExprNoExpressionLoaded
	}
	progs := make([]compiledExpr, len(e.programs))
	copy(progs, e.programs)
	env := e.snapshotEnv()
	e.mu.RUnlock()

	var lastResult any
	for _, p := range progs {
		result, err := vm.Run(p.program, env)
		if err != nil {
			wrapped := fmt.Errorf("%w: %s: %v", ErrExprRunFailed, p.name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
		lastResult = result
	}

	e.ClearError()
	return lastResult, nil
}

// ExecuteFromKey loads the Expr expression identified by `key` from the bound
// Source and immediately runs it.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return nil, ErrExprEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("expr engine: no source bound")
		e.setLastError(err)
		return nil, err
	}
	code, err := src.Load(ctx, key)
	if err != nil {
		e.setLastError(err)
		return nil, err
	}
	return e.ExecuteString(ctx, key, code)
}

// ExecuteFromKeys is the multi-key variant of ExecuteFromKey.
func (e *engine) ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error) {
	results := make([]any, 0, len(keys))
	for _, k := range keys {
		res, err := e.ExecuteFromKey(ctx, k)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// ExecuteString compiles and immediately evaluates an inline Expr expression.
func (e *engine) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return nil, ErrExprEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.mu.RLock()
	env := e.snapshotEnv()
	e.mu.RUnlock()

	program, err := expr.Compile(code, expr.Env(env))
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrExprCompileFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	result, err := vm.Run(program, env)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrExprRunFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return result, nil
}

////////////////////////////////////////////////////////////////////////////////
// Global Variable Registration
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers or overwrites a named variable visible to Expr
// expressions. The variable becomes available as a top-level identifier in the
// expression environment.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	e.mu.Lock()
	e.env[name] = value
	e.mu.Unlock()

	e.ClearError()
	return nil
}

// GetGlobal reads the value of a registered global variable.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return nil, ErrExprEngineNotInitialized
	}

	e.mu.RLock()
	val, ok := e.env[name]
	e.mu.RUnlock()
	if !ok {
		wrapped := fmt.Errorf("%w: %s", ErrExprGlobalNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return val, nil
}

////////////////////////////////////////////////////////////////////////////////
// Function Call
////////////////////////////////////////////////////////////////////////////////

// RegisterFunction registers a Go function that Expr expressions can call by
// `name`. Expr uses duck typing: the function is compiled as an `any` value,
// and Expr resolves parameter types at runtime.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	if reflect.TypeOf(fn).Kind() != reflect.Func {
		err := fmt.Errorf("expr engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	e.env[name] = fn
	e.mu.Unlock()

	e.ClearError()
	return nil
}

// CallFunction invokes a registered host function by name. Note that Expr
// expressions call functions directly via Expr syntax (e.g. `myFunc(a, b)`);
// this method provides an imperative way to call them from Go.
func (e *engine) CallFunction(_ context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return nil, ErrExprEngineNotInitialized
	}

	e.mu.RLock()
	fn, ok := e.env[name]
	e.mu.RUnlock()
	if !ok {
		wrapped := fmt.Errorf("%w: %s", ErrExprFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	fnVal := reflect.ValueOf(fn)
	if fnVal.Kind() != reflect.Func {
		wrapped := fmt.Errorf("%w: %s is not a function", ErrExprFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	argVals := make([]reflect.Value, len(args))
	for i, a := range args {
		argVals[i] = reflect.ValueOf(a)
	}

	results := fnVal.Call(argVals)
	e.ClearError()
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Interface(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Module Management
////////////////////////////////////////////////////////////////////////////////

// RegisterModule registers a set of variables/functions under a namespace.
// `module` must be of type map[string]any; its entries become accessible in the
// expression environment under the module name (e.g. `config.timeout`).
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("expr engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	e.env[name] = syms
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Error handling
////////////////////////////////////////////////////////////////////////////////

func (e *engine) GetLastError() error {
	e.lastErrorMu.RLock()
	defer e.lastErrorMu.RUnlock()
	return e.lastError
}

func (e *engine) setLastError(err error) {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = err
}

func (e *engine) ClearError() {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = nil
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

// StartWatch starts watching the Expr expression identified by `key` for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrExprEngineNotInitialized)
		return ErrExprEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("expr engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("expr engine: source does not implement Watcher")
		e.setLastError(err)
		return err
	}

	e.StopWatch(key)

	watchCtx, cancel := context.WithCancel(ctx)

	e.watchersMu.Lock()
	e.watchers[key] = cancel
	e.watchersMu.Unlock()

	go func() {
		ch, err := watcher.Watch(watchCtx, key)
		if err != nil {
			cancel()
			return
		}
		for range ch {
			if loadErr := e.Load(watchCtx, key); loadErr != nil {
				scriptEngine.GetLogger().Warn(watchCtx, "expr engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the Expr expression identified by `key`.
func (e *engine) StopWatch(key string) error {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	if cancel, ok := e.watchers[key]; ok {
		cancel()
		delete(e.watchers, key)
	}
	return nil
}

// stopAllWatchers cancels all active watch goroutines.
func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
