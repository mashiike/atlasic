package atlasic

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/mashiike/atlasic/a2a"
)

type TaskHandle interface {
	// Task information access
	GetContextID() string
	GetTaskID() string
	GetTask(ctx context.Context, historyLength int) (*a2a.Task, error)
	GetInitialStatus() a2a.TaskStatus // Get task status at the time of dequeue/creation
	GetAcceptedOutputModes() []string // Get accepted output modes for this task
	GetIncomingMessage() a2a.Message  // Get the incoming message that triggered this task
	GetHTTPHeaders() http.Header      // Get HTTP headers from the original request

	// Task state mutations - implementation handles streaming notifications automatically
	// Role is fixed to RoleAgent for all TaskHandle operations
	AddMessage(ctx context.Context, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error)
	UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error)
	UpsertArtifact(ctx context.Context, artifact a2a.Artifact) error

	// History-specific operations - enables differential message retrieval
	GetLastMessageID(ctx context.Context) (string, error)
	GetHistorySince(ctx context.Context, sinceMessageID string) ([]a2a.Message, error)

	// Event-specific operations - enables differential event retrieval for task change tracking
	GetLastEventVersion(ctx context.Context) (uint64, error)
	GetEventsSince(ctx context.Context, sinceVersion uint64) ([]a2a.StreamResponse, error)

	// Context virtual filesystem operations - enables context-scoped file sharing
	OpenContextFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error)
	ListContextFiles(ctx context.Context, pathPrefix string) ([]string, error)
	DeleteContextFile(ctx context.Context, path string) error

	// Task virtual filesystem operations - enables task-scoped file operations
	OpenTaskFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error)
	ListTaskFiles(ctx context.Context, pathPrefix string) ([]string, error)
	DeleteTaskFile(ctx context.Context, path string) error
}

// taskHandle is the concrete implementation of TaskHandle interface
// It delegates operations to AgentService for actual task management
type taskHandle struct {
	contextID           string
	taskID              string
	svc                 *AgentService
	initialStatus       a2a.TaskStatus // Status at the time of task handle creation
	acceptedOutputModes []string       // Accepted output modes for this task
	incomingMessage     a2a.Message    // The incoming message that triggered this task
	httpHeaders         http.Header    // HTTP headers from the original request
}

func (h *taskHandle) GetContextID() string {
	return h.contextID
}

func (h *taskHandle) GetTaskID() string {
	return h.taskID
}

func (h *taskHandle) GetInitialStatus() a2a.TaskStatus {
	return h.initialStatus
}

func (h *taskHandle) GetAcceptedOutputModes() []string {
	return h.acceptedOutputModes
}

func (h *taskHandle) GetTask(ctx context.Context, historyLength int) (*a2a.Task, error) {
	// Use configured history length if provided, otherwise use the parameter
	task, _, err := h.svc.Storage.GetTask(ctx, h.taskID, historyLength)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return task, nil
}

func (h *taskHandle) GetIncomingMessage() a2a.Message {
	return h.incomingMessage
}

func (h *taskHandle) GetHTTPHeaders() http.Header {
	return h.httpHeaders
}

func (h *taskHandle) AddMessage(ctx context.Context, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error) {
	// Generate MessageID, ContextID, TaskID automatically
	messageID := h.svc.getIDGenerator().GenerateMessageID()
	return h.svc.addMessage(ctx, h.taskID, messageID, a2a.RoleAgent, parts, optFns...)
}

func (h *taskHandle) UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error) {
	return h.svc.updateStatus(ctx, h.taskID, state, parts, optFns...)
}

func (h *taskHandle) UpsertArtifact(ctx context.Context, artifact a2a.Artifact) error {
	if err := h.svc.upsertArtifact(ctx, h.taskID, artifact); err != nil {
		return fmt.Errorf("failed to upsert artifact: %w", err)
	}
	return nil
}

// History-specific operations

