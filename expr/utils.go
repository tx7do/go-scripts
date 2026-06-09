package expr

////////////////////////////////////////////////////////////////////////////////
// Internal helpers
////////////////////////////////////////////////////////////////////////////////

// snapshotEnv creates a shallow copy of the environment map for compilation
// and evaluation. Caller must hold e.mu (or its read lock).
func (e *engine) snapshotEnv() map[string]any {
	env := make(map[string]any, len(e.env))
	for k, v := range e.env {
		env[k] = v
	}
	return env
}
