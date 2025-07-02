package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/mashiike/atlasic/a2a"
)

func TestToolResultPart_DirectUsage(t *testing.T) {
	// Test direct usage of ToolResultPart without message wrapper
	originalContent := []a2a.Part{
		a2a.NewTextPart("Result content"),
		a2a.NewTextPart(" additional text"),
	}

	// Create tool result part directly
	toolResultPart := NewToolResultPart("test-tool-call-123", "test_function", originalContent)

	// Create message with tool result part
	msg := a2a.NewMessage("test-msg-1", a2a.RoleUser, []a2a.Part{toolResultPart})

	// Check that message contains tool result parts
	if !HasToolResultParts(msg) {
		t.Error("Expected message to contain tool result parts")
	}

	// Extract tool results using Part-based approach
	toolResults := GetToolResultParts(msg)
	if len(toolResults) != 1 {
		t.Errorf("Expected 1 tool result, got %d", len(toolResults))
	}

	extractedResult := toolResults[0]

	// Check that the extracted result matches the original
	if extractedResult.ID != "test-tool-call-123" {
		t.Errorf("ID mismatch: expected %s, got %s", "test-tool-call-123", extractedResult.ID)
	}

	if extractedResult.ToolName != "test_function" {
		t.Errorf("ToolName mismatch: expected %s, got %s", "test_function", extractedResult.ToolName)
	}

	// Check Parts (content should be preserved)
	if len(extractedResult.Parts) != len(originalContent) {
		t.Errorf("Parts length mismatch: expected %d, got %d", len(originalContent), len(extractedResult.Parts))
	}

	for i, part := range extractedResult.Parts {
		if i >= len(originalContent) {
			break
		}
		expectedPart := originalContent[i]
		if !reflect.DeepEqual(part, expectedPart) {
			t.Errorf("Part[%d] mismatch: expected %v, got %v", i, expectedPart, part)
		}
	}
}

func TestNewToolUsePart_AsToolUsePart_Symmetry(t *testing.T) {
	// Test data
	originalToolID := "test-tool-use-456"
	originalToolName := "calculate_sum"
	originalArguments := map[string]interface{}{
		"numbers": []interface{}{1, 2, 3, 4, 5},
		"decimal": 3.14,
	}

	// Create tool use part
	part := NewToolUsePart(originalToolID, originalToolName, originalArguments)

	// Convert back to ToolUse
	extractedToolUse, ok := AsToolUsePart(part)
	if !ok {
		t.Fatal("AsToolUsePart should return true for a valid tool use part")
	}

	// Check symmetry
	if extractedToolUse.ID != originalToolID {
		t.Errorf("ID mismatch: expected %s, got %s", originalToolID, extractedToolUse.ID)
	}

	if extractedToolUse.ToolName != originalToolName {
		t.Errorf("ToolName mismatch: expected %s, got %s", originalToolName, extractedToolUse.ToolName)
	}

	// Check Arguments (should be preserved)
	if !reflect.DeepEqual(extractedToolUse.Arguments, originalArguments) {
		t.Errorf("Arguments mismatch: expected %v, got %v", originalArguments, extractedToolUse.Arguments)
	}

	// Check that metadata contains our tool use metadata
	if intent, ok := extractedToolUse.Metadata["intent"].(string); !ok || intent != "tool_use" {
		t.Errorf("Metadata intent missing or incorrect: %v", extractedToolUse.Metadata["intent"])
	}
}

func TestHasToolUseParts_and_GetToolUseParts(t *testing.T) {
	// Create a message with mixed parts including tool use
	toolUsePart := NewToolUsePart("use_123", "get_weather", map[string]interface{}{
		"city": "Tokyo",
		"unit": "celsius",
	})

	msg := a2a.NewMessage("test-msg-tool-use", a2a.RoleAgent, []a2a.Part{
		a2a.NewTextPart("I'll check the weather for you."),
		toolUsePart,
		a2a.NewTextPart("Let me get that information."),
	})

	// Test HasToolUseParts
	if !HasToolUseParts(msg) {
		t.Error("Expected HasToolUseParts to return true")
	}

	// Test GetToolUseParts
	toolUses := GetToolUseParts(msg)
	if len(toolUses) != 1 {
		t.Errorf("Expected 1 tool use, got %d", len(toolUses))
	}

	toolUse := toolUses[0]
	if toolUse.ID != "use_123" {
		t.Errorf("Expected tool use ID 'use_123', got %s", toolUse.ID)
	}

	if toolUse.ToolName != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", toolUse.ToolName)
	}

	// Test message without tool use parts
	msgWithoutTools := a2a.NewMessage("test-msg-no-tools", a2a.RoleAgent, []a2a.Part{
		a2a.NewTextPart("Just a regular message"),
	})

	if HasToolUseParts(msgWithoutTools) {
		t.Error("Expected HasToolUseParts to return false for message without tools")
	}

	if len(GetToolUseParts(msgWithoutTools)) != 0 {
		t.Error("Expected GetToolUseParts to return empty slice for message without tools")
	}
}