func (h *taskHandle) GetLastMessageID(ctx context.Context) (string, error) {
	task, _, err := h.svc.Storage.GetTask(ctx, h.taskID, -1) // Get full history
	if err != nil {
		return "", fmt.Errorf("failed to get task: %w", err)
	}

	if len(task.History) == 0 {
		return "", nil // No messages yet
	}

	// Return the MessageID of the last message
	return task.History[len(task.History)-1].MessageID, nil
}

func (h *taskHandle) GetHistorySince(ctx context.Context, sinceMessageID string) ([]a2a.Message, error) {
	task, _, err := h.svc.Storage.GetTask(ctx, h.taskID, -1) // Get full history
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if sinceMessageID == "" {
		// If empty string, return all history
		return task.History, nil
	}

	// Find the position of sinceMessageID
	afterIndex := len(task.History) // Default: no messages after (empty result)

	for i, msg := range task.History {
		if msg.MessageID == sinceMessageID {
			afterIndex = i + 1 // Start from the message after sinceMessageID
			break
		}
	}

	// Return messages after the specified MessageID
	if afterIndex < len(task.History) {
		return task.History[afterIndex:], nil
	}

	// No messages found after the specified MessageID
	return []a2a.Message{}, nil
}

// Event-specific operations

func (h *taskHandle) GetLastEventVersion(ctx context.Context) (uint64, error) {
	_, lastVersion, err := h.svc.Storage.Load(ctx, h.contextID, h.taskID, 0, -1) // Get all events
	if err != nil {
		return 0, fmt.Errorf("failed to load events: %w", err)
	}

	// Return the version after all events (this is the "last version")
	return lastVersion, nil
}

func (h *taskHandle) GetEventsSince(ctx context.Context, sinceVersion uint64) ([]a2a.StreamResponse, error) {
	events, _, err := h.svc.Storage.Load(ctx, h.contextID, h.taskID, sinceVersion, -1) // Get events from sinceVersion onwards
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	// Storage.Load already returns events from the specified version onwards
	return events, nil
}

// Context virtual filesystem operations

func (h *taskHandle) OpenContextFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error) {
	return h.svc.Storage.OpenContextFile(ctx, h.contextID, path, flag, perm)
}

func (h *taskHandle) ListContextFiles(ctx context.Context, pathPrefix string) ([]string, error) {
	return h.svc.Storage.ListContextFiles(ctx, h.contextID, pathPrefix)
}

func (h *taskHandle) DeleteContextFile(ctx context.Context, path string) error {
	return h.svc.Storage.DeleteContextFile(ctx, h.contextID, path)
}

// Task virtual filesystem operations

func (h *taskHandle) OpenTaskFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error) {
	return h.svc.Storage.OpenTaskFile(ctx, h.taskID, path, flag, perm)
}

func (h *taskHandle) ListTaskFiles(ctx context.Context, pathPrefix string) ([]string, error) {
	return h.svc.Storage.ListTaskFiles(ctx, h.taskID, pathPrefix)
}

func (h *taskHandle) DeleteTaskFile(ctx context.Context, path string) error {
	return h.svc.Storage.DeleteTaskFile(ctx, h.taskID, path)
}

type TaskHandleParams struct {
	ContextID           string
	TaskID              string
	InitialStatus       a2a.TaskStatus // Status at the time of task handle creation
	AcceptedOutputModes []string       // Accepted output modes for this task
	IncomingMessage     a2a.Message    // The incoming message that triggered this task
	HTTPHeaders         http.Header    // HTTP headers from the original request
}

func (s *AgentService) NewTaskHandle(ctx context.Context, params TaskHandleParams) TaskHandle {
	return &taskHandle{
		contextID:           params.ContextID,
		taskID:              params.TaskID,
		svc:                 s,
		initialStatus:       params.InitialStatus,
		acceptedOutputModes: params.AcceptedOutputModes,
		incomingMessage:     params.IncomingMessage,
		httpHeaders:         params.HTTPHeaders,
	}
}
