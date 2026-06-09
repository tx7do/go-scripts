package js

import (
	"errors"
	"os"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/process"
	"github.com/dop251/goja_nodejs/require"
)

// virtualMachine wraps a goja runtime together with a require registry and
// the currently-loaded program. It is the lower-level building block used by
// the engine implementation.
type virtualMachine struct {
	vm       *goja.Runtime
	program  *goja.Program
	registry *require.Registry
}

// newVirtualMachine creates a virtualMachine with a fresh goja runtime and the
// standard nodejs modules (require, console, process) enabled.
func newVirtualMachine() *virtualMachine {
	exec := &virtualMachine{
		vm:       goja.New(),
		registry: new(require.Registry),
	}
	exec.init()
	return exec
}

// init enables the require/console/process modules on the runtime.
func (e *virtualMachine) init() {
	_ = e.registry.Enable(e.vm)
	console.Enable(e.vm)
	process.Enable(e.vm)
}

// Destroy tears the virtual machine down. For performance reasons this is a
// no-op for now; recycle via a pool if needed.
func (e *virtualMachine) Destroy() {
}

// LoadString compiles the given source string into a goja program.
func (e *virtualMachine) LoadString(source string) error {
	program, err := goja.Compile("", source, true)
	if err != nil {
		return err
	}

	e.program = program

	return nil
}

// LoadFile reads and compiles the script at the given file path.
func (e *virtualMachine) LoadFile(filePath string) error {
	code, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	program, err := goja.Compile("", string(code), false)
	if err != nil {
		return err
	}

	e.program = program

	return nil
}

// Execute runs the previously-compiled program.
func (e *virtualMachine) Execute() error {
	if e.program == nil {
		return errors.New("no js")
	}
	_, err := e.vm.RunProgram(e.program)
	return err
}

// ExecuteString immediately runs the given source string.
func (e *virtualMachine) ExecuteString(source string) error {
	_, err := e.vm.RunString(source)
	return err
}

// ExecuteFile immediately runs the script at the given file path.
func (e *virtualMachine) ExecuteFile(filePath string) error {
	code, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	_, err = e.vm.RunScript("", string(code))
	return err
}

// Register binds a Go value (variable or function) into the JS global scope.
func (e *virtualMachine) Register(name string, value interface{}) error {
	err := e.vm.Set(name, value)
	return err
}

// GetFunction exports the JS value named `name` into the Go target `fn`.
func (e *virtualMachine) GetFunction(name string, fn interface{}) error {
	err := e.vm.ExportTo(e.vm.Get(name), fn)
	return err
}
