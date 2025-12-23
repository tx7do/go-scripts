package script_engine

import (
	"context"
	"io"
)

// Engine Define the interface for script engines
type Engine interface {
	// GetType get the type of the script engine
	GetType() Type

	//////////////////////////////////////////////////////////////////////////////////////////
	// Lifecycle Management
	//////////////////////////////////////////////////////////////////////////////////////////

	// Init initialize the script engine
	Init(ctx context.Context) error
	// Close the script engine and release resources
	Close() error
	// IsInitialized check if the engine is initialized
	IsInitialized() bool

	//////////////////////////////////////////////////////////////////////////////////////////
	// Script Loading
	//////////////////////////////////////////////////////////////////////////////////////////

	// LoadString load script from string source
	LoadString(ctx context.Context, source string) error
	// LoadStrings load multiple scripts from string sources
	LoadStrings(ctx context.Context, sources []string) error
	// LoadFile load script from file path
	LoadFile(ctx context.Context, filePath string) error
	// LoadFiles load multiple scripts from file paths
	LoadFiles(ctx context.Context, filePaths []string) error
	// LoadReader load script from io.Reader
	LoadReader(ctx context.Context, reader io.Reader, name string) error

	//////////////////////////////////////////////////////////////////////////////////////////
	// Script Execution
	//////////////////////////////////////////////////////////////////////////////////////////

	// ExecuteLoaded execute the previously loaded script(s)
	ExecuteLoaded(ctx context.Context) (any, error)
	// ExecuteStrings execute multiple scripts from string sources (immediate execution)
	ExecuteStrings(ctx context.Context, sources []string) ([]any, error)
	// ExecuteFiles execute multiple scripts from file paths (immediate execution)
	ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error)
	// ExecuteString execute script from string source
	ExecuteString(ctx context.Context, source string) (any, error)
	// ExecuteFile execute script from file path
	ExecuteFile(ctx context.Context, filePath string) (any, error)

	//////////////////////////////////////////////////////////////////////////////////////////
	// Global Variable Registration
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterGlobal register a global variable
	RegisterGlobal(name string, value any) error
	// GetGlobal get a global variable
	GetGlobal(name string) (any, error)

	//////////////////////////////////////////////////////////////////////////////////////////
	// Function Call
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterFunction register a function with the given name
	RegisterFunction(name string, fn any) error
	// CallFunction call a function with the given name and arguments
	CallFunction(ctx context.Context, name string, args ...any) (any, error)

	//////////////////////////////////////////////////////////////////////////////////////////
	// Module Management
	//////////////////////////////////////////////////////////////////////////////////////////

	// RegisterModule register a module with the given name
	RegisterModule(name string, module any) error

	//////////////////////////////////////////////////////////////////////////////////////////
	// Error Handling
	//////////////////////////////////////////////////////////////////////////////////////////

	// GetLastError get the last error occurred in the engine
	GetLastError() error
	// ClearError clear the last error
	ClearError()
}
