package atlasic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
)

// Summarizer generates summaries of ReAct execution logs
type Summarizer interface {
	// Summarize generates a new summary based on existing summary and new ReAct logs
	// If existingSummary is empty, creates a new summary from logs
	// Returns updated summary text
	Summarize(ctx context.Context, handle TaskHandle, existingSummary string, logs []ReActLog) (string, error)
}

// LLMSummarizer implements Summarizer using LLM
type LLMSummarizer struct {
	ModelProvider string
	ModelID       string
	MaxTokens     int // Maximum tokens for summary output
}

// NewLLMSummarizer creates a new LLMSummarizer
func NewLLMSummarizer(modelProvider, modelID string) *LLMSummarizer {
	return &LLMSummarizer{
		ModelProvider: modelProvider,
		ModelID:       modelID,
		MaxTokens:     500, // Default max tokens
	}
}

// Summarize implements Summarizer interface
func (s *LLMSummarizer) Summarize(ctx context.Context, handle TaskHandle, existingSummary string, logs []ReActLog) (string, error) {
	if len(logs) == 0 {
		return existingSummary, nil
	}

	// Get model for generation
	m, err := model.GetModel(ctx, s.ModelProvider, s.ModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get model: %w", err)
	}

	// Build system prompt for summarization
	systemPrompt := s.buildSystemPrompt()

	// Build user prompt with existing summary and new logs
	userPrompt, err := s.buildUserPrompt(ctx, handle, existingSummary, logs)
	if err != nil {
		return "", fmt.Errorf("failed to build user prompt: %w", err)
	}

	// Create generation request
	req := &model.GenerateRequest{
		System: systemPrompt,
		Messages: []a2a.Message{
			{
				Role:  a2a.RoleUser,
				Parts: []a2a.Part{a2a.NewTextPart(userPrompt)},
			},
		},
		Options: &model.GenerationOptions{
			MaxTokens: model.Ptr(s.MaxTokens),
		},
	}

	// Generate summary
	response, err := m.Generate(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	// Extract text from response
	if len(response.Message.Parts) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	// Get text content from the first part
	textContent := ""
	for _, part := range response.Message.Parts {
		if part.Kind == a2a.KindTextPart && part.Text != "" {
			textContent = part.Text
			break
		}
	}

	if textContent == "" {
		return "", fmt.Errorf("no text content in response")
	}

	return textContent, nil
}

// buildSystemPrompt creates the system prompt for summarization
func (s *LLMSummarizer) buildSystemPrompt() string {
	return `# ReAct Execution Log Summarizer

You are an AI assistant specialized in summarizing ReAct (Reasoning and Acting) execution logs.

## Your Task
Create a concise summary of ReAct execution logs that captures:
- Key reasoning patterns and decisions made
- Important tool executions and their results
- Error patterns and recovery strategies
- Overall execution flow and outcomes

## Summary Guidelines
- Focus on actionable insights and patterns
- Highlight successful strategies and common failure points
- Include relevant context about task progression
- Keep technical details that inform future iterations
- Maintain chronological flow when important
- Be concise but comprehensive

## Output Format
Provide a clean, structured summary in plain text format without markdown headers.`
}

// buildUserPrompt creates the user prompt with existing summary and new logs
func (s *LLMSummarizer) buildUserPrompt(ctx context.Context, handle TaskHandle, existingSummary string, logs []ReActLog) (string, error) {
	var prompt string

	// Add task context if available
	if task, err := handle.GetTask(ctx, 1); err == nil {
		prompt += fmt.Sprintf("Task Context: %s\n", task.Status.State)
		prompt += fmt.Sprintf("Task ID: %s\n", task.ID)
		if task.Status.Message != nil {
			// Extract text from status message parts
			for _, part := range task.Status.Message.Parts {
				if part.Kind == a2a.KindTextPart && part.Text != "" {
					prompt += fmt.Sprintf("Status: %s\n", part.Text)
				}
			}
		}
		prompt += "\n"
	}

	// Add existing summary if present
	if existingSummary != "" {
		prompt += "Previous Summary:\n"
		prompt += existingSummary + "\n\n"
	}

	// Add new logs
	prompt += "New ReAct Logs to Summarize:\n"
	for _, log := range logs {
		logJSON, err := json.MarshalIndent(log, "", "  ")
		if err != nil {
			continue // Skip malformed logs
		}
		prompt += string(logJSON) + "\n"
	}

	if existingSummary != "" {
		prompt += "\nPlease update the previous summary with insights from the new logs. "
		prompt += "Focus on new patterns, decisions, and outcomes that weren't captured before."
	} else {
		prompt += "\nPlease create a comprehensive summary of these ReAct execution logs."
	}

	return prompt, nil
}
