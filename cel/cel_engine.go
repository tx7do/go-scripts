// Package cel provides a script engine implementation using Google's CEL-Go
// (Common Expression Language — https://github.com/google/cel-go). CEL is a
// non-Turing-complete expression language designed for simplicity, safety, and
// fast evaluation. Unlike full scripting languages, CEL evaluates a single
// expression per "script" and supports compile-time type checking.
//
// Construction:
//
//	eng, _ := cel.New(ctx)
//	eng.Init(ctx)
//	eng.RegisterGlobal("name", "world")
//	result, _ := eng.ExecuteString(ctx, "greet", `"Hello " + name`)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package cel

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/cel-go/cel"
	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.CELType, func() (scriptEngine.Engine, error) {
		return newCELEngine()
	})
}

// compiledExpr holds a compiled CEL expression (AST + Program).
type compiledExpr struct {
	name string
	expr string
	ast  *cel.Ast
	prg  cel.Program
}

// funcInfo stores metadata for a registered host function.
type funcInfo struct {
	fn       any          // the Go function
	params   []*cel.Type  // parameter types
	retType  *cel.Type    // return type
	funcType reflect.Type // Go reflect.Type of fn
}

// globalInfo stores metadata for a registered global variable.
type globalInfo struct {
	value any       // the Go value
	typ   *cel.Type // CEL type
}

// engine is the CEL expression engine implementation.
//
// Lock ordering convention (same as other engines):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	env       *cel.Env
	programs  []compiledExpr
	hostFuncs map[string]*funcInfo
	globals   map[string]*globalInfo

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, programs, source, globals, hostFuncs
	execMu      sync.Mutex   // protects env
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newCELEngine creates a CEL engine instance.
func newCELEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.CELType
}

// Init initializes the engine by creating a fresh CEL environment.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrCELEngineAlreadyInitialized)
		return ErrCELEngineAlreadyInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	env, err := cel.NewEnv()
	if err != nil {
		wrapped := fmt.Errorf("cel engine: init env: %w", err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.env = env
	e.hostFuncs = make(map[string]*funcInfo)
	e.globals = make(map[string]*globalInfo)
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases underlying resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	e.stopAllWatchers()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.env = nil
	e.programs = nil
	e.hostFuncs = nil
	e.globals = nil
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

// Load loads a CEL expression from the bound Source using the given key.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("cel engine: no source bound")
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

// LoadMulti loads multiple CEL expressions from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString compiles an inline CEL expression. `name` is used for diagnostics.
// The expression is compiled against the current environment (including all
// registered globals and functions) and appended to the queue.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.env == nil {
		e.setLastError(ErrCLEEnvNotInitialized)
		return ErrCLEEnvNotInitialized
	}

	ast, prg, err := e.compileAndProgram(code)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrCELCompileFailed, name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.mu.Lock()
	e.programs = append(e.programs, compiledExpr{
		name: name,
		expr: code,
		ast:  ast,
		prg:  prg,
	})
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute evaluates all loaded CEL expressions in order and returns the result
// of the last one. Registered globals are automatically passed as variables.
func (e *engine) Execute(_ context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return nil, ErrCELEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.programs) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrCELNoExpressionLoaded)
		return nil, ErrCELNoExpressionLoaded
	}
	progs := make([]compiledExpr, len(e.programs))
	copy(progs, e.programs)
	vars := e.evalVars()
	e.mu.RUnlock()

	var lastResult any
	for _, p := range progs {
		result, _, err := p.prg.Eval(vars)
		if err != nil {
			wrapped := fmt.Errorf("%w: %s: %v", ErrCELEvalFailed, p.name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
		lastResult = celValToGo(result)
	}

	e.ClearError()
	return lastResult, nil
}

// ExecuteFromKey loads the CEL expression identified by `key` from the bound
// Source and immediately runs it.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return nil, ErrCELEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("cel engine: no source bound")
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

// ExecuteString compiles and immediately evaluates an inline CEL expression.
func (e *engine) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return nil, ErrCELEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.env == nil {
		e.setLastError(ErrCLEEnvNotInitialized)
		return nil, ErrCLEEnvNotInitialized
	}

	_, prg, err := e.compileAndProgram(code)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrCELCompileFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.mu.RLock()
	vars := e.evalVars()
	e.mu.RUnlock()

	result, _, err := prg.Eval(vars)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrCELEvalFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return celValToGo(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Global Variable Registration
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers or overwrites a named variable visible to CEL
// expressions. The CEL type is inferred from the Go value.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	celType := inferCelType(reflect.TypeOf(value))

	e.mu.Lock()
	e.globals[name] = &globalInfo{value: value, typ: celType}
	err := e.rebuildEnvLocked()
	e.mu.Unlock()

	if err != nil {
		e.setLastError(err)
		return err
	}

	e.ClearError()
	return nil
}

// GetGlobal reads the value of a registered global variable.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return nil, ErrCELEngineNotInitialized
	}

	e.mu.RLock()
	g, ok := e.globals[name]
	e.mu.RUnlock()
	if !ok {
		wrapped := fmt.Errorf("%w: %s", ErrCELGlobalNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return g.value, nil
}

////////////////////////////////////////////////////////////////////////////////
// Function Call
////////////////////////////////////////////////////////////////////////////////

// RegisterFunction registers a Go function that CEL expressions can call by
// `name`. The function signature is inferred via reflection.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		err := fmt.Errorf("cel engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	params := make([]*cel.Type, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		params[i] = inferCelType(fnType.In(i))
	}

	var retType *cel.Type
	if fnType.NumOut() > 0 {
		retType = inferCelType(fnType.Out(0))
	} else {
		retType = cel.NullType
	}

	e.mu.Lock()
	e.hostFuncs[name] = &funcInfo{
		fn:       fn,
		params:   params,
		retType:  retType,
		funcType: fnType,
	}
	err := e.rebuildEnvLocked()
	e.mu.Unlock()

	if err != nil {
		e.setLastError(err)
		return err
	}

	e.ClearError()
	return nil
}

// CallFunction invokes a registered host function by name. Note that CEL
// expressions call functions directly via CEL syntax (e.g. `myFunc(a, b)`);
// this method provides an imperative way to call them from Go.
func (e *engine) CallFunction(_ context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return nil, ErrCELEngineNotInitialized
	}

	e.mu.RLock()
	fi, ok := e.hostFuncs[name]
	e.mu.RUnlock()
	if !ok {
		wrapped := fmt.Errorf("%w: %s", ErrCELFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// Convert args to reflect.Value and call.
	fnVal := reflect.ValueOf(fi.fn)
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

// RegisterModule registers a set of variables under a namespace. `module` must
// be of type map[string]any; its keys become registered globals prefixed with
// the module name (e.g. module "config" with key "timeout" becomes "config.timeout").
// CEL identifiers use underscores instead of dots, so "config.timeout" becomes
// "config_timeout" internally.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("cel engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	for key, val := range syms {
		globalName := name + "_" + key
		e.globals[globalName] = &globalInfo{
			value: val,
			typ:   inferCelType(reflect.TypeOf(val)),
		}
	}
	err := e.rebuildEnvLocked()
	e.mu.Unlock()

	if err != nil {
		e.setLastError(err)
		return err
	}

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

// StartWatch starts watching the CEL expression identified by `key` for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrCELEngineNotInitialized)
		return ErrCELEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("cel engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("cel engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "cel engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the CEL expression identified by `key`.
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
