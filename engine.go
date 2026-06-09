package script_engine

import (
	"context"
	"errors"

	"github.com/tx7do/go-scripts/source"
)

// ErrCapabilityNotSupported is returned when an engine does not implement an
// optional capability interface (e.g. ModuleRegistrar, ScriptWatcher).
//
// Callers that receive this error should gracefully degrade instead of
// treating it as a fatal failure.
var ErrCapabilityNotSupported = errors.New("script engine: capability not supported by this engine type")

////////////////////////////////////////////////////////////////////////////////////////////
// Core Lifecycle — ALL engines must implement this interface.
////////////////////////////////////////////////////////////////////////////////////////////

// ScriptEngine is the minimal lifecycle interface that every engine
// implementation — regardless of its feature set — must satisfy.
//
// Full-featured engines (Lua, JavaScript) additionally implement the optional
// capability interfaces below and are aggregated by [Engine].
// Lightweight engines (CEL, Expr) may implement only ScriptEngine +
// [ScriptExecutor] and skip the rest.
type ScriptEngine interface {
	// GetType returns the Type identifier of this script engine.
	GetType() Type

	// Init initializes the script engine. Must be called before any Load*/Execute*.
	// Returns an error if initialization fails or if the engine is already initialized.
	Init(ctx context.Context) error

	// Close releases all resources held by the engine (runtime, VM, handles).
	// Returns an error if teardown fails. After Close, the engine must be re-Init'd
	// before reuse.
	Close() error

	// IsInitialized reports whether the engine has been initialized and not yet closed.
	IsInitialized() bool

	// GetLastError returns the last error recorded by the engine, or nil if none.
	GetLastError() error

	// ClearError clears the engine's last-error state.
	ClearError()
}

////////////////////////////////////////////////////////////////////////////////////////////
// Capability Interfaces — each represents an orthogonal feature set.
// Engines implement only the ones they support.
////////////////////////////////////////////////////////////////////////////////////////////

// ScriptLoader provides source-driven script loading.
//
// Loading is uniformly driven by the bound ScriptSource so the engine itself
// stays decoupled from concrete IO mechanisms (filesystem, S3, memory, ...).
type ScriptLoader interface {
	// SetSource binds a ScriptSource (FileSource / S3 / Mem / Multi / ...) to the
	// engine. Subsequent Load / ExecuteFromKey / ExecuteFromKeys calls read through it.
	// Passing nil clears any previously bound source.
	SetSource(source source.Reader)

	// GetSource returns the currently bound ScriptSource, or nil if none has been set.
	GetSource() source.Reader

	// Load loads a single script from the bound Source using the given key
	// (path / object key / script id, ...). Loaded scripts are kept by the engine
	// for later Execute runs.
	Load(ctx context.Context, key string) error

	// LoadMulti loads multiple scripts from the bound Source in order.
	// It aborts on the first error.
	LoadMulti(ctx context.Context, keys []string) error

	// LoadString compiles an inline script given directly as a string. It does NOT
	// go through the bound Source; use Load/LoadMulti for source-driven loading.
	// `name` is used for diagnostics (stack traces, error messages).
	LoadString(ctx context.Context, name string, code string) error
}

// ScriptExecutor provides script execution capabilities.
type ScriptExecutor interface {
	// Execute runs every script previously loaded via Load/LoadMulti/LoadString and
	// returns the combined result.
	Execute(ctx context.Context) (any, error)

	// ExecuteFromKey loads the script identified by `key` from the bound Source and
	// immediately runs it, all in one step.
	ExecuteFromKey(ctx context.Context, key string) (any, error)

	// ExecuteFromKeys is the multi-key variant of ExecuteFromKey; results are
	// returned in the same order as `keys`.
	ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error)

	// ExecuteString compiles and immediately runs an inline string script, bypassing
	// the bound Source. `name` is used for diagnostics.
	ExecuteString(ctx context.Context, name string, code string) (any, error)
}

// GlobalAccessor provides read/write access to global variables visible to scripts.
type GlobalAccessor interface {
	// RegisterGlobal registers or overwrites a global variable visible to scripts.
	RegisterGlobal(name string, value any) error

	// GetGlobal reads the value of a global variable. Returns an error if the name
	// is undefined.
	GetGlobal(name string) (any, error)
}

