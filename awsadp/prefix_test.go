package awsadp

import (
	"context"
	"os"
	"testing"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
)

func TestS3Storage_EmptyPrefix(t *testing.T) {
	// Skip integration tests if minio is not available
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Integration tests are disabled")
	}

	ctx := context.Background()
	cfg := DefaultTestingConfig()

	// Create S3 client and ensure bucket exists
	client, err := NewS3ClientForTesting(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create S3 client: %v", err)
	}

	if err := EnsureBucketExists(ctx, client, cfg.Bucket); err != nil {
		t.Fatalf("Failed to ensure bucket exists: %v", err)
	}

	// Create S3Storage instance WITHOUT prefix (empty string)
	storage := NewS3Storage(S3StorageConfig{
		Client: client,
		Bucket: cfg.Bucket,
		Prefix: "", // Explicitly empty prefix
	})

	// Cleanup after test
	defer func() {
		if err := CleanupTestObjects(ctx, client, cfg.Bucket, ""); err != nil {
			t.Logf("Warning: Failed to cleanup test objects: %v", err)
		}
	}()

	// Test basic operations with empty prefix
	t.Run("TaskOperationsWithEmptyPrefix", func(t *testing.T) {
		testBasicTaskOperations(t, storage)
	})

	t.Run("EventStreamOperationsWithEmptyPrefix", func(t *testing.T) {
		testBasicEventOperations(t, storage)
	})
}

func testBasicTaskOperations(t *testing.T, storage atlasic.Storage) {
	ctx := context.Background()

	// Create a test task
	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "empty-prefix-task-123",
		ContextID: "empty-prefix-context-456",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateWorking,
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "test-msg-1",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{
						Kind: a2a.KindTextPart,
						Text: "Hello from empty prefix!",
					},
				},
			},
		},
	}

	// Save the task
	err := storage.SaveTask(ctx, task, 0, 1)
	if err != nil {
		t.Fatalf("Failed to save task with empty prefix: %v", err)
	}

	// Retrieve the task
	retrievedTask, version, err := storage.GetTask(ctx, task.ID, atlasic.HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to get task with empty prefix: %v", err)
	}

	if version != 1 {
		t.Errorf("Expected version 1, got: %d", version)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected task ID %s, got: %s", task.ID, retrievedTask.ID)
	}

	// Test ListTasksByContext with empty prefix
	tasks, versions, err := storage.ListTasksByContext(ctx, task.ContextID, atlasic.HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to list tasks by context with empty prefix: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task in context, got: %d", len(tasks))
	}

	if len(versions) != 1 {
		t.Errorf("Expected 1 version in context, got: %d", len(versions))
	}

	t.Logf("✅ Empty prefix operations successful - Task saved and retrieved correctly")
}

func testBasicEventOperations(t *testing.T, storage atlasic.Storage) {
	ctx := context.Background()
	contextID := "empty-prefix-events-context"
	taskID := "empty-prefix-events-task"

	// Create test events
	testEvents := []a2a.StreamResponse{
		{
			Message: &a2a.Message{
				Kind:      a2a.KindMessage,
				MessageID: "empty-prefix-msg-1",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{
						Kind: a2a.KindTextPart,
						Text: "Event from empty prefix",
					},
				},
			},
		},
	}

	// Append events
	newVersion, err := storage.Append(ctx, contextID, taskID, 0, testEvents)
	if err != nil {
		t.Fatalf("Failed to append events with empty prefix: %v", err)
	}

	if newVersion != 1 {
		t.Errorf("Expected version 1 after appending 1 event, got: %d", newVersion)
	}

	// Load events
	loadedEvents, loadedVersion, err := storage.Load(ctx, contextID, taskID, 0, 10)
	if err != nil {
		t.Fatalf("Failed to load events with empty prefix: %v", err)
	}

	if loadedVersion != 1 {
		t.Errorf("Expected loaded version 1, got: %d", loadedVersion)
	}

	if len(loadedEvents) != 1 {
		t.Errorf("Expected 1 loaded event, got: %d", len(loadedEvents))
	}

	t.Logf("✅ Empty prefix event operations successful - Events saved and retrieved correctly")
}