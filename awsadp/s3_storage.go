package awsadp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
)

// S3Storage implements Storage interface using AWS S3
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string // Object key prefix for namespacing (useful for testing)
}

// S3StorageConfig provides configuration for S3Storage
type S3StorageConfig struct {
	Client *s3.Client
	Bucket string
	Prefix string // Optional prefix for all object keys (useful for testing isolation)
}

// NewS3Storage creates a new S3Storage instance
func NewS3Storage(config S3StorageConfig) *S3Storage {
	return &S3Storage{
		client: config.Client,
		bucket: config.Bucket,
		prefix: config.Prefix,
	}
}

// Task operations (Query side)

func (s *S3Storage) GetTask(ctx context.Context, taskID string, historyLength int) (*a2a.Task, uint64, error) {
	key := s.getTaskKey(taskID)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, 0, atlasic.ErrTaskNotFound
		}
		return nil, 0, fmt.Errorf("failed to get task from S3: %w", err)
	}
	defer result.Body.Close()

	var taskData struct {
		Task    *a2a.Task `json:"task"`
		Version uint64    `json:"version"`
	}

	if err := json.NewDecoder(result.Body).Decode(&taskData); err != nil {
		return nil, 0, fmt.Errorf("failed to decode task: %w", err)
	}

	task := taskData.Task
	if historyLength == 0 {
		// Return empty history
		task.History = []a2a.Message{}
	} else if historyLength > 0 && len(task.History) > historyLength {
		// Limit history length
		task.History = task.History[len(task.History)-historyLength:]
	}

	return task, taskData.Version, nil
}

func (s *S3Storage) SaveTask(ctx context.Context, task *a2a.Task, expectedVersion, newVersion uint64) error {
	// Check current version for optimistic concurrency control
	if existingTask, currentVersion, err := s.GetTask(ctx, task.ID, atlasic.HistoryLengthAll); err == nil && existingTask != nil {
		if expectedVersion != currentVersion {
			return fmt.Errorf("version mismatch: expected %d, got %d", expectedVersion, currentVersion)
		}
	} else if err != atlasic.ErrTaskNotFound {
		return fmt.Errorf("failed to check current version: %w", err)
	}
	// If task doesn't exist and expectedVersion is 0, that's ok (new task)

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

	key := s.getTaskKey(task.ID)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to put task to S3: %w", err)
	}

	// Create context index entry
	contextKey := s.getContextIndexKey(task.ContextID, task.ID)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(contextKey),
		Body:   bytes.NewReader([]byte(task.ID)),
	})
	if err != nil {
		return fmt.Errorf("failed to put context index to S3: %w", err)
	}

	return nil
}

func (s *S3Storage) ListTasksByContext(ctx context.Context, contextID string, historyLength int) ([]*a2a.Task, []uint64, error) {
	prefix := s.getContextPrefix(contextID)

	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list context objects: %w", err)
	}

	if len(result.Contents) == 0 {
		return nil, nil, atlasic.ErrContextNotFound
	}

	var tasks []*a2a.Task
	var versions []uint64

	for _, obj := range result.Contents {
		// Extract taskID from object key
		taskID := s.extractTaskIDFromContextKey(*obj.Key)
		if taskID == "" {
			continue
		}

		if task, version, err := s.GetTask(ctx, taskID, historyLength); err == nil {
			tasks = append(tasks, task)
			versions = append(versions, version)
		}
	}

	return tasks, versions, nil
}

// Event stream operations (Command side)

func (s *S3Storage) Append(ctx context.Context, contextID string, taskID string, expected uint64, events []a2a.StreamResponse) (next uint64, err error) {
	key := s.getEventsKey(contextID, taskID)

	// Read existing events
	var existingEvents []a2a.StreamResponse
	var currentVersion uint64 = 0

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		var eventsData struct {
			Events  []a2a.StreamResponse `json:"events"`
			Version uint64               `json:"version"`
		}
		if err := json.NewDecoder(result.Body).Decode(&eventsData); err == nil {
			existingEvents = eventsData.Events
			currentVersion = eventsData.Version
		}
		result.Body.Close()
	} else {
		// Check if it's a "not found" error, which is expected for new event streams
		if !strings.Contains(err.Error(), "NoSuchKey") {
			return 0, fmt.Errorf("failed to get existing events: %w", err)
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

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to put events to S3: %w", err)
	}

	return newVersion, nil
}

