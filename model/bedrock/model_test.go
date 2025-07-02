package bedrock

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
)

// MockBedrockClient implements BedrockClient for testing
type MockBedrockClient struct {
	ConverseFunc func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

func (m *MockBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if m.ConverseFunc != nil {
		return m.ConverseFunc(ctx, params, optFns...)
	}

	// Default mock response
	return &bedrockruntime.ConverseOutput{
		Output: &types.ConverseOutputMemberMessage{
			Value: types.Message{
				Role: types.ConversationRoleAssistant,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{
						Value: "Mock response",
					},
				},
			},
		},
		Usage: &types.TokenUsage{
			InputTokens:  aws.Int32(10),
			OutputTokens: aws.Int32(5),
			TotalTokens:  aws.Int32(15),
		},
	}, nil
}

func TestModelProvider_GetModel(t *testing.T) {
	provider := &ModelProvider{}
	mockClient := &MockBedrockClient{}
	provider.SetClient(mockClient)

	ctx := context.Background()

	// Test successful model creation
	model, err := provider.GetModel(ctx, "claude-3-sonnet")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if model.ID() != "claude-3-sonnet" {
		t.Errorf("Expected model ID 'claude-3-sonnet', got %s", model.ID())
	}

	// Test empty model ID
	_, err = provider.GetModel(ctx, "")
	if err == nil {
		t.Error("Expected error for empty model ID")
	}
}

func TestModel_Generate_TextOnly(t *testing.T) {
	mockClient := &MockBedrockClient{
		ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			// Verify request parameters
			if aws.ToString(params.ModelId) != "claude-3-sonnet" {
				t.Errorf("Expected model ID 'claude-3-sonnet', got %s", aws.ToString(params.ModelId))
			}

			if len(params.Messages) != 1 {
				t.Errorf("Expected 1 message, got %d", len(params.Messages))
			}

			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role: types.ConversationRoleAssistant,
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{
								Value: "Hello! How can I help you?",
							},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(8),
					TotalTokens:  aws.Int32(18),
				},
			}, nil
		},
	}

	m := &Model{
		modelID: "claude-3-sonnet",
		client:  mockClient,
	}

	req := &model.GenerateRequest{
		System: "You are a helpful assistant.",
		Messages: []a2a.Message{
			a2a.NewMessage("test-bedrock-msg-1", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("Hello"),
			}),
		},
		Options: &model.GenerationOptions{
			Temperature: aws.Float32(0.7),
			MaxTokens:   aws.Int(100),
		},
	}

	ctx := context.Background()
	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(resp.Message.Parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(resp.Message.Parts))
	}

	if resp.Message.Parts[0].Kind != a2a.KindTextPart {
		t.Error("Expected text part")
	}

	if resp.Message.Parts[0].Text != "Hello! How can I help you?" {
		t.Errorf("Expected 'Hello! How can I help you?', got %s", resp.Message.Parts[0].Text)
	}

	if resp.Usage.TotalTokens != 18 {
		t.Errorf("Expected 18 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestModel_Generate_WithToolCalls(t *testing.T) {
	mockClient := &MockBedrockClient{
		ConverseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			// Verify tools are passed correctly
			if params.ToolConfig == nil {
				t.Error("Expected tool config to be set")
			}

			if len(params.ToolConfig.Tools) != 1 {
				t.Errorf("Expected 1 tool, got %d", len(params.ToolConfig.Tools))
			}

			// Extract tool from union type
			toolMember := params.ToolConfig.Tools[0]
			if toolSpec, ok := toolMember.(*types.ToolMemberToolSpec); ok {
				if aws.ToString(toolSpec.Value.Name) != "get_weather" {
					t.Errorf("Expected tool name 'get_weather', got %s", aws.ToString(toolSpec.Value.Name))
				}
			} else {
				t.Error("Expected ToolMemberToolSpec")
			}

			// Mock tool use response
			toolInput := map[string]interface{}{
				"city": "Tokyo",
			}

			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role: types.ConversationRoleAssistant,
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{
								Value: "I'll check the weather for you.",
							},
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("tool_call_123"),
									Name:      aws.String("get_weather"),
									Input:     document.NewLazyDocument(toolInput),
								},
							},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(25),
					OutputTokens: aws.Int32(15),
					TotalTokens:  aws.Int32(40),
				},
			}, nil
		},
	}

	m := &Model{
		modelID: "claude-3-sonnet",
		client:  mockClient,
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
			a2a.NewMessage("test-bedrock-msg-2", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("What's the weather in Tokyo?"),
			}),
		},
		Tools: []model.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather information for a city",
				Schema:      toolSchema,
			},
		},
		ToolChoice: &model.ToolChoice{
			Type: model.ToolChoiceRequired,
		},
	}

	ctx := context.Background()
	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check that response contains tool use parts
	if !model.HasToolUseParts(resp.Message) {
		t.Error("Expected message to contain tool use parts")
	}

	toolUses := model.GetToolUseParts(resp.Message)
	if len(toolUses) != 1 {
		t.Errorf("Expected 1 tool use, got %d", len(toolUses))
	}

	toolUse := toolUses[0]
	if toolUse.ID != "tool_call_123" {
		t.Errorf("Expected tool use ID 'tool_call_123', got %s", toolUse.ID)
	}

	if toolUse.ToolName != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", toolUse.ToolName)
	}

	if city, ok := toolUse.Arguments["city"].(string); !ok || city != "Tokyo" {
		t.Errorf("Expected city argument 'Tokyo', got %v", toolUse.Arguments["city"])
	}

	// Check that stop reason is correctly set
	if resp.StopReason != model.StopReasonToolUse {
		t.Errorf("Expected stop reason %s, got %s", model.StopReasonToolUse, resp.StopReason)
	}
}

