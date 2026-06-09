package lua

import (
	"sync"

	Lua "github.com/yuin/gopher-lua"
)

// defaultMaxSaved is the default upper bound on recycled LState instances.
const defaultMaxSaved = 10

func init() {
	luaPool = newStatePool()
}

// luaPool is the package-level pool used by newVirtualMachine.
var luaPool = newStatePool()

// luaStateArray is a stack of LState pointers awaiting reuse.
type luaStateArray []*Lua.LState

// statePool recycles LState instances to avoid the cost of creating new ones
// on every script run.
type statePool struct {
	m        sync.Mutex
	saved    luaStateArray
	maxSaved int
	closed   bool
	options  Lua.Options
}

// newStatePool creates a statePool with sensible defaults.
func newStatePool() *statePool {
	return newStatePoolWithOptions(Lua.Options{
		CallStackSize:       4096,
		RegistrySize:        4096,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})
}

// newStatePoolWithOptions creates a statePool using the supplied Lua.Options.
func newStatePoolWithOptions(opts Lua.Options) *statePool {
	return &statePool{
		saved:    make(luaStateArray, 0, defaultMaxSaved),
		maxSaved: defaultMaxSaved,
		options:  opts,
	}
}

// SetOptions updates the options used when creating new LState instances.
// Thread-safe.
func (pl *statePool) SetOptions(opts Lua.Options) {
	pl.m.Lock()
	pl.options = opts
	pl.m.Unlock()
}

// createLuaState creates a new LState using the default options.
func (pl *statePool) createLuaState() *Lua.LState {
	vm := pl.createLuaStateWithOptions(Lua.Options{
		CallStackSize:       4096,
		RegistrySize:        4096,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})
	return vm
}

// createLuaStateWithOptions creates a new LState using the given options.
func (pl *statePool) createLuaStateWithOptions(options Lua.Options) *Lua.LState {
	vm := Lua.NewState(options)
	return vm
}

// Borrow returns an LState from the pool, creating a new one when none is idle.
func (pl *statePool) Borrow() *Lua.LState {
	pl.m.Lock()
	n := len(pl.saved)
	if n > 0 {
		x := pl.saved[n-1]
		pl.saved = pl.saved[:n-1]
		pl.m.Unlock()
		return x
	}
	closed := pl.closed
	pl.m.Unlock()

	// Pool empty: even if the pool is closed we still create a new state; the
	// caller may decide whether to keep using it.
	if closed {
		return pl.createLuaState()
	}
	return pl.createLuaState()
}

// Return puts an LState back into the pool. If the pool is closed or full, the
// LState is Closed instead to release its resources.
func (pl *statePool) Return(L *Lua.LState) {
	if L == nil {
		return
	}

	pl.m.Lock()
	if pl.closed {
		pl.m.Unlock()
		// Pool is closed; release L immediately.
		L.Close()
		return
	}

	if len(pl.saved) < pl.maxSaved {
		pl.saved = append(pl.saved, L)
		pl.m.Unlock()
		return
	}
	pl.m.Unlock()

	// Pool is full; close L to release resources.
	L.Close()
}

// Shutdown closes the pool and releases every idle LState it owns.
func (pl *statePool) Shutdown() {
	pl.m.Lock()
	if pl.closed {
		pl.m.Unlock()
		return
	}
	pl.closed = true
	toClose := pl.saved
	pl.saved = nil
	pl.m.Unlock()

	for _, L := range toClose {
		if L != nil {
			L.Close()
		}
	}
}
