package ollama

import (
	"context"
	"encoding/json"
	"flag"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
)

var shot = flag.Bool("shot", false, "run tests against real Ollama instance")

func TestModelProvider_GetModel(t *testing.T) {
	provider := &ModelProvider{
		Endpoint: DefaultEndpoint,
	}

	ctx := context.Background()

	// Test successful model creation
	model, err := provider.GetModel(ctx, "llama3.2")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if model.ID() != "llama3.2" {
		t.Errorf("Expected model ID 'llama3.2', got %s", model.ID())
	}

	// Test empty model ID
	_, err = provider.GetModel(ctx, "")
	if err == nil {
		t.Error("Expected error for empty model ID")
	}
}

func TestModel_ConvertMessage_Regular(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	// Test user message
	userMsg := a2a.NewMessage("test-user-msg", a2a.RoleUser, []a2a.Part{
		a2a.NewTextPart("Hello, how are you?"),
	})

	ollamaMsg, err := m.convertMessage(userMsg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if ollamaMsg.Role != "user" {
		t.Errorf("Expected role 'user', got %s", ollamaMsg.Role)
	}

	if ollamaMsg.Content != "Hello, how are you?" {
		t.Errorf("Expected content 'Hello, how are you?', got %s", ollamaMsg.Content)
	}

	// Test agent message
	agentMsg := a2a.NewMessage("test-agent-msg", a2a.RoleAgent, []a2a.Part{
		a2a.NewTextPart("I'm doing well, thank you!"),
	})

	ollamaMsg, err = m.convertMessage(agentMsg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if ollamaMsg.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got %s", ollamaMsg.Role)
	}

	if ollamaMsg.Content != "I'm doing well, thank you!" {
		t.Errorf("Expected content 'I'm doing well, thank you!', got %s", ollamaMsg.Content)
	}
}

func TestModel_ConvertMessage_ToolUse(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	// Create an assistant message with tool use parts
	toolUsePart := model.NewToolUsePart("call_123", "get_weather", map[string]any{
		"city": "Tokyo",
	})
	
	agentMsg := a2a.NewMessage("test-agent-msg", a2a.RoleAgent, []a2a.Part{
		a2a.NewTextPart("I'll check the weather for you."),
		toolUsePart,
	})

	ollamaMsg, err := m.convertMessage(agentMsg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if ollamaMsg.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got %s", ollamaMsg.Role)
	}

	if ollamaMsg.Content != "I'll check the weather for you." {
		t.Errorf("Expected content 'I'll check the weather for you.', got %s", ollamaMsg.Content)
	}

	if len(ollamaMsg.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(ollamaMsg.ToolCalls))
	}

	toolCall := ollamaMsg.ToolCalls[0]
	if toolCall.ID != "call_123" {
		t.Errorf("Expected tool call ID 'call_123', got %s", toolCall.ID)
	}

	if toolCall.Type != "function" {
		t.Errorf("Expected tool call type 'function', got %s", toolCall.Type)
	}

	if toolCall.Function.Name != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got %s", toolCall.Function.Name)
	}

	// Parse and verify arguments
	var args map[string]any
	if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
		t.Fatalf("Failed to parse tool arguments: %v", err)
	}

	if city, ok := args["city"].(string); !ok || city != "Tokyo" {
		t.Errorf("Expected city argument 'Tokyo', got %v", args["city"])
	}
}

func TestModel_ConvertMessage_ToolResult(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	// Create tool result message using Part-based approach
	toolResultPart := model.NewToolResultPart("call_123", "get_weather", []a2a.Part{
		a2a.NewTextPart(`{"weather": "sunny", "temperature": 25}`),
	})

	toolResultMsg := a2a.NewMessage("test-tool-result-msg", a2a.RoleUser, []a2a.Part{toolResultPart})

	ollamaMsg, err := m.convertMessage(toolResultMsg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if ollamaMsg.Role != "tool" {
		t.Errorf("Expected role 'tool', got %s", ollamaMsg.Role)
	}

	if ollamaMsg.ToolCallID != "call_123" {
		t.Errorf("Expected tool call ID 'call_123', got %s", ollamaMsg.ToolCallID)
	}

	if ollamaMsg.Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", ollamaMsg.Name)
	}

	expectedContent := `{"weather": "sunny", "temperature": 25}`
	if ollamaMsg.Content != expectedContent {
		t.Errorf("Expected content %s, got %s", expectedContent, ollamaMsg.Content)
	}
}

func TestModel_ConvertResponse_TextOnly(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	ollamaResp := ChatResponse{
		Message: Message{
			Role:    "assistant",
			Content: "Hello! How can I help you today?",
		},
	}

	resp, err := m.convertResponse(ollamaResp)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.StopReason != model.StopReasonEndTurn {
		t.Errorf("Expected stop reason %s, got %s", model.StopReasonEndTurn, resp.StopReason)
	}

	if len(resp.Message.Parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(resp.Message.Parts))
	}

	if resp.Message.Parts[0].Kind != a2a.KindTextPart {
		t.Error("Expected text part")
	}

	if resp.Message.Parts[0].Text != "Hello! How can I help you today?" {
		t.Errorf("Expected 'Hello! How can I help you today?', got %s", resp.Message.Parts[0].Text)
	}
}

