package model

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewRunner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock model
	mockModel := NewMockModel(ctrl)
	mockModel.EXPECT().ID().Return("test-model").AnyTimes()

	// Create a request
	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	// Create runner
	runner := NewRunner(mockModel, request)

	// Verify runner is properly initialized
	require.NotNil(t, runner)
	require.Equal(t, mockModel, runner.Model)
	require.Len(t, runner.GenerateRequest.Messages, 1)
	require.NotNil(t, runner.ToolResolver)
	require.Nil(t, runner.lastStep)
}

func TestRunner_Next_BasicFlow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock model that returns a simple text response
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Hello world")}),
		StopReason: StopReasonEndTurn,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Execute first step
	step, err := runner.Next(context.Background())
	require.NoError(t, err)

	// Verify step details
	require.Equal(t, 0, step.Index)
	require.Equal(t, 0, step.Attempt)
	require.NotNil(t, step.Response)
	require.Equal(t, StopReasonEndTurn, step.Response.StopReason)
	require.Empty(t, step.ToolCalls)
	require.Empty(t, step.ToolResults)
}

func TestRunner_Next_WithToolUse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a tool use part
	toolUsePart := NewToolUsePart("tool-123", "test_tool", map[string]interface{}{
		"arg1": "value1",
	})

	// Create a mock model that returns a tool use response
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Using tool"), toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Use a tool")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "test_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed successfully")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Execute step
	step, err := runner.Next(context.Background())
	require.NoError(t, err)

	// Verify tool execution
	require.Len(t, step.ToolCalls, 1)
	require.Len(t, step.ToolResults, 1)

	toolCall := step.ToolCalls[0]
	require.Equal(t, "tool-123", toolCall.ID)
	require.Equal(t, "test_tool", toolCall.ToolName)

	toolResult := step.ToolResults[0]
	require.Equal(t, "tool-123", toolResult.ID)
	require.Equal(t, "test_tool", toolResult.ToolName)
}

func TestRunner_Next_ToolError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a tool use part
	toolUsePart := NewToolUsePart("tool-456", "failing_tool", map[string]interface{}{
		"arg1": "value1",
	})

	// Create a mock model that returns a tool use response
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Use a tool")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver with failing tool
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "failing_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return nil, errors.New("tool execution failed")
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Execute step
	step, err := runner.Next(context.Background())

	// Should return ToolUseErrors
	var toolErrs ToolUseErrors
	require.True(t, errors.As(err, &toolErrs))
	require.Len(t, toolErrs, 1)

	toolErr := toolErrs[0]
	require.Equal(t, "tool-456", toolErr.ToolUse.ID)
	require.Equal(t, "tool execution failed", toolErr.Err.Error())

	// Step should still contain tool results with error messages
	require.Len(t, step.ToolResults, 1)
	toolResult := step.ToolResults[0]
	require.Equal(t, "tool-456", toolResult.ID)
	require.NotEmpty(t, toolResult.Parts)
}

func TestRunner_Next_UnregisteredTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a tool use part for an unregistered tool
	toolUsePart := NewToolUsePart("tool-789", "unregistered_tool", map[string]interface{}{
		"arg1": "value1",
	})

	// Create a mock model that returns a tool use response
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Use a tool")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver that doesn't support the requested tool
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		return nil, fmt.Errorf("tool %q not registered", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Execute step (without registering the tool)
	step, err := runner.Next(context.Background())

	// Should return ToolUseErrors
	var toolErrs ToolUseErrors
	require.True(t, errors.As(err, &toolErrs))
	require.Len(t, toolErrs, 1)

	toolErr := toolErrs[0]
	require.Equal(t, "unregistered_tool", toolErr.ToolUse.ToolName)

	// Error should indicate tool not registered
	expectedErrMsg := "tool \"unregistered_tool\" not registered"
	require.Equal(t, expectedErrMsg, toolErr.Err.Error())

	// Step should still contain tool results with error messages
	require.Len(t, step.ToolResults, 1)
}

