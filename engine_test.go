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

// TestSandboxConfigurator_IsStandaloneCapability guards the contract that
// SandboxConfigurator is an OPTIONAL, standalone capability — NOT embedded in
// the Engine aggregate interface. Engines without a standard-library concept
// (CEL, Expr, Starlark, ...) satisfy Engine without implementing SetOpenLibs,
// and AsSandboxConfigurator returns nil for them.
func TestSandboxConfigurator_IsStandaloneCapability(t *testing.T) {
	// An Engine implementation that does NOT implement SetOpenLibs must STILL
	// satisfy the Engine aggregate interface (sandbox is optional).
	noSandbox := struct {
		ScriptEngine
		ScriptLoader
		ScriptExecutor
		GlobalAccessor
		FunctionRegistrar
		ModuleRegistrar
		ScriptWatcher
	}{}
	var _ Engine = noSandbox // compiles only if Engine does NOT require SetOpenLibs

	// It implements ScriptWatcher (one of the embedded capabilities)...
	var _ ScriptWatcher = noSandbox
	// ...but NOT the sandbox capability.
	assert.Nil(t, AsSandboxConfigurator(noSandbox),
		"engine without SetOpenLibs must not be a SandboxConfigurator")

	// mockEngine DOES implement SetOpenLibs (see manager_test.go), so it is a
	// SandboxConfigurator in addition to being an Engine.
	eng := newMockEngine("mock")
	require.NotNil(t, AsSandboxConfigurator(eng))
}