func (s *S3Storage) Load(ctx context.Context, contextID string, taskID string, from uint64, limit int) ([]a2a.StreamResponse, uint64, error) {
	key := s.getEventsKey(contextID, taskID)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return []a2a.StreamResponse{}, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to get events from S3: %w", err)
	}
	defer result.Body.Close()

	var eventsData struct {
		Events  []a2a.StreamResponse `json:"events"`
		Version uint64               `json:"version"`
	}

	if err := json.NewDecoder(result.Body).Decode(&eventsData); err != nil {
		return nil, 0, fmt.Errorf("failed to decode events: %w", err)
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

func (s *S3Storage) SaveTaskPushNotificationConfig(ctx context.Context, config a2a.TaskPushNotificationConfig) error {
	key := s.getPushNotificationConfigKey(config.TaskID, config.PushNotificationConfig.ID)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal push notification config: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to put push notification config to S3: %w", err)
	}

	return nil
}

func (s *S3Storage) GetTaskPushNotificationConfig(ctx context.Context, taskID, configID string) (a2a.TaskPushNotificationConfig, error) {
	key := s.getPushNotificationConfigKey(taskID, configID)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return a2a.TaskPushNotificationConfig{}, atlasic.ErrPushNotificationConfigNotFound
		}
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to get push notification config from S3: %w", err)
	}
	defer result.Body.Close()

	var config a2a.TaskPushNotificationConfig
	if err := json.NewDecoder(result.Body).Decode(&config); err != nil {
		return a2a.TaskPushNotificationConfig{}, fmt.Errorf("failed to decode push notification config: %w", err)
	}

	return config, nil
}

func (s *S3Storage) ListTaskPushNotificationConfig(ctx context.Context, taskID string) ([]a2a.TaskPushNotificationConfig, error) {
	prefix := s.getPushNotificationConfigPrefix(taskID)

	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list push notification configs: %w", err)
	}

	var configs []a2a.TaskPushNotificationConfig
	for _, obj := range result.Contents {
		// Extract configID from object key
		configID := s.extractConfigIDFromKey(*obj.Key)
		if configID == "" {
			continue
		}

		if config, err := s.GetTaskPushNotificationConfig(ctx, taskID, configID); err == nil {
			configs = append(configs, config)
		}
	}

	return configs, nil
}

func (s *S3Storage) DeleteTaskPushNotificationConfig(ctx context.Context, taskID, configID string) error {
	key := s.getPushNotificationConfigKey(taskID, configID)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete push notification config from S3: %w", err)
	}

	return nil
}

// Context virtual filesystem operations

func (s *S3Storage) PutContextFile(ctx context.Context, contextID, path string, data []byte) error {
	key := s.getContextFileKey(contextID, path)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})

	if err != nil {
		return fmt.Errorf("failed to put context file: %w", err)
	}

	return nil
}

func (s *S3Storage) GetContextFile(ctx context.Context, contextID, path string) ([]byte, error) {
	key := s.getContextFileKey(contextID, path)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, atlasic.ErrFileNotFound
		}
		return nil, fmt.Errorf("failed to get context file: %w", err)
	}

	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read context file body: %w", err)
	}

	return data, nil
}

func (s *S3Storage) ListContextFiles(ctx context.Context, contextID, pathPrefix string) ([]string, error) {
	prefix := s.getContextFilePrefix(contextID, pathPrefix)

	var files []string
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list context files: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			if relPath := s.extractContextFileRelPath(contextID, key); relPath != "" {
				files = append(files, relPath)
			}
		}
	}

	return files, nil
}

func (s *S3Storage) DeleteContextFile(ctx context.Context, contextID, path string) error {
	key := s.getContextFileKey(contextID, path)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete context file: %w", err)
	}

	return nil
}

// Task virtual filesystem operations

func (s *S3Storage) PutTaskFile(ctx context.Context, taskID, path string, data []byte) error {
	key := s.getTaskFileKey(taskID, path)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})

	if err != nil {
		return fmt.Errorf("failed to put task file: %w", err)
	}

	return nil
}

func (s *S3Storage) GetTaskFile(ctx context.Context, taskID, path string) ([]byte, error) {
	key := s.getTaskFileKey(taskID, path)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, atlasic.ErrFileNotFound
		}
		return nil, fmt.Errorf("failed to get task file: %w", err)
	}

	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read task file body: %w", err)
	}

	return data, nil
}

