package expr

import (
	"fmt"
	"reflect"

	"github.com/go-python/gpython/py"
)

// goToPyObj converts a Go value to a Python object.
func goToPyObj(v any) py.Object {
	if v == nil {
		return py.None
	}
	switch val := v.(type) {
	case nil:
		return py.None
	case bool:
		return py.Bool(val)
	case int:
		return py.Int(val)
	case int32:
		return py.Int(int64(val))
	case int64:
		return py.Int(val)
	case uint:
		return py.Int(int64(val))
	case uint32:
		return py.Int(int64(val))
	case uint64:
		return py.Int(int64(val))
	case float32:
		return py.Float(val)
	case float64:
		return py.Float(val)
	case string:
		return py.String(val)
	case []byte:
		return py.String(string(val))
	case []string:
		return py.NewListFromStrings(val)
	default:
		// For unsupported types, return None.
		return py.None
	}
}

// pyObjToGo converts a Python object to a Go value.
func pyObjToGo(obj py.Object) any {
	if obj == nil {
		return nil
	}
	switch v := obj.(type) {
	case py.Int:
		return int64(v)
	case *py.Int:
		return int64(*v)
	case py.Float:
		return float64(v)
	case *py.Float:
		return float64(*v)
	case py.String:
		return string(v)
	case *py.String:
		return string(*v)
	case py.Bool:
		return bool(v)
	case *py.Bool:
		return bool(*v)
	case *py.BigInt:
		goVal, err := v.GoInt64()
		if err != nil {
			return nil
		}
		return goVal
	case *py.NoneType:
		return nil
	case *py.List:
		result := make([]any, len(v.Items))
		for i, item := range v.Items {
			result[i] = pyObjToGo(item)
		}
		return result
	case py.Tuple:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = pyObjToGo(item)
		}
		return result
	case py.StringDict:
		result := make(map[string]any)
		for k, val := range v {
			result[k] = pyObjToGo(val)
		}
		return result
	default:
		return obj
	}
}

// createPythonCallable is a placeholder. gpython v0.2.0 does not support
// creating custom callable objects directly from Go. Host functions are
// stored internally and can be called via CallFunction from Go.
func createPythonCallable(name string, fn any) py.Object {
	return py.None
}

// buildPythonCallCode generates a Python expression string that calls a
// function by name with the given number of arguments.
func buildPythonCallCode(name string, numArgs int) string {
	if numArgs == 0 {
		return name + "()"
	}
	code := name + "(__arg0__"
	for i := 1; i < numArgs; i++ {
		code += fmt.Sprintf(", __arg%d__", i)
	}
	code += ")"
	return code
}

// callHostFunc invokes a Go function via reflection with converted arguments.
func callHostFunc(fn any, args []any) any {
	fnVal := reflect.ValueOf(fn)
	fnType := fnVal.Type()

	argVals := make([]reflect.Value, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		if i < len(args) {
			argVals[i] = reflect.ValueOf(args[i])
			// Try to convert if types don't match.
			if argVals[i].Type() != fnType.In(i) {
				if argVals[i].Type().ConvertibleTo(fnType.In(i)) {
					argVals[i] = argVals[i].Convert(fnType.In(i))
				}
			}
		}
	}

	results := fnVal.Call(argVals)
	if len(results) == 0 {
		return nil
	}
	return results[0].Interface()
}
