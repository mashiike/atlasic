package atlasictest

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/stretchr/testify/require"
)

func TestNewCapture(t *testing.T) {
	// Test with minimal task
	task := &a2a.Task{}
	capture := NewCapture(task)
	defer capture.Cleanup()

	// Verify defaults were filled
	require.NotEmpty(t, capture.GetTaskID())
	require.NotEmpty(t, capture.GetContextID())
	require.Equal(t, a2a.TaskStateSubmitted, capture.GetInitialStatus().State)

	// Verify task has default history
	taskData, err := capture.GetTask(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, taskData.History, 1)
	require.Equal(t, a2a.RoleUser, taskData.History[0].Role)
}

func TestNewCapture_WithCompleteTask(t *testing.T) {
	// Test with complete task
	task := &a2a.Task{
		ID:        "test-task-123",
		ContextID: "test-context-456",
		Kind:      "task",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		History: []a2a.Message{
			{
				Kind:      "message",
				MessageID: "msg-1",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					a2a.NewTextPart("Custom test message"),
				},
			},
		},
		Artifacts: []a2a.Artifact{
			{
				ArtifactID: "artifact-1",
				Name:       "test-artifact",
				Parts: []a2a.Part{
					a2a.NewTextPart("test content"),
				},
			},
		},
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	capture := NewCapture(task)
	defer capture.Cleanup()

	// Verify original values are preserved
	require.Equal(t, "test-task-123", capture.GetTaskID())
	require.Equal(t, "test-context-456", capture.GetContextID())
	require.Equal(t, a2a.TaskStateWorking, capture.GetInitialStatus().State)

	taskData, err := capture.GetTask(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, taskData.History, 1)
	require.Equal(t, "Custom test message", taskData.History[0].Parts[0].Text)
	require.Len(t, taskData.Artifacts, 1)
	require.Equal(t, "test-artifact", taskData.Artifacts[0].Name)
}

func TestTaskCapture_AddMessage(t *testing.T) {
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}
	capture := NewCapture(task)
	defer capture.Cleanup()

	ctx := context.Background()
	parts := []a2a.Part{
		a2a.NewTextPart("Agent response"),
	}

	messageID, err := capture.AddMessage(ctx, parts)
	require.NoError(t, err)
	require.NotEmpty(t, messageID)

	// Check captured messages
	messages := capture.Messages()
	require.Len(t, messages, 1)
	require.Equal(t, messageID, messages[0].MessageID)
	require.Len(t, messages[0].Parts, 1)
	require.Equal(t, "Agent response", messages[0].Parts[0].Text)

	// Check task history was updated
	taskData, err := capture.GetTask(ctx, 0)
	require.NoError(t, err)
	require.Len(t, taskData.History, 2) // Original + new message
	lastMessage := taskData.History[len(taskData.History)-1]
	require.Equal(t, a2a.RoleAgent, lastMessage.Role)
	require.Equal(t, "Agent response", lastMessage.Parts[0].Text)
}

func TestTaskCapture_UpdateStatus(t *testing.T) {
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}
	capture := NewCapture(task)
	defer capture.Cleanup()

	ctx := context.Background()
	parts := []a2a.Part{
		a2a.NewTextPart("Task completed successfully"),
	}

	status, err := capture.UpdateStatus(ctx, a2a.TaskStateCompleted, parts)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, status.State)
	require.NotNil(t, status.Timestamp)

	// Check captured status updates
	updates := capture.StatusUpdates()
	require.Len(t, updates, 1)
	require.Equal(t, a2a.TaskStateCompleted, updates[0].State)
	require.Len(t, updates[0].Parts, 1)
	require.Equal(t, "Task completed successfully", updates[0].Parts[0].Text)

	// Check task status was updated
	taskData, err := capture.GetTask(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, taskData.Status.State)
	require.NotNil(t, taskData.Status.Message)
	require.Equal(t, "Task completed successfully", taskData.Status.Message.Parts[0].Text)
}

