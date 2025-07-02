package awsadp

import (
	"context"
	"os"
	"testing"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
)

func TestS3Storage_Integration(t *testing.T) {
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

	// Create S3Storage instance with random prefix
	storage, err := NewS3StorageForTesting(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create S3Storage: %v", err)
	}

	// Cleanup after test
	defer func() {
		if err := CleanupTestObjects(ctx, client, cfg.Bucket, storage.prefix); err != nil {
			t.Logf("Warning: Failed to cleanup test objects: %v", err)
		}
	}()

	// Run storage interface tests
	t.Run("TaskOperations", func(t *testing.T) {
		testTaskOperations(t, storage)
	})

	t.Run("EventStreamOperations", func(t *testing.T) {
		testEventStreamOperations(t, storage)
	})

	t.Run("PushNotificationConfigOperations", func(t *testing.T) {
		testPushNotificationConfigOperations(t, storage)
	})
}

func testTaskOperations(t *testing.T, storage atlasic.Storage) {
	ctx := context.Background()

	// Test GetTask for non-existent task
	_, _, err := storage.GetTask(ctx, "non-existent", atlasic.HistoryLengthAll)
	if err != atlasic.ErrTaskNotFound {
		t.Errorf("Expected ErrTaskNotFound, got: %v", err)
	}

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
				MessageID: "test-msg-1",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{
						Kind: a2a.KindTextPart,
						Text: "Hello, world!",
					},
				},
			},
		},
	}

	// Save the task
	err = storage.SaveTask(ctx, task, 0, 1)
	if err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Retrieve the task
	retrievedTask, version, err := storage.GetTask(ctx, task.ID, atlasic.HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if version != 1 {
		t.Errorf("Expected version 1, got: %d", version)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected task ID %s, got: %s", task.ID, retrievedTask.ID)
	}

	if len(retrievedTask.History) != 1 {
		t.Errorf("Expected 1 history item, got: %d", len(retrievedTask.History))
	}

	// Test history length limiting
	limitedTask, _, err := storage.GetTask(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("Failed to get task with history limit: %v", err)
	}

	if len(limitedTask.History) != 0 {
		t.Errorf("Expected 0 history items with limit 0, got: %d", len(limitedTask.History))
	}

	// Test ListTasksByContext
	tasks, versions, err := storage.ListTasksByContext(ctx, task.ContextID, atlasic.HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to list tasks by context: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task in context, got: %d", len(tasks))
	}

	if len(versions) != 1 {
		t.Errorf("Expected 1 version in context, got: %d", len(versions))
	}

	// Test optimistic concurrency control
	err = storage.SaveTask(ctx, task, 0, 2) // Wrong expected version
	if err == nil {
		t.Error("Expected version mismatch error, got nil")
	}
}

func testEventStreamOperations(t *testing.T, storage atlasic.Storage) {
	ctx := context.Background()
	contextID := "test-context-events"
	taskID := "test-task-events"

	// Test Load for non-existent stream
	events, version, err := storage.Load(ctx, contextID, taskID, 0, 10)
	if err != nil {
		t.Fatalf("Failed to load non-existent stream: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events for non-existent stream, got: %d", len(events))
	}

	if version != 0 {
		t.Errorf("Expected version 0 for non-existent stream, got: %d", version)
	}

	// Create test events
	testEvents := []a2a.StreamResponse{
		{
			Message: &a2a.Message{
				Kind:      a2a.KindMessage,
				MessageID: "test-msg-1",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{
						Kind: a2a.KindTextPart,
						Text: "Event 1",
					},
				},
			},
		},
		{
			Message: &a2a.Message{
				Kind:      a2a.KindMessage,
				MessageID: "test-msg-2",
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					{
						Kind: a2a.KindTextPart,
						Text: "Event 2",
					},
				},
			},
		},
	}

	// Append events
	newVersion, err := storage.Append(ctx, contextID, taskID, 0, testEvents)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}

	if newVersion != 2 {
		t.Errorf("Expected version 2 after appending 2 events, got: %d", newVersion)
	}

	// Load events
	loadedEvents, loadedVersion, err := storage.Load(ctx, contextID, taskID, 0, 10)
	if err != nil {
		t.Fatalf("Failed to load events: %v", err)
	}

	if loadedVersion != 2 {
		t.Errorf("Expected loaded version 2, got: %d", loadedVersion)
	}

	if len(loadedEvents) != 2 {
		t.Errorf("Expected 2 loaded events, got: %d", len(loadedEvents))
	}

	// Test pagination
	partialEvents, _, err := storage.Load(ctx, contextID, taskID, 1, 1)
	if err != nil {
		t.Fatalf("Failed to load partial events: %v", err)
	}

	if len(partialEvents) != 1 {
		t.Errorf("Expected 1 partial event, got: %d", len(partialEvents))
	}

	if len(partialEvents[0].Message.Parts) == 0 || partialEvents[0].Message.Parts[0].Text != "Event 2" {
		if len(partialEvents[0].Message.Parts) > 0 {
			t.Errorf("Expected 'Event 2', got: %s", partialEvents[0].Message.Parts[0].Text)
		} else {
			t.Error("Expected message parts, got empty")
		}
	}
}

func testPushNotificationConfigOperations(t *testing.T, storage atlasic.Storage) {
	ctx := context.Background()
	taskID := "test-task-push"

	// Test GetTaskPushNotificationConfig for non-existent config
	_, err := storage.GetTaskPushNotificationConfig(ctx, taskID, "non-existent")
	if err != atlasic.ErrPushNotificationConfigNotFound {
		t.Errorf("Expected ErrPushNotificationConfigNotFound, got: %v", err)
	}

	// Create test config
	config := a2a.TaskPushNotificationConfig{
		TaskID: taskID,
		PushNotificationConfig: a2a.PushNotificationConfig{
			ID:  "test-config-123",
			URL: "https://example.com/webhook",
		},
	}

	// Save config
	err = storage.SaveTaskPushNotificationConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to save push notification config: %v", err)
	}

	// Retrieve config
	retrievedConfig, err := storage.GetTaskPushNotificationConfig(ctx, taskID, config.PushNotificationConfig.ID)
	if err != nil {
		t.Fatalf("Failed to get push notification config: %v", err)
	}

	if retrievedConfig.PushNotificationConfig.URL != config.PushNotificationConfig.URL {
		t.Errorf("Expected URL %s, got: %s", config.PushNotificationConfig.URL, retrievedConfig.PushNotificationConfig.URL)
	}

	// List configs
	configs, err := storage.ListTaskPushNotificationConfig(ctx, taskID)
	if err != nil {
		t.Fatalf("Failed to list push notification configs: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("Expected 1 config, got: %d", len(configs))
	}

	// Delete config
	err = storage.DeleteTaskPushNotificationConfig(ctx, taskID, config.PushNotificationConfig.ID)
	if err != nil {
		t.Fatalf("Failed to delete push notification config: %v", err)
	}

	// Verify deletion
	_, err = storage.GetTaskPushNotificationConfig(ctx, taskID, config.PushNotificationConfig.ID)
	if err != atlasic.ErrPushNotificationConfigNotFound {
		t.Errorf("Expected ErrPushNotificationConfigNotFound after deletion, got: %v", err)
	}
}