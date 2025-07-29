// Package bedrock provides integration with AWS Bedrock for model inference and conversation APIs.
package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
)

func init() {
	p := &ModelProvider{}
	model.Register("bedrock", p)
}

type BedrockClient interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type ModelProvider struct {
	once    sync.Once
	awsCfg  *aws.Config
	client  BedrockClient
	loadErr error
}

func (p *ModelProvider) SetAWSConfig(cfg *aws.Config) {
	p.once.Do(func() {
		p.awsCfg = cfg
	})
}

func (p *ModelProvider) GetClient() (BedrockClient, error) {
	p.once.Do(func() {
		if p.client != nil {
			return
		}
		if p.awsCfg == nil {
			cfg, err := config.LoadDefaultConfig(context.Background())
			if err != nil {
				p.loadErr = err
				return
			}
			p.awsCfg = &cfg
		}
		p.client = bedrockruntime.NewFromConfig(*p.awsCfg)
	})
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	if p.client == nil {
		return nil, errors.New("bedrock client is not initialized")
	}
	return p.client, nil
}

func (p *ModelProvider) SetClient(client BedrockClient) {
	p.once.Do(func() {
		p.client = client
	})
}

func (p *ModelProvider) GetModel(ctx context.Context, modelID string) (model.Model, error) {
	if modelID == "" {
		return nil, errors.New("model ID cannot be empty")
	}
	client, err := p.GetClient()
	if err != nil {
		return nil, err
	}
	// Here you would typically create and return a specific model instance
	// For simplicity, we return a generic Model instance
	return &Model{
		modelID: modelID,
		client:  client,
	}, nil
}

type Model struct {
	modelID string
	client  BedrockClient
}

func (m *Model) ID() string {
	return m.modelID
}

func (m *Model) Generate(ctx context.Context, req *model.GenerateRequest) (*model.GenerateResponse, error) {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(m.modelID),
	}

	// Convert system message
	if req.System != "" {
		input.System = []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{
				Value: req.System,
			},
		}
	}

	// Convert messages
	var messages []types.Message
	for _, msg := range req.Messages {
		bedrockMsg, err := m.convertMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		if bedrockMsg != nil {
			messages = append(messages, *bedrockMsg)
		}
	}
	input.Messages = messages

	// Convert tools
	if len(req.Tools) > 0 {
		var toolConfig types.ToolConfiguration
		for _, tool := range req.Tools {
			var schema map[string]any
			if err := json.Unmarshal(tool.Schema, &schema); err != nil {
				slog.WarnContext(ctx, "failed to parse tool schema, skip this tool", "error", err, "tool", tool.Name)
				continue
			}
			bedrockTool := &types.ToolMemberToolSpec{
				Value: types.ToolSpecification{
					Name:        aws.String(tool.Name),
					Description: aws.String(tool.Description),
					InputSchema: &types.ToolInputSchemaMemberJson{
						Value: document.NewLazyDocument(schema),
					},
				},
			}
			toolConfig.Tools = append(toolConfig.Tools, bedrockTool)
		}

		// Set tool choice if provided
		if req.ToolChoice != nil {
			// Convert tool choice to Bedrock format
			// Note: Bedrock does not have a direct equivalent for tool choice, so this
			// is a placeholder for future expansion if needed.
			switch req.ToolChoice.Type {
			case model.ToolChoiceAuto:
				toolConfig.ToolChoice = &types.ToolChoiceMemberAuto{}
			case model.ToolChoiceRequired:
				toolConfig.ToolChoice = &types.ToolChoiceMemberAny{}
			case model.ToolChoiceSpecific:
				toolConfig.ToolChoice = &types.ToolChoiceMemberTool{
					Value: types.SpecificToolChoice{
						Name: aws.String(req.ToolChoice.ToolName),
					},
				}
			}
		}

		// Always set ToolConfig when tools are provided
		input.ToolConfig = &toolConfig
	}

	// Set inference configuration
	if req.Options != nil {
		var inferenceConfig types.InferenceConfiguration
		if req.Options.Temperature != nil {
			inferenceConfig.Temperature = req.Options.Temperature
		}
		if req.Options.TopP != nil {
			inferenceConfig.TopP = req.Options.TopP
		}
		if req.Options.MaxTokens != nil {
			inferenceConfig.MaxTokens = aws.Int32(int32(*req.Options.MaxTokens))
		}
		input.InferenceConfig = &inferenceConfig
	}
	if bs, err := json.Marshal(input); err == nil {
		slog.DebugContext(ctx, "Bedrock Converse Input", "input", string(bs))
	}
	// Call Bedrock
	output, err := m.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse failed: %w", err)
	}
	if bs, err := json.Marshal(output); err == nil {
		slog.DebugContext(ctx, "Bedrock Converse Output", "output", string(bs))
	}
	// Convert response
	return m.convertResponse(output)
}

func (m *Model) InputModes() []string {
	return []string{"text/plain", "image/png", "image/jpeg", "image/gif", "image/webp"}
}

func (m *Model) OutputModes() []string {
	return []string{"text/plain"}
}

