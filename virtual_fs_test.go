package atlasic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileSystemStorage_VirtualFS(t *testing.T) {
	// Create FileSystemStorage
	storage, err := NewFileSystemStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Context file operations", func(t *testing.T) {
		contextID := "test-context-001"

		// Test PutContextFile
		data := []byte("test context file content")
		err := storage.PutContextFile(ctx, contextID, "shared/plan.json", data)
		require.NoError(t, err)

		// Test hierarchical path
		err = storage.PutContextFile(ctx, contextID, "shared/resources/doc1.txt", []byte("document content"))
		require.NoError(t, err)

		// Test GetContextFile
		retrievedData, err := storage.GetContextFile(ctx, contextID, "shared/plan.json")
		require.NoError(t, err)
		require.Equal(t, data, retrievedData)

		// Test ListContextFiles
		files, err := storage.ListContextFiles(ctx, contextID, "shared/")
		require.NoError(t, err)
		require.Len(t, files, 2)
		require.Contains(t, files, "shared/plan.json")
		require.Contains(t, files, "shared/resources/doc1.txt")

		// Test DeleteContextFile
		err = storage.DeleteContextFile(ctx, contextID, "shared/plan.json")
		require.NoError(t, err)

		// Verify deletion
		_, err = storage.GetContextFile(ctx, contextID, "shared/plan.json")
		require.Equal(t, ErrFileNotFound, err)
	})

	t.Run("Task file operations", func(t *testing.T) {
		taskID := "test-task-001"

		// Test PutTaskFile
		data := []byte("test task file content")
		err := storage.PutTaskFile(ctx, taskID, "work/analysis.txt", data)
		require.NoError(t, err)

		// Test hierarchical path
		err = storage.PutTaskFile(ctx, taskID, "work/output/result.json", []byte(`{"result": "success"}`))
		require.NoError(t, err)

		// Test GetTaskFile
		retrievedData, err := storage.GetTaskFile(ctx, taskID, "work/analysis.txt")
		require.NoError(t, err)
		require.Equal(t, data, retrievedData)

		// Test ListTaskFiles
		files, err := storage.ListTaskFiles(ctx, taskID, "work/")
		require.NoError(t, err)
		require.Len(t, files, 2)
		require.Contains(t, files, "work/analysis.txt")
		require.Contains(t, files, "work/output/result.json")

		// Test DeleteTaskFile
		err = storage.DeleteTaskFile(ctx, taskID, "work/analysis.txt")
		require.NoError(t, err)

		// Verify deletion
		_, err = storage.GetTaskFile(ctx, taskID, "work/analysis.txt")
		require.Equal(t, ErrFileNotFound, err)
	})
}

func TestTaskHandle_VirtualFS(t *testing.T) {
	storage, err := NewFileSystemStorage(t.TempDir())
	require.NoError(t, err)

	service := NewAgentService(storage, nil)

	ctx := context.Background()
	contextID := "test-context-002"
	taskID := "test-task-002"

	// Create TaskHandle
	handle := service.NewTaskHandle(ctx, TaskHandleParams{
		ContextID: contextID,
		TaskID:    taskID,
	})

	t.Run("Context file operations via TaskHandle", func(t *testing.T) {
		// Test PutContextFile
		data := []byte("context data from taskhandle")
		err := handle.PutContextFile(ctx, "shared/state.json", data)
		require.NoError(t, err)

		// Test GetContextFile
		retrievedData, err := handle.GetContextFile(ctx, "shared/state.json")
		require.NoError(t, err)
		require.Equal(t, data, retrievedData)

		// Test ListContextFiles
		files, err := handle.ListContextFiles(ctx, "shared/")
		require.NoError(t, err)
		require.Contains(t, files, "shared/state.json")

		// Test DeleteContextFile
		err = handle.DeleteContextFile(ctx, "shared/state.json")
		require.NoError(t, err)

		// Verify deletion
		_, err = handle.GetContextFile(ctx, "shared/state.json")
		require.Equal(t, ErrFileNotFound, err)
	})

	t.Run("Task file operations via TaskHandle", func(t *testing.T) {
		// Test PutTaskFile
		data := []byte("task data from taskhandle")
		err := handle.PutTaskFile(ctx, "temp/workspace.json", data)
		require.NoError(t, err)

		// Test GetTaskFile
		retrievedData, err := handle.GetTaskFile(ctx, "temp/workspace.json")
		require.NoError(t, err)
		require.Equal(t, data, retrievedData)

		// Test ListTaskFiles
		files, err := handle.ListTaskFiles(ctx, "temp/")
		require.NoError(t, err)
		require.Contains(t, files, "temp/workspace.json")

		// Test DeleteTaskFile
		err = handle.DeleteTaskFile(ctx, "temp/workspace.json")
		require.NoError(t, err)

		// Verify deletion
		_, err = handle.GetTaskFile(ctx, "temp/workspace.json")
		require.Equal(t, ErrFileNotFound, err)
	})
}