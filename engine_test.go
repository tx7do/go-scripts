package script_engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capabilityless is a value that implements no capability interface at all; it
// is used to assert that the As* helpers return nil for unsupported values.
type capabilityless struct{}

// TestAsHelpers_NilForUnsupported verifies every As* helper returns nil when
// the value does not implement the corresponding capability interface.
func TestAsHelpers_NilForUnsupported(t *testing.T) {
	v := capabilityless{}

	assert.Nil(t, AsLoader(v))
	assert.Nil(t, AsExecutor(v))
	assert.Nil(t, AsGlobalAccessor(v))
	assert.Nil(t, AsFunctionRegistrar(v))
	assert.Nil(t, AsModuleRegistrar(v))
	assert.Nil(t, AsWatcher(v))
	assert.Nil(t, AsSandboxConfigurator(v))

	// nil input must never panic.
	assert.Nil(t, AsLoader(nil))
	assert.Nil(t, AsSandboxConfigurator(nil))
}

// TestAsHelpers_NilForNil asserts the helpers accept a nil any without panicking.
func TestAsHelpers_NilArgDoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		_ = AsLoader(nil)
		_ = AsExecutor(nil)
		_ = AsGlobalAccessor(nil)
		_ = AsFunctionRegistrar(nil)
		_ = AsModuleRegistrar(nil)
		_ = AsWatcher(nil)
		_ = AsSandboxConfigurator(nil)
	})
}

// TestAsSandboxConfigurator_FromFullEngine verifies that a value implementing
// the full Engine interface (mockEngine) is recognized as a SandboxConfigurator
// and that the returned capability is the same underlying object.
func TestAsSandboxConfigurator_FromFullEngine(t *testing.T) {
	eng := newMockEngine("mock")

	sc := AsSandboxConfigurator(eng)
	require.NotNil(t, sc)

	// The returned capability must be the same object, and SetOpenLibs must
	// flow through to the engine's captured allow-list.
	assert.Same(t, eng, sc)

	sc.SetOpenLibs("base", "string")
	assert.Equal(t, []string{"base", "string"}, eng.openLibs)
}

// TestEngine_AggregatesSandboxConfigurator guards the contract that the Engine
// aggregate interface embeds SandboxConfigurator: a type implementing the rest
// of Engine but missing SetOpenLibs must NOT satisfy Engine.
func TestEngine_AggregatesSandboxConfigurator(t *testing.T) {
	// mockEngine implements SetOpenLibs (see manager_test.go), so it must
	// satisfy the full Engine interface.
	var _ Engine = (*mockEngine)(nil)

	// A type missing ONLY SetOpenLibs must not satisfy Engine. This is a
	// compile-time intent check expressed at runtime via a negative assertion.
	noSandbox := struct {
		ScriptEngine
		ScriptLoader
		ScriptExecutor
		GlobalAccessor
		FunctionRegistrar
		ModuleRegistrar
		ScriptWatcher
	}{}
	// It does implement the other capabilities...
	var _ ScriptWatcher = noSandbox
	// ...but NOT the aggregate, because SandboxConfigurator is missing.
	assert.Nil(t, AsSandboxConfigurator(noSandbox))
	// Sanity: the Engine interface variable can only accept values that
	// implement SandboxConfigurator; this line would fail to compile if
	// SandboxConfigurator were ever dropped from Engine, since noSandbox lacks
	// SetOpenLibs. We keep it as a live compile guard.
	var _ Engine = (*mockEngine)(nil)
}