func TestRunner_Next_MultipleSteps(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responses := []*GenerateResponse{
		{
			Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-1", "test_tool", map[string]interface{}{"step": 1}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message:    a2a.NewMessage("response-2", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Task completed")}),
			StopReason: StopReasonEndTurn,
		},
	}

	// Create a mock model that returns different responses on each call
	mockModel := NewMockModel(ctrl)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[0], nil)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[1], nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "test_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// First step - should use tool
	step1, err := runner.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, step1.Index)
	require.Equal(t, StopReasonToolUse, step1.Response.StopReason)
	require.Len(t, step1.ToolCalls, 1)

	// Second step - should be final
	step2, err := runner.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, step2.Index)
	require.Equal(t, StopReasonEndTurn, step2.Response.StopReason)
	require.Empty(t, step2.ToolCalls)

	// Verify messages were accumulated correctly
	// original + response1 + tool_result = 3 (second response not added yet)
	require.Len(t, runner.GenerateRequest.Messages, 3)
}

func TestRunner_Response(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Hello")}),
		StopReason: StopReasonEndTurn,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Before any steps, should return error response
	resp := runner.Response()
	require.Equal(t, StopReasonError, resp.StopReason)

	// After executing a step
	_, err := runner.Next(context.Background())
	require.NoError(t, err)

	resp = runner.Response()
	require.Equal(t, StopReasonEndTurn, resp.StopReason)
	require.Equal(t, runner.lastStep.Response, resp)
}

func TestToolUseError_Error(t *testing.T) {
	toolUse := &ToolUse{
		ID:       "test-tool-123",
		ToolName: "test_tool",
	}

	err := &ToolUseError{
		ToolUse: toolUse,
		Err:     errors.New("execution failed"),
	}

	expectedMsg := "tool use error: execution failed, tool: test_tool"
	require.Equal(t, expectedMsg, err.Error())
}

func TestToolUseError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	toolUse := &ToolUse{
		ID:       "test-tool-123",
		ToolName: "test_tool",
	}

	err := &ToolUseError{
		ToolUse: toolUse,
		Err:     originalErr,
	}

	unwrapped := err.Unwrap()
	require.Equal(t, originalErr, unwrapped)
}

func TestToolUseErrors_Error(t *testing.T) {
	// Test empty errors
	var emptyErrs ToolUseErrors
	expectedEmpty := "no tool use errors"
	require.Equal(t, expectedEmpty, emptyErrs.Error())

	// Test multiple errors
	errs := ToolUseErrors{
		&ToolUseError{
			ToolUse: &ToolUse{ID: "tool-1", ToolName: "tool1"},
			Err:     errors.New("error1"),
		},
		&ToolUseError{
			ToolUse: &ToolUse{ID: "tool-2", ToolName: "tool2"},
			Err:     errors.New("error2"),
		},
	}

	errMsg := errs.Error()
	require.Contains(t, errMsg, "tool use errors:")
	require.Contains(t, errMsg, "error1")
	require.Contains(t, errMsg, "error2")
	require.Greater(t, len(errMsg), 20, "Error message should contain meaningful content")
}

func TestToolUseErrors_Unwrap(t *testing.T) {
	originalErr1 := &ToolUseError{
		ToolUse: &ToolUse{ID: "tool-1", ToolName: "tool1"},
		Err:     errors.New("error1"),
	}
	originalErr2 := &ToolUseError{
		ToolUse: &ToolUse{ID: "tool-2", ToolName: "tool2"},
		Err:     errors.New("error2"),
	}

	errs := ToolUseErrors{originalErr1, originalErr2}

	unwrapped := errs.Unwrap()
	require.Len(t, unwrapped, 2)
	require.Equal(t, originalErr1, unwrapped[0])
	require.Equal(t, originalErr2, unwrapped[1])
}

