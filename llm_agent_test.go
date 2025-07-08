package atlasic

import (
	"context"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"go.uber.org/mock/gomock"
)

func TestLLMAgent_RequestOptions(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	tests := []struct {
		name        string
		reqOptions  []func(*model.GenerateRequest)
		expectError bool
		validate    func(t *testing.T, req *model.GenerateRequest)
	}{
		{
			name:       "nil RequestOptions",
			reqOptions: nil,
			validate: func(t *testing.T, req *model.GenerateRequest) {
				// Default values should be set
				if req.Options == nil {
					t.Error("Expected default Options to be set")
				}
				if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
					t.Errorf("Expected default temperature 0.7, got %v", req.Options.Temperature)
				}
				if req.ToolChoice == nil || req.ToolChoice.Type != model.ToolChoiceRequired {
					t.Error("Expected default ToolChoice to be Required")
				}
			},
		},
		{
			name:       "empty RequestOptions",
			reqOptions: []func(*model.GenerateRequest){},
			validate: func(t *testing.T, req *model.GenerateRequest) {
				// Default values should be set
				if req.Options == nil {
					t.Error("Expected default Options to be set")
				}
				if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
					t.Errorf("Expected default temperature 0.7, got %v", req.Options.Temperature)
				}
			},
		},
		{
			name: "Extensions configuration",
			reqOptions: []func(*model.GenerateRequest){
				func(req *model.GenerateRequest) {
					req.Extensions = map[string]any{
						"thinking": map[string]any{
							"type":          "enabled",
							"budget_tokens": 10000,
						},
					}
				},
			},
			validate: func(t *testing.T, req *model.GenerateRequest) {
				if req.Extensions == nil {
					t.Error("Expected Extensions to be set")
				}
				thinking, ok := req.Extensions["thinking"].(map[string]any)
				if !ok {
					t.Error("Expected thinking extension to be set")
				}
				if thinking["type"] != "enabled" {
					t.Error("Expected thinking type to be enabled")
				}
				if thinking["budget_tokens"] != 10000 {
					t.Error("Expected thinking budget_tokens to be 10000")
				}
			},
		},
		{
			name: "GenerationOptions configuration",
			reqOptions: []func(*model.GenerateRequest){
				func(req *model.GenerateRequest) {
					req.Options = &model.GenerationOptions{
						Temperature: model.Ptr(float32(0.5)),
						MaxTokens:   model.Ptr(2000),
					}
				},
			},
			validate: func(t *testing.T, req *model.GenerateRequest) {
				if req.Options == nil {
					t.Error("Expected Options to be set")
				}
				if req.Options.Temperature == nil || *req.Options.Temperature != 0.5 {
					t.Errorf("Expected temperature 0.5, got %v", req.Options.Temperature)
				}
				if req.Options.MaxTokens == nil || *req.Options.MaxTokens != 2000 {
					t.Errorf("Expected MaxTokens 2000, got %v", req.Options.MaxTokens)
				}
			},
		},
		{
			name: "Multiple RequestOptions",
			reqOptions: []func(*model.GenerateRequest){
				func(req *model.GenerateRequest) {
					req.Extensions = map[string]any{
						"thinking": map[string]any{
							"type": "enabled",
						},
					}
				},
				func(req *model.GenerateRequest) {
					req.Options = &model.GenerationOptions{
						Temperature: model.Ptr(float32(0.3)),
					}
				},
			},
			validate: func(t *testing.T, req *model.GenerateRequest) {
				if req.Extensions == nil {
					t.Error("Expected Extensions to be set")
				}
				if req.Options == nil {
					t.Error("Expected Options to be set")
				}
				if req.Options.Temperature == nil || *req.Options.Temperature != 0.3 {
					t.Errorf("Expected temperature 0.3, got %v", req.Options.Temperature)
				}
			},
		},
		{
			name: "Custom ToolChoice",
			reqOptions: []func(*model.GenerateRequest){
				func(req *model.GenerateRequest) {
					req.ToolChoice = &model.ToolChoice{
						Type: model.ToolChoiceAuto,
					}
				},
			},
			validate: func(t *testing.T, req *model.GenerateRequest) {
				if req.ToolChoice == nil || req.ToolChoice.Type != model.ToolChoiceAuto {
					t.Error("Expected ToolChoice to be Auto")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &LLMAgent{
				Name:           "TestAgent",
				ModelProvider:  "mock",
				ModelID:        "mock-model",
				Instructions:   "Test instructions",
				RequestOptions: tt.reqOptions,
				// Set custom builders to avoid model lookup
				SystemPromptBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) (string, error) {
					return "test system prompt", nil
				},
				MessagesBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
					return []a2a.Message{
						{
							Role:  a2a.RoleUser,
							Parts: []a2a.Part{a2a.NewTextPart("test message")},
						},
					}, nil, nil
				},
			}

			req, err := agent.newGenerateRequest(ctx, mockHandle)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if err == nil && tt.validate != nil {
				tt.validate(t, req)
			}
		})
	}
}

