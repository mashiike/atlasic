package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

// TaskDataTool provides unified access to task-related data that was abbreviated in prompts
type TaskDataTool struct {
	TaskID      string
	FileMap     map[string]FileData     // file_id -> file data
	ArtifactMap map[string]a2a.Artifact // artifact_id -> artifact
	FullHistory []a2a.Message           // complete conversation history
	TaskDetail  *a2a.Task               // full task information
}

// FileData represents file information for tool access
type FileData struct {
	Filename string
	Data     []byte
	MimeType string
}

// NewTaskDataTool creates a new task data tool with the given data
func NewTaskDataTool(taskID string) *TaskDataTool {
	return &TaskDataTool{
		TaskID:      taskID,
		FileMap:     make(map[string]FileData),
		ArtifactMap: make(map[string]a2a.Artifact),
		FullHistory: make([]a2a.Message, 0),
	}
}

// Name returns the tool name
func (t *TaskDataTool) Name() string {
	return "get_task_data"
}

// Description returns the tool description
func (t *TaskDataTool) Description() string {
	return "Retrieve detailed task data that was abbreviated in the conversation context"
}

// Schema returns the JSON schema for the tool parameters
func (t *TaskDataTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"data_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"file", "artifact", "full_history", "task_detail"},
				"description": "Type of data to retrieve",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the specific item (required for file/artifact)",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"raw", "summary", "json", "markdown"},
				"description": "Format of returned data (default: raw)",
			},
		},
		"required": []string{"data_type"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		// This should never happen with a static schema
		return []byte("{}")
	}
	return data
}

// Execute executes the tool with the given arguments
func (t *TaskDataTool) Execute(ctx context.Context, args map[string]interface{}) (a2a.Part, error) {
	dataType, ok := args["data_type"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("data_type is required")
	}

	format := "raw"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	switch dataType {
	case "file":
		id, ok := args["id"].(string)
		if !ok {
			return a2a.Part{}, fmt.Errorf("id is required for file data type")
		}
		return t.getFileData(id, format)

	case "artifact":
		id, ok := args["id"].(string)
		if !ok {
			return a2a.Part{}, fmt.Errorf("id is required for artifact data type")
		}
		return t.getArtifactData(id, format)

	case "full_history":
		return t.getFullHistory(format)

	case "task_detail":
		return t.getTaskDetail(format)

	default:
		return a2a.Part{}, fmt.Errorf("unknown data_type: %s", dataType)
	}
}

// AddFile adds file data to the tool
func (t *TaskDataTool) AddFile(id string, filename string, data []byte, mimeType string) {
	t.FileMap[id] = FileData{
		Filename: filename,
		Data:     data,
		MimeType: mimeType,
	}
}

// AddArtifact adds artifact data to the tool
func (t *TaskDataTool) AddArtifact(id string, artifact a2a.Artifact) {
	t.ArtifactMap[id] = artifact
}

// SetFullHistory sets the complete conversation history
func (t *TaskDataTool) SetFullHistory(history []a2a.Message) {
	t.FullHistory = history
}

// SetTaskDetail sets the full task information
func (t *TaskDataTool) SetTaskDetail(task *a2a.Task) {
	t.TaskDetail = task
}

// getFileData retrieves file data in the specified format
func (t *TaskDataTool) getFileData(id, format string) (a2a.Part, error) {
	fileData, exists := t.FileMap[id]
	if !exists {
		return a2a.Part{}, fmt.Errorf("file with id %s not found", id)
	}

	switch format {
	case "raw":
		part := a2a.NewFilePartWithBytes(string(fileData.Data), fileData.Filename, fileData.MimeType)
		part.Metadata = map[string]interface{}{
			"file_id":   id,
			"filename":  fileData.Filename,
			"mime_type": fileData.MimeType,
			"size":      len(fileData.Data),
		}
		return part, nil

	case "summary":
		summary := fmt.Sprintf("File: %s (%.1f KB, %s)",
			fileData.Filename,
			float64(len(fileData.Data))/1024,
			fileData.MimeType)
		part := a2a.NewTextPart(summary)
		part.Metadata = map[string]interface{}{
			"file_id":  id,
			"filename": fileData.Filename,
			"size":     len(fileData.Data),
		}
		return part, nil

	case "json":
		data := map[string]interface{}{
			"file_id":   id,
			"filename":  fileData.Filename,
			"mime_type": fileData.MimeType,
			"size":      len(fileData.Data),
			"data":      fileData.Data,
		}
		jsonData, err := json.Marshal(data)
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to marshal file data: %w", err)
		}
		return a2a.NewTextPart(string(jsonData)), nil

	default:
		return t.getFileData(id, "raw")
	}
}

