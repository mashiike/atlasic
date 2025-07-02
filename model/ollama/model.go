// Package ollama provides a model provider implementation for interacting with the Ollama API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"gopkg.in/yaml.v3"
)

func init() {
	endpoint := DefaultEndpoint
	if ollamaHost := os.Getenv("OLLAMA_HOST"); ollamaHost != "" {
		endpoint = ollamaHost + "/api/chat"
	}
	
	p := &ModelProvider{
		Endpoint: endpoint,
	}
	model.Register("ollama", p)
}

const DefaultEndpoint = "http://localhost:11434/api/chat"

type ModelProvider struct {
	Endpoint string
}

type Model struct {
	endpoint string
	modelID  string
}

func (p *ModelProvider) GetModel(ctx context.Context, modelID string) (model.Model, error) {
	if modelID == "" {
		return nil, errors.New("model ID cannot be empty")
	}
	return &Model{
		endpoint: p.Endpoint,
		modelID:  modelID,
	}, nil
}

func (m *Model) ID() string {
	return m.modelID
}

func (m *Model) Generate(ctx context.Context, req *model.GenerateRequest) (*model.GenerateResponse, error) {
	// Convert request to Ollama format
	ollamaReq := ChatRequest{
		Model:  m.modelID,
		Stream: false,
	}

	// Convert system message
	if req.System != "" {
		ollamaReq.Messages = append(ollamaReq.Messages, Message{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert messages
	for _, msg := range req.Messages {
		ollamaMsg, err := m.convertMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		if ollamaMsg != nil {
			ollamaReq.Messages = append(ollamaReq.Messages, *ollamaMsg)
		}
	}

	// Convert tools
	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			ollamaTool := ToolDef{
				Type: "function",
				Function: ToolDefDetail{
					Name:        tool.Name,
					Description: tool.Description,
				},
			}

			// Parse schema to parameters
			var schema map[string]interface{}
			if err := json.Unmarshal(tool.Schema, &schema); err == nil {
				ollamaTool.Function.Parameters = schema
			}

			ollamaReq.Tools = append(ollamaReq.Tools, ollamaTool)
		}
	}

	// Make HTTP request to Ollama
	return m.callOllama(ctx, ollamaReq)
}

func (m *Model) InputModes() []string {
	return []string{"text/plain"}
}

func (m *Model) OutputModes() []string {
	return []string{"text/plain"}
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

type ChatResponse struct {
	Message Message `json:"message"`
}

type ToolDef struct {
	Type     string        `json:"type"` // "function"
	Function ToolDefDetail `json:"function"`
}

type ToolDefDetail struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// convertMessage converts a2a.Message to Ollama Message
func (m *Model) convertMessage(msg a2a.Message) (*Message, error) {
	var role string
	switch msg.Role {
	case a2a.RoleUser:
		role = "user"
	case a2a.RoleAgent:
		role = "assistant"
	default:
		return nil, fmt.Errorf("unsupported message role: %s", msg.Role)
	}

	// Check for tool result parts in the message
	toolResults := model.GetToolResultParts(msg)
	if len(toolResults) > 0 {
		// Use the first tool result for Ollama (which expects one tool result per message)
		toolResult := toolResults[0]
		var content string
		for _, part := range toolResult.Parts {
			if part.Kind == a2a.KindTextPart {
				content += part.Text
			}
		}

		return &Message{
			Role:       "tool",
			ToolCallID: toolResult.ID,
			Name:       toolResult.ToolName,
			Content:    content,
		}, nil
	}

	// Regular message conversion
	var content string
	for _, part := range msg.Parts {
		if part.Kind == a2a.KindTextPart {
			content += part.Text
		}
	}

	return &Message{
		Role:    role,
		Content: content,
	}, nil
}

// callOllama makes HTTP request to Ollama API
func (m *Model) callOllama(ctx context.Context, req ChatRequest) (*model.GenerateResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", m.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read error response body: %w", err)
		}
		return nil, fmt.Errorf("ollama API error: %s - %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return m.convertResponse(chatResp)
}

// convertResponse converts Ollama response to model.GenerateResponse
func (m *Model) convertResponse(resp ChatResponse) (*model.GenerateResponse, error) {
	response := &model.GenerateResponse{
		StopReason: model.StopReasonEndTurn, // Default
	}

	// Create parts array
	var parts []a2a.Part
	hasToolUse := false

	// Add content if present
	if resp.Message.Content != "" {
		parts = append(parts, a2a.NewTextPart(resp.Message.Content))
	}
	// Convert tool calls if present
	if len(resp.Message.ToolCalls) > 0 {
		for _, tc := range resp.Message.ToolCalls {
			var args map[string]interface{}

			// First try JSON parsing
			if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
				// Fallback to YAML parsing (YAML is a superset of JSON and more tolerant)
				if yamlErr := yaml.Unmarshal(tc.Function.Arguments, &args); yamlErr != nil {
					return nil, fmt.Errorf("failed to parse tool arguments: %w", yamlErr)
				}
			}

			// Create tool use part
			toolUsePart := model.NewToolUsePart(tc.ID, tc.Function.Name, args)
			parts = append(parts, toolUsePart)
			hasToolUse = true
		}
	}

	// Set stop reason based on content
	if hasToolUse {
		response.StopReason = model.StopReasonToolUse
	}

	// Create the response message
	response.Message = a2a.NewMessage("ollama-response", a2a.RoleAgent, parts)

	// Note: Ollama doesn't provide token usage information in the same way
	// You might need to estimate or leave it empty
	response.Usage = &model.Usage{
		PromptTokens:     0, // Not provided by Ollama
		CompletionTokens: 0, // Not provided by Ollama
		TotalTokens:      0, // Not provided by Ollama
	}

	return response, nil
}