func TestNewToolResultPart_AsToolResultPart_Symmetry(t *testing.T) {
	// Test data
	originalToolID := "result-456"
	originalToolName := "get_weather"
	originalContent := []a2a.Part{
		a2a.NewTextPart("The weather in Tokyo is sunny, 22°C"),
		a2a.NewTextPart("Humidity: 65%"),
	}

	// Create tool result part
	part := NewToolResultPart(originalToolID, originalToolName, originalContent)

	// Convert back to ToolResult
	extractedToolResult, ok := AsToolResultPart(part)
	if !ok {
		t.Fatal("AsToolResultPart should return true for a valid tool result part")
	}

	// Check symmetry
	if extractedToolResult.ID != originalToolID {
		t.Errorf("ID mismatch: expected %s, got %s", originalToolID, extractedToolResult.ID)
	}

	if extractedToolResult.ToolName != originalToolName {
		t.Errorf("ToolName mismatch: expected %s, got %s", originalToolName, extractedToolResult.ToolName)
	}

	// Check content (should be preserved)
	if len(extractedToolResult.Parts) != len(originalContent) {
		t.Errorf("Content length mismatch: expected %d, got %d", len(originalContent), len(extractedToolResult.Parts))
	}

	for i, part := range extractedToolResult.Parts {
		if i >= len(originalContent) {
			break
		}
		if !reflect.DeepEqual(part, originalContent[i]) {
			t.Errorf("Content[%d] mismatch: expected %v, got %v", i, originalContent[i], part)
		}
	}

	// Check that metadata contains our tool result metadata
	if intent, ok := extractedToolResult.Metadata["intent"].(string); !ok || intent != "tool_result" {
		t.Errorf("Metadata intent missing or incorrect: %v", extractedToolResult.Metadata["intent"])
	}
}

func TestHasToolResultParts_and_GetToolResultParts(t *testing.T) {
	// Create a message with mixed parts including tool results
	toolResultPart1 := NewToolResultPart("result_1", "get_weather", []a2a.Part{
		a2a.NewTextPart("Tokyo: 22°C, sunny"),
	})

	toolResultPart2 := NewToolResultPart("result_2", "get_time", []a2a.Part{
		a2a.NewTextPart("Current time: 15:30"),
	})

	msg := a2a.NewMessage("test-msg-tool-results", a2a.RoleUser, []a2a.Part{
		a2a.NewTextPart("Here are the results:"),
		toolResultPart1,
		toolResultPart2,
		a2a.NewTextPart("That's all!"),
	})

	// Test HasToolResultParts
	if !HasToolResultParts(msg) {
		t.Error("Expected HasToolResultParts to return true")
	}

	// Test GetToolResultParts
	toolResults := GetToolResultParts(msg)
	if len(toolResults) != 2 {
		t.Errorf("Expected 2 tool results, got %d", len(toolResults))
	}

	// Check first tool result
	if toolResults[0].ID != "result_1" {
		t.Errorf("Expected first tool result ID 'result_1', got %s", toolResults[0].ID)
	}

	if toolResults[0].ToolName != "get_weather" {
		t.Errorf("Expected first tool name 'get_weather', got %s", toolResults[0].ToolName)
	}

	// Check second tool result
	if toolResults[1].ID != "result_2" {
		t.Errorf("Expected second tool result ID 'result_2', got %s", toolResults[1].ID)
	}

	if toolResults[1].ToolName != "get_time" {
		t.Errorf("Expected second tool name 'get_time', got %s", toolResults[1].ToolName)
	}

	// Test message without tool result parts
	msgWithoutResults := a2a.NewMessage("test-msg-no-results", a2a.RoleUser, []a2a.Part{
		a2a.NewTextPart("Just a regular message"),
	})

	if HasToolResultParts(msgWithoutResults) {
		t.Error("Expected HasToolResultParts to return false for message without tool results")
	}

	if len(GetToolResultParts(msgWithoutResults)) != 0 {
		t.Error("Expected GetToolResultParts to return empty slice for message without tool results")
	}
}

// MockModel for testing
type MockModel struct {
	id     string
	result *GenerateResponse
	err    error
}

func (m *MockModel) ID() string {
	return m.id
}

func (m *MockModel) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	return m.result, m.err
}

func (m *MockModel) InputModes() []string {
	return []string{"text"}
}

func (m *MockModel) OutputModes() []string {
	return []string{"text"}
}

// MockModelProvider for testing
type MockModelProvider struct {
	models map[string]Model
}