func TestModel_ConvertMessage_WithToolUsePart(t *testing.T) {
	m := &Model{modelID: "test"}

	// Create a message with tool use part
	toolUsePart := model.NewToolUsePart("call_123", "get_weather", map[string]interface{}{
		"city": "Tokyo",
		"unit": "celsius",
	})

	msg := a2a.NewMessage("test-bedrock-tooluse", a2a.RoleAgent, []a2a.Part{
		a2a.NewTextPart("I'll check the weather for you."),
		toolUsePart,
	})

	bedrockMsg, err := m.convertMessage(msg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if bedrockMsg.Role != types.ConversationRoleAssistant {
		t.Errorf("Expected role assistant, got %v", bedrockMsg.Role)
	}

	if len(bedrockMsg.Content) != 2 {
		t.Errorf("Expected 2 content blocks, got %d", len(bedrockMsg.Content))
	}

	// Check text content
	textBlock, ok := bedrockMsg.Content[0].(*types.ContentBlockMemberText)
	if !ok {
		t.Error("Expected first content to be text block")
	} else if textBlock.Value != "I'll check the weather for you." {
		t.Errorf("Expected text 'I'll check the weather for you.', got %s", textBlock.Value)
	}

	// Check tool use content
	toolUseBlock, ok := bedrockMsg.Content[1].(*types.ContentBlockMemberToolUse)
	if !ok {
		t.Error("Expected second content to be tool use block")
	} else {
		if aws.ToString(toolUseBlock.Value.ToolUseId) != "call_123" {
			t.Errorf("Expected tool use ID 'call_123', got %s", aws.ToString(toolUseBlock.Value.ToolUseId))
		}
		if aws.ToString(toolUseBlock.Value.Name) != "get_weather" {
			t.Errorf("Expected tool name 'get_weather', got %s", aws.ToString(toolUseBlock.Value.Name))
		}
	}
}

func TestModel_ConvertMessage_WithToolResultPart(t *testing.T) {
	m := &Model{modelID: "test"}

	// Create a message with tool result part
	toolResultPart := model.NewToolResultPart("call_123", "get_weather", []a2a.Part{
		a2a.NewTextPart("The weather in Tokyo is 22°C and sunny."),
		a2a.NewTextPart("Humidity is 65%."),
	})

	msg := a2a.NewMessage("test-bedrock-toolresult", a2a.RoleUser, []a2a.Part{
		a2a.NewTextPart("Here is the weather information:"),
		toolResultPart,
	})

	bedrockMsg, err := m.convertMessage(msg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if bedrockMsg.Role != types.ConversationRoleUser {
		t.Errorf("Expected role user, got %v", bedrockMsg.Role)
	}

	if len(bedrockMsg.Content) != 2 {
		t.Errorf("Expected 2 content blocks, got %d", len(bedrockMsg.Content))
	}

	// Check text content
	textBlock, ok := bedrockMsg.Content[0].(*types.ContentBlockMemberText)
	if !ok {
		t.Error("Expected first content to be text block")
	} else if textBlock.Value != "Here is the weather information:" {
		t.Errorf("Expected text 'Here is the weather information:', got %s", textBlock.Value)
	}

	// Check tool result content
	toolResultBlock, ok := bedrockMsg.Content[1].(*types.ContentBlockMemberToolResult)
	if !ok {
		t.Error("Expected second content to be tool result block")
	} else {
		if aws.ToString(toolResultBlock.Value.ToolUseId) != "call_123" {
			t.Errorf("Expected tool use ID 'call_123', got %s", aws.ToString(toolResultBlock.Value.ToolUseId))
		}

		// Check content blocks inside tool result
		if len(toolResultBlock.Value.Content) != 2 {
			t.Errorf("Expected 2 content blocks in tool result, got %d", len(toolResultBlock.Value.Content))
		}
	}
}

func TestModel_InputOutputModes(t *testing.T) {
	m := &Model{modelID: "test"}

	inputModes := m.InputModes()
	expectedInputModes := []string{"text/plain", "image/png", "image/jpeg", "image/gif", "image/webp"}
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
