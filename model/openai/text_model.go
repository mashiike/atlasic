package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// TextModel implements the model.Model interface for OpenAI text generation models like GPT-4o
type TextModel struct {
	modelID string
	client  OpenAIClient
}

// ID returns the model identifier
func (m *TextModel) ID() string {
	return m.modelID
}

// InputModes returns the supported input modes for text generation
func (m *TextModel) InputModes() []string {
	return []string{
		"text/plain", // Text prompts
		"image/png",  // Image inputs for vision models
		"image/jpeg", // Image inputs for vision models
		"image/webp", // Image inputs for vision models
	}
}

// OutputModes returns the supported output modes for text generation
func (m *TextModel) OutputModes() []string {
	return []string{
		"text/plain", // Text responses
	}
}

// Generate generates text responses using OpenAI Chat API
func (m *TextModel) Generate(ctx context.Context, req *model.GenerateRequest) (*model.GenerateResponse, error) {
	// Convert to OpenAI chat format
	params := openai.ChatCompletionNewParams{
		Model: m.modelID, // Model is shared.ChatModel (string)
	}

	// Add messages using helper functions
	var messages []openai.ChatCompletionMessageParamUnion

	// Add system message if provided
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}

	// Convert A2A messages to OpenAI format
	for _, msg := range req.Messages {
		openaiMsgs, err := m.convertMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, openaiMsgs...)
	}
	params.Messages = messages

	// Add tools if provided
	if len(req.Tools) > 0 {
		var tools []openai.ChatCompletionToolParam
		for _, tool := range req.Tools {
			// Convert tool schema to OpenAI format
			var schema map[string]interface{}
			if err := json.Unmarshal(tool.Schema, &schema); err != nil {
				return nil, fmt.Errorf("failed to parse tool schema for %s: %w", tool.Name, err)
			}

			openaiTool := openai.ChatCompletionToolParam{
				Function: shared.FunctionDefinitionParam{
					Name:        tool.Name, // Name is string, not param.Opt[string]
					Description: openai.String(tool.Description),
					Parameters:  schema,
				},
			}
			tools = append(tools, openaiTool)
		}
		params.Tools = tools

		// Set tool choice if provided
		if req.ToolChoice != nil {
			switch req.ToolChoice.Type {
			case model.ToolChoiceAuto:
				params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
					OfAuto: openai.String("auto"),
				}
			case model.ToolChoiceRequired:
				params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
					OfAuto: openai.String("required"),
				}
			case model.ToolChoiceSpecific:
				// For specific tool choice
				params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
					OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
						Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
							Name: req.ToolChoice.ToolName,
						},
					},
				}
			}
		}
	}

	// Set generation options
	if req.Options != nil {
		if req.Options.Temperature != nil {
			params.Temperature = openai.Float(float64(*req.Options.Temperature))
		}
		if req.Options.TopP != nil {
			params.TopP = openai.Float(float64(*req.Options.TopP))
		}
		if req.Options.MaxTokens != nil {
			params.MaxTokens = openai.Int(int64(*req.Options.MaxTokens))
		}
	}

	// Call OpenAI Chat API
	response, err := m.client.GetChat().Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate text response: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from OpenAI API")
	}

	// Convert response
	return m.convertResponse(response)
}

