package prompt

import (
	"context"
	"testing"

	"github.com/mashiike/atlasic/a2a"
)

func TestBuilder_BasicOperations(t *testing.T) {
	builder := NewBuilder()

	// Test WriteString
	n, err := builder.WriteString("Hello World")
	if err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}
	if n != 11 {
		t.Errorf("Expected 11 bytes written, got %d", n)
	}

	// Test Write (io.Writer interface)
	n, err = builder.Write([]byte(" from io.Writer"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 15 {
		t.Errorf("Expected 15 bytes written, got %d", n)
	}

	// Test content blocks
	if builder.Len() != 2 {
		t.Errorf("Expected 2 content blocks, got %d", builder.Len())
	}

	// Test String output
	result := builder.String()
	expected := "Hello World from io.Writer"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuilder_WriteTask(t *testing.T) {
	builder := NewBuilder()

	// Create a test task
	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task-123",
		ContextID: "test-context-456",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		History: []a2a.Message{
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
		},
		Artifacts: []a2a.Artifact{
			{
				ArtifactID:  "artifact-1",
				Name:        "Test Document",
				Description: "A test document",
				Parts:       []a2a.Part{a2a.NewTextPart("Document content")},
			},
		},
	}

	// Test with explicit WithoutArtifacts to force tool generation
	builder.WriteTask(task, WithoutArtifacts())

	if builder.Len() != 1 {
		t.Errorf("Expected 1 content block, got %d", builder.Len())
	}

	// Test system prompt generation
	systemPrompt := builder.String()
	if systemPrompt == "" {
		t.Error("System prompt should not be empty")
	}

	// Debug: print the system prompt to see what we're actually getting
	t.Logf("System prompt:\n%s", systemPrompt)

	// Should contain task information
	if !contains(systemPrompt, "test-task-123") {
		t.Errorf("System prompt should contain task ID, got: %s", systemPrompt)
	}
	if !contains(systemPrompt, "test-context-456") {
		t.Errorf("System prompt should contain context ID, got: %s", systemPrompt)
	}
	if !contains(systemPrompt, "working") {
		t.Errorf("System prompt should contain task state, got: %s", systemPrompt)
	}

	// Test tool generation after String() call
	tool := builder.GetTaskDataTool()
	if tool == nil {
		t.Error("TaskDataTool should be generated when task history is truncated or artifacts are referenced")
	} else {
		t.Logf("TaskDataTool generated with %d files, %d artifacts", len(tool.FileMap), len(tool.ArtifactMap))
		if len(tool.ArtifactMap) > 0 {
			for k, v := range tool.ArtifactMap {
				t.Logf("Artifact %s: %s", k, v.Name)
			}
		}
	}

	// Reset and test messages generation
	builder.Reset()
	builder.WriteTask(task)

	messages := builder.Messages()
	if len(messages) == 0 {
		t.Error("Messages should not be empty")
	}
}

func TestBuilder_WriteMessage(t *testing.T) {
	builder := NewBuilder()

	// Test text message
	textMsg := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "msg-text",
		Role:      a2a.RoleUser,
		Parts:     []a2a.Part{a2a.NewTextPart("Hello, how are you?")},
	}

	builder.WriteMessage(textMsg)

	messages := builder.Messages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	if messages[0].Role != a2a.RoleUser {
		t.Errorf("Expected role %s, got %s", a2a.RoleUser, messages[0].Role)
	}

	if len(messages[0].Parts) != 1 || messages[0].Parts[0].Text != "Hello, how are you?" {
		t.Error("Message content not preserved correctly")
	}
}

func TestBuilder_WriteMessageWithFile(t *testing.T) {
	builder := NewBuilder()

	// Test message with file
	fileMsg := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "msg-file",
		Role:      a2a.RoleUser,
		Parts: []a2a.Part{
			a2a.NewTextPart("Please check this file:"),
			a2a.NewFilePartWithBytes("aGVsbG8gd29ybGQ=", "test.txt", "text/plain"),
		},
	}

	// Test with file parts included
	builder.WriteMessage(fileMsg, WithoutFileParts())

	messages := builder.Messages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	// Should have tool reference instead of file
	hasToolRef := false
	for _, part := range messages[0].Parts {
		if part.Kind == a2a.KindTextPart && contains(part.Text, "get_task_data") {
			hasToolRef = true
			break
		}
	}

	if !hasToolRef {
		t.Error("Expected tool reference for file, but not found")
	}

	// Should generate TaskDataTool
	tool := builder.GetTaskDataTool()
	if tool == nil {
		t.Error("TaskDataTool should be generated when files are referenced")
		return
	}

	if len(tool.FileMap) == 0 {
		t.Error("TaskDataTool should contain file data")
	}
}

