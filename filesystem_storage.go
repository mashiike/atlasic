package atlasic

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mashiike/atlasic/a2a"
)

// FileSystemStorage implements Storage interface using filesystem with JSON files
type FileSystemStorage struct {
	basePath string
	mu       sync.RWMutex
}

// NewFileSystemStorage creates a new FileSystemStorage instance
func NewFileSystemStorage(basePath string) (*FileSystemStorage, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &FileSystemStorage{
		basePath: basePath,
	}, nil
}

// Task operations (Query side)

func (fs *FileSystemStorage) GetTask(ctx context.Context, taskID string, historyLength int) (*a2a.Task, uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	taskPath := fs.getTaskPath(taskID)
	data, err := os.ReadFile(taskPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrTaskNotFound
		}
		return nil, 0, fmt.Errorf("failed to read task file: %w", err)
	}

	var taskData struct {
		Task    *a2a.Task `json:"task"`
		Version uint64    `json:"version"`
	}

	if err := json.Unmarshal(data, &taskData); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	task := taskData.Task
	if historyLength > 0 && len(task.History) > historyLength {
		// Limit history length
		task.History = task.History[len(task.History)-historyLength:]
	}

	return task, taskData.Version, nil
}

func (fs *FileSystemStorage) SaveTask(ctx context.Context, task *a2a.Task, expectedVersion, newVersion uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check current version for optimistic concurrency control
	if existingTask, currentVersion, err := fs.getTaskWithoutLock(task.ID); err == nil && existingTask != nil {
		if expectedVersion != currentVersion {
			return fmt.Errorf("version mismatch: expected %d, got %d", expectedVersion, currentVersion)
		}
	} else if err != ErrTaskNotFound {
		return fmt.Errorf("failed to check current version: %w", err)
	}
	// If task doesn't exist and expectedVersion is 0, that's ok (new task)

	// Save with the provided new version
	taskData := struct {
		Task    *a2a.Task `json:"task"`
		Version uint64    `json:"version"`
	}{
		Task:    task,
		Version: newVersion,
	}

	data, err := json.MarshalIndent(taskData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	taskPath := fs.getTaskPath(task.ID)
	if err := os.MkdirAll(filepath.Dir(taskPath), 0750); err != nil {
		return fmt.Errorf("failed to create task directory: %w", err)
	}

	if err := os.WriteFile(taskPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write task file: %w", err)
	}

	return nil
}

func (fs *FileSystemStorage) ListTasksByContext(ctx context.Context, contextID string, historyLength int) ([]*a2a.Task, []uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	contextPath := fs.getContextPath(contextID)
	if _, err := os.Stat(contextPath); os.IsNotExist(err) {
		return nil, nil, ErrContextNotFound
	}

	entries, err := os.ReadDir(contextPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read context directory: %w", err)
	}

	var tasks []*a2a.Task
	var versions []uint64

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			taskID := strings.TrimSuffix(entry.Name(), ".json")
			if task, version, err := fs.getTaskWithoutLock(taskID); err == nil {
				if historyLength > 0 && len(task.History) > historyLength {
					task.History = task.History[len(task.History)-historyLength:]
				}
				tasks = append(tasks, task)
				versions = append(versions, version)
			}
		}
	}

	return tasks, versions, nil
}

// Event stream operations (Command side)

func (fs *FileSystemStorage) Append(ctx context.Context, contextID string, taskID string, expected uint64, events []a2a.StreamResponse) (next uint64, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	eventsPath := fs.getEventsPath(contextID, taskID)

	// Read existing events
	var existingEvents []a2a.StreamResponse
	var currentVersion uint64 = 0

	if data, err := os.ReadFile(eventsPath); err == nil {
		var eventsData struct {
			Events  []a2a.StreamResponse `json:"events"`
			Version uint64               `json:"version"`
		}
		if err := json.Unmarshal(data, &eventsData); err == nil {
			existingEvents = eventsData.Events
			currentVersion = eventsData.Version
		}
	}

	// Check expected version
	if expected != currentVersion {
		return 0, fmt.Errorf("version mismatch: expected %d, got %d", expected, currentVersion)
	}

	// Append new events
	allEvents := append(existingEvents, events...)
	newVersion := currentVersion + uint64(len(events))

	eventsData := struct {
		Events  []a2a.StreamResponse `json:"events"`
		Version uint64               `json:"version"`
	}{
		Events:  allEvents,
		Version: newVersion,
	}

	data, err := json.MarshalIndent(eventsData, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal events: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(eventsPath), 0750); err != nil {
		return 0, fmt.Errorf("failed to create events directory: %w", err)
	}

	if err := os.WriteFile(eventsPath, data, 0600); err != nil {
		return 0, fmt.Errorf("failed to write events file: %w", err)
	}

	return newVersion, nil
}

