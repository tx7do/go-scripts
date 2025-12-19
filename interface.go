package script_engine

import (
	"context"
	"io"
)

// Engine Define the interface for script engines
type Engine interface {
	// 生命周期管理

	Init(ctx context.Context) error
	Destroy() error
	IsInitialized() bool

	// 脚本加载

	LoadString(ctx context.Context, source string) error
	LoadFile(ctx context.Context, filePath string) error
	LoadReader(ctx context.Context, reader io.Reader, name string) error

	// 脚本执行

	Execute(ctx context.Context) (any, error)
	ExecuteString(ctx context.Context, source string) (any, error)
	ExecuteFile(ctx context.Context, filePath string) (any, error)

	// 全局变量注册

	RegisterGlobal(name string, value any) error
	GetGlobal(name string) (any, error)

	// 函数调用

	RegisterFunction(name string, fn any) error
	CallFunction(ctx context.Context, name string, args ...any) (any, error)

	// 模块管理

	RegisterModule(name string, module any) error

	// 错误处理

	GetLastError() error
	ClearError()
}