func TestTaskCapture_UpsertArtifact(t *testing.T) {
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}
	capture := NewCapture(task)
	defer capture.Cleanup()

	ctx := context.Background()
	artifact := a2a.Artifact{
		ArtifactID: "output-1",
		Name:       "output.txt",
		Parts: []a2a.Part{
			a2a.NewTextPart("output content"),
		},
		Metadata: map[string]interface{}{
			"type": "text",
		},
	}

	err := capture.UpsertArtifact(ctx, artifact)
	require.NoError(t, err)

	// Check captured artifacts
	artifacts := capture.Artifacts()
	require.Len(t, artifacts, 1)
	require.Equal(t, "output.txt", artifacts[0].Name)
	require.Equal(t, "output-1", artifacts[0].ArtifactID)

	// Check task artifacts were updated
	taskData, err := capture.GetTask(ctx, 0)
	require.NoError(t, err)
	require.Len(t, taskData.Artifacts, 1)
	require.Equal(t, "output.txt", taskData.Artifacts[0].Name)

	// Test updating existing artifact
	updatedArtifact := a2a.Artifact{
		ArtifactID: "output-1",
		Name:       "output.txt",
		Parts: []a2a.Part{
			a2a.NewTextPart("updated content"),
		},
	}
	err = capture.UpsertArtifact(ctx, updatedArtifact)
	require.NoError(t, err)

	// Should have 2 captured artifacts (both operations)
	artifacts = capture.Artifacts()
	require.Len(t, artifacts, 2)

	// But task should still have only 1 artifact (updated)
	taskData, err = capture.GetTask(ctx, 0)
	require.NoError(t, err)
	require.Len(t, taskData.Artifacts, 1)
	require.Equal(t, "updated content", taskData.Artifacts[0].Parts[0].Text)
}