func (fs *FileSystemStorage) Load(ctx context.Context, contextID string, taskID string, from uint64, limit int) ([]a2a.StreamResponse, uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	eventsPath := fs.getEventsPath(contextID, taskID)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []a2a.StreamResponse{}, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to read events file: %w", err)
	}

	var eventsData struct {
		Events  []a2a.StreamResponse `json:"events"`
		Version uint64               `json:"version"`
	}

	if err := json.Unmarshal(data, &eventsData); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal events: %w", err)
	}

	// Filter events from specified version
	var filteredEvents []a2a.StreamResponse
	for i, event := range eventsData.Events {
		if uint64(i) >= from {
			filteredEvents = append(filteredEvents, event)
			if limit > 0 && len(filteredEvents) >= limit {
				break
			}
		}
	}

	return filteredEvents, eventsData.Version, nil
}

// Push notification configuration operations

func (fs *FileSystemStorage) SaveTaskPushNotificationConfig(ctx context.Context, config a2a.TaskPushNotificationConfig) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	configPath := fs.getPushNotificationConfigPath(config.TaskID, config.PushNotificationConfig.ID)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal push notification config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		return fmt.Errorf("failed to create push notification config directory: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write push notification config file: %w", err)
	}

	return nil
}

func (fs *FileSystemStorage) GetTaskPushNotificationConfig(ctx context.Context, taskID, configID string) (a2a.TaskPushNotificationConfig, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	configPath := fs.getPushNotificationConfigPath(taskID, configID)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return a2a.TaskPushNotificationConfig{}, ErrPushNotificationConfigNotFound
		}
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to read push notification config file: %w", err)
	}

	var config a2a.TaskPushNotificationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to unmarshal push notification config: %w", err)
	}

	return config, nil
}

func (fs *FileSystemStorage) ListTaskPushNotificationConfig(ctx context.Context, taskID string) ([]a2a.TaskPushNotificationConfig, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	configDir := fs.getPushNotificationConfigDir(taskID)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []a2a.TaskPushNotificationConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read push notification config directory: %w", err)
	}

	var configs []a2a.TaskPushNotificationConfig
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			configID := strings.TrimSuffix(entry.Name(), ".json")
			if config, err := fs.getTaskPushNotificationConfigWithoutLock(taskID, configID); err == nil {
				configs = append(configs, config)
			}
		}
	}

	return configs, nil
}

func (fs *FileSystemStorage) DeleteTaskPushNotificationConfig(ctx context.Context, taskID, configID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	configPath := fs.getPushNotificationConfigPath(taskID, configID)
	if err := os.Remove(configPath); err != nil {
		if os.IsNotExist(err) {
			return ErrPushNotificationConfigNotFound
		}
		return fmt.Errorf("failed to delete push notification config file: %w", err)
	}

	return nil
}

// Context virtual filesystem operations

func (fs *FileSystemStorage) OpenContextFile(ctx context.Context, contextID, path string, flag int, perm os.FileMode) (fs.File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := fs.getContextFilePath(contextID, path)
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		return nil, fmt.Errorf("failed to create context file directory: %w", err)
	}

	file, err := os.OpenFile(filePath, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open context file: %w", err)
	}

	return file, nil
}

