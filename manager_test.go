package script_engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// mockEngine is a minimal Engine implementation for testing the root-module
// types (Manager, EnginePool, AutoGrowEnginePool) without depending on the
// javascript/lua submodules.
type mockEngine struct {
	mu sync.Mutex

	typ         Type
	initErr     error
	closeErr    error
	initialized bool

	// Captured observations.
	source     source.Reader
	lastKey    string
	lastCode   string
	loaded     int
	executed   int
	registered map[string]any
	modules    map[string]any
	globals    map[string]any
	functions  map[string]any
	lastError  error
	callResult any
	callErr    error
	closeCount int
	initCount  int
	loadErr    error
	execErr    error
}

func newMockEngine(typ Type) *mockEngine {
	return &mockEngine{
		typ:        typ,
		registered: make(map[string]any),
		modules:    make(map[string]any),
		globals:    make(map[string]any),
		functions:  make(map[string]any),
	}
}

func (m *mockEngine) GetType() Type { return m.typ }

func (m *mockEngine) Init(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCount++
	if m.initErr != nil {
		return m.initErr
	}
	m.initialized = true
	return nil
}

func (m *mockEngine) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCount++
	if m.closeErr != nil {
		return m.closeErr
	}
	m.initialized = false
	return nil
}

func (m *mockEngine) IsInitialized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initialized
}

func (m *mockEngine) SetSource(s source.Reader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.source = s
}

func (m *mockEngine) GetSource() source.Reader {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.source
}

func (m *mockEngine) Load(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastKey = key
	m.loaded++
	if m.loadErr != nil {
		return m.loadErr
	}
	if m.source != nil {
		code, err := m.source.Load(context.Background(), key)
		if err != nil {
			return err
		}
		m.lastCode = code
	}
	return nil
}

func (m *mockEngine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := m.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockEngine) LoadString(_ context.Context, _ string, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCode = code
	m.loaded++
	return nil
}

func (m *mockEngine) Execute(_ context.Context) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executed++
	return "exec-result", m.execErr
}

func (m *mockEngine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if err := m.Load(ctx, key); err != nil {
		return nil, err
	}
	return m.Execute(ctx)
}

