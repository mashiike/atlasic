// Package model provides core types and utilities for language model integration,
// tool use, reasoning, and model provider management in the atlasic project.
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mashiike/atlasic/a2a"
)

// =============================================================================
// CORE MODEL TYPES
// =============================================================================

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema,omitempty"` // JSON Schema for parameters
}

type GenerationOptions struct {
	Temperature    *float32 `json:"temperature,omitempty"`
	TopP           *float32 `json:"top_p,omitempty"`
	MaxTokens      *int     `json:"max_tokens,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

const (
	ToolChoiceAuto     = "auto"     // Automatically choose tool based on input
	ToolChoiceRequired = "required" // Always use a tool if available
	ToolChoiceSpecific = "specific" // Use a specific tool if available
)

type ToolChoice struct {
	Type string `json:"type"` // "auto", "required", or "specific"
	// ToolName is only used if Type is "specific"
	ToolName string `json:"tool_name,omitempty"` // Name of the specific tool to use
}

type GenerateRequest struct {
	System     string             `json:"system,omitempty"`
	Messages   []a2a.Message      `json:"messages"`              // Conversation history
	Tools      []Tool             `json:"tools,omitempty"`       // Available tools for the LLM
	Options    *GenerationOptions `json:"options,omitempty"`     // Generation options
	Extensions map[string]any     `json:"extensions,omitempty"`  // Additional extensions
	ToolChoice *ToolChoice        `json:"tool_choice,omitempty"` // Tool choice for tool use
}

// Model represents a language model interface
type Model interface {
	// ID returns the unique identifier for this model
	ID() string
	// Generate generates a completion based on conversation history and tools
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)

	// InputModes returns the input modes supported by this model
	InputModes() []string

	// OutputModes returns the output modes supported by this model
	OutputModes() []string
}

// StopReason represents why the LLM stopped generating
type StopReason string

const (
	StopReasonMaxTokens StopReason = "max_tokens" // Hit maximum token limit
	StopReasonStopWord  StopReason = "stop_word"  // Hit a stop word/sequence
	StopReasonToolUse   StopReason = "tool_use"   // Requested tool use
	StopReasonEndTurn   StopReason = "end_turn"   // Natural end of response
	StopReasonError     StopReason = "error"      // Error occurred
)

// GenerateResponse contains the response from text generation
type GenerateResponse struct {
	Message     a2a.Message    `json:"message"`
	StopReason  StopReason     `json:"stop_reason"`
	Usage       *Usage         `json:"usage,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	RawResponse any            `json:"raw_response,omitempty"` // Provider-specific raw response (e.g., openai.ChatCompletion, bedrockruntime.ConverseOutput)
}

// Usage contains comprehensive token usage information
type Usage struct {
	// Token usage
	InputTokens  *int `json:"input_tokens,omitempty"`  // Input/prompt tokens
	OutputTokens *int `json:"output_tokens,omitempty"` // Output/completion tokens
	TotalTokens  *int `json:"total_tokens,omitempty"`  // Total tokens used

	// Image-specific usage
	ImageTokens     *int    `json:"image_tokens,omitempty"`     // Tokens used for image processing
	ImageRequests   *int    `json:"image_requests,omitempty"`   // Number of image generation requests
	ImageResolution *string `json:"image_resolution,omitempty"` // Image resolution (e.g., "1024x1024")

	// Provider-specific usage details
	ProviderUsage any `json:"provider_usage,omitempty"` // Raw usage data from provider (e.g., openai.Usage, bedrock usage metrics)

	// Model information
	ModelID string `json:"model_id,omitempty"` // Model used for generation
}

// ModelProvider manages models for a specific provider (e.g., ollama, openai)
type ModelProvider interface {
	// GetModel returns a specific model by ID
	GetModel(ctx context.Context, modelID string) (Model, error)
}

// =============================================================================
// MODEL HOOKS
// =============================================================================

// PreGenerateHook is called before Model.Generate is executed
type PreGenerateHook func(ctx context.Context, providerID string, m Model, req *GenerateRequest) error

// PostGenerateHook is called after Model.Generate is executed
type PostGenerateHook func(ctx context.Context, providerID string, m Model, req *GenerateRequest, resp *GenerateResponse, err error) error

// ModelHooks contains hook functions for model operations
type ModelHooks struct {
	PreGenerate  []PreGenerateHook  // Called before Generate
	PostGenerate []PostGenerateHook // Called after Generate
}

// HookedModel wraps a Model with hooks
type HookedModel struct {
	model      Model
	hooks      *ModelHooks
	providerID string
}

// ID returns the ID of the wrapped model
func (h *HookedModel) ID() string {
	return h.model.ID()
}

