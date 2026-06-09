// Package tcl provides a script engine implementation using the modernc.org/tcl
// library — a CGo-free port of the Tool Command Language (https://tcl.tk).
//
// Construction:
//
//	eng, _ := tcl.New(ctx)
//	eng.Init(ctx)
//	eng.LoadString(ctx, "hello", `puts "hello world"`)
//	eng.Execute(ctx)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package tcl

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	tcllib "modernc.org/tcl"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.TclType, func() (scriptEngine.Engine, error) {
		return newTCLEngine()
	})
}

// scriptEntry holds a TCL script source and its diagnostic name.
type scriptEntry struct {
	name string
	src  string
}

// engine is the TCL script engine implementation.
//
// Lock ordering convention:
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	// The TCL interpreter; nil when not initialized.
	interp *tcllib.Interp

	scripts []scriptEntry

	// hostFuncs stores Go functions registered via RegisterFunction.
	hostFuncs map[string]any

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, scripts, source, hostFuncs
	execMu      sync.Mutex   // protects interp
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newTCLEngine creates a TCL engine instance.
func newTCLEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
		hostFuncs:   make(map[string]any),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.TclType
}

// Init initializes the engine by creating a fresh TCL interpreter.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrTCLEngineAlreadyInitialized)
		return ErrTCLEngineAlreadyInitialized
	}

	// Mount the TCL library VFS if TCL_LIBRARY is not set.
	if os.Getenv("TCL_LIBRARY") == "" {
		if mountPoint, err := tcllib.MountLibraryVFS(); err == nil {
			os.Setenv("TCL_LIBRARY", mountPoint)
		}
	}

	interp, err := tcllib.NewInterp()
	if err != nil {
		wrapped := fmt.Errorf("tcl engine: create interpreter: %w", err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.execMu.Lock()
	e.interp = interp
	e.execMu.Unlock()

	e.scripts = nil
	e.hostFuncs = make(map[string]any)
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	e.stopAllWatchers()

	e.execMu.Lock()
	// Note: On experimental platforms (e.g. Windows), modernc.org/tcl's
	// Close() can panic or deadlock the notifier subsystem when multiple
	// interpreters are created/destroyed in sequence. We skip Close() and
	// rely on process exit for cleanup. The Go-side state is still reset
	// properly.
	e.interp = nil
	e.execMu.Unlock()

	e.scripts = nil
	e.hostFuncs = nil
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

// GetSource returns the currently bound ScriptSource, or nil.
func (e *engine) GetSource() source.Reader {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

////////////////////////////////////////////////////////////////////////////////
// Script loading
////////////////////////////////////////////////////////////////////////////////

// Load loads a TCL script from the bound Source.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("tcl engine: no source bound")
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

// LoadMulti loads multiple scripts from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString stores an inline TCL source string for later execution.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	e.mu.Lock()
	e.scripts = append(e.scripts, scriptEntry{name: name, src: code})
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs all previously loaded TCL scripts in order. All scripts share
// the same interpreter, so variables/procedures defined in one are visible to
// subsequent ones.
func (e *engine) Execute(_ context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.scripts) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrTCLNoScriptLoaded)
		return nil, ErrTCLNoScriptLoaded
	}
	scripts := make([]scriptEntry, len(e.scripts))
	copy(scripts, e.scripts)
	e.mu.RUnlock()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	var lastResult string
	for _, s := range scripts {
		result, err := e.interp.Eval(s.src)
		if err != nil {
			wrapped := fmt.Errorf("%w: %s: %v", ErrTCLEvalFailed, s.name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
		lastResult = result
	}

	e.ClearError()
	return lastResult, nil
}

// ExecuteFromKey loads and immediately runs a script from the bound Source.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("tcl engine: no source bound")
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

// ExecuteFromKeys is the multi-key variant.
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

// ExecuteString compiles and immediately runs an inline TCL source string.
func (e *engine) ExecuteString(_ context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	result, err := e.interp.Eval(code)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrTCLEvalFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return parseTCLValue(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Global Variable Registration
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers or overwrites a named variable visible to TCL
// scripts. The Go value is converted to a TCL-compatible string representation.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	tclVal := goToTCLString(value)
	script := fmt.Sprintf("set %s %s", name, tclVal)

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	_, err := e.interp.Eval(script)
	if err != nil {
		wrapped := fmt.Errorf("tcl engine: register global %q: %w", name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.ClearError()
	return nil
}

// GetGlobal reads the value of a global variable. Returns the value as a string.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	// In TCL, `set varName` without a value returns the current value.
	script := fmt.Sprintf("set %s", name)
	result, err := e.interp.Eval(script)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s", ErrTCLGlobalNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return parseTCLValue(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Function Call
////////////////////////////////////////////////////////////////////////////////

// RegisterFunction registers a Go function that TCL scripts can call by name.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	if reflect.TypeOf(fn).Kind() != reflect.Func {
		err := fmt.Errorf("tcl engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	e.hostFuncs[name] = fn
	e.mu.Unlock()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	// Register the Go function as a TCL command.
	cmdName := sanitizeCmdName(name)
	_, err := e.interp.NewCommand(cmdName,
		func(clientData interface{}, in *tcllib.Interp, args []string) int {
			// args[0] is the command name; convert remaining args to []any.
			goArgs := make([]any, len(args)-1)
			for i, s := range args[1:] {
				goArgs[i] = parseTCLValue(s)
			}
			result := callHostFunc(fn, goArgs)
			in.SetResult(goToTCLString(result))
			return 0 // TCL_OK
		},
		nil, // clientData
		nil, // deleteProc
	)
	if err != nil {
		wrapped := fmt.Errorf("tcl engine: register function %q: %w", name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.ClearError()
	return nil
}

// CallFunction invokes a TCL procedure by name with the given arguments.
func (e *engine) CallFunction(_ context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return nil, ErrTCLEngineNotInitialized
	}

	// Check if it's a host function stored internally.
	e.mu.RLock()
	hostFn, isHost := e.hostFuncs[name]
	e.mu.RUnlock()
	if isHost {
		result := callHostFunc(hostFn, args)
		e.ClearError()
		return result, nil
	}

	// Build the TCL call script.
	cmdName := sanitizeCmdName(name)
	parts := []string{cmdName}
	for _, a := range args {
		parts = append(parts, goToTCLString(a))
	}
	script := strings.Join(parts, " ")

	result, err := e.interp.Eval(script)
	if err != nil {
		// Check if the error is about an unknown command.
		errStr := err.Error()
		if strings.Contains(errStr, "invalid command name") ||
			strings.Contains(errStr, "return code: 1") {
			wrapped := fmt.Errorf("%w: %s", ErrTCLFunctionNotFound, name)
			e.setLastError(wrapped)
			return nil, wrapped
		}
		wrapped := fmt.Errorf("%w: %s: %v", ErrTCLCallFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return parseTCLValue(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Module Management
////////////////////////////////////////////////////////////////////////////////

// RegisterModule registers a module from a map of values. Each key becomes a
// global variable prefixed with the module name.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("tcl engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.interp == nil {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	for key, val := range syms {
		globalName := name + "_" + key
		script := fmt.Sprintf("set %s %s", globalName, goToTCLString(val))
		_, err := e.interp.Eval(script)
		if err != nil {
			wrapped := fmt.Errorf("tcl engine: register module %q key %q: %w", name, key, err)
			e.setLastError(wrapped)
			return wrapped
		}
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

// StartWatch starts watching the script identified by key for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrTCLEngineNotInitialized)
		return ErrTCLEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("tcl engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("tcl engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "tcl engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the script identified by key.
func (e *engine) StopWatch(key string) error {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	if cancel, ok := e.watchers[key]; ok {
		cancel()
		delete(e.watchers, key)
	}
	return nil
}

func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
