package atlasic

import (
	"context"
	"fmt"
	"io/fs"
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

	// Task state mutations - implementation handles streaming notifications automatically
	// Role is fixed to RoleAgent for all TaskHandle operations
	AddMessage(ctx context.Context, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error)
	UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error)
	UpsertArtifact(ctx context.Context, artifact a2a.Artifact) error

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
}

func (s *AgentService) NewTaskHandle(ctx context.Context, params TaskHandleParams) TaskHandle {
	return &taskHandle{
		contextID:           params.ContextID,
		taskID:              params.TaskID,
		svc:                 s,
		initialStatus:       params.InitialStatus,
		acceptedOutputModes: params.AcceptedOutputModes,
		incomingMessage:     params.IncomingMessage,
	}
}