func (fs *FileSystemStorage) ListContextFiles(ctx context.Context, contextID, pathPrefix string) ([]string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	baseDir := fs.getContextFileDir(contextID)
	if pathPrefix == "" {
		pathPrefix = "."
	}
	prefixDir := filepath.Join(baseDir, pathPrefix)

	var files []string
	err := filepath.WalkDir(prefixDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relPath))
		}
		return nil
	})

	if err != nil && os.IsNotExist(err) {
		return []string{}, nil
	}

	return files, err
}

func (fs *FileSystemStorage) DeleteContextFile(ctx context.Context, contextID, path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := fs.getContextFilePath(contextID, path)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf("failed to delete context file: %w", err)
	}

	return nil
}

// Task virtual filesystem operations

func (fs *FileSystemStorage) OpenTaskFile(ctx context.Context, taskID, path string, flag int, perm os.FileMode) (fs.File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := fs.getTaskFilePath(taskID, path)
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		return nil, fmt.Errorf("failed to create task file directory: %w", err)
	}

	file, err := os.OpenFile(filePath, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open task file: %w", err)
	}

	return file, nil
}

func (fs *FileSystemStorage) ListTaskFiles(ctx context.Context, taskID, pathPrefix string) ([]string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	baseDir := fs.getTaskFileDir(taskID)
	if pathPrefix == "" {
		pathPrefix = "."
	}
	prefixDir := filepath.Join(baseDir, pathPrefix)

	var files []string
	err := filepath.WalkDir(prefixDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relPath))
		}
		return nil
	})

	if err != nil && os.IsNotExist(err) {
		return []string{}, nil
	}

	return files, err
}

func (fs *FileSystemStorage) DeleteTaskFile(ctx context.Context, taskID, path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := fs.getTaskFilePath(taskID, path)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf("failed to delete task file: %w", err)
	}

	return nil
}

// Helper methods

func (fs *FileSystemStorage) getTaskPath(taskID string) string {
	return filepath.Join(fs.basePath, "tasks", taskID+".json")
}

func (fs *FileSystemStorage) getContextPath(contextID string) string {
	return filepath.Join(fs.basePath, "contexts", contextID)
}

func (fs *FileSystemStorage) getEventsPath(contextID, taskID string) string {
	return filepath.Join(fs.basePath, "events", contextID, taskID+".json")
}

func (fs *FileSystemStorage) getPushNotificationConfigDir(taskID string) string {
	return filepath.Join(fs.basePath, "push-configs", taskID)
}

func (fs *FileSystemStorage) getPushNotificationConfigPath(taskID, configID string) string {
	return filepath.Join(fs.getPushNotificationConfigDir(taskID), configID+".json")
}

func (fs *FileSystemStorage) getTaskWithoutLock(taskID string) (*a2a.Task, uint64, error) {
	taskPath := fs.getTaskPath(taskID)
	data, err := os.ReadFile(taskPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrTaskNotFound
		}
		return nil, 0, fmt.Errorf("failed to read task file: %w", err)
	}

	var taskData struct {
		Task    *a2a.Task `json:"task"`
		Version uint64    `json:"version"`
	}

	if err := json.Unmarshal(data, &taskData); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return taskData.Task, taskData.Version, nil
}

func (fs *FileSystemStorage) getTaskPushNotificationConfigWithoutLock(taskID, configID string) (a2a.TaskPushNotificationConfig, error) {
	configPath := fs.getPushNotificationConfigPath(taskID, configID)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return a2a.TaskPushNotificationConfig{}, ErrPushNotificationConfigNotFound
		}
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to read push notification config file: %w", err)
	}

	var config a2a.TaskPushNotificationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to unmarshal push notification config: %w", err)
	}

	return config, nil
}

func (fs *FileSystemStorage) getContextFileDir(contextID string) string {
	return filepath.Join(fs.basePath, "fs", "context", contextID)
}

func (fs *FileSystemStorage) getContextFilePath(contextID, path string) string {
	return filepath.Join(fs.getContextFileDir(contextID), path)
}

func (fs *FileSystemStorage) getTaskFileDir(taskID string) string {
	return filepath.Join(fs.basePath, "fs", "task", taskID)
}

func (fs *FileSystemStorage) getTaskFilePath(taskID, path string) string {
	return filepath.Join(fs.getTaskFileDir(taskID), path)
}
