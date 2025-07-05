// Package atlasic provides A2A (Agent-to-Agent) Toolkit Library
// for building supervisor-style multi-agent collaborative systems.
package atlasic

import (
	"context"
	"errors"

	"github.com/mashiike/atlasic/a2a"
)

// Storage constants
const (
	// HistoryLengthAll specifies that GetTask should return all history
	// When this value is passed as historyLength, the Storage implementation
	// should return the complete task history without limitation
	HistoryLengthAll = -1
)

// Storage error variables that implementations should return
var (
	// ErrTaskNotFound is returned when a requested task does not exist
	ErrTaskNotFound = errors.New("task not found")
	// ErrContextNotFound is returned when a requested context does not exist
	ErrContextNotFound = errors.New("context not found")
	// ErrPushNotificationConfigNotFound is returned when push notification config does not exist
	ErrPushNotificationConfigNotFound = errors.New("push notification config not found")
)

// Storage provides task persistence and event stream operations (CQRS-based)
// Implementations should return well-defined errors for consistent handling:
//   - ErrTaskNotFound: when a requested task does not exist
//   - ErrContextNotFound: when a requested context does not exist
//   - ErrPushNotificationConfigNotFound: when push notification config does not exist
type Storage interface {
	// Task operations (Query side)
	GetTask(ctx context.Context, taskID string, historyLength int) (*a2a.Task, uint64, error)
	SaveTask(ctx context.Context, task *a2a.Task, expectedVersion, newVersion uint64) error
	ListTasksByContext(ctx context.Context, contextID string, historyLength int) ([]*a2a.Task, []uint64, error)

	// Event stream operations (Command side) - enables streaming capabilities
	Append(ctx context.Context, contextID string, taskID string, expected uint64, events []a2a.StreamResponse) (next uint64, err error)
	Load(ctx context.Context, contextID string, taskID string, from uint64, limit int) ([]a2a.StreamResponse, uint64, error)

	// Push notification configuration operations (supports multiple configs per task)
	SaveTaskPushNotificationConfig(ctx context.Context, config a2a.TaskPushNotificationConfig) error
	GetTaskPushNotificationConfig(ctx context.Context, taskID, configID string) (a2a.TaskPushNotificationConfig, error)
	ListTaskPushNotificationConfig(ctx context.Context, taskID string) ([]a2a.TaskPushNotificationConfig, error)
	DeleteTaskPushNotificationConfig(ctx context.Context, taskID, configID string) error
}

