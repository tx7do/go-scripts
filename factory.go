package script_engine

import (
	"fmt"
	"sync"
)

// FactoryFunc 是用于创建 Engine 实例的工厂函数类型。
type FactoryFunc func() (Engine, error)

var (
	factoryMu sync.RWMutex
	factories = make(map[Type]FactoryFunc)
)

// NewScriptEngine 使用已注册的工厂函数创建一个 Engine 实例。
func NewScriptEngine(typ Type) (Engine, error) {
	f, ok := GetFactory(typ)
	if !ok {
		return nil, fmt.Errorf("script engine factory %s not registered", typ)
	}
	return f()
}

// Register registers a FactoryFunc for a given Type.
func Register(typ Type, f FactoryFunc) error {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		return fmt.Errorf("script engine factory %s already registered", typ)
	}
	factories[typ] = f
	return nil
}

// GetFactory returns a registered FactoryFunc for a given Type and whether it existed.
func GetFactory(typ Type) (FactoryFunc, bool) {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	f, ok := factories[typ]
	return f, ok
}

// ListFactories returns a slice of currently registered Types.
func ListFactories() []Type {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	res := make([]Type, 0, len(factories))
	for k := range factories {
		res = append(res, k)
	}
	return res
}

// Unregister removes a registered factory by Type. It returns true if a factory was removed.
func Unregister(typ Type) bool {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		delete(factories, typ)
		return true
	}
	return false
}