func TestTaskCapture_FileOperations(t *testing.T) {
	task := &a2a.Task{ID: "test-task", ContextID: "test-context"}
	capture := NewCapture(task)
	defer capture.Cleanup()

	ctx := context.Background()

	// Test context file operations
	file, err := capture.OpenContextFile(ctx, "test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	require.NotNil(t, file)

	// Write some data
	writer, ok := file.(io.Writer)
	require.True(t, ok)
	_, err = writer.Write([]byte("Hello context"))
	require.NoError(t, err)
	file.Close()

	// List context files
	files, err := capture.ListContextFiles(ctx, "")
	require.NoError(t, err)
	require.Contains(t, files, "test.txt")

	// Test task file operations
	taskFile, err := capture.OpenTaskFile(ctx, "task-file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	require.NotNil(t, taskFile)

	writer, ok = taskFile.(io.Writer)
	require.True(t, ok)
	_, err = writer.Write([]byte("Hello task"))
	require.NoError(t, err)
	taskFile.Close()

	// List task files
	taskFiles, err := capture.ListTaskFiles(ctx, "")
	require.NoError(t, err)
	require.Contains(t, taskFiles, "task-file.txt")

	// Check captured file operations
	fileOps := capture.FileOperations()
	require.Len(t, fileOps, 4) // 2 opens + 2 lists

	// Find context file open
	var contextOpen *CapturedFileOp
	for _, op := range fileOps {
		if op.Operation == "open" && op.IsContext && op.Path == "test.txt" {
			contextOpen = &op
			break
		}
	}
	require.NotNil(t, contextOpen)
	require.Equal(t, os.O_CREATE|os.O_WRONLY, contextOpen.Flag)
	require.Equal(t, os.FileMode(0644), contextOpen.Perm)

	// Test delete operations
	err = capture.DeleteContextFile(ctx, "test.txt")
	require.NoError(t, err)

	err = capture.DeleteTaskFile(ctx, "task-file.txt")
	require.NoError(t, err)

	// Should have 2 more delete operations captured
	fileOps = capture.FileOperations()
	require.Len(t, fileOps, 6) // 2 opens + 2 lists + 2 deletes
}

func TestTaskCapture_GetTask_HistoryLength(t *testing.T) {
	task := &a2a.Task{
		ID:        "test-task",
		ContextID: "test-context",
		History: []a2a.Message{
			{Role: a2a.RoleUser, Parts: []a2a.Part{a2a.NewTextPart("Message 1")}},
			{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("Message 2")}},
			{Role: a2a.RoleUser, Parts: []a2a.Part{a2a.NewTextPart("Message 3")}},
		},
	}

	capture := NewCapture(task)
	defer capture.Cleanup()

	ctx := context.Background()

	// Get all history
	taskData, err := capture.GetTask(ctx, 0)
	require.NoError(t, err)
	require.Len(t, taskData.History, 3)

	// Get limited history
	taskData, err = capture.GetTask(ctx, 2)
	require.NoError(t, err)
	require.Len(t, taskData.History, 2)
	require.Equal(t, "Message 1", taskData.History[0].Parts[0].Text)
	require.Equal(t, "Message 2", taskData.History[1].Parts[0].Text)
}

func TestTaskCapture_GetIncomingMessage(t *testing.T) {
	// Test with history
	task := &a2a.Task{
		ID:        "test-task",
		ContextID: "test-context",
		History: []a2a.Message{
			{
				Role: a2a.RoleUser,
				Parts: []a2a.Part{
					a2a.NewTextPart("Incoming message"),
				},
			},
		},
	}

	capture := NewCapture(task)
	defer capture.Cleanup()

	incoming := capture.GetIncomingMessage()
	require.Equal(t, a2a.RoleUser, incoming.Role)
	require.Equal(t, "Incoming message", incoming.Parts[0].Text)

	// Test with empty history
	emptyTask := &a2a.Task{ID: "empty", ContextID: "empty"}
	emptyCapture := NewCapture(emptyTask) // This will add default history
	defer emptyCapture.Cleanup()

	incoming = emptyCapture.GetIncomingMessage()
	require.Equal(t, a2a.RoleUser, incoming.Role)
	require.Equal(t, "Test task execution", incoming.Parts[0].Text)
}

// Integration test: Use TaskCapture with a real agent
func TestTaskCapture_IntegrationWithAgent(t *testing.T) {
	// Create a test agent that uses various TaskHandle methods
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name: "Test Integration Agent",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		// Agent behavior:
		// 1. Get task info
		task, err := handle.GetTask(ctx, 10)
		if err != nil {
			return nil, err
		}

		// 2. Add a progress message
		_, err = handle.AddMessage(ctx, []a2a.Part{
			a2a.NewTextPart("Processing task: " + task.ID),
		})
		if err != nil {
			return nil, err
		}

		// 3. Create a context file
		file, err := handle.OpenContextFile(ctx, "work.txt", os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		writer := file.(io.Writer)
		writer.Write([]byte("Agent working..."))
		file.Close()

		// 4. Update status
		_, err = handle.UpdateStatus(ctx, a2a.TaskStateCompleted, []a2a.Part{
			a2a.NewTextPart("Task completed"),
		})
		if err != nil {
			return nil, err
		}

		// 5. Add artifact
		err = handle.UpsertArtifact(ctx, a2a.Artifact{
			ArtifactID: "result-1",
			Name:       "result.txt",
			Parts: []a2a.Part{
				a2a.NewTextPart("result content"),
			},
		})
		if err != nil {
			return nil, err
		}

		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Task executed successfully"),
			},
		}, nil
	})

	// Create TaskCapture
	task := &a2a.Task{
		ID:        "integration-test",
		ContextID: "integration-context",
	}
	capture := NewCapture(task)
	defer capture.Cleanup()

	// Execute agent with TaskCapture
	ctx := context.Background()
	response, err := agent.Execute(ctx, capture)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, a2a.RoleAgent, response.Role)
	require.Equal(t, "Task executed successfully", response.Parts[0].Text)

	// Verify all interactions were captured
	messages := capture.Messages()
	require.Len(t, messages, 1)
	require.Contains(t, messages[0].Parts[0].Text, "integration-test")

	statusUpdates := capture.StatusUpdates()
	require.Len(t, statusUpdates, 1)
	require.Equal(t, a2a.TaskStateCompleted, statusUpdates[0].State)

	artifacts := capture.Artifacts()
	require.Len(t, artifacts, 1)
	require.Equal(t, "result.txt", artifacts[0].Name)

	fileOps := capture.FileOperations()
	require.NotEmpty(t, fileOps)

	// Find the file creation operation
	var fileCreate *CapturedFileOp
	for _, op := range fileOps {
		if op.Operation == "open" && op.Path == "work.txt" && op.IsContext {
			fileCreate = &op
			break
		}
	}
	require.NotNil(t, fileCreate)
	require.Equal(t, os.O_CREATE|os.O_WRONLY, fileCreate.Flag)

	// Verify task state was updated
	finalTask := capture.Task()
	require.Equal(t, a2a.TaskStateCompleted, finalTask.Status.State)
	require.Len(t, finalTask.Artifacts, 1)
}