func TestBuilder_FunctionalOptions(t *testing.T) {
	builder := NewBuilder()

	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task",
		ContextID: "test-context",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		History: make([]a2a.Message, 50), // Large history to trigger truncation
	}

	// Fill history with test messages
	for i := range 50 {
		task.History[i] = a2a.Message{
			Kind:      a2a.KindMessage,
			MessageID: "msg-" + string(rune('0'+i%10)),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.NewTextPart("Message " + string(rune('0'+i%10)))},
		}
	}

	// Test WithMaxHistory option
	builder.WriteTask(task, WithMaxHistory(10))

	systemPrompt := builder.String()
	if !contains(systemPrompt, "last 10 of 50") {
		t.Error("System prompt should indicate history truncation")
	}

	// Test Summary option
	builder.Reset()
	builder.WriteTask(task, Summary())

	systemPrompt = builder.String()
	if systemPrompt == "" {
		t.Error("Summary system prompt should not be empty")
	}

	// Test Detailed option
	builder.Reset()
	builder.WriteTask(task, Detailed())

	messages := builder.Messages()
	if len(messages) == 0 {
		t.Error("Detailed messages should not be empty")
	}
}

func TestBuilder_Reset(t *testing.T) {
	builder := NewBuilder()

	builder.WriteString("Test content")

	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task",
		ContextID: "test-context",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
	}
	builder.WriteTask(task)

	// Verify content exists
	if builder.Len() != 2 {
		t.Errorf("Expected 2 content blocks before reset, got %d", builder.Len())
	}

	if builder.String() == "" {
		t.Error("String output should not be empty before reset")
	}

	// Reset
	builder.Reset()

	// Verify reset worked
	if builder.Len() != 0 {
		t.Errorf("Expected 0 content blocks after reset, got %d", builder.Len())
	}

	if builder.String() != "" {
		t.Error("String output should be empty after reset")
	}

	if builder.GetTaskDataTool() != nil {
		t.Error("TaskDataTool should be nil after reset")
	}
}

func TestTaskDataTool_Integration(t *testing.T) {
	builder := NewBuilder()

	// Create task with artifacts and files
	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "integration-test",
		ContextID: "integration-context",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		Artifacts: []a2a.Artifact{
			{
				ArtifactID:  "artifact-1",
				Name:        "Test Artifact",
				Description: "Integration test artifact",
				Parts:       []a2a.Part{a2a.NewTextPart("Artifact content")},
			},
		},
	}

	// Write task with options that generate tool references
	builder.WriteTask(task, WithoutArtifacts())

	// Generate system prompt to trigger tool creation
	systemPrompt := builder.String()
	if systemPrompt == "" {
		t.Error("System prompt should not be empty")
	}

	tool := builder.GetTaskDataTool()
	if tool == nil {
		t.Fatal("TaskDataTool should be generated")
	}

	// Test task detail access
	if tool.TaskDetail == nil {
		t.Error("TaskDataTool should have task detail")
	}

	if tool.TaskDetail.ID != "integration-test" {
		t.Errorf("Expected task ID %s, got %s", "integration-test", tool.TaskDetail.ID)
	}

	// Test artifact access
	if len(tool.ArtifactMap) == 0 {
		t.Error("TaskDataTool should contain artifacts")
	}

	// Test tool execution
	ctx := context.Background()

	// Test get task detail
	result, err := tool.Execute(ctx, map[string]any{
		"data_type": "task_detail",
		"format":    "summary",
	})
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if result.Kind != a2a.KindTextPart {
		t.Error("Expected text part result")
	}

	if !contains(result.Text, "integration-test") {
		t.Error("Result should contain task ID")
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
