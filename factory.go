package script_engine

import (
	"fmt"
	"sync"
)

// FactoryFunc is the factory function signature used to create an Engine instance.
type FactoryFunc func() (Engine, error)

// factories is the global registry of engine factories, keyed by Type.
var (
	factoryMu sync.RWMutex
	factories = make(map[Type]FactoryFunc)
)

// NewScriptEngine creates an Engine instance using the factory registered for typ.
func NewScriptEngine(typ Type) (Engine, error) {
	f, ok := GetFactory(typ)
	if !ok {
		return nil, fmt.Errorf("script engine factory %s not registered", typ)
	}
	return f()
}

// Register registers a FactoryFunc for the given Type.
// Returns an error if a factory for typ is already registered.
func Register(typ Type, f FactoryFunc) error {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		return fmt.Errorf("script engine factory %s already registered", typ)
	}
	factories[typ] = f
	return nil
}

// GetFactory returns the FactoryFunc registered for typ and whether it existed.
func GetFactory(typ Type) (FactoryFunc, bool) {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	f, ok := factories[typ]
	return f, ok
}

// ListFactories returns a snapshot of all currently registered Types.
func ListFactories() []Type {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	res := make([]Type, 0, len(factories))
	for k := range factories {
		res = append(res, k)
	}
	return res
}

// Unregister removes the factory registered for typ.
// It returns true if a factory was removed.
func Unregister(typ Type) bool {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		delete(factories, typ)
		return true
	}
	return false
}
