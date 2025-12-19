package lua

import (
	"sync"

	Lua "github.com/yuin/gopher-lua"
)

const defaultMaxSaved = 10

func init() {
	luaPool = newStatePool()
}

var luaPool = newStatePool()

// luaStateArray Lua 状态数组
type luaStateArray []*Lua.LState

// statePool Lua 状态池
type statePool struct {
	m        sync.Mutex
	saved    luaStateArray
	maxSaved int
	closed   bool
	options  Lua.Options
}

// newStatePool 创建新的 Lua 状态池
func newStatePool() *statePool {
	return newStatePoolWithOptions(Lua.Options{
		CallStackSize:       4096,
		RegistrySize:        4096,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})
}

func newStatePoolWithOptions(opts Lua.Options) *statePool {
	return &statePool{
		saved:    make(luaStateArray, 0, defaultMaxSaved),
		maxSaved: defaultMaxSaved,
		options:  opts,
	}
}

// SetOptions 在运行时更改池创建新 LState 时使用的选项（线程安全）
func (pl *statePool) SetOptions(opts Lua.Options) {
	pl.m.Lock()
	pl.options = opts
	pl.m.Unlock()
}

// createLuaState 创建新的 Lua 状态实例
func (pl *statePool) createLuaState() *Lua.LState {
	vm := pl.createLuaStateWithOptions(Lua.Options{
		CallStackSize:       4096,
		RegistrySize:        4096,
		SkipOpenLibs:        true,
		IncludeGoStackTrace: true,
	})
	return vm
}

// createLuaStateWithOptions 使用指定选项创建新的 Lua 状态实例
func (pl *statePool) createLuaStateWithOptions(options Lua.Options) *Lua.LState {
	vm := Lua.NewState(options)
	return vm
}

// Borrow 从池中借用一个 Lua 状态实例
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

	// 池为空：若池已关闭仍可创建新的状态（调用者可决定是否继续使用）
	if closed {
		return pl.createLuaState()
	}
	return pl.createLuaState()
}

// Return 将 Lua 状态实例归还到池中
func (pl *statePool) Return(L *Lua.LState) {
	if L == nil {
		return
	}

	pl.m.Lock()
	if pl.closed {
		pl.m.Unlock()
		// 池已关闭，直接释放 L
		L.Close()
		return
	}

	if len(pl.saved) < pl.maxSaved {
		pl.saved = append(pl.saved, L)
		pl.m.Unlock()
		return
	}
	pl.m.Unlock()

	// 池已满，关闭 L 以释放资源
	L.Close()
}

// Shutdown 关闭状态池中的所有 Lua 状态实例
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
