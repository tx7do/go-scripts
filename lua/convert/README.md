# Lua ↔ Go 深度值转换

[`convert`](convert.go) 提供 Go 与 Lua 值的双向深度递归转换，供宿主层在 VM 边界交换嵌套数据结构（本模块 `host` 桥与宿主业务代码共用）。

## API

### Go → Lua

```go
func ToLuaValue(L *lua.LState, val any) lua.LValue
```

- 已是 `lua.LValue` 的值原样返回；
- 基础类型（bool / string / 各整型 / float32/64）直转；
- `map[string]any` / `[]any` 递归建表；
- 其余复杂类型（struct、其他 map/slice）走反射兜底。

### Lua → Go

```go
func ToGoValue(val lua.LValue) any
```

- `LTString` → `string`、`LTBool` → `bool`、`LTNil` → `nil`、`LTNumber` → `float64`；
- `LTTable` 按结构判定为 `map[string]any` 或 `[]any`，深度递归。

### 便捷标量

`ToString` / `ToNumber` / `ToBool`——对常见标量做宽容取值。

## 注意

Lua 5.1 的 number 一律是 `float64`：从 Lua 取回的"整数"需要自行判断/取整后再交给期望 int 的 Go 接口。
