package starlark

import (
	"reflect"

	"go.starlark.net/starlark"
)

// goToStarlark converts a Go value to a Starlark value.
func goToStarlark(v any) starlark.Value {
	if v == nil {
		return starlark.None
	}
	switch val := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(val)
	case int:
		return starlark.MakeInt(val)
	case int32:
		return starlark.MakeInt64(int64(val))
	case int64:
		return starlark.MakeInt64(val)
	case uint:
		return starlark.MakeInt64(int64(val))
	case uint32:
		return starlark.MakeInt64(int64(val))
	case uint64:
		return starlark.MakeInt64(int64(val))
	case float32:
		return starlark.Float(val)
	case float64:
		return starlark.Float(val)
	case string:
		return starlark.String(val)
	case []byte:
		return starlark.String(string(val))
	case []string:
		elems := make([]starlark.Value, len(val))
		for i, s := range val {
			elems[i] = starlark.String(s)
		}
		return starlark.NewList(elems)
	case []any:
		elems := make([]starlark.Value, len(val))
		for i, item := range val {
			elems[i] = goToStarlark(item)
		}
		return starlark.NewList(elems)
	case map[string]any:
		d := starlark.NewDict(len(val))
		for k, vv := range val {
			_ = d.SetKey(starlark.String(k), goToStarlark(vv))
		}
		return d
	default:
		return starlark.None
	}
}

// starlarkToGo converts a Starlark value to a Go value.
func starlarkToGo(v starlark.Value) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(val)
	case starlark.Int:
		n, ok := val.Int64()
		if ok {
			return n
		}
		return val.String()
	case starlark.Float:
		return float64(val)
	case starlark.String:
		return string(val)
	case *starlark.List:
		result := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	case starlark.Tuple:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = starlarkToGo(item)
		}
		return result
	case *starlark.Dict:
		result := make(map[string]any)
		for _, item := range val.Items() {
			kv, _ := item[0].(starlark.String)
			result[string(kv)] = starlarkToGo(item[1])
		}
		return result
	case starlark.Callable:
		return val.String()
	default:
		return val.String()
	}
}

// callHostFunc invokes a Go function via reflection.
func callHostFunc(fn any, args []any) any {
	fnVal := reflect.ValueOf(fn)
	fnType := fnVal.Type()

	argVals := make([]reflect.Value, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		if i < len(args) {
			argVals[i] = reflect.ValueOf(args[i])
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

// callHostFuncFromStarlark is the callback for starlark.NewBuiltin.
func callHostFuncFromStarlark(fn any, args starlark.Tuple) (starlark.Value, error) {
	fnVal := reflect.ValueOf(fn)
	fnType := fnVal.Type()

	numIn := fnType.NumIn()
	goArgs := make([]any, numIn)
	for i := 0; i < numIn; i++ {
		if i < len(args) {
			goArgs[i] = starlarkToGo(args[i])
		}
	}

	result := callHostFunc(fn, goArgs)
	return goToStarlark(result), nil
}
