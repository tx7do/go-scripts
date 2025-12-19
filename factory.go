package script_engine

import (
	"fmt"
	"sync"
)

type FactoryFunc func() (Engine, error)

var (
	factoryMu sync.RWMutex
	factories = make(map[Type]FactoryFunc)
)

func Register(typ Type, f FactoryFunc) error {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		return fmt.Errorf("script engine factory %s already registered", typ)
	}
	factories[typ] = f
	return nil
}

func GetFactory(typ Type) (FactoryFunc, bool) {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	f, ok := factories[typ]
	return f, ok
}

func ListFactories() []Type {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	res := make([]Type, 0, len(factories))
	for k := range factories {
		res = append(res, k)
	}
	return res
}

func Unregister(typ Type) bool {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if _, ok := factories[typ]; ok {
		delete(factories, typ)
		return true
	}
	return false
}
