package script_engine

import (
	"context"

	"github.com/tx7do/go-scripts/source"
)

// Engine defines the interface for script engines.
// Implementations are expected to be safe for concurrent use of the methods
// documented as such; see each method's comment for details.
type Engine interface {
	// GetType returns the Type identifier of this script engine.
	GetType() Type

	//////////////////////////////////////////////////////////////////////////////////////////
	// Lifecycle Management
	//////////////////////////////////////////////////////////////////////////////////////////

	// Init initializes the script engine. Must be called before any Load*/Execute*.
	// Returns an error if initialization fails or if the engine is already initialized.
	Init(ctx context.Context) error

	// Close releases all resources held by the engine (runtime, VM, handles).
	// Returns an error if teardown fails. After Close, the engine must be re-Init'd
	// before reuse.
	Close() error

	// IsInitialized reports whether the engine has been initialized and not yet closed.
	IsInitialized() bool

	//////////////////////////////////////////////////////////////////////////////////////////
	// ScriptSource Injection & Access
	//////////////////////////////////////////////////////////////////////////////////////////

	// SetSource binds a ScriptSource (FileSource / S3 / Mem / Multi / ...) to the
	// engine. Subsequent Load / ExecuteFromKey / ExecuteFromKeys calls read through it.
	// Passing nil clears any previously bound source.
	SetSource(source source.Reader)

	// GetSource returns the currently bound ScriptSource, or nil if none has been set.
	GetSource() source.Reader

	//////////////////////////////////////////////////////////////////////////////////////////
	// Script Loading
	// Loading is uniformly driven by the bound ScriptSource so the engine itself
	// stays decoupled from concrete IO mechanisms (filesystem, S3, memory, ...).
	//////////////////////////////////////////////////////////////////////////////////////////

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

	//////////////////////////////////////////////////////////////////////////////////////////
	// Script Execution
	//////////////////////////////////////////////////////////////////////////////////////////

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

	//////////////////////////////////////////////////////////////////////////////////////////
	// Global Variable Registration
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterGlobal registers or overwrites a global variable visible to scripts.
	RegisterGlobal(name string, value any) error

	// GetGlobal reads the value of a global variable. Returns an error if the name
	// is undefined.
	GetGlobal(name string) (any, error)

	//////////////////////////////////////////////////////////////////////////////////////////
	// Function Call
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterFunction registers a host function that scripts can call by `name`.
	// The concrete type accepted for `fn` depends on the engine implementation.
	RegisterFunction(name string, fn any) error

	// CallFunction invokes the script-side function registered as `name` with the
	// given arguments and returns its result. ctx can be used to cancel/timeout
	// the call.
	CallFunction(ctx context.Context, name string, args ...any) (any, error)

	//////////////////////////////////////////////////////////////////////////////////////////
	// Module Management
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterModule registers a module (e.g. map[string]any, native loader, ...)
	// under `name` so scripts can require/use it. The accepted shape of `module`
	// depends on the engine implementation.
	RegisterModule(name string, module any) error

	//////////////////////////////////////////////////////////////////////////////////////////
	// Hot Reload (Watch)
	//////////////////////////////////////////////////////////////////////////////////////////

	// StartWatch starts watching the script identified by `key` for changes via the
	// bound Source's Watch capability. When a change is detected, the script is
	// automatically reloaded. Returns an error if the source doesn't implement
	// Watcher or is not bound.
	StartWatch(ctx context.Context, key string) error

	// StopWatch stops watching the script identified by `key` and cleans up
	// the associated goroutine.
	StopWatch(key string) error

	//////////////////////////////////////////////////////////////////////////////////////////
	// Error Handling
	//////////////////////////////////////////////////////////////////////////////////////////

	// GetLastError returns the last error recorded by the engine, or nil if none.
	GetLastError() error

	// ClearError clears the engine's last-error state.
	ClearError()
}
