package script_engine

import (
	"context"
	"errors"
	"sync"
)

// Manager manages the lifecycle and access of multiple Engine instances.
//   - Useful when the application needs multiple engine instances, unified
//     Init/Close, or name-based lookup.
//   - If the project only needs a single global Engine, Manager is unnecessary.
type Manager struct {
	mu      sync.RWMutex
	engines map[string]Engine
	// defaultName optionally records the default engine name for nameless lookup.
	defaultName string
}

// NewManager creates a Manager.
func NewManager() *Manager {
	return &Manager{
		engines: make(map[string]Engine),
	}
}

// Register registers an Engine without initializing it.
// Returns an error if name already exists.
func (m *Manager) Register(name string, eng Engine) error {
	if name == "" || eng == nil {
		return errors.New("invalid name or engine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.engines[name]; ok {
		return errors.New("engine already registered")
	}
	m.engines[name] = eng
	return nil
}

// Get returns the registered Engine for the given name.
func (m *Manager) Get(name string) (Engine, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	eng, ok := m.engines[name]
	return eng, ok
}

// InitAll calls Init on every registered Engine.
func (m *Manager) InitAll(ctx context.Context) error {
	m.mu.RLock()
	list := make([]Engine, 0, len(m.engines))
	for _, e := range m.engines {
		list = append(list, e)
	}
	m.mu.RUnlock()

	for _, e := range list {
		if err := e.Init(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CloseAll closes every registered Engine. Individual Close errors are ignored;
// the last non-nil error is returned.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	list := make([]Engine, 0, len(m.engines))
	for _, e := range m.engines {
		list = append(list, e)
	}
	// Clear the registry to prevent double Close.
	m.engines = make(map[string]Engine)
	m.mu.Unlock()

	var lastErr error
	for _, e := range list {
		if err := e.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Remove unregisters the Engine with the given name.
// If closeIfExists is true and the Engine exists, it is also Closed.
func (m *Manager) Remove(name string, closeIfExists bool) {
	m.mu.Lock()
	e, ok := m.engines[name]
	if ok {
		delete(m.engines, name)
	}
	m.mu.Unlock()

	if ok && closeIfExists {
		_ = e.Close()
	}
}

// SetDefault sets the default engine name used by GetDefault.
func (m *Manager) SetDefault(name string) {
	m.mu.Lock()
	m.defaultName = name
	m.mu.Unlock()
}

// GetDefault returns the default Engine.
func (m *Manager) GetDefault() (Engine, bool) {
	return m.Get(m.defaultName)
}
