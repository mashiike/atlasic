package prompt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mashiike/atlasic/a2a"
)

func TestTaskDataTool_BasicOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task-123")

	if tool.TaskID != "test-task-123" {
		t.Errorf("Expected TaskID 'test-task-123', got %s", tool.TaskID)
	}

	if tool.Name() != "get_task_data" {
		t.Errorf("Expected name 'get_task_data', got %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}

	// Test schema
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema should not be empty")
	}

	// Verify schema is valid JSON
	var schemaObj map[string]interface{}
	if err := json.Unmarshal(schema, &schemaObj); err != nil {
		t.Errorf("Schema is not valid JSON: %v", err)
	}

	// Check required properties exist
	props, ok := schemaObj["properties"].(map[string]interface{})
	if !ok {
		t.Error("Schema should have properties")
	}

	if _, ok := props["data_type"]; !ok {
		t.Error("Schema should have data_type property")
	}
}

func TestTaskDataTool_FileOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task")
	ctx := context.Background()

	// Add test file
	testData := []byte("Hello, World!")
	tool.AddFile("file-1", "test.txt", testData, "text/plain")

	// Test file retrieval - raw format
	result, err := tool.Execute(ctx, map[string]interface{}{
		"data_type": "file",
		"id":        "file-1",
		"format":    "raw",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Kind != a2a.KindFilePart {
		t.Errorf("Expected file part, got %s", result.Kind)
	}

	if result.File.Name != "test.txt" {
		t.Errorf("Expected filename 'test.txt', got %s", result.File.Name)
	}

	// Test file retrieval - summary format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "file",
		"id":        "file-1",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Kind != a2a.KindTextPart {
		t.Errorf("Expected text part, got %s", result.Kind)
	}

	if !contains(result.Text, "test.txt") {
		t.Error("Summary should contain filename")
	}

	if !contains(result.Text, "text/plain") {
		t.Error("Summary should contain mime type")
	}

	// Test file retrieval - json format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "file",
		"id":        "file-1",
		"format":    "json",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Kind != a2a.KindTextPart {
		t.Errorf("Expected text part, got %s", result.Kind)
	}

	// Verify JSON is valid
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(result.Text), &jsonData); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	// Test non-existent file
	_, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "file",
		"id":        "non-existent",
		"format":    "raw",
	})
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestTaskDataTool_ArtifactOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task")
	ctx := context.Background()

	// Add test artifact
	artifact := a2a.Artifact{
		ArtifactID:  "artifact-1",
		Name:        "Test Document",
		Description: "A test document for testing",
		Parts: []a2a.Part{
			a2a.NewTextPart("This is the content of the test document."),
		},
	}
	tool.AddArtifact("ref-1", artifact)

	// Test artifact retrieval - raw format
	result, err := tool.Execute(ctx, map[string]interface{}{
		"data_type": "artifact",
		"id":        "ref-1",
		"format":    "raw",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Kind != a2a.KindTextPart {
		t.Errorf("Expected text part, got %s", result.Kind)
	}

	if !contains(result.Text, "This is the content") {
		t.Error("Raw format should contain artifact content")
	}

	// Test artifact retrieval - summary format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "artifact",
		"id":        "ref-1",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "Test Document") {
		t.Error("Summary should contain artifact name")
	}

	if !contains(result.Text, "A test document for testing") {
		t.Error("Summary should contain artifact description")
	}

	// Test artifact retrieval - markdown format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "artifact",
		"id":        "ref-1",
		"format":    "markdown",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "# Test Document") {
		t.Error("Markdown format should contain header with artifact name")
	}

	// Test non-existent artifact
	_, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "artifact",
		"id":        "non-existent",
		"format":    "raw",
	})
	if err == nil {
		t.Error("Expected error for non-existent artifact")
	}
}

func TestTaskDataTool_HistoryOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task")
	ctx := context.Background()

	// Test empty history
	result, err := tool.Execute(ctx, map[string]interface{}{
		"data_type": "full_history",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "No conversation history") {
		t.Error("Should indicate no conversation history")
	}

	// Add test history
	history := []a2a.Message{
		{
			Kind:      a2a.KindMessage,
			MessageID: "msg-1",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.NewTextPart("Hello")},
		},
		{
			Kind:      a2a.KindMessage,
			MessageID: "msg-2",
			Role:      a2a.RoleAgent,
			Parts:     []a2a.Part{a2a.NewTextPart("Hi there!")},
		},
	}
	tool.SetFullHistory(history)

	// Test history retrieval - summary format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "full_history",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "2 messages") {
		t.Error("Summary should contain message count")
	}

	// Test history retrieval - json format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "full_history",
		"format":    "json",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify JSON is valid
	var jsonData []a2a.Message
	if err := json.Unmarshal([]byte(result.Text), &jsonData); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	if len(jsonData) != 2 {
		t.Errorf("Expected 2 messages in JSON, got %d", len(jsonData))
	}

	// Test history retrieval - markdown format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "full_history",
		"format":    "markdown",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "# Complete Conversation History") {
		t.Error("Markdown should contain header")
	}

	if !contains(result.Text, "Hello") {
		t.Error("Markdown should contain message content")
	}
}

func TestTaskDataTool_TaskDetailOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task")
	ctx := context.Background()

	// Test without task detail
	result, err := tool.Execute(ctx, map[string]interface{}{
		"data_type": "task_detail",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "No detailed task information") {
		t.Error("Should indicate no task information")
	}

	// Add task detail
	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task-123",
		ContextID: "test-context-456",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		History:   []a2a.Message{},
		Artifacts: []a2a.Artifact{},
	}
	tool.SetTaskDetail(task)

	// Test task detail retrieval - summary format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "task_detail",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "test-task-123") {
		t.Error("Summary should contain task ID")
	}

	if !contains(result.Text, "test-context-456") {
		t.Error("Summary should contain context ID")
	}

	if !contains(result.Text, "working") {
		t.Error("Summary should contain task state")
	}

	// Test task detail retrieval - json format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "task_detail",
		"format":    "json",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify JSON is valid
	var jsonData a2a.Task
	if err := json.Unmarshal([]byte(result.Text), &jsonData); err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	if jsonData.ID != "test-task-123" {
		t.Errorf("Expected task ID 'test-task-123', got %s", jsonData.ID)
	}

	// Test task detail retrieval - markdown format
	result, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "task_detail",
		"format":    "markdown",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !contains(result.Text, "# Task Details") {
		t.Error("Markdown should contain header")
	}
}

func TestTaskDataTool_InvalidOperations(t *testing.T) {
	tool := NewTaskDataTool("test-task")
	ctx := context.Background()

	// Test missing data_type
	_, err := tool.Execute(ctx, map[string]interface{}{
		"format": "raw",
	})
	if err == nil {
		t.Error("Expected error for missing data_type")
	}

	// Test invalid data_type
	_, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "invalid",
		"format":    "raw",
	})
	if err == nil {
		t.Error("Expected error for invalid data_type")
	}

	// Test missing id for file
	_, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "file",
		"format":    "raw",
	})
	if err == nil {
		t.Error("Expected error for missing id for file")
	}

	// Test missing id for artifact
	_, err = tool.Execute(ctx, map[string]interface{}{
		"data_type": "artifact",
		"format":    "raw",
	})
	if err == nil {
		t.Error("Expected error for missing id for artifact")
	}
}
