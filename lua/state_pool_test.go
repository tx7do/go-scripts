package lua

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yuin/gluamapper"
	lua "github.com/yuin/gopher-lua"
)

// testWorker runs a short Lua script using a LState borrowed from the pool.
// The pool is created with SkipOpenLibs=true, so each borrower must OpenLibs
// itself before it can use the standard library (print, etc.).
func testWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	L := luaPool.Borrow()
	defer luaPool.Return(L)
	L.OpenLibs()
	if err := L.DoString(`print("hello")`); err != nil {
		panic(err)
	}
}

func TestStatePool(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go testWorker(&wg)
	go testWorker(&wg)
	wg.Wait()
}

func TestLuaTableMap(t *testing.T) {
	// Map a Lua table onto a Go struct.
	type Role struct {
		Name string
	}

	type Person struct {
		Name      string
		Age       int
		WorkPlace string
		Role      []*Role
	}

	L := luaPool.Borrow()
	if err := L.DoString(`
person = {
  name = "Michel",
  age  = "31", -- weakly input
  work_place = "San Jose",
  role = {
    {
      name = "Administrator"
    },
    {
      name = "Operator"
    }
  }
}
`); err != nil {
		panic(err)
	}

	var person Person
	if err := gluamapper.Map(L.GetGlobal("person").(*lua.LTable), &person); err != nil {
		panic(err)
	}
	fmt.Printf("%v+", person)
}