// InputModes returns the input modes of the wrapped model
func (h *HookedModel) InputModes() []string {
	return h.model.InputModes()
}

// OutputModes returns the output modes of the wrapped model
func (h *HookedModel) OutputModes() []string {
	return h.model.OutputModes()
}

// Generate executes pre-hooks, calls the original Generate, then executes post-hooks
func (h *HookedModel) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	// Execute pre-generate hooks
	for _, hook := range h.hooks.PreGenerate {
		if err := hook(ctx, h.providerID, h.model, req); err != nil {
			return nil, fmt.Errorf("pre-generate hook failed: %w", err)
		}
	}

	// Call original Generate
	resp, err := h.model.Generate(ctx, req)

	// Execute post-generate hooks
	for _, hook := range h.hooks.PostGenerate {
		if hookErr := hook(ctx, h.providerID, h.model, req, resp, err); hookErr != nil {
			// Log the hook error but don't override the original error
			// In a real implementation, you might want to use a proper logger
			fmt.Printf("Warning: post-generate hook failed: %v\n", hookErr)
		}
	}

	return resp, err
}

// =============================================================================
// MODEL REGISTRY
// =============================================================================

// Registry manages registered model providers
type Registry struct {
	providers map[string]ModelProvider
	hooks     *ModelHooks // global hooks for all providers
	mutex     sync.RWMutex
}

var globalRegistry = &Registry{
	providers: make(map[string]ModelProvider),
	hooks:     &ModelHooks{},
}

// Register registers a model provider with the given name
func Register(name string, provider ModelProvider) {
	globalRegistry.Register(name, provider)
}

// AddPreGenerateHook adds a global pre-generate hook
func AddPreGenerateHook(hook PreGenerateHook) {
	globalRegistry.AddPreGenerateHook(hook)
}

// AddPostGenerateHook adds a global post-generate hook
func AddPostGenerateHook(hook PostGenerateHook) {
	globalRegistry.AddPostGenerateHook(hook)
}

// Register registers a model provider with the given name
func (r *Registry) Register(name string, provider ModelProvider) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.providers[name] = provider
}

// AddPreGenerateHook adds a global pre-generate hook
func (r *Registry) AddPreGenerateHook(hook PreGenerateHook) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.hooks == nil {
		r.hooks = &ModelHooks{}
	}
	r.hooks.PreGenerate = append(r.hooks.PreGenerate, hook)
}

// AddPostGenerateHook adds a global post-generate hook
func (r *Registry) AddPostGenerateHook(hook PostGenerateHook) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.hooks == nil {
		r.hooks = &ModelHooks{}
	}
	r.hooks.PostGenerate = append(r.hooks.PostGenerate, hook)
}

// GetProvider returns a registered provider by name
func GetProvider(name string) (ModelProvider, error) {
	return globalRegistry.GetProvider(name)
}

// GetProvider returns a registered provider by name
func (r *Registry) GetProvider(name string) (ModelProvider, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", name)
	}
	return provider, nil
}

// ListProviders returns all registered provider names
func ListProviders() []string {
	return globalRegistry.ListProviders()
}

// ListProviders returns all registered provider names
func (r *Registry) ListProviders() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetModel is a convenience function to get a model from a provider
func GetModel(ctx context.Context, providerName, modelID string) (Model, error) {
	return globalRegistry.GetModel(ctx, providerName, modelID)
}

// GetModel gets a model and wraps it with hooks if any are registered
func (r *Registry) GetModel(ctx context.Context, providerName, modelID string) (Model, error) {
	provider, err := r.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	model, err := provider.GetModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	// Check if there are global hooks
	r.mutex.RLock()
	hooks := r.hooks
	r.mutex.RUnlock()

	// If no hooks, return the original model
	if hooks == nil || (len(hooks.PreGenerate) == 0 && len(hooks.PostGenerate) == 0) {
		return model, nil
	}

	// Wrap with hooks
	return &HookedModel{
		model:      model,
		hooks:      hooks,
		providerID: providerName,
	}, nil
}

// =============================================================================
// TOOL USE FUNCTIONALITY
// =============================================================================

type ToolResult struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Parts    []a2a.Part     `json:"content"`            // Result content
	Metadata map[string]any `json:"metadata,omitempty"` // Additional metadata
}

type ToolUse struct {
	ID        string                 `json:"id"`
	ToolName  string                 `json:"tool_name"`
	Arguments map[string]interface{} `json:"arguments"`
	Metadata  map[string]any         `json:"metadata,omitempty"` // Additional metadata
}