func TestRunner_Steps_Iterator(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responses := []*GenerateResponse{
		{
			Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-1", "test_tool", map[string]interface{}{"step": 1}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message: a2a.NewMessage("response-2", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-2", "test_tool", map[string]interface{}{"step": 2}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message:    a2a.NewMessage("response-3", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Task completed")}),
			StopReason: StopReasonEndTurn,
		},
	}

	// Create a mock model that cycles through responses
	mockModel := NewMockModel(ctrl)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[0], nil)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[1], nil)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[2], nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "test_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Use Steps iterator
	stepCount := 0
	for step, err := range runner.Steps(context.Background()) {
		require.NoError(t, err)
		require.Equal(t, stepCount, step.Index)

		stepCount++

		// Should terminate after final step
		if step.Response != nil && step.Response.StopReason == StopReasonEndTurn {
			break
		}

		// Prevent infinite loop in case of test failure
		require.LessOrEqual(t, stepCount, 5, "Too many iterations, possible infinite loop")
	}

	// Should have executed 3 steps (2 tool uses + 1 final)
	require.Equal(t, 3, stepCount)
}

func TestRunner_Steps_ContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock model that would return infinite tool uses
	mockModel := NewMockModel(ctrl)
	infiniteResponse := &GenerateResponse{
		Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
			NewToolUsePart("tool-1", "infinite_tool", map[string]interface{}{}),
		}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(infiniteResponse, nil).AnyTimes()

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "infinite_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancel is always called

	stepCount := 0
	for step, err := range runner.Steps(ctx) {
		stepCount++

		// Cancel after first step
		if stepCount == 1 {
			cancel()
		}

		// Should get context cancellation error on second iteration
		if stepCount == 2 {
			require.Error(t, err)
			require.Equal(t, context.Canceled, err)
			break
		}

		// Use step to avoid unused variable warning
		_ = step

		// Prevent infinite loop
		require.LessOrEqual(t, stepCount, 3, "Too many iterations without cancellation")
	}

	require.Equal(t, 2, stepCount, "Expected 2 iterations (1 success + 1 cancellation)")
}

func TestRunner_Steps_WithToolErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a mock model that uses tools
	mockModel := NewMockModel(ctrl)
	response := &GenerateResponse{
		Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
			NewToolUsePart("tool-1", "failing_tool", map[string]interface{}{}),
		}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(response, nil).AnyTimes()

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver with failing tool
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "failing_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return nil, errors.New("tool failed")
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Use Steps iterator and handle tool errors
	stepCount := 0
	toolErrorsEncountered := 0

	for step, err := range runner.Steps(context.Background()) {
		stepCount++

		if err != nil {
			var toolErrs ToolUseErrors
			if errors.As(err, &toolErrs) {
				toolErrorsEncountered++
				// Break after encountering tool errors since we only want to test error handling
				break
			} else {
				t.Fatalf("Unexpected error type: %T: %v", err, err)
			}
		}

		// Should terminate after processing tool error
		if step.Response != nil && step.Response.StopReason != StopReasonToolUse {
			break
		}

		// Use step to avoid unused variable warning
		_ = step

		// Prevent infinite loop
		require.LessOrEqual(t, stepCount, 3, "Too many iterations")
	}

	require.Equal(t, 1, toolErrorsEncountered)
}

func TestRunner_Steps_EarlyBreak(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responses := []*GenerateResponse{
		{
			Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-1", "test_tool", map[string]interface{}{"step": 1}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message: a2a.NewMessage("response-2", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-2", "test_tool", map[string]interface{}{"step": 2}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message:    a2a.NewMessage("response-3", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Task completed")}),
			StopReason: StopReasonEndTurn,
		},
	}

	// Create a mock model
	mockModel := NewMockModel(ctrl)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[0], nil)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[1], nil)
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(responses[2], nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "test_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Use Steps iterator but break early
	stepCount := 0
	for step, err := range runner.Steps(context.Background()) {
		require.NoError(t, err)
		stepCount++

		// Break after 2 steps
		if stepCount == 2 {
			break
		}

		// Use step to avoid unused variable warning
		_ = step
	}

	// Should have only executed 2 steps
	require.Equal(t, 2, stepCount)

	// Should be able to continue with Next() manually
	step, err := runner.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, step.Index)
}