// convertMessage converts A2A message to OpenAI ChatCompletionMessageParamUnion
func (m *TextModel) convertMessage(msg a2a.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	// Check for tool use parts first
	toolUses := model.GetToolUseParts(msg)
	if len(toolUses) > 0 && msg.Role == a2a.RoleAgent {
		// Assistant message with tool calls
		var content string
		var toolCalls []openai.ChatCompletionMessageToolCallParam

		// Extract text content
		for _, part := range msg.Parts {
			if part.Kind == a2a.KindTextPart {
				content += part.Text
			}
		}

		// Convert tool uses
		for _, toolUse := range toolUses {
			argsStr, err := json.Marshal(toolUse.Arguments)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal tool arguments: %w", err)
			}

			toolCall := openai.ChatCompletionMessageToolCallParam{
				ID: toolUse.ID,
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      toolUse.ToolName,
					Arguments: string(argsStr),
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}

		assistantMsgParam := openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(content),
			},
			ToolCalls: toolCalls,
		}
		return []openai.ChatCompletionMessageParamUnion{
			{
				OfAssistant: &assistantMsgParam,
			},
		}, nil
	}

	// Check for tool result parts
	toolResults := model.GetToolResultParts(msg)
	if len(toolResults) > 0 {
		var messages []openai.ChatCompletionMessageParamUnion
		// Tool result messages - each needs separate tool message
		for _, toolResult := range toolResults {
			var content string
			for _, part := range toolResult.Parts {
				if part.Kind == a2a.KindTextPart {
					content += part.Text
				}
			}

			toolMsg := openai.ToolMessage(content, toolResult.ID)
			messages = append(messages, toolMsg)
		}
		return messages, nil
	}

	// Build message content with text and images support
	var messageContent []openai.ChatCompletionContentPartUnionParam
	var hasText bool

	for _, part := range msg.Parts {
		if part.Kind == a2a.KindTextPart {
			if part.Text != "" {
				messageContent = append(messageContent, openai.TextContentPart(part.Text))
				hasText = true
			}
		} else if part.Kind == a2a.KindFilePart && part.File != nil {
			if isImageMimeType(part.File.MimeType) {
				// Create image URL for OpenAI Vision API
				imageURL := fmt.Sprintf("data:%s;base64,%s", part.File.MimeType, part.File.Bytes)
				messageContent = append(messageContent, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: imageURL,
				}))
			}
		}
	}

	// If no text content, add a default prompt for images
	if !hasText && len(messageContent) > 0 {
		messageContent = append([]openai.ChatCompletionContentPartUnionParam{
			openai.TextContentPart("What do you see in this image?"),
		}, messageContent...)
	}

	switch msg.Role {
	case a2a.RoleUser:
		if len(messageContent) == 0 {
			return []openai.ChatCompletionMessageParamUnion{openai.UserMessage("")}, nil
		} else if len(messageContent) == 1 {
			// Single text part - use simple string format
			return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(messageContent)}, nil
		}
		// Multiple parts or image content - use content array format
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(messageContent)}, nil
	case a2a.RoleAgent:
		// Assistant messages are typically text-only
		var textContent string
		for _, part := range msg.Parts {
			if part.Kind == a2a.KindTextPart {
				textContent += part.Text
			}
		}
		return []openai.ChatCompletionMessageParamUnion{openai.AssistantMessage(textContent)}, nil
	default:
		return nil, fmt.Errorf("unsupported message role: %s", msg.Role)
	}
}

// convertResponse converts OpenAI response to model.GenerateResponse
func (m *TextModel) convertResponse(response *openai.ChatCompletion) (*model.GenerateResponse, error) {
	choice := response.Choices[0]

	var parts []a2a.Part
	var stopReason = model.StopReasonEndTurn

	// Add text content if present
	if choice.Message.Content != "" {
		parts = append(parts, a2a.NewTextPart(choice.Message.Content))
	}

	// Add tool calls if present
	if len(choice.Message.ToolCalls) > 0 {
		for _, toolCall := range choice.Message.ToolCalls {
			// Parse arguments
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
			}

			// Create tool use part
			toolUsePart := model.NewToolUsePart(
				toolCall.ID,
				toolCall.Function.Name,
				args,
			)
			parts = append(parts, toolUsePart)
		}
		stopReason = model.StopReasonToolUse
	}

	// Map finish reason
	switch choice.FinishReason {
	case "stop":
		stopReason = model.StopReasonEndTurn
	case "length":
		stopReason = model.StopReasonMaxTokens
	case "tool_calls":
		stopReason = model.StopReasonToolUse
	case "content_filter":
		stopReason = model.StopReasonStopWord // Closest mapping
	}

	// Create response message
	responseMessage := a2a.NewMessage("openai-text", a2a.RoleAgent, parts)

	// Create usage info
	usage := &model.Usage{
		PromptTokens:     int(response.Usage.PromptTokens),
		CompletionTokens: int(response.Usage.CompletionTokens),
		TotalTokens:      int(response.Usage.TotalTokens),
	}

	return &model.GenerateResponse{
		Message:    responseMessage,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}