// getArtifactData retrieves artifact data in the specified format
func (t *TaskDataTool) getArtifactData(id, format string) (a2a.Part, error) {
	artifact, exists := t.ArtifactMap[id]
	if !exists {
		return a2a.Part{}, fmt.Errorf("artifact with id %s not found", id)
	}

	switch format {
	case "summary":
		summary := fmt.Sprintf("Artifact: %s\nDescription: %s",
			artifact.Name, artifact.Description)
		part := a2a.NewTextPart(summary)
		part.Metadata = map[string]interface{}{
			"artifact_id": id,
			"name":        artifact.Name,
		}
		return part, nil

	case "json":
		jsonData, err := json.Marshal(artifact)
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to marshal artifact data: %w", err)
		}
		return a2a.NewTextPart(string(jsonData)), nil

	case "markdown":
		var content strings.Builder
		for _, part := range artifact.Parts {
			if part.Kind == a2a.KindTextPart {
				content.WriteString(part.Text)
			}
		}
		md := fmt.Sprintf("# %s\n\n**Description**: %s\n\n**Content**:\n%s",
			artifact.Name, artifact.Description, content.String())
		return a2a.NewTextPart(md), nil

	default: // "raw"
		// Return the first text part or empty if none
		for _, part := range artifact.Parts {
			if part.Kind == a2a.KindTextPart {
				resultPart := a2a.NewTextPart(part.Text)
				resultPart.Metadata = map[string]interface{}{
					"artifact_id": id,
					"name":        artifact.Name,
				}
				return resultPart, nil
			}
		}
		return a2a.NewTextPart(""), nil
	}
}

// getFullHistory retrieves the complete conversation history
func (t *TaskDataTool) getFullHistory(format string) (a2a.Part, error) {
	if len(t.FullHistory) == 0 {
		return a2a.NewTextPart("No conversation history available"), nil
	}

	switch format {
	case "json":
		jsonData, err := json.Marshal(t.FullHistory)
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to marshal history data: %w", err)
		}
		return a2a.NewTextPart(string(jsonData)), nil

	case "markdown":
		var md strings.Builder
		md.WriteString("# Complete Conversation History\n\n")

		for i, msg := range t.FullHistory {
			md.WriteString(fmt.Sprintf("## Message %d (%s)\n\n", i+1, msg.Role))

			for _, part := range msg.Parts {
				switch part.Kind {
				case a2a.KindTextPart:
					md.WriteString(part.Text)
					md.WriteString("\n\n")
				case a2a.KindFilePart:
					if part.File != nil {
						md.WriteString(fmt.Sprintf("📎 **File**: %s\n\n", part.File.Name))
					}
				case a2a.KindDataPart:
					md.WriteString("📊 **Data**: [structured data]\n\n")
				}
			}
		}

		return a2a.NewTextPart(md.String()), nil

	default: // "raw" or "summary"
		summary := fmt.Sprintf("Conversation history contains %d messages", len(t.FullHistory))
		part := a2a.NewTextPart(summary)
		part.Metadata = map[string]interface{}{
			"message_count": len(t.FullHistory),
		}
		return part, nil
	}
}

// getTaskDetail retrieves detailed task information
func (t *TaskDataTool) getTaskDetail(format string) (a2a.Part, error) {
	if t.TaskDetail == nil {
		return a2a.NewTextPart("No detailed task information available"), nil
	}

	switch format {
	case "json":
		jsonData, err := json.Marshal(t.TaskDetail)
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to marshal task detail data: %w", err)
		}
		return a2a.NewTextPart(string(jsonData)), nil

	case "markdown":
		md := fmt.Sprintf(`# Task Details

**Task ID**: %s
**Context ID**: %s
**Status**: %s
**Messages**: %d
**Artifacts**: %d

## Current Status
State: %s
`,
			t.TaskDetail.ID,
			t.TaskDetail.ContextID,
			t.TaskDetail.Status.State,
			len(t.TaskDetail.History),
			len(t.TaskDetail.Artifacts),
			t.TaskDetail.Status.State)

		return a2a.NewTextPart(md), nil

	default: // "raw" or "summary"
		summary := fmt.Sprintf("Task %s in context %s (status: %s)",
			t.TaskDetail.ID, t.TaskDetail.ContextID, t.TaskDetail.Status.State)
		return a2a.NewTextPart(summary), nil
	}
}
