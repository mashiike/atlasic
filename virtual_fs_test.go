package atlasic

import (
	"context"
	"io"
	"os"
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

		// Test OpenContextFile for writing
		data := []byte("test context file content")
		file, err := storage.OpenContextFile(ctx, contextID, "shared/plan.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer := file.(io.Writer)
		_, err = writer.Write(data)
		require.NoError(t, err)
		file.Close()

		// Test hierarchical path
		file2, err := storage.OpenContextFile(ctx, contextID, "shared/resources/doc1.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer2 := file2.(io.Writer)
		_, err = writer2.Write([]byte("document content"))
		require.NoError(t, err)
		file2.Close()

		// Test OpenContextFile for reading
		file3, err := storage.OpenContextFile(ctx, contextID, "shared/plan.json", os.O_RDONLY, 0)
		require.NoError(t, err)
		reader := file3.(io.Reader)
		retrievedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		file3.Close()
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
		_, err = storage.OpenContextFile(ctx, contextID, "shared/plan.json", os.O_RDONLY, 0)
		require.Error(t, err)
	})

	t.Run("Task file operations", func(t *testing.T) {
		taskID := "test-task-001"

		// Test OpenTaskFile for writing
		data := []byte("test task file content")
		file, err := storage.OpenTaskFile(ctx, taskID, "work/analysis.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer := file.(io.Writer)
		_, err = writer.Write(data)
		require.NoError(t, err)
		file.Close()

		// Test hierarchical path
		file2, err := storage.OpenTaskFile(ctx, taskID, "work/output/result.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer2 := file2.(io.Writer)
		_, err = writer2.Write([]byte(`{"result": "success"}`))
		require.NoError(t, err)
		file2.Close()

		// Test OpenTaskFile for reading
		file3, err := storage.OpenTaskFile(ctx, taskID, "work/analysis.txt", os.O_RDONLY, 0)
		require.NoError(t, err)
		reader := file3.(io.Reader)
		retrievedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		file3.Close()
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
		_, err = storage.OpenTaskFile(ctx, taskID, "work/analysis.txt", os.O_RDONLY, 0)
		require.Error(t, err)
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
		// Test OpenContextFile for writing
		data := []byte("context data from taskhandle")
		file, err := handle.OpenContextFile(ctx, "shared/state.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer := file.(io.Writer)
		_, err = writer.Write(data)
		require.NoError(t, err)
		file.Close()

		// Test OpenContextFile for reading
		file2, err := handle.OpenContextFile(ctx, "shared/state.json", os.O_RDONLY, 0)
		require.NoError(t, err)
		reader := file2.(io.Reader)
		retrievedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		file2.Close()
		require.Equal(t, data, retrievedData)

		// Test ListContextFiles
		files, err := handle.ListContextFiles(ctx, "shared/")
		require.NoError(t, err)
		require.Contains(t, files, "shared/state.json")

		// Test DeleteContextFile
		err = handle.DeleteContextFile(ctx, "shared/state.json")
		require.NoError(t, err)

		// Verify deletion
		_, err = handle.OpenContextFile(ctx, "shared/state.json", os.O_RDONLY, 0)
		require.Error(t, err)
	})

	t.Run("Task file operations via TaskHandle", func(t *testing.T) {
		// Test OpenTaskFile for writing
		data := []byte("task data from taskhandle")
		file, err := handle.OpenTaskFile(ctx, "temp/workspace.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		require.NoError(t, err)
		writer := file.(io.Writer)
		_, err = writer.Write(data)
		require.NoError(t, err)
		file.Close()

		// Test OpenTaskFile for reading
		file2, err := handle.OpenTaskFile(ctx, "temp/workspace.json", os.O_RDONLY, 0)
		require.NoError(t, err)
		reader := file2.(io.Reader)
		retrievedData, err := io.ReadAll(reader)
		require.NoError(t, err)
		file2.Close()
		require.Equal(t, data, retrievedData)

		// Test ListTaskFiles
		files, err := handle.ListTaskFiles(ctx, "temp/")
		require.NoError(t, err)
		require.Contains(t, files, "temp/workspace.json")

		// Test DeleteTaskFile
		err = handle.DeleteTaskFile(ctx, "temp/workspace.json")
		require.NoError(t, err)

		// Verify deletion
		_, err = handle.OpenTaskFile(ctx, "temp/workspace.json", os.O_RDONLY, 0)
		require.Error(t, err)
	})
}
