package cel

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

////////////////////////////////////////////////////////////////////////////////
// Internal helpers
////////////////////////////////////////////////////////////////////////////////

// compileAndProgram compiles a CEL expression into an AST and Program using the
// current environment. Caller must hold execMu.
func (e *engine) compileAndProgram(code string) (*cel.Ast, cel.Program, error) {
	ast, iss := e.env.Compile(code)
	if iss.Err() != nil {
		return nil, nil, iss.Err()
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, nil, err
	}
	return ast, prg, nil
}

// rebuildEnvLocked rebuilds the CEL environment with all registered globals and
// functions, then recompiles all stored programs. Caller must hold e.mu.
func (e *engine) rebuildEnvLocked() error {
	e.execMu.Lock()
	defer e.execMu.Unlock()

	// Build env options from registered globals and functions.
	opts := make([]cel.EnvOption, 0, len(e.globals)+len(e.hostFuncs))

	for name, g := range e.globals {
		opts = append(opts, cel.Variable(name, g.typ))
	}

	for name, fi := range e.hostFuncs {
		opts = append(opts,
			cel.Function(name,
				cel.Overload(name,
					fi.params,
					fi.retType,
					cel.FunctionBinding(createBinding(fi)),
				),
			),
		)
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return fmt.Errorf("cel engine: rebuild env: %w", err)
	}
	e.env = env

	// Recompile all stored programs with the new environment.
	for i, p := range e.programs {
		ast, iss := env.Compile(p.expr)
		if iss.Err() != nil {
			return fmt.Errorf("cel engine: recompile %q: %w", p.name, iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			return fmt.Errorf("cel engine: reprogram %q: %w", p.name, err)
		}
		e.programs[i].ast = ast
		e.programs[i].prg = prg
	}

	return nil
}

// evalVars builds the variable map from registered globals for CEL evaluation.
// Caller must hold e.mu (or its read lock).
func (e *engine) evalVars() map[string]any {
	vars := make(map[string]any, len(e.globals))
	for name, g := range e.globals {
		vars[name] = g.value
	}
	return vars
}

// createBinding creates a cel.FunctionBinding from a funcInfo. The binding
// converts ref.Val arguments to Go values, calls the underlying Go function
// via reflection, and converts the result back to ref.Val.
func createBinding(fi *funcInfo) func(args ...ref.Val) ref.Val {
	return func(args ...ref.Val) ref.Val {
		goArgs := make([]reflect.Value, len(args))
		for i, arg := range args {
			goArgs[i] = reflect.ValueOf(celValToGo(arg))
			// Ensure the argument is assignable to the parameter type.
			if i < fi.funcType.NumIn() {
				paramType := fi.funcType.In(i)
				if goArgs[i].Type() != paramType {
					// Try to convert.
					if goArgs[i].Type().ConvertibleTo(paramType) {
						goArgs[i] = goArgs[i].Convert(paramType)
					}
				}
			}
		}

		results := reflect.ValueOf(fi.fn).Call(goArgs)
		if len(results) == 0 {
			return types.NullValue
		}
		return types.DefaultTypeAdapter.NativeToValue(results[0].Interface())
	}
}

// inferCelType converts a Go reflect.Type to the corresponding CEL type.
func inferCelType(t reflect.Type) *cel.Type {
	if t == nil {
		return cel.DynType
	}

	switch t.Kind() {
	case reflect.String:
		return cel.StringType
	case reflect.Bool:
		return cel.BoolType
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cel.IntType
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return cel.UintType
	case reflect.Float32, reflect.Float64:
		return cel.DoubleType
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return cel.BytesType
		}
		return cel.ListType(inferCelType(t.Elem()))
	case reflect.Map:
		return cel.MapType(inferCelType(t.Key()), inferCelType(t.Elem()))
	case reflect.Ptr:
		return inferCelType(t.Elem())
	default:
		// Check for time.Time
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return cel.TimestampType
		}
		if t.PkgPath() == "time" && t.Name() == "Duration" {
			return cel.DurationType
		}
		return cel.DynType
	}
}

// celValToGo converts a ref.Val to a Go native value.
func celValToGo(v ref.Val) any {
	if v == nil {
		return nil
	}
	// Try the underlying Go value.
	switch val := v.(type) {
	case types.Bool:
		return bool(val)
	case types.Int:
		return int64(val)
	case types.Uint:
		return uint64(val)
	case types.Double:
		return float64(val)
	case types.String:
		return string(val)
	case types.Bytes:
		return []byte(val)
	case types.Null:
		return nil
	case traits.Lister:
		size := int64(val.Size().(types.Int))
		result := make([]any, size)
		for i := int64(0); i < size; i++ {
			result[i] = celValToGo(val.Get(types.Int(i)))
		}
		return result
	case traits.Mapper:
		result := make(map[string]any)
		it := val.Iterator()
		for it.HasNext() == types.True {
			k := it.Next()
			v2 := val.Get(k)
			result[string(k.(types.String))] = celValToGo(v2)
		}
		return result
	default:
		// Use Value() for other types.
		return v.Value()
	}
}