func (s *S3Storage) ListTaskFiles(ctx context.Context, taskID, pathPrefix string) ([]string, error) {
	prefix := s.getTaskFilePrefix(taskID, pathPrefix)

	var files []string
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list task files: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			if relPath := s.extractTaskFileRelPath(taskID, key); relPath != "" {
				files = append(files, relPath)
			}
		}
	}

	return files, nil
}

func (s *S3Storage) DeleteTaskFile(ctx context.Context, taskID, path string) error {
	key := s.getTaskFileKey(taskID, path)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete task file: %w", err)
	}

	return nil
}

// Helper methods for S3 key generation

func (s *S3Storage) getTaskKey(taskID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/tasks/%s.json", s.prefix, taskID)
	}
	return fmt.Sprintf("tasks/%s.json", taskID)
}

func (s *S3Storage) getContextPrefix(contextID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/contexts/%s/", s.prefix, contextID)
	}
	return fmt.Sprintf("contexts/%s/", contextID)
}

func (s *S3Storage) getContextIndexKey(contextID, taskID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/contexts/%s/%s.txt", s.prefix, contextID, taskID)
	}
	return fmt.Sprintf("contexts/%s/%s.txt", contextID, taskID)
}

func (s *S3Storage) getEventsKey(contextID, taskID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/events/%s/%s.json", s.prefix, contextID, taskID)
	}
	return fmt.Sprintf("events/%s/%s.json", contextID, taskID)
}

func (s *S3Storage) getPushNotificationConfigKey(taskID, configID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/push-configs/%s/%s.json", s.prefix, taskID, configID)
	}
	return fmt.Sprintf("push-configs/%s/%s.json", taskID, configID)
}

func (s *S3Storage) getPushNotificationConfigPrefix(taskID string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/push-configs/%s/", s.prefix, taskID)
	}
	return fmt.Sprintf("push-configs/%s/", taskID)
}

func (s *S3Storage) extractTaskIDFromContextKey(key string) string {
	// Extract from "{prefix}/contexts/{contextID}/{taskID}.txt" or "contexts/{contextID}/{taskID}.txt"
	parts := strings.Split(key, "/")

	// Find the "contexts" part and extract taskID from 2 positions after it
	for i, part := range parts {
		if part == "contexts" && i+2 < len(parts) {
			filename := parts[i+2]
			return strings.TrimSuffix(filename, ".txt")
		}
	}
	return ""
}

func (s *S3Storage) extractConfigIDFromKey(key string) string {
	// Extract from "{prefix}/push-configs/{taskID}/{configID}.json" or "push-configs/{taskID}/{configID}.json"
	parts := strings.Split(key, "/")

	// Find the "push-configs" part and extract configID from 2 positions after it
	for i, part := range parts {
		if part == "push-configs" && i+2 < len(parts) {
			filename := parts[i+2]
			return strings.TrimSuffix(filename, ".json")
		}
	}
	return ""
}

func (s *S3Storage) getContextFileKey(contextID, path string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/fs/context/%s/%s", s.prefix, contextID, path)
	}
	return fmt.Sprintf("fs/context/%s/%s", contextID, path)
}

func (s *S3Storage) getContextFilePrefix(contextID, pathPrefix string) string {
	if pathPrefix == "" {
		pathPrefix = ""
	}
	if s.prefix != "" {
		return fmt.Sprintf("%s/fs/context/%s/%s", s.prefix, contextID, pathPrefix)
	}
	return fmt.Sprintf("fs/context/%s/%s", contextID, pathPrefix)
}

func (s *S3Storage) extractContextFileRelPath(contextID, key string) string {
	prefix := s.getContextFilePrefix(contextID, "")
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):]
	}
	return ""
}

func (s *S3Storage) getTaskFileKey(taskID, path string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/fs/task/%s/%s", s.prefix, taskID, path)
	}
	return fmt.Sprintf("fs/task/%s/%s", taskID, path)
}

func (s *S3Storage) getTaskFilePrefix(taskID, pathPrefix string) string {
	if pathPrefix == "" {
		pathPrefix = ""
	}
	if s.prefix != "" {
		return fmt.Sprintf("%s/fs/task/%s/%s", s.prefix, taskID, pathPrefix)
	}
	return fmt.Sprintf("fs/task/%s/%s", taskID, pathPrefix)
}

func (s *S3Storage) extractTaskFileRelPath(taskID, key string) string {
	prefix := s.getTaskFilePrefix(taskID, "")
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):]
	}
	return ""
}