func TestRunner_WithCustomToolResolver(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a custom tool resolver
	customResolver := &DefaultToolResolver{}
	customResolver.RegisterTool("custom_tool", func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
		return &ToolResult{
			Parts: []a2a.Part{a2a.NewTextPart("Custom tool executed")},
		}, nil
	})

	// Create a tool use part
	toolUsePart := NewToolUsePart("tool-123", "custom_tool", map[string]interface{}{
		"arg1": "value1",
	})

	// Create a mock model
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)
	// Set custom resolver
	runner.ToolResolver = customResolver

	// Execute step
	step, err := runner.Next(context.Background())
	require.NoError(t, err)

	// Verify custom tool was used
	require.Len(t, step.ToolResults, 1)

	toolResult := step.ToolResults[0]
	require.NotEmpty(t, toolResult.Parts)

	require.Equal(t, a2a.KindTextPart, toolResult.Parts[0].Kind)
	require.Equal(t, "Custom tool executed", toolResult.Parts[0].Text)
}

func TestRunner_WithToolResolverFunc(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a ToolResolverFunc
	resolverFunc := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "dynamic_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Dynamic tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not supported by dynamic resolver", toolUse.ToolName)
	})

	// Create a tool use part
	toolUsePart := NewToolUsePart("tool-456", "dynamic_tool", map[string]interface{}{
		"arg1": "value1",
	})

	// Create a mock model
	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)
	// Set function resolver
	runner.ToolResolver = resolverFunc

	// Execute step
	step, err := runner.Next(context.Background())
	require.NoError(t, err)

	// Verify dynamic tool was used
	require.Len(t, step.ToolResults, 1)

	toolResult := step.ToolResults[0]
	require.NotEmpty(t, toolResult.Parts)

	require.Equal(t, a2a.KindTextPart, toolResult.Parts[0].Kind)
	require.Equal(t, "Dynamic tool executed", toolResult.Parts[0].Text)
}

func TestDefaultToolResolver_RegisterTool(t *testing.T) {
	resolver := &DefaultToolResolver{}

	// Test successful registration
	resolver.RegisterTool("test_tool", func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
		return &ToolResult{}, nil
	})

	// Test duplicate registration (should panic)
	require.Panics(t, func() {
		resolver.RegisterTool("test_tool", func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
			return &ToolResult{}, nil
		})
	}, "Expected panic for duplicate tool registration")
}

func TestDefaultToolResolver_RegisterTool_InvalidInputs(t *testing.T) {
	resolver := &DefaultToolResolver{}

	// Test empty tool name (should panic)
	require.Panics(t, func() {
		resolver.RegisterTool("", func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
			return &ToolResult{}, nil
		})
	}, "Expected panic for empty tool name")
}

func TestDefaultToolResolver_RegisterTool_NilHandler(t *testing.T) {
	resolver := &DefaultToolResolver{}

	// Test nil handler (should panic)
	require.Panics(t, func() {
		resolver.RegisterTool("test_tool", nil)
	}, "Expected panic for nil handler")
}

func TestDefaultToolResolver_Resolve(t *testing.T) {
	resolver := &DefaultToolResolver{}

	// Register a tool
	expectedHandler := func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
		return &ToolResult{}, nil
	}
	resolver.RegisterTool("test_tool", expectedHandler)

	// Test successful resolution
	toolUse := &ToolUse{
		ID:       "test-123",
		ToolName: "test_tool",
	}

	handler, err := resolver.Resolve(context.Background(), toolUse)
	require.NoError(t, err)
	require.NotNil(t, handler)

	// Test unregistered tool
	unregisteredToolUse := &ToolUse{
		ID:       "test-456",
		ToolName: "unregistered_tool",
	}

	_, err = resolver.Resolve(context.Background(), unregisteredToolUse)
	require.Error(t, err)

	expectedErrMsg := "tool \"unregistered_tool\" not registered"
	require.Equal(t, expectedErrMsg, err.Error())
}

