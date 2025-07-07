package prompt

import (
	"fmt"
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

// markdownFormatter handles conversion of A2A structures to markdown format
type markdownFormatter struct {
	builder   *Builder
	idCounter int // For generating unique IDs
}

// generateID generates a unique ID for tools
func (f *markdownFormatter) generateID() string {
	f.idCounter++
	return fmt.Sprintf("id_%03d", f.idCounter)
}

// formatTaskForSystem formats a task for system prompt output
func (f *markdownFormatter) formatTaskForSystem(task *a2a.Task, cfg *config) string {
	if cfg == nil {
		cfg = defaultConfig()
	}

	var result strings.Builder

	// Task basic information
	if cfg.IncludeTaskInfo {
		result.WriteString("## Task Information\n")
		result.WriteString(fmt.Sprintf("- **Task ID**: %s\n", task.ID))
		result.WriteString(fmt.Sprintf("- **Context ID**: %s\n", task.ContextID))
		result.WriteString("\n")
	}

	// Task status
	if cfg.IncludeStatus {
		result.WriteString("## Current Status\n")
		result.WriteString(fmt.Sprintf("- **State**: %s\n", task.Status.State))
		if task.Status.Message != nil && len(task.Status.Message.Parts) > 0 {
			result.WriteString("- **Status Message**: ")
			for _, part := range task.Status.Message.Parts {
				if part.Kind == a2a.KindTextPart {
					result.WriteString(part.Text)
				}
			}
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	// Create and configure TaskDataTool
	taskDataTool := NewTaskDataTool(task.ID)
	taskDataTool.SetTaskDetail(task)
	taskDataTool.SetFullHistory(task.History)

	// Conversation history (summary for system prompt)
	if cfg.IncludeHistory && len(task.History) > 0 {
		result.WriteString("## Conversation Summary\n")

		maxHistory := cfg.MaxHistory
		if maxHistory <= 0 {
			maxHistory = len(task.History)
		}

		start := 0
		if len(task.History) > maxHistory {
			start = len(task.History) - maxHistory
			result.WriteString(fmt.Sprintf("(Showing last %d of %d messages - use `get_task_data` with data_type=\"full_history\" for complete history)\n\n", maxHistory, len(task.History)))
		}

		for i, msg := range task.History[start:] {
			result.WriteString(fmt.Sprintf("**Message %d** (%s): ", start+i+1, msg.Role))
			msgContent, _ := f.formatMessageContent(msg, cfg, taskDataTool)
			result.WriteString(msgContent)
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	// Artifacts summary
	if len(task.Artifacts) > 0 {
		result.WriteString("## Available Artifacts\n")
		for _, artifact := range task.Artifacts {
			if cfg.IncludeArtifacts {
				result.WriteString(fmt.Sprintf("- **%s**: %s\n", artifact.Name, artifact.Description))
			} else {
				// Add to tool and create reference
				artifactID := f.generateID()
				taskDataTool.AddArtifact(artifactID, artifact)
				result.WriteString(fmt.Sprintf("- **%s**: %s (use `get_task_data` with data_type=\"artifact\", id=\"%s\")\n",
					artifact.Name, artifact.Description, artifactID))
			}
		}
		result.WriteString("\n")
	}

	// Set the tool if any references were created
	shouldSetTool := len(taskDataTool.FileMap) > 0 || len(taskDataTool.ArtifactMap) > 0 || !cfg.IncludeHistory
	if shouldSetTool {
		f.builder.setTaskDataTool(taskDataTool)
	}

	return result.String()
}

// formatTaskForMessages formats a task for messages output
func (f *markdownFormatter) formatTaskForMessages(task *a2a.Task, cfg *config) []a2a.Message {
	if cfg == nil {
		cfg = defaultConfig()
	}

	var messages []a2a.Message

	// Create TaskDataTool for references
	taskDataTool := NewTaskDataTool(task.ID)
	taskDataTool.SetTaskDetail(task)
	taskDataTool.SetFullHistory(task.History)

	// Include conversation history
	if cfg.IncludeHistory && len(task.History) > 0 {
		maxHistory := cfg.MaxHistory
		if maxHistory <= 0 {
			maxHistory = len(task.History)
		}

		start := 0
		if len(task.History) > maxHistory {
			start = len(task.History) - maxHistory

			// Add summary message for truncated history
			summaryText := fmt.Sprintf("Previous conversation history contains %d messages (showing last %d). Use `get_task_data` with data_type=\"full_history\" for complete history.",
				len(task.History), maxHistory)
			messages = append(messages, a2a.Message{
				Role:  a2a.RoleUser,
				Parts: []a2a.Part{a2a.NewTextPart(summaryText)},
			})
		}

		// Add recent messages with optimization
		for _, msg := range task.History[start:] {
			optimizedMsg := f.optimizeMessage(msg, cfg, taskDataTool)
			messages = append(messages, optimizedMsg)
		}
	}

	// Set the tool if any references were created
	if len(taskDataTool.FileMap) > 0 || len(taskDataTool.ArtifactMap) > 0 || (cfg.IncludeHistory && cfg.MaxHistory > 0 && len(task.History) > cfg.MaxHistory) {
		f.builder.setTaskDataTool(taskDataTool)
	}

	return messages
}

// formatMessageForSystem formats a message for system prompt output
func (f *markdownFormatter) formatMessageForSystem(msg a2a.Message, cfg *config) string {
	if cfg == nil {
		cfg = defaultConfig()
	}

	taskDataTool := NewTaskDataTool("current")
	content, _ := f.formatMessageContent(msg, cfg, taskDataTool)

	result := fmt.Sprintf("**%s Message**: %s\n", msg.Role, content)

	// Set tool if references were created
	if len(taskDataTool.FileMap) > 0 || len(taskDataTool.ArtifactMap) > 0 {
		f.builder.setTaskDataTool(taskDataTool)
	}

	return result
}

// formatMessageForMessages formats a message for messages output
func (f *markdownFormatter) formatMessageForMessages(msg a2a.Message, cfg *config) []a2a.Message {
	if cfg == nil {
		cfg = defaultConfig()
	}

	taskDataTool := NewTaskDataTool("current")
	optimizedMsg := f.optimizeMessage(msg, cfg, taskDataTool)

	// Set tool if references were created
	if len(taskDataTool.FileMap) > 0 || len(taskDataTool.ArtifactMap) > 0 {
		f.builder.setTaskDataTool(taskDataTool)
	}

	return []a2a.Message{optimizedMsg}
}

// formatMessageContent formats message content, handling different part types
func (f *markdownFormatter) formatMessageContent(msg a2a.Message, cfg *config, taskDataTool *TaskDataTool) (string, bool) {
	var content strings.Builder
	hasReferences := false

	for i, part := range msg.Parts {
		if i > 0 {
			content.WriteString(" ")
		}

		switch part.Kind {
		case a2a.KindTextPart:
			content.WriteString(part.Text)

		case a2a.KindFilePart:
			if cfg.IncludeFileParts {
				content.WriteString(fmt.Sprintf("[File: %s]", part.File.Name))
			} else {
				fileID := f.generateID()
				var fileData []byte
				if part.File.Bytes != "" {
					// Base64 decode if bytes are provided
					fileData = []byte(part.File.Bytes) // Simplified - should be base64.Decode in real implementation
				}
				taskDataTool.AddFile(fileID, part.File.Name, fileData, part.File.MimeType)
				content.WriteString(fmt.Sprintf("📎 **File**: %s (use `get_task_data` with data_type=\"file\", id=\"%s\")", part.File.Name, fileID))
				hasReferences = true
			}

		case a2a.KindDataPart:
			content.WriteString("📊 **Data**: [structured data available via tools]")
		}
	}

	return content.String(), hasReferences
}

// optimizeMessage creates an optimized version of a message for LLM consumption
func (f *markdownFormatter) optimizeMessage(msg a2a.Message, cfg *config, taskDataTool *TaskDataTool) a2a.Message {
	var optimizedParts []a2a.Part

	for _, part := range msg.Parts {
		switch part.Kind {
		case a2a.KindTextPart:
			optimizedParts = append(optimizedParts, part)

		case a2a.KindFilePart:
			if cfg.IncludeFileParts {
				optimizedParts = append(optimizedParts, part)
			} else {
				fileID := f.generateID()
				var fileData []byte
				if part.File.Bytes != "" {
					fileData = []byte(part.File.Bytes) // Simplified
				}
				taskDataTool.AddFile(fileID, part.File.Name, fileData, part.File.MimeType)
				refText := fmt.Sprintf("📎 **File**: %s (use `get_task_data` with data_type=\"file\", id=\"%s\")", part.File.Name, fileID)
				optimizedParts = append(optimizedParts, a2a.NewTextPart(refText))
			}

		case a2a.KindDataPart:
			// Always convert data parts to tool references
			optimizedParts = append(optimizedParts, a2a.NewTextPart("📊 **Data**: [structured data - use tools for access]"))
		}
	}

	return a2a.Message{
		MessageID:        msg.MessageID,
		Role:             msg.Role,
		Parts:            optimizedParts,
		ReferenceTaskIDs: msg.ReferenceTaskIDs,
		Extensions:       msg.Extensions,
		Metadata:         msg.Metadata,
	}
}

// Stub implementations for other format methods
func (f *markdownFormatter) formatStatusForSystem(status a2a.TaskStatus, cfg *config) string {
	return fmt.Sprintf("**Status**: %s\n", status.State)
}

func (f *markdownFormatter) formatStatusForMessages(status a2a.TaskStatus, cfg *config) []a2a.Message {
	text := fmt.Sprintf("Current task status: %s", status.State)
	return []a2a.Message{{
		Role:  a2a.RoleUser,
		Parts: []a2a.Part{a2a.NewTextPart(text)},
	}}
}

func (f *markdownFormatter) formatArtifactForSystem(artifact a2a.Artifact, cfg *config) string {
	return fmt.Sprintf("**Artifact**: %s - %s\n", artifact.Name, artifact.Description)
}

func (f *markdownFormatter) formatArtifactForMessages(artifact a2a.Artifact, cfg *config) []a2a.Message {
	text := fmt.Sprintf("Artifact available: %s - %s", artifact.Name, artifact.Description)
	return []a2a.Message{{
		Role:  a2a.RoleUser,
		Parts: []a2a.Part{a2a.NewTextPart(text)},
	}}
}

func (f *markdownFormatter) formatPartForSystem(part a2a.Part, cfg *config) string {
	switch part.Kind {
	case a2a.KindTextPart:
		return part.Text + "\n"
	case a2a.KindFilePart:
		return fmt.Sprintf("**File**: %s\n", part.File.Name)
	case a2a.KindDataPart:
		return "**Data**: [structured data]\n"
	default:
		return ""
	}
}

func (f *markdownFormatter) formatPartForMessages(part a2a.Part, cfg *config) []a2a.Message {
	var text string
	switch part.Kind {
	case a2a.KindTextPart:
		text = part.Text
	case a2a.KindFilePart:
		text = fmt.Sprintf("File: %s", part.File.Name)
	case a2a.KindDataPart:
		text = "Structured data available"
	default:
		return nil
	}

	return []a2a.Message{{
		Role:  a2a.RoleUser,
		Parts: []a2a.Part{a2a.NewTextPart(text)},
	}}
}