// HasToolUseParts checks if a message contains any tool use parts
func HasToolUseParts(msg a2a.Message) bool {
	for _, part := range msg.Parts {
		if _, ok := AsToolUsePart(part); ok {
			return true
		}
	}
	return false
}

// GetToolUseParts extracts all tool use parts from a message
func GetToolUseParts(msg a2a.Message) []*ToolUse {
	var toolUses []*ToolUse
	for _, part := range msg.Parts {
		if toolUse, ok := AsToolUsePart(part); ok {
			toolUses = append(toolUses, toolUse)
		}
	}
	return toolUses
}

// HasToolResultParts checks if a message contains any tool result parts
func HasToolResultParts(msg a2a.Message) bool {
	for _, part := range msg.Parts {
		if _, ok := AsToolResultPart(part); ok {
			return true
		}
	}
	return false
}

// GetToolResultParts extracts all tool result parts from a message
func GetToolResultParts(msg a2a.Message) []*ToolResult {
	var toolResults []*ToolResult
	for _, part := range msg.Parts {
		if toolResult, ok := AsToolResultPart(part); ok {
			toolResults = append(toolResults, toolResult)
		}
	}
	return toolResults
}

// NewToolUsePart creates a DataPart representing a tool use
func NewToolUsePart(toolID, toolName string, arguments map[string]interface{}) a2a.Part {
	part := a2a.NewDataPart(map[string]interface{}{
		"tool_id":   toolID,
		"tool_name": toolName,
		"arguments": arguments,
	})
	part.Metadata = map[string]interface{}{
		"intent": "tool_use",
	}
	return part
}

// NewToolResultPart creates a DataPart representing a tool result
func NewToolResultPart(toolID, toolName string, content []a2a.Part) a2a.Part {
	part := a2a.NewDataPart(map[string]interface{}{
		"tool_id":   toolID,
		"tool_name": toolName,
		"content":   content,
	})
	part.Metadata = map[string]interface{}{
		"intent": "tool_result",
	}
	return part
}

// AsToolUsePart checks if a Part is a tool use and extracts the tool use information
func AsToolUsePart(part a2a.Part) (*ToolUse, bool) {
	if part.Kind != a2a.KindDataPart {
		return nil, false
	}

	// Check intent in metadata
	if part.Metadata == nil {
		return nil, false
	}

	intent, ok := part.Metadata["intent"].(string)
	if !ok || intent != "tool_use" {
		return nil, false
	}

	// Extract tool information from data
	toolID, ok := part.Data["tool_id"].(string)
	if !ok {
		toolID = ""
	}

	toolName, ok := part.Data["tool_name"].(string)
	if !ok || toolName == "" {
		return nil, false
	}

	arguments, ok := part.Data["arguments"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	return &ToolUse{
		ID:        toolID,
		ToolName:  toolName,
		Arguments: arguments,
		Metadata:  part.Metadata,
	}, true
}

// AsToolResultPart checks if a Part is a tool result and extracts the tool result information
func AsToolResultPart(part a2a.Part) (*ToolResult, bool) {
	if part.Kind != a2a.KindDataPart {
		return nil, false
	}

	// Check intent in metadata
	if part.Metadata == nil {
		return nil, false
	}

	intent, ok := part.Metadata["intent"].(string)
	if !ok || intent != "tool_result" {
		return nil, false
	}

	// Extract tool information from data
	toolID, ok := part.Data["tool_id"].(string)
	if !ok || toolID == "" {
		return nil, false
	}

	toolName, ok := part.Data["tool_name"].(string)
	if !ok || toolName == "" {
		return nil, false
	}

	// Extract content (should be []a2a.Part)
	content, ok := part.Data["content"].([]a2a.Part)
	if !ok {
		return nil, false
	}

	return &ToolResult{
		ID:       toolID,
		ToolName: toolName,
		Parts:    content,
		Metadata: part.Metadata,
	}, true
}

// =============================================================================
// REASONING FUNCTIONALITY
// =============================================================================

func MarkReasoning(parts ...a2a.Part) []a2a.Part {
	for i, part := range parts {
		// Set intent for reasoning parts
		if part.Metadata == nil {
			part.Metadata = make(map[string]interface{})
		}
		part.Metadata["intent"] = "reasoning"
		parts[i] = part
	}
	return parts
}

func IsReasoning(part a2a.Part) bool {
	// Check intent in metadata
	if part.Metadata == nil {
		return false
	}

	intent, ok := part.Metadata["intent"].(string)
	return ok && intent == "reasoning"
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// Ptr returns a pointer to the value v.
// This is a generic helper function for creating pointers to values.
func Ptr[T any](v T) *T {
	return &v
}