// convertMessage converts a2a.Message to Bedrock Message
func (m *Model) convertMessage(msg a2a.Message) (*types.Message, error) {
	var role types.ConversationRole
	switch msg.Role {
	case a2a.RoleUser:
		role = types.ConversationRoleUser
	case a2a.RoleAgent:
		role = types.ConversationRoleAssistant
	default:
		return nil, fmt.Errorf("unsupported message role: %s", msg.Role)
	}

	var content []types.ContentBlock
	for _, part := range msg.Parts {
		if toolUse, ok := model.AsToolUsePart(part); ok {
			content = append(content, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{
					ToolUseId: aws.String(toolUse.ID),
					Name:      aws.String(toolUse.ToolName),
					Input:     document.NewLazyDocument(toolUse.Arguments),
				},
			})
			continue
		}
		if toolResult, ok := model.AsToolResultPart(part); ok {
			// Handle tool result part
			var resultContent []types.ToolResultContentBlock
			for _, resultPart := range toolResult.Parts {
				if resultPart.Kind == a2a.KindTextPart {
					resultContent = append(resultContent, &types.ToolResultContentBlockMemberText{
						Value: resultPart.Text,
					})
				}
			}
			content = append(content, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(toolResult.ID),
					Content:   resultContent,
				},
			})
			continue
		}
		if model.IsReasoning(part) {
			if part.Kind == a2a.KindTextPart {
				reasoning := types.ReasoningTextBlock{
					Text: aws.String(part.Text),
				}
				if part.Metadata != nil {
					if sig, ok := part.Metadata["signature"].(string); ok {
						reasoning.Signature = aws.String(sig)
					}
				}
				content = append(content, &types.ContentBlockMemberReasoningContent{
					Value: &types.ReasoningContentBlockMemberReasoningText{
						Value: reasoning,
					},
				})
				continue
			}
		}
		switch part.Kind {
		case a2a.KindTextPart:
			content = append(content, &types.ContentBlockMemberText{
				Value: part.Text,
			})
		case a2a.KindFilePart:
			// Handle file parts (images, etc.)
			if part.File != nil && part.File.MimeType != "" && part.File.Bytes != "" {
				// For images
				content = append(content, &types.ContentBlockMemberImage{
					Value: types.ImageBlock{
						Format: types.ImageFormat(part.File.MimeType),
						Source: &types.ImageSourceMemberBytes{
							Value: []byte(part.File.Bytes), // Assume base64 decoded
						},
					},
				})
			}
		case a2a.KindDataPart:
			bs, err := json.Marshal(part.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal data part: %w", err)
			}
			content = append(content, &types.ContentBlockMemberText{
				Value: string(bs),
			})
		}
	}

	return &types.Message{
		Role:    role,
		Content: content,
	}, nil
}

// convertResponse converts Bedrock response to model.GenerateResponse
func (m *Model) convertResponse(output *bedrockruntime.ConverseOutput) (*model.GenerateResponse, error) {
	response := &model.GenerateResponse{
		StopReason: model.StopReasonEndTurn, // Default
	}

	// Convert usage information
	if output.Usage != nil {
		inputTokens := int(aws.ToInt32(output.Usage.InputTokens))
		outputTokens := int(aws.ToInt32(output.Usage.OutputTokens))
		totalTokens := int(aws.ToInt32(output.Usage.TotalTokens))

		response.Usage = &model.Usage{
			InputTokens:   model.Ptr(inputTokens),
			OutputTokens:  model.Ptr(outputTokens),
			TotalTokens:   model.Ptr(totalTokens),
			ModelID:       m.modelID,
			ProviderUsage: output.Usage, // Store raw Bedrock usage data
		}
	}

	// Convert output message
	if output.Output != nil {
		switch msgOutput := output.Output.(type) {
		case *types.ConverseOutputMemberMessage:
			msg := msgOutput.Value

			// Process content blocks and build parts
			var parts []a2a.Part
			hasToolUse := false

			for _, content := range msg.Content {
				switch block := content.(type) {
				case *types.ContentBlockMemberText:
					parts = append(parts, a2a.NewTextPart(block.Value))
				case *types.ContentBlockMemberReasoningContent:
					// Handle reasoning content
					switch value := block.Value.(type) {
					case *types.ReasoningContentBlockMemberReasoningText:
						part := a2a.NewTextPart((*value.Value.Text))
						if value.Value.Signature != nil {
							part.Metadata = map[string]any{
								"signature": *value.Value.Signature,
							}
						}
						parts = append(parts, model.MarkReasoning(part)...)
					case *types.ReasoningContentBlockMemberRedactedContent:
						part := a2a.NewDataPart(map[string]any{
							"redacted": base64.StdEncoding.EncodeToString(value.Value),
						})
						parts = append(parts, model.MarkReasoning(part)...)
					}
				case *types.ContentBlockMemberToolUse:
					// Parse arguments
					var args map[string]interface{}
					if block.Value.Input != nil {
						inputBytes, err := block.Value.Input.MarshalSmithyDocument()
						if err != nil {
							slog.Warn("failed to marshal smithy document", "error", err, "tool_use_id", aws.ToString(block.Value.ToolUseId))
							continue
						}
						if err := json.Unmarshal(inputBytes, &args); err != nil {
							slog.Warn("failed to unmarshal tool arguments", "error", err, "tool_use_id", aws.ToString(block.Value.ToolUseId))
						}
					}

					// Create tool use part
					toolUsePart := model.NewToolUsePart(
						aws.ToString(block.Value.ToolUseId),
						aws.ToString(block.Value.Name),
						args,
					)
					parts = append(parts, toolUsePart)
					hasToolUse = true
				}
			}

			// Set stop reason based on content
			if hasToolUse {
				response.StopReason = model.StopReasonToolUse
			}

			// Create the response message
			response.Message = a2a.NewMessage("bedrock-response", a2a.RoleAgent, parts)

			// Map Bedrock stop reason if available
			switch output.StopReason {
			case types.StopReasonMaxTokens:
				response.StopReason = model.StopReasonMaxTokens
			case types.StopReasonStopSequence:
				response.StopReason = model.StopReasonStopWord
			case types.StopReasonToolUse:
				response.StopReason = model.StopReasonToolUse
			case types.StopReasonEndTurn:
				response.StopReason = model.StopReasonEndTurn
			}
		}
	}

	// Store the complete Bedrock response
	response.RawResponse = output

	return response, nil
}
