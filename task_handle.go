package atlasic

import (
	"context"
	"fmt"

	"github.com/mashiike/atlasic/a2a"
)

type TaskHandle interface {
	// Task information access
	GetContextID() string
	GetTaskID() string
	GetTask(ctx context.Context, historyLength int) (*a2a.Task, error)
	GetInitialStatus() a2a.TaskStatus // Get task status at the time of dequeue/creation
	GetAcceptedOutputModes() []string // Get accepted output modes for this task

	// Task state mutations - implementation handles streaming notifications automatically
	// Role is fixed to RoleAgent for all TaskHandle operations
	AddMessage(ctx context.Context, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error)
	UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error)
	UpsertArtifact(ctx context.Context, artifact a2a.Artifact) error

	// TODO: Internal conversation functions for Supervisor implementation
	// These will be used for Supervisor Reasoning/Action and Specialist Agent coordination
	// AddConversation(ctx context.Context, conversation a2a.Message) error
	// CompactConversations(ctx context.Context, keepRecent int) error
	// GetConversationsSince(ctx context.Context, timestamp int64) ([]a2a.Message, error)
}

// taskHandle is the concrete implementation of TaskHandle interface
// It delegates operations to AgentService for actual task management
type taskHandle struct {
	contextID           string
	taskID              string
	svc                 *AgentService
	initialStatus       a2a.TaskStatus // Status at the time of task handle creation
	acceptedOutputModes []string       // Accepted output modes for this task
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

type TaskHandleParams struct {
	ContextID           string
	TaskID              string
	InitialStatus       a2a.TaskStatus // Status at the time of task handle creation
	AcceptedOutputModes []string       // Accepted output modes for this task
}

func (s *AgentService) NewTaskHandle(ctx context.Context, params TaskHandleParams) TaskHandle {
	return &taskHandle{
		contextID:           params.ContextID,
		taskID:              params.TaskID,
		svc:                 s,
		initialStatus:       params.InitialStatus,
		acceptedOutputModes: params.AcceptedOutputModes,
	}
}
