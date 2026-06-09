package wazero

import (
	"context"
	"fmt"

	wzr "github.com/tetratelabs/wazero"
)

////////////////////////////////////////////////////////////////////////////////
// Internal helpers
////////////////////////////////////////////////////////////////////////////////

// instantiateAndRun instantiates a compiled module and calls _start if present.
// Acquires execMu internally.
func (e *engine) instantiateAndRun(ctx context.Context, compiled wzr.CompiledModule) (any, error) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	return e.instantiateAndRunLocked(ctx, compiled)
}

// instantiateAndRunLocked is the same as instantiateAndRun but assumes the
// caller already holds execMu.
func (e *engine) instantiateAndRunLocked(ctx context.Context, compiled wzr.CompiledModule) (any, error) {
	if e.runtime == nil {
		e.setLastError(ErrWazeroRuntimeNotInitialized)
		return nil, ErrWazeroRuntimeNotInitialized
	}

	// Close the previous instance if any.
	if e.instance != nil {
		_ = e.instance.Close(context.Background())
		e.instance = nil
	}

	mod, err := e.runtime.InstantiateModule(ctx, compiled, wzr.NewModuleConfig())
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrWazeroInstantiateFailed, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}
	e.instance = mod

	// Call _start if the module exports it (WASI convention).
	if fn := mod.ExportedFunction("_start"); fn != nil {
		if _, err := fn.Call(ctx); err != nil {
			wrapped := fmt.Errorf("wazero engine: _start failed: %w", err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
	}

	e.ClearError()
	return nil, nil
}

// rebuildHostModule closes the old host module instance (if any) and rebuilds
// it with all accumulated host functions. Caller must hold e.mu.
func (e *engine) rebuildHostModule() error {
	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.runtime == nil {
		return ErrWazeroRuntimeNotInitialized
	}

	// Close old host module.
	if e.hostModule != nil {
		_ = e.hostModule.Close(context.Background())
		e.hostModule = nil
	}

	if len(e.hostFunctions) == 0 {
		return nil
	}

	builder := e.runtime.NewHostModuleBuilder(hostModuleName)
	for name, fn := range e.hostFunctions {
		builder.NewFunctionBuilder().WithFunc(fn).Export(name)
	}

	mod, err := builder.Instantiate(context.Background())
	if err != nil {
		return fmt.Errorf("wazero engine: rebuild host module: %w", err)
	}
	e.hostModule = mod
	return nil
}

// toUint64 converts common numeric Go types to uint64 for WASM function calls.
func toUint64(v any) uint64 {
	switch val := v.(type) {
	case uint64:
		return val
	case uint32:
		return uint64(val)
	case int:
		return uint64(val)
	case int32:
		return uint64(val)
	case int64:
		return uint64(val)
	case float64:
		return uint64(val)
	case float32:
		return uint64(val)
	default:
		return 0
	}
}
