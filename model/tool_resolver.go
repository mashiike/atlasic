package model

import (
	"context"
	"fmt"
	"sync"
)

type ToolResolver interface {
	Resolve(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error)
}

// ToolResolverFunc is an adapter to allow the use of ordinary functions as ToolResolvers
type ToolResolverFunc func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error)

// Resolve implements ToolResolver interface
func (f ToolResolverFunc) Resolve(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
	return f(ctx, toolUse)
}

// DefaultToolResolver implements http.ServeMux-like tool routing
type DefaultToolResolver struct {
	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error)
}

// RegisterTool registers a tool handler for the given tool name
// Panics if the tool name is already registered (similar to http.ServeMux)
func (d *DefaultToolResolver) RegisterTool(toolName string, handler func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if toolName == "" {
		panic("model: invalid tool name")
	}

	if handler == nil {
		panic("model: nil tool handler")
	}

	if d.handlers == nil {
		d.handlers = make(map[string]func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error))
	}

	if _, exists := d.handlers[toolName]; exists {
		panic(fmt.Sprintf("model: multiple registrations for tool %q", toolName))
	}

	d.handlers[toolName] = handler
}

// Resolve implements ToolResolver interface
func (d *DefaultToolResolver) Resolve(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	handler, exists := d.handlers[toolUse.ToolName]
	if !exists {
		return nil, fmt.Errorf("tool %q not registered", toolUse.ToolName)
	}

	return handler, nil
}

var globalToolResolver DefaultToolResolver

// RegisterTool registers a tool handler with the global tool resolver
// Panics if the tool name is already registered
func RegisterTool(toolName string, handler func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error)) {
	globalToolResolver.RegisterTool(toolName, handler)
}