func (p *MockModelProvider) GetModel(ctx context.Context, modelID string) (Model, error) {
	model, exists := p.models[modelID]
	if !exists {
		return nil, fmt.Errorf("model %s not found", modelID)
	}
	return model, nil
}

func TestModelHooks_PreGenerateHook(t *testing.T) {
	// Create a test registry
	registry := &Registry{
		providers: make(map[string]ModelProvider),
		hooks:     &ModelHooks{},
	}

	// Create mock provider and model
	mockProvider := &MockModelProvider{
		models: map[string]Model{
			"test-model": &MockModel{
				id: "test-model",
				result: &GenerateResponse{
					Message:    a2a.NewMessage("test-response-1", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Test response")}),
					StopReason: StopReasonEndTurn,
				},
			},
		},
	}

	// Register provider
	registry.Register("test-provider", mockProvider)

	// Track hook execution
	var preHookCalled bool
	var postHookCalled bool

	// Add hooks
	registry.AddPreGenerateHook(func(ctx context.Context, req *GenerateRequest) error {
		preHookCalled = true
		return nil
	})

	registry.AddPostGenerateHook(func(ctx context.Context, req *GenerateRequest, resp *GenerateResponse, err error) error {
		postHookCalled = true
		return nil
	})

	// Get model (should be wrapped with hooks)
	model, err := registry.GetModel(context.Background(), "test-provider", "test-model")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Call Generate
	req := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	_, err = model.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}

	// Verify hooks were called
	if !preHookCalled {
		t.Error("Pre-generate hook was not called")
	}

	if !postHookCalled {
		t.Error("Post-generate hook was not called")
	}
}

func TestModelHooks_PreGenerateHookError(t *testing.T) {
	// Create a test registry
	registry := &Registry{
		providers: make(map[string]ModelProvider),
		hooks:     &ModelHooks{},
	}

	// Create mock provider and model
	mockProvider := &MockModelProvider{
		models: map[string]Model{
			"test-model": &MockModel{id: "test-model"},
		},
	}

	// Register provider
	registry.Register("test-provider", mockProvider)

	// Add failing pre-hook
	registry.AddPreGenerateHook(func(ctx context.Context, req *GenerateRequest) error {
		return errors.New("pre-hook error")
	})

	// Get model
	model, err := registry.GetModel(context.Background(), "test-provider", "test-model")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Call Generate (should fail due to pre-hook error)
	req := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-req-2", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	_, err = model.Generate(context.Background(), req)
	if err == nil {
		t.Error("Expected error from pre-hook, but got nil")
	}

	if err.Error() != "pre-generate hook failed: pre-hook error" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestModelHooks_WithoutHooks(t *testing.T) {
	// Create a test registry
	registry := &Registry{
		providers: make(map[string]ModelProvider),
		hooks:     &ModelHooks{},
	}

	// Create mock provider and model
	mockModel := &MockModel{id: "test-model"}
	mockProvider := &MockModelProvider{
		models: map[string]Model{"test-model": mockModel},
	}

	// Register provider without hooks
	registry.Register("test-provider", mockProvider)

	// Get model (should return original model, not wrapped)
	model, err := registry.GetModel(context.Background(), "test-provider", "test-model")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Should return the original model, not a HookedModel
	if _, isHooked := model.(*HookedModel); isHooked {
		t.Error("Expected original model, but got HookedModel when no hooks are registered")
	}

	// Should be the same instance
	if model != mockModel {
		t.Error("Expected same model instance when no hooks are registered")
	}
}

func TestGlobalRegistryHooks(t *testing.T) {
	// Test global registry functions
	testProvider := &MockModelProvider{
		models: map[string]Model{
			"global-test": &MockModel{id: "global-test"},
		},
	}

	// Register with global registry
	Register("global-provider", testProvider)

	// Track hook calls
	var preHookCalled bool
	var postHookCalled bool

	// Add hooks via global functions
	AddPreGenerateHook(func(ctx context.Context, req *GenerateRequest) error {
		preHookCalled = true
		return nil
	})

	AddPostGenerateHook(func(ctx context.Context, req *GenerateRequest, resp *GenerateResponse, err error) error {
		postHookCalled = true
		return nil
	})

	// Get model via global function
	model, err := GetModel(context.Background(), "global-provider", "global-test")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Should be wrapped with hooks
	if _, isHooked := model.(*HookedModel); !isHooked {
		t.Error("Expected HookedModel when hooks are registered")
	}

	// Call Generate
	_, err = model.Generate(context.Background(), &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-req-3", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	})
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}

	// Verify hooks were called
	if !preHookCalled {
		t.Error("Global pre-generate hook was not called")
	}

	if !postHookCalled {
		t.Error("Global post-generate hook was not called")
	}
}