func TestLLMAgent_RequestOptions_CoreFieldProtection(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	// Test that core fields (System, Messages, Tools) cannot be overwritten
	agent := &LLMAgent{
		Name:          "TestAgent",
		ModelProvider: "mock",
		ModelID:       "mock-model",
		Instructions:  "Test instructions",
		RequestOptions: []func(*model.GenerateRequest){
			func(req *model.GenerateRequest) {
				// Try to overwrite core fields
				req.System = "overwritten system"
				req.Messages = []a2a.Message{
					{
						Role:  a2a.RoleUser,
						Parts: []a2a.Part{a2a.NewTextPart("overwritten message")},
					},
				}
				req.Tools = []model.Tool{
					{
						Name:        "overwritten_tool",
						Description: "This should be overwritten",
					},
				}
			},
		},
		SystemPromptBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) (string, error) {
			return "protected system prompt", nil
		},
		MessagesBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
			return []a2a.Message{
				{
					Role:  a2a.RoleUser,
					Parts: []a2a.Part{a2a.NewTextPart("protected message")},
				},
			}, nil, nil
		},
	}

	req, err := agent.newGenerateRequest(ctx, mockHandle)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	// Verify that core fields are protected
	if req.System != "protected system prompt" {
		t.Errorf("Expected System to be protected, got: %s", req.System)
	}

	if len(req.Messages) != 1 {
		t.Error("Expected Messages to be protected")
	} else if len(req.Messages[0].Parts) != 1 {
		t.Error("Expected Messages to be protected")
	} else if req.Messages[0].Parts[0].Text != "protected message" {
		t.Error("Expected Messages to be protected")
	}

	// Verify that tools are from LLMAgent's default tools (not overwritten)
	foundStopTool := false
	for _, tool := range req.Tools {
		if tool.Name == "stop" {
			foundStopTool = true
			break
		}
	}
	if !foundStopTool {
		t.Error("Expected Tools to be protected (should contain default 'stop' tool)")
	}

	// Verify that tools don't contain the overwritten tool
	foundOverwrittenTool := false
	for _, tool := range req.Tools {
		if tool.Name == "overwritten_tool" {
			foundOverwrittenTool = true
			break
		}
	}
	if foundOverwrittenTool {
		t.Error("Expected overwritten tool to be rejected")
	}
}

func TestLLMAgent_RequestOptions_BedrockThinking(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	// Test Bedrock thinking configuration as used in the example
	agent := &LLMAgent{
		Name:          "TestAgent",
		ModelProvider: "bedrock",
		ModelID:       "us.anthropic.claude-3-sonnet-20240229-v1:0",
		Instructions:  "Test instructions",
		RequestOptions: []func(*model.GenerateRequest){
			func(req *model.GenerateRequest) {
				req.Extensions = map[string]any{
					"thinking": map[string]any{
						"type":          "enabled",
						"budget_tokens": 10000,
					},
				}
			},
		},
		SystemPromptBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) (string, error) {
			return "test system prompt", nil
		},
		MessagesBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
			return []a2a.Message{
				{
					Role:  a2a.RoleUser,
					Parts: []a2a.Part{a2a.NewTextPart("test message")},
				},
			}, nil, nil
		},
	}

	req, err := agent.newGenerateRequest(ctx, mockHandle)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	// Verify Extensions are set correctly
	if req.Extensions == nil {
		t.Fatal("Expected Extensions to be set")
	}

	thinking, ok := req.Extensions["thinking"].(map[string]any)
	if !ok {
		t.Fatal("Expected thinking extension to be set")
	}

	if thinking["type"] != "enabled" {
		t.Errorf("Expected thinking type to be 'enabled', got %v", thinking["type"])
	}

	if thinking["budget_tokens"] != 10000 {
		t.Errorf("Expected thinking budget_tokens to be 10000, got %v", thinking["budget_tokens"])
	}

	// Verify default options are still applied
	if req.Options == nil {
		t.Error("Expected default Options to be set")
	}
	if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
		t.Errorf("Expected default temperature 0.7, got %v", req.Options.Temperature)
	}
}