func TestModel_ConvertResponse_WithToolCalls(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	// Use actual Ollama response format with JSON string arguments
	ollamaResp := ChatResponse{
		Message: Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: ToolFunction{
						Name:      "get_weather",
						Arguments: json.RawMessage(`{"city":"Tokyo"}`), // json.RawMessage format
					},
				},
			},
		},
	}

	resp, err := m.convertResponse(ollamaResp)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check that response contains tool use parts
	if !model.HasToolUseParts(resp.Message) {
		t.Error("Expected message to contain tool use parts")
	}

	if resp.StopReason != model.StopReasonToolUse {
		t.Errorf("Expected stop reason %s, got %s", model.StopReasonToolUse, resp.StopReason)
	}

	toolUses := model.GetToolUseParts(resp.Message)
	if len(toolUses) != 1 {
		t.Errorf("Expected 1 tool use, got %d", len(toolUses))
	}

	toolUse := toolUses[0]
	if toolUse.ID != "call_123" {
		t.Errorf("Expected tool use ID 'call_123', got %s", toolUse.ID)
	}

	if toolUse.ToolName != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", toolUse.ToolName)
	}

	if city, ok := toolUse.Arguments["city"].(string); !ok || city != "Tokyo" {
		t.Errorf("Expected city argument 'Tokyo', got %v", toolUse.Arguments["city"])
	}
}

func TestModel_ConvertResponse_WithContentToolCall(t *testing.T) {
	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	// Test case where tool call is in content field as JSON string
	toolCallContent := `{"name":"stop","arguments":{"message":"Task completed","state":"completed"}}`
	
	ollamaResp := ChatResponse{
		Message: Message{
			Role:    "assistant",
			Content: toolCallContent,
		},
	}

	resp, err := m.convertResponse(ollamaResp)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check that response contains tool use parts
	if !model.HasToolUseParts(resp.Message) {
		t.Error("Expected message to contain tool use parts")
	}

	if resp.StopReason != model.StopReasonToolUse {
		t.Errorf("Expected stop reason %s, got %s", model.StopReasonToolUse, resp.StopReason)
	}

	toolUses := model.GetToolUseParts(resp.Message)
	if len(toolUses) != 1 {
		t.Errorf("Expected 1 tool use, got %d", len(toolUses))
	}

	toolUse := toolUses[0]
	if toolUse.ToolName != "stop" {
		t.Errorf("Expected tool name 'stop', got %s", toolUse.ToolName)
	}

	if message, ok := toolUse.Arguments["message"].(string); !ok || message != "Task completed" {
		t.Errorf("Expected message argument 'Task completed', got %v", toolUse.Arguments["message"])
	}

	if state, ok := toolUse.Arguments["state"].(string); !ok || state != "completed" {
		t.Errorf("Expected state argument 'completed', got %v", toolUse.Arguments["state"])
	}
}

func TestModel_InputOutputModes(t *testing.T) {
	m := &Model{modelID: "test"}

	inputModes := m.InputModes()
	expectedInputModes := []string{"text/plain"}
	if len(inputModes) != len(expectedInputModes) {
		t.Errorf("Expected %d input modes, got %d", len(expectedInputModes), len(inputModes))
	}

	for i, mode := range expectedInputModes {
		if inputModes[i] != mode {
			t.Errorf("Expected input mode %s, got %s", mode, inputModes[i])
		}
	}

	outputModes := m.OutputModes()
	expectedOutputModes := []string{"text/plain"}
	if len(outputModes) != len(expectedOutputModes) {
		t.Errorf("Expected %d output modes, got %d", len(expectedOutputModes), len(outputModes))
	}

	for i, mode := range expectedOutputModes {
		if outputModes[i] != mode {
			t.Errorf("Expected output mode %s, got %s", mode, outputModes[i])
		}
	}
}

// TestModel_Generate_Integration tests against real Ollama instance
func TestModel_Generate_Integration(t *testing.T) {
	if !*shot {
		t.Skip("Skipping integration test. Use --shot flag to run against real Ollama instance")
	}

	m := &Model{
		modelID:  "llama3.2",
		endpoint: DefaultEndpoint,
	}

	req := &model.GenerateRequest{
		System: "You are a helpful assistant. Be concise.",
		Messages: []a2a.Message{
			a2a.NewMessage("test-integration-msg", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("Say hello in one word"),
			}),
		},
	}

	ctx := context.Background()
	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(resp.Message.Parts) == 0 {
		t.Error("Expected at least one part in response")
	}

	t.Logf("Response: %+v", resp)
}

// TestModel_Generate_WithTools_Integration tests tool calling against real Ollama
func TestModel_Generate_WithTools_Integration(t *testing.T) {
	if !*shot {
		t.Skip("Skipping integration test. Use --shot flag to run against real Ollama instance")
	}

	m := &Model{
		modelID:  "llama3.2", // Make sure this model supports tool calling
		endpoint: DefaultEndpoint,
	}

	toolSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {
				"type": "string",
				"description": "The city name"
			}
		},
		"required": ["city"]
	}`)

	req := &model.GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-tool-msg", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("What's the weather in Tokyo?"),
			}),
		},
		Tools: []model.Tool{
			{
				Name:        "get_weather",
				Description: "Get current weather information for a city",
				Schema:      toolSchema,
			},
		},
	}

	ctx := context.Background()
	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	t.Logf("Tool call response: %+v", resp)

	if model.HasToolUseParts(resp.Message) {
		toolUses := model.GetToolUseParts(resp.Message)
		t.Logf("Tool uses detected: %+v", toolUses)
	}
}