func (m *mockEngine) ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error) {
	results := make([]any, 0, len(keys))
	for _, k := range keys {
		r, err := m.ExecuteFromKey(ctx, k)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func (m *mockEngine) ExecuteString(_ context.Context, _, code string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCode = code
	m.executed++
	return code, m.execErr
}

func (m *mockEngine) RegisterGlobal(name string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globals[name] = value
	return nil
}

func (m *mockEngine) GetGlobal(name string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.globals[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m *mockEngine) RegisterFunction(name string, fn any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
	return nil
}

func (m *mockEngine) CallFunction(_ context.Context, _ string, _ ...any) (any, error) {
	return m.callResult, m.callErr
}

func (m *mockEngine) RegisterModule(name string, module any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modules[name] = module
	return nil
}

func (m *mockEngine) GetLastError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastError
}

func (m *mockEngine) ClearError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = nil
}

func (m *mockEngine) StartWatch(_ context.Context, _ string) error { return nil }
func (m *mockEngine) StopWatch(_ string) error                     { return nil }

// mockEngineFactory produces mock engines of a fixed type and remembers every
// instance it created. Useful for pool tests that need to count live engines.
type mockEngineFactory struct {
	mu       sync.Mutex
	typ      Type
	created  []*mockEngine
	initErr  error // injected into every produced engine
	closeErr error
}

func (f *mockEngineFactory) new() (*mockEngine, FactoryFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := newMockEngine(f.typ)
	m.initErr = f.initErr
	m.closeErr = f.closeErr
	f.created = append(f.created, m)
	return m, func() (Engine, error) {
		// Rebind: each call produces a fresh mockEngine of the same type.
		f.mu.Lock()
		mm := newMockEngine(f.typ)
		mm.initErr = f.initErr
		mm.closeErr = f.closeErr
		f.created = append(f.created, mm)
		f.mu.Unlock()
		return mm, nil
	}
}

// withFactoryType registers a factory under typ for the duration of the test,
// returning the factory + a cleanup func that un-registers it.
func withFactoryType(t *testing.T, typ Type, initErr, closeErr error) (*mockEngineFactory, func()) {
	t.Helper()
	// Make sure nothing else is registered under this name.
	Unregister(typ)
	f := &mockEngineFactory{typ: typ, initErr: initErr, closeErr: closeErr}
	require.NoError(t, Register(typ, func() (Engine, error) {
		f.mu.Lock()
		mm := newMockEngine(typ)
		mm.initErr = f.initErr
		mm.closeErr = f.closeErr
		f.created = append(f.created, mm)
		f.mu.Unlock()
		return mm, nil
	}))
	return f, func() { Unregister(typ) }
}

////////////////////////////////////////////////////////////////////////////////
// Manager
////////////////////////////////////////////////////////////////////////////////

const managerTestType Type = "mock-manager"

func TestManager_RegisterAndGet(t *testing.T) {
	mgr := NewManager()
	eng := newMockEngine(managerTestType)

	require.NoError(t, mgr.Register("a", eng))

	got, ok := mgr.Get("a")
	require.True(t, ok)
	assert.Same(t, eng, got)

	_, ok = mgr.Get("missing")
	assert.False(t, ok)
}

func TestManager_Register_Duplicate(t *testing.T) {
	mgr := NewManager()
	eng := newMockEngine(managerTestType)

	require.NoError(t, mgr.Register("a", eng))
	err := mgr.Register("a", newMockEngine(managerTestType))
	require.Error(t, err)
}

func TestManager_Register_InvalidArgs(t *testing.T) {
	mgr := NewManager()

	require.Error(t, mgr.Register("", newMockEngine(managerTestType)))
	require.Error(t, mgr.Register("a", nil))
}

func TestManager_InitAll(t *testing.T) {
	mgr := NewManager()
	a, b := newMockEngine(managerTestType), newMockEngine(managerTestType)
	require.NoError(t, mgr.Register("a", a))
	require.NoError(t, mgr.Register("b", b))

	require.NoError(t, mgr.InitAll(context.Background()))
	assert.True(t, a.IsInitialized())
	assert.True(t, b.IsInitialized())
}

func TestManager_InitAll_AbortsOnError(t *testing.T) {
	mgr := NewManager()
	a := newMockEngine(managerTestType)
	b := newMockEngine(managerTestType)
	b.initErr = errors.New("boom")
	require.NoError(t, mgr.Register("a", a))
	require.NoError(t, mgr.Register("b", b))

	// InitAll iterates over a map, so order isn't guaranteed; either way, the
	// error must propagate.
	err := mgr.InitAll(context.Background())
	require.Error(t, err)
}

func TestManager_CloseAll(t *testing.T) {
	mgr := NewManager()
	a, b := newMockEngine(managerTestType), newMockEngine(managerTestType)
	require.NoError(t, mgr.Register("a", a))
	require.NoError(t, mgr.Register("b", b))

	require.NoError(t, mgr.CloseAll())
	assert.Equal(t, 1, a.closeCount)
	assert.Equal(t, 1, b.closeCount)

	// After CloseAll the registry is cleared.
	_, ok := mgr.Get("a")
	assert.False(t, ok)
}

func TestManager_Remove(t *testing.T) {
	mgr := NewManager()
	eng := newMockEngine(managerTestType)
	require.NoError(t, mgr.Register("a", eng))

	mgr.Remove("a", true)
	_, ok := mgr.Get("a")
	assert.False(t, ok)
	assert.Equal(t, 1, eng.closeCount)
}

func TestManager_Remove_NoClose(t *testing.T) {
	mgr := NewManager()
	eng := newMockEngine(managerTestType)
	require.NoError(t, mgr.Register("a", eng))

	mgr.Remove("a", false)
	_, ok := mgr.Get("a")
	assert.False(t, ok)
	assert.Equal(t, 0, eng.closeCount)
}

func TestManager_Default(t *testing.T) {
	mgr := NewManager()
	eng := newMockEngine(managerTestType)
	require.NoError(t, mgr.Register("default", eng))
	mgr.SetDefault("default")

	got, ok := mgr.GetDefault()
	require.True(t, ok)
	assert.Same(t, eng, got)
}