func TestGlobalToolResolver_RegisterTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Note: This test modifies global state, so it might affect other tests
	// In a real test suite, you'd want to reset global state or use dependency injection

	// Test that global RegisterTool works
	RegisterTool("global_test_tool_unique", func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
		return &ToolResult{
			Parts: []a2a.Part{a2a.NewTextPart("Global tool executed")},
		}, nil
	})

	// Verify it's accessible through a runner using the global resolver
	toolUsePart := NewToolUsePart("tool-789", "global_test_tool_unique", map[string]interface{}{})

	mockModel := NewMockModel(ctrl)
	expectedResponse := &GenerateResponse{
		Message:    a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{toolUsePart}),
		StopReason: StopReasonToolUse,
	}
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).Return(expectedResponse, nil)

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)
	// Uses global resolver by default

	step, err := runner.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, step.ToolResults, 1)

	toolResult := step.ToolResults[0]
	require.NotEmpty(t, toolResult.Parts)

	require.Equal(t, a2a.KindTextPart, toolResult.Parts[0].Kind)
	require.Equal(t, "Global tool executed", toolResult.Parts[0].Text)
}

func TestRunner_MessageAccumulation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	responses := []*GenerateResponse{
		{
			Message: a2a.NewMessage("response-1", a2a.RoleAgent, []a2a.Part{
				NewToolUsePart("tool-1", "test_tool", map[string]interface{}{"step": 1}),
			}),
			StopReason: StopReasonToolUse,
		},
		{
			Message:    a2a.NewMessage("response-2", a2a.RoleAgent, []a2a.Part{a2a.NewTextPart("Task completed")}),
			StopReason: StopReasonEndTurn,
		},
	}

	// Create a mock model that tracks what messages it receives
	var receivedMessages [][]a2a.Message
	mockModel := NewMockModel(ctrl)

	// Set up expectations with DoAndReturn to capture messages
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
		// Record messages received
		receivedMessages = append(receivedMessages, append([]a2a.Message{}, req.Messages...))
		return responses[0], nil
	})
	mockModel.EXPECT().Generate(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
		// Record messages received
		receivedMessages = append(receivedMessages, append([]a2a.Message{}, req.Messages...))
		return responses[1], nil
	})

	request := &GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("req-1", a2a.RoleUser, []a2a.Part{a2a.NewTextPart("Hello")}),
		},
	}

	runner := NewRunner(mockModel, request)

	// Create custom tool resolver
	toolResolver := ToolResolverFunc(func(ctx context.Context, toolUse *ToolUse) (func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error), error) {
		if toolUse.ToolName == "test_tool" {
			return func(ctx context.Context, toolUse *ToolUse) (*ToolResult, error) {
				return &ToolResult{
					Parts: []a2a.Part{a2a.NewTextPart("Tool executed")},
				}, nil
			}, nil
		}
		return nil, fmt.Errorf("tool %q not found", toolUse.ToolName)
	})
	runner.ToolResolver = toolResolver

	// Execute two steps
	_, err := runner.Next(context.Background())
	require.NoError(t, err)

	_, err = runner.Next(context.Background())
	require.NoError(t, err)

	// Verify message accumulation
	require.Len(t, receivedMessages, 2, "Expected 2 Generate calls")

	// First call should have only the original message
	require.Len(t, receivedMessages[0], 1, "Expected 1 message in first call")

	// Second call should have original + agent response + tool result
	require.Len(t, receivedMessages[1], 3, "Expected 3 messages in second call")

	// Verify message roles and types
	secondCallMessages := receivedMessages[1]
	require.Equal(t, a2a.RoleUser, secondCallMessages[0].Role)
	require.Equal(t, a2a.RoleAgent, secondCallMessages[1].Role)
	require.Equal(t, a2a.RoleUser, secondCallMessages[2].Role, "Expected third message to be user role (tool results)")

	// Third message should contain tool result parts
	require.True(t, HasToolResultParts(secondCallMessages[2]), "Expected third message to contain tool result parts")
}