// FunctionRegistrar provides host-function registration and script-function invocation.
type FunctionRegistrar interface {
	// RegisterFunction registers a host function that scripts can call by `name`.
	// The concrete type accepted for `fn` depends on the engine implementation.
	RegisterFunction(name string, fn any) error

	// CallFunction invokes the script-side function registered as `name` with the
	// given arguments and returns its result. ctx can be used to cancel/timeout
	// the call.
	CallFunction(ctx context.Context, name string, args ...any) (any, error)
}

// ModuleRegistrar provides module registration for engines that support a
// module system (e.g. Lua's require, JavaScript's import).
//
// Lightweight expression engines (CEL, Expr) typically do NOT implement this.
type ModuleRegistrar interface {
	// RegisterModule registers a module (e.g. map[string]any, native loader, ...)
	// under `name` so scripts can require/use it. The accepted shape of `module`
	// depends on the engine implementation.
	RegisterModule(name string, module any) error
}

// ScriptWatcher provides hot-reload (Watch) capabilities for scripts bound via
// a Source that implements [source.Watcher].
//
// Engines that do not support hot-reload (CEL, Expr, ...) do NOT implement this.
type ScriptWatcher interface {
	// StartWatch starts watching the script identified by `key` for changes via the
	// bound Source's Watch capability. When a change is detected, the script is
	// automatically reloaded. Returns an error if the source doesn't implement
	// Watcher or is not bound.
	StartWatch(ctx context.Context, key string) error

	// StopWatch stops watching the script identified by `key` and cleans up
	// the associated goroutine.
	StopWatch(key string) error
}

////////////////////////////////////////////////////////////////////////////////////////////
// Aggregate Interface — full-featured engines implement this.
////////////////////////////////////////////////////////////////////////////////////////////

// Engine is the aggregate interface combining all capability interfaces.
// Full-featured engines (Lua, JavaScript, ...) implement this.
//
// Lightweight engines (CEL, Expr) may implement only a subset; callers should
// use the capability helper functions (AsLoader, AsExecutor, ...) or direct
// type assertions to check for support.
//
// Implementations are expected to be safe for concurrent use of the methods
// documented as such; see each method's comment for details.
type Engine interface {
	ScriptEngine
	ScriptLoader
	ScriptExecutor
	GlobalAccessor
	FunctionRegistrar
	ModuleRegistrar
	ScriptWatcher
}

////////////////////////////////////////////////////////////////////////////////////////////
// Capability Helpers — convenience functions for safe type assertions.
////////////////////////////////////////////////////////////////////////////////////////////

// AsLoader returns the ScriptLoader capability of e, or nil if unsupported.
func AsLoader(e any) ScriptLoader {
	if l, ok := e.(ScriptLoader); ok {
		return l
	}
	return nil
}

// AsExecutor returns the ScriptExecutor capability of e, or nil if unsupported.
func AsExecutor(e any) ScriptExecutor {
	if ex, ok := e.(ScriptExecutor); ok {
		return ex
	}
	return nil
}

// AsGlobalAccessor returns the GlobalAccessor capability of e, or nil if unsupported.
func AsGlobalAccessor(e any) GlobalAccessor {
	if g, ok := e.(GlobalAccessor); ok {
		return g
	}
	return nil
}

// AsFunctionRegistrar returns the FunctionRegistrar capability of e, or nil if unsupported.
func AsFunctionRegistrar(e any) FunctionRegistrar {
	if f, ok := e.(FunctionRegistrar); ok {
		return f
	}
	return nil
}

// AsModuleRegistrar returns the ModuleRegistrar capability of e, or nil if unsupported.
func AsModuleRegistrar(e any) ModuleRegistrar {
	if m, ok := e.(ModuleRegistrar); ok {
		return m
	}
	return nil
}

// AsWatcher returns the ScriptWatcher capability of e, or nil if unsupported.
func AsWatcher(e any) ScriptWatcher {
	if w, ok := e.(ScriptWatcher); ok {
		return w
	}
	return nil
}
