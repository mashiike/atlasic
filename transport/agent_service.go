package transport

import (
	"context"

	"github.com/mashiike/atlasic/a2a"
)

// PlaceholderURL is used as a default URL for AgentCard when the actual URL is managed by transport layer
const PlaceholderURL = "http://0.0.0.0"

//go:generate go tool mockgen -source=agent_service.go -destination=./agent_service_test.go -package transport

// AgentService defines the interface for A2A agent implementations.
// This interface uses A2A JSON structures directly instead of protobuf types.
type AgentService interface {
	// SendMessage sends a message to the agent and returns the result.
	// This is a blocking call that will return the task once it is completed,
	// or immediately if blocking is false in the configuration.
	SendMessage(ctx context.Context, params a2a.MessageSendParams) (*a2a.SendMessageResult, error)

	// SendStreamingMessage sends a message to the agent with streaming response.
	// This is a streaming call that will return a channel of stream responses
	// until the Task is in an interrupted or terminal state.
	// The channel will be closed when streaming is complete.
	SendStreamingMessage(ctx context.Context, params a2a.MessageSendParams) (<-chan a2a.StreamResponse, error)

	// GetTask retrieves the current state of a task from the agent.
	GetTask(ctx context.Context, params a2a.TaskQueryParams) (*a2a.Task, error)

	// CancelTask cancels a task from the agent.
	// If supported, no more task updates should be expected for the task.
	CancelTask(ctx context.Context, params a2a.TaskIDParams) (*a2a.Task, error)

	// TaskResubscription creates a streaming subscription to task updates.
	// This attaches the stream to an existing in-process task.
	// If the task is complete, the stream will return the completed task and close.
	// The channel will be closed when subscription is complete.
	TaskResubscription(ctx context.Context, params a2a.TaskIDParams) (<-chan a2a.StreamResponse, error)

	// SetTaskPushNotificationConfig sets a push notification config for a task.
	SetTaskPushNotificationConfig(ctx context.Context, params a2a.TaskPushNotificationConfig) (*a2a.TaskPushNotificationConfig, error)

	// GetTaskPushNotificationConfig gets a push notification config for a task.
	GetTaskPushNotificationConfig(ctx context.Context, params a2a.GetTaskPushNotificationConfigParams) (*a2a.TaskPushNotificationConfig, error)

	// ListTaskPushNotificationConfig lists push notification configurations for a task.
	ListTaskPushNotificationConfig(ctx context.Context, params a2a.TaskIDParams) ([]a2a.TaskPushNotificationConfig, error)

	// DeleteTaskPushNotificationConfig deletes a push notification configuration for a task.
	DeleteTaskPushNotificationConfig(ctx context.Context, params a2a.DeleteTaskPushNotificationConfigParams) error

	// GetAgentCard returns the agent card for this agent.
	GetAgentCard(ctx context.Context) (*a2a.AgentCard, error)

	// SupportedOutputModes returns the output modes supported by this agent.
	SupportedOutputModes(ctx context.Context) ([]string, error)
}
