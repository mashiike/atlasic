package atlasic

import (
	"context"
	"fmt"

	"github.com/Songmu/flextime"
	"github.com/mashiike/atlasic/a2a"
)

// TaskUpdater handles task state updates using Event Sourcing pattern
// It maintains the current task state and applies StreamResponse events to update it
type TaskUpdater struct {
	task           *a2a.Task
	initialVersion uint64 // Version when TaskUpdater was created (for optimistic concurrency control)
	version        uint64 // Current version after applying pending events
	pendingEvents  []a2a.StreamResponse
}

// NewTaskUpdater creates a new TaskUpdater with initial snapshot and version
func NewTaskUpdater(snapshotTask *a2a.Task, snapshotVersion uint64) *TaskUpdater {
	// Create a deep copy of the task to avoid modifying the original
	taskCopy := *snapshotTask
	taskCopy.History = make([]a2a.Message, len(snapshotTask.History))
	copy(taskCopy.History, snapshotTask.History)
	taskCopy.Artifacts = make([]a2a.Artifact, len(snapshotTask.Artifacts))
	copy(taskCopy.Artifacts, snapshotTask.Artifacts)

	return &TaskUpdater{
		task:           &taskCopy,
		initialVersion: snapshotVersion,
		version:        snapshotVersion,
		pendingEvents:  make([]a2a.StreamResponse, 0),
	}
}

// AddMessage adds a message to the task and creates corresponding event
func (tu *TaskUpdater) AddMessage(message a2a.Message) {
	// Update task state
	cloned := message
	cloned.ContextID = tu.task.ContextID
	cloned.TaskID = tu.task.ID
	tu.task.History = append(tu.task.History, message)

	// Create event
	streamResponse := a2a.StreamResponse{
		Message: &cloned,
	}
	tu.pendingEvents = append(tu.pendingEvents, streamResponse)
}

// UpdateStatus updates the task status and creates corresponding event
func (tu *TaskUpdater) UpdateStatus(status a2a.TaskStatus) {
	// Update task state
	tu.task.Status = status

	// State transitions are permanently disabled (A2A 0.3.x placeholder feature)
	// Status messages are not added to task history

	// Create status update event
	statusEvent := &a2a.TaskStatusUpdateEvent{
		Kind:      a2a.KindStatusUpdate,
		TaskID:    tu.task.ID,
		ContextID: tu.task.ContextID,
		Status:    status,
		Final:     status.State.IsTerminal(),
		Metadata:  make(map[string]any),
	}

	streamResponse := a2a.StreamResponse{
		Status: statusEvent,
	}
	tu.pendingEvents = append(tu.pendingEvents, streamResponse)
}

// UpsertArtifact upserts an artifact and creates corresponding event
func (tu *TaskUpdater) UpsertArtifact(artifact a2a.Artifact) {
	// Update task state
	found := false
	for i, existingArtifact := range tu.task.Artifacts {
		if existingArtifact.ArtifactID == artifact.ArtifactID {
			tu.task.Artifacts[i] = artifact
			found = true
			break
		}
	}
	if !found {
		tu.task.Artifacts = append(tu.task.Artifacts, artifact)
	}

	// Create artifact update event
	artifactEvent := &a2a.TaskArtifactUpdateEvent{
		Kind:      a2a.KindArtifactUpdate,
		TaskID:    tu.task.ID,
		ContextID: tu.task.ContextID,
		Artifact:  artifact,
		Append:    true,
		LastChunk: true,
		Metadata:  make(map[string]any),
	}

	streamResponse := a2a.StreamResponse{
		Artifact: artifactEvent,
	}
	tu.pendingEvents = append(tu.pendingEvents, streamResponse)
}

// GetTask returns the current task state
func (tu *TaskUpdater) GetTask() *a2a.Task {
	return tu.task
}

// GetPendingEvents returns all pending events that need to be appended to the stream
func (tu *TaskUpdater) GetPendingEvents() []a2a.StreamResponse {
	return tu.pendingEvents
}

// GetVersion returns the current version
func (tu *TaskUpdater) GetVersion() uint64 {
	return tu.version
}

// GetExpectedVersion returns the initial version for optimistic concurrency control
func (tu *TaskUpdater) GetExpectedVersion() uint64 {
	return tu.initialVersion
}

// GetNewVersion returns the version that the task should have after applying all pending events
func (tu *TaskUpdater) GetNewVersion() uint64 {
	return tu.initialVersion + uint64(len(tu.pendingEvents))
}

// Event Sourcing helper methods for AgentService