func TestLLMAgent_RequestOptions_DefaultsAppliedWhenNotSet(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	// Test that defaults are applied when options are not set
	agent := &LLMAgent{
		Name:          "TestAgent",
		ModelProvider: "mock",
		ModelID:       "mock-model",
		Instructions:  "Test instructions",
		RequestOptions: []func(*model.GenerateRequest){
			func(req *model.GenerateRequest) {
				// Set some Extensions but don't set Options or ToolChoice
				req.Extensions = map[string]any{
					"custom": "value",
				}
			},
		},
		SystemPromptBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) (string, error) {
			return "test system prompt", nil
		},
		MessagesBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
			return []a2a.Message{
				{
					Role:  a2a.RoleUser,
					Parts: []a2a.Part{a2a.NewTextPart("test message")},
				},
			}, nil, nil
		},
	}

	req, err := agent.newGenerateRequest(ctx, mockHandle)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	// Verify Extensions are preserved
	if req.Extensions == nil {
		t.Fatal("Expected Extensions to be set")
	}
	if req.Extensions["custom"] != "value" {
		t.Error("Expected custom extension to be preserved")
	}

	// Verify defaults are applied
	if req.Options == nil {
		t.Error("Expected default Options to be set")
	}
	if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
		t.Errorf("Expected default temperature 0.7, got %v", req.Options.Temperature)
	}
	if req.ToolChoice == nil || req.ToolChoice.Type != model.ToolChoiceRequired {
		t.Error("Expected default ToolChoice to be Required")
	}
}

func TestLLMAgent_RequestOptions_NoOverwriteExistingOptions(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	// Test that defaults don't overwrite existing options
	agent := &LLMAgent{
		Name:          "TestAgent",
		ModelProvider: "mock",
		ModelID:       "mock-model",
		Instructions:  "Test instructions",
		RequestOptions: []func(*model.GenerateRequest){
			func(req *model.GenerateRequest) {
				// Set custom values
				req.Options = &model.GenerationOptions{
					Temperature: model.Ptr(float32(0.9)),
					MaxTokens:   model.Ptr(1500),
				}
				req.ToolChoice = &model.ToolChoice{
					Type: model.ToolChoiceAuto,
				}
			},
		},
		SystemPromptBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) (string, error) {
			return "test system prompt", nil
		},
		MessagesBuilder: func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
			return []a2a.Message{
				{
					Role:  a2a.RoleUser,
					Parts: []a2a.Part{a2a.NewTextPart("test message")},
				},
			}, nil, nil
		},
	}

	req, err := agent.newGenerateRequest(ctx, mockHandle)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	// Verify custom values are preserved (not overwritten by defaults)
	if req.Options == nil {
		t.Error("Expected Options to be set")
	}
	if req.Options.Temperature == nil || *req.Options.Temperature != 0.9 {
		t.Errorf("Expected temperature 0.9, got %v", req.Options.Temperature)
	}
	if req.Options.MaxTokens == nil || *req.Options.MaxTokens != 1500 {
		t.Errorf("Expected MaxTokens 1500, got %v", req.Options.MaxTokens)
	}
	if req.ToolChoice == nil || req.ToolChoice.Type != model.ToolChoiceAuto {
		t.Error("Expected ToolChoice to be Auto")
	}
}
