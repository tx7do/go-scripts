package yaegi

import (
	"context"
	"fmt"
)

////////////////////////////////////////////////////////////////////////////////
// Internal helpers
////////////////////////////////////////////////////////////////////////////////

// eval runs a Go source string through the Yaegi interpreter while holding
// execMu. Context cancellation is best-effort: Yaegi does not support
// interrupting an in-progress Eval, so ctx is checked before evaluation begins.
func (e *engine) eval(ctx context.Context, code string) (any, error) {
	// Check context before attempting eval.
	if err := ctx.Err(); err != nil {
		e.setLastError(err)
		return nil, err
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return nil, ErrYaegiInterpreterNotInitialized
	}

	var result any
	var evalErr error

	func() {
		defer func() {
			if r := recover(); r != nil {
				evalErr = fmt.Errorf("yaegi engine: panic during eval: %v", r)
			}
		}()

		v, err := e.interp.Eval(code)
		if err != nil {
			evalErr = fmt.Errorf("%w: %v", ErrYaegiEvalFailed, err)
			return
		}
		if v.IsValid() {
			result = v.Interface()
		}
	}()

	if evalErr != nil {
		e.setLastError(evalErr)
		return nil, evalErr
	}

	e.ClearError()
	return result, nil
}
