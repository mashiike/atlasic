// Package prompt provides utilities for building prompts and messages for LLM interactions.
// It supports both SystemPrompt and Messages generation from A2A protocol structures
// with automatic optimization and tool reference generation.
package prompt

import (
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

// contentBlock represents a piece of content in the builder
type contentBlock struct {
	Type    string  // "text", "task", "message", "status", "artifact", "part"
	Data    any     // The actual content
	Options *config // Applied options for this block
}

// Builder provides a strings.Builder-like interface for constructing prompts and messages
// from A2A protocol structures. It implements io.Writer for standard library compatibility.
type Builder struct {
	content      []contentBlock
	taskDataTool *TaskDataTool // Single generated tool instance
}

// NewBuilder creates a new prompt builder
func NewBuilder() *Builder {
	return &Builder{
		content: make([]contentBlock, 0),
	}
}

// Write implements io.Writer interface for standard library compatibility
func (b *Builder) Write(p []byte) (int, error) {
	return b.WriteString(string(p))
}

// WriteString writes a string to the builder
func (b *Builder) WriteString(s string) (int, error) {
	b.content = append(b.content, contentBlock{
		Type: "text",
		Data: s,
	})
	return len(s), nil
}

// WriteTask adds a task to the builder with optional configuration
func (b *Builder) WriteTask(task *a2a.Task, opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	b.content = append(b.content, contentBlock{
		Type:    "task",
		Data:    task,
		Options: cfg,
	})
}

// WriteMessage adds a message to the builder with optional configuration
func (b *Builder) WriteMessage(msg a2a.Message, opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	b.content = append(b.content, contentBlock{
		Type:    "message",
		Data:    msg,
		Options: cfg,
	})
}

// WriteTaskStatus adds a task status to the builder with optional configuration
func (b *Builder) WriteTaskStatus(status a2a.TaskStatus, opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	b.content = append(b.content, contentBlock{
		Type:    "status",
		Data:    status,
		Options: cfg,
	})
}

// WriteArtifact adds an artifact to the builder with optional configuration
func (b *Builder) WriteArtifact(artifact a2a.Artifact, opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	b.content = append(b.content, contentBlock{
		Type:    "artifact",
		Data:    artifact,
		Options: cfg,
	})
}

// WritePart adds a message part to the builder with optional configuration
func (b *Builder) WritePart(part a2a.Part, opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	b.content = append(b.content, contentBlock{
		Type:    "part",
		Data:    part,
		Options: cfg,
	})
}

// String builds and returns the system prompt
func (b *Builder) String() string {
	var result strings.Builder

	for _, block := range b.content {
		formatted := b.formatBlockForSystem(block)
		result.WriteString(formatted)
	}

	return result.String()
}

// Messages builds and returns messages for LLM conversation
func (b *Builder) Messages() []a2a.Message {
	var messages []a2a.Message

	for _, block := range b.content {
		msgs := b.formatBlockForMessages(block)
		messages = append(messages, msgs...)
	}

	return messages
}

// GetTaskDataTool returns the generated TaskDataTool (if any data was referenced)
func (b *Builder) GetTaskDataTool() *TaskDataTool {
	return b.taskDataTool
}

// Reset clears all content and generated tools
func (b *Builder) Reset() {
	b.content = b.content[:0]
	b.taskDataTool = nil
}

// Len returns the number of content blocks
func (b *Builder) Len() int {
	return len(b.content)
}

// setTaskDataTool sets or updates the TaskDataTool
func (b *Builder) setTaskDataTool(tool *TaskDataTool) {
	b.taskDataTool = tool
}

// formatBlockForSystem formats a content block for system prompt output
func (b *Builder) formatBlockForSystem(block contentBlock) string {
	formatter := &markdownFormatter{builder: b}

	switch block.Type {
	case "text":
		return block.Data.(string) //nolint:errcheck // type guaranteed by block.Type
	case "task":
		return formatter.formatTaskForSystem(block.Data.(*a2a.Task), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "message":
		return formatter.formatMessageForSystem(block.Data.(a2a.Message), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "status":
		return formatter.formatStatusForSystem(block.Data.(a2a.TaskStatus), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "artifact":
		return formatter.formatArtifactForSystem(block.Data.(a2a.Artifact), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "part":
		return formatter.formatPartForSystem(block.Data.(a2a.Part), block.Options) //nolint:errcheck // type guaranteed by block.Type
	default:
		return ""
	}
}

// formatBlockForMessages formats a content block for messages output
func (b *Builder) formatBlockForMessages(block contentBlock) []a2a.Message {
	formatter := &markdownFormatter{builder: b}

	switch block.Type {
	case "text":
		if text := strings.TrimSpace(block.Data.(string)); text != "" { //nolint:errcheck // type guaranteed by block.Type
			return []a2a.Message{{
				Role:  a2a.RoleUser,
				Parts: []a2a.Part{a2a.NewTextPart(text)},
			}}
		}
		return nil
	case "task":
		return formatter.formatTaskForMessages(block.Data.(*a2a.Task), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "message":
		return formatter.formatMessageForMessages(block.Data.(a2a.Message), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "status":
		return formatter.formatStatusForMessages(block.Data.(a2a.TaskStatus), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "artifact":
		return formatter.formatArtifactForMessages(block.Data.(a2a.Artifact), block.Options) //nolint:errcheck // type guaranteed by block.Type
	case "part":
		return formatter.formatPartForMessages(block.Data.(a2a.Part), block.Options) //nolint:errcheck // type guaranteed by block.Type
	default:
		return nil
	}
}