// applyTaskUpdates applies the updates from TaskUpdater to storage (append events + save snapshot)
func (s *AgentService) applyTaskUpdates(ctx context.Context, updater *TaskUpdater) error {
	events := updater.GetPendingEvents()
	if len(events) == 0 {
		return nil // No changes to apply
	}

	task := updater.GetTask()

	// Append events to stream (using initial version as expected event stream version)
	newEventVersion, err := s.Storage.Append(ctx, task.ContextID, task.ID, updater.GetExpectedVersion(), events)
	if err != nil {
		return fmt.Errorf("failed to append events: %w", err)
	}

	// Save task snapshot with version control
	if err := s.Storage.SaveTask(ctx, task, updater.GetExpectedVersion(), updater.GetNewVersion()); err != nil {
		return fmt.Errorf("failed to save task snapshot: %w", err)
	}

	// Send push notifications for each event (at-least-once delivery) - only if enabled
	if s.pushNotificationsEnabled() {
		s.sendPushNotifications(ctx, task.ID, events)
	}

	// Update the updater's version to the new version for potential future use
	updater.version = newEventVersion

	return nil
}

// sendPushNotifications sends push notifications for events (at-least-once delivery)
func (s *AgentService) sendPushNotifications(ctx context.Context, taskID string, events []a2a.StreamResponse) {
	// Check if push notifications are enabled
	if !s.pushNotificationsEnabled() {
		return
	}

	// Get push notification config for this task
	configs, err := s.Storage.ListTaskPushNotificationConfig(ctx, taskID)
	if err != nil {
		s.Logger.Error("Failed to get push notification configs", "error", err, "taskID", taskID)
		return
	}

	if len(configs) == 0 {
		return // No push notification configs for this task
	}

	// Send notification for each event to all configured endpoints
	for _, event := range events {
		for _, config := range configs {
			if err := s.PushNotifier.Notify(ctx, config.PushNotificationConfig, event); err != nil {
				s.Logger.Error("Failed to send push notification",
					"error", err, "taskID", taskID, "url", config.PushNotificationConfig.URL)
				// Continue with other events (at-least-once - we logged the failure)
			} else {
				s.Logger.Debug("Push notification sent successfully",
					"taskID", taskID, "url", config.PushNotificationConfig.URL)
			}
		}
	}
}

// createNewTask creates a new task using TaskUpdater pattern
func (s *AgentService) createNewTask(ctx context.Context, taskID, contextID string, userMessage a2a.Message) (*a2a.Task, error) {
	// Create initial task (empty, without history)
	task := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        taskID,
		ContextID: contextID,
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateSubmitted,
			Message: nil,
		},
		History: []a2a.Message{
			userMessage,
		},
		Artifacts: []a2a.Artifact{},
		Metadata:  make(map[string]any),
	}

	// Create updater starting from empty task (version 0)
	updater := NewTaskUpdater(task, 0)
	// Create task creation event
	taskCreationEvent := a2a.StreamResponse{
		Task: updater.GetTask(),
	}

	// Manually add task creation event to pending events
	updater.pendingEvents = append([]a2a.StreamResponse{taskCreationEvent}, updater.pendingEvents...)

	// Apply all updates
	if err := s.applyTaskUpdates(ctx, updater); err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}

	return updater.GetTask(), nil
}

// addMessage adds a message to task using TaskUpdater
func (s *AgentService) addMessage(ctx context.Context, taskID string, messageID string, role a2a.Role, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error) {
	// Create message with auto-generated IDs and fixed Agent role
	message := a2a.NewMessage(messageID, role, parts, optFns...)

	task, version, err := s.Storage.GetTask(ctx, taskID, HistoryLengthAll)
	if err != nil {
		return "", fmt.Errorf("failed to get task: %w", err)
	}

	// Create updater and add message
	updater := NewTaskUpdater(task, version)
	updater.AddMessage(message)
	return messageID, s.applyTaskUpdates(ctx, updater)
}

// updateStatus updates task status using TaskUpdater
func (s *AgentService) updateStatus(ctx context.Context, taskID string, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error) {
	var messageID string
	var message *a2a.Message

	// Create message if parts are provided
	if parts != nil {
		messageID = s.getIDGenerator().GenerateMessageID()
		msg := a2a.NewMessage(messageID, a2a.RoleAgent, parts, optFns...)
		message = &msg
	}
	status := a2a.TaskStatus{
		State:   state,
		Message: message,
	}
	status.SetTimestamp(flextime.Now())

	task, version, err := s.Storage.GetTask(ctx, taskID, HistoryLengthAll)
	if err != nil {
		return a2a.TaskStatus{}, fmt.Errorf("failed to get task: %w", err)
	}

	// Check if task is already cancelled (cannot update status of cancelled task)
	if task.Status.State == a2a.TaskStateCanceled {
		return a2a.TaskStatus{}, fmt.Errorf("cannot update status of cancelled task %s", taskID)
	}

	// Create updater and update status (state transitions permanently disabled)
	updater := NewTaskUpdater(task, version)
	updater.UpdateStatus(status)
	return status, s.applyTaskUpdates(ctx, updater)
}

// upsertArtifact upserts an artifact using TaskUpdater
func (s *AgentService) upsertArtifact(ctx context.Context, taskID string, artifact a2a.Artifact) error {
	task, version, err := s.Storage.GetTask(ctx, taskID, HistoryLengthAll)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Create updater and upsert artifact
	updater := NewTaskUpdater(task, version)
	updater.UpsertArtifact(artifact)

	return s.applyTaskUpdates(ctx, updater)
}
