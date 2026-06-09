package tcl

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// goToTCLString converts a Go value to a TCL-compatible string representation.
// For strings, it wraps the value in double quotes with proper escaping.
func goToTCLString(v any) string {
	if v == nil {
		return "\"\""
	}
	switch val := v.(type) {
	case nil:
		return "\"\""
	case bool:
		if val {
			return "1"
		}
		return "0"
	case int:
		return strconv.Itoa(val)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case string:
		return tclQuote(val)
	case []byte:
		return tclQuote(string(val))
	case []string:
		parts := make([]string, len(val))
		for i, s := range val {
			parts[i] = tclQuote(s)
		}
		return "{" + strings.Join(parts, " ") + "}"
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = goToTCLString(item)
		}
		return "{" + strings.Join(parts, " ") + "}"
	default:
		return tclQuote(fmt.Sprint(v))
	}
}

// tclQuote wraps a string in double quotes with TCL-specific escaping.
func tclQuote(s string) string {
	// If the string is a simple number, no quoting needed.
	if s == "" {
		return "\"\""
	}
	// Use brace quoting for strings with special characters.
	// Brace quoting in TCL treats everything literally except nested braces.
	if strings.ContainsAny(s, " \t\n;[]$\\\"{}") {
		// Use brace quoting if the string doesn't contain unbalanced braces.
		if !strings.ContainsAny(s, "{}") {
			return "{" + s + "}"
		}
		// Fall back to backslash escaping.
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		s = strings.ReplaceAll(s, "$", "\\$")
		s = strings.ReplaceAll(s, "[", "\\[")
		s = strings.ReplaceAll(s, "]", "\\]")
		return "\"" + s + "\""
	}
	return s
}

// parseTCLValue attempts to parse a TCL string result into a Go type.
func parseTCLValue(s string) any {
	if s == "" {
		return ""
	}
	// Try int.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	// Try float.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Try bool.
	switch s {
	case "1", "true", "True", "TRUE", "yes", "on":
		// Only "1"/"0" are canonical TCL bool; but be permissive.
		if s == "1" {
			return true
		}
	case "0", "false", "False", "FALSE", "no", "off":
		if s == "0" {
			return false
		}
	}
	return s
}

// sanitizeCmdName replaces dots/hyphens in module-style names to valid TCL
// command names (alphanumeric, underscore, colon).
func sanitizeCmdName(name string) string {
	return strings.ReplaceAll(name, ".", "::")
}

// callHostFunc invokes a Go function via reflection with string args.
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
