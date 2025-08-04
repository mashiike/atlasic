// Package atlasic provides A2A (Agent-to-Agent) Toolkit Library
// for building supervisor-style multi-agent collaborative systems.
package atlasic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/fujiwara/ridge"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

const (
	// agentServiceContextKey is the key for storing AgentService in context
	agentServiceContextKey contextKey = "atlasic:agent-service"
	// extensionsContextKey is the key for storing Extensions in context
	extensionsContextKey contextKey = "atlasic:extensions"
)

//go:generate go tool mockgen -source=atlasic.go -destination=mock_test.go -package=atlasic
//go:generate go tool mockgen -source=storage.go -destination=mock_storage_test.go -package=atlasic
//go:generate go tool mockgen -source=id_generator.go -destination=mock_id_generator_test.go -package=atlasic
//go:generate go tool mockgen -source=task_handle.go -destination=mock_task_handle_test.go -package=atlasic

// AgentMetadata represents Agent-specific metadata
// AgentService is responsible for server-side settings like URLs, version, etc.
type AgentMetadata struct {
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Skills             []a2a.AgentSkill   `json:"skills"`
	DefaultInputModes  []string           `json:"default_input_modes,omitempty"`
	DefaultOutputModes []string           `json:"default_output_modes,omitempty"`
	Version            string             `json:"version"`
	Provider           *a2a.AgentProvider `json:"provider,omitempty"` // Optional provider information
}

type Agent interface {
	GetMetadata(ctx context.Context) (*AgentMetadata, error)
	Execute(ctx context.Context, handle TaskHandle) (*a2a.Message, error)
}

type agentFunc struct {
	metadata *AgentMetadata
	f        func(ctx context.Context, handle TaskHandle) (*a2a.Message, error)
}

func NewAgent(metadata *AgentMetadata, f func(ctx context.Context, handle TaskHandle) (*a2a.Message, error)) Agent {
	if metadata == nil {
		metadata = &AgentMetadata{}
	}
	if metadata.Name == "" {
		metadata.Name = "Unnamed Agent"
	}
	if metadata.Version == "" {
		metadata.Version = "1.0.0"
	}
	return &agentFunc{
		metadata: metadata,
		f:        f,
	}
}

func (a *agentFunc) GetMetadata(_ context.Context) (*AgentMetadata, error) {
	return a.metadata, nil
}

func (a *agentFunc) Execute(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
	return a.f(ctx, handle)
}

// TaskLocker provides task-level locking to prevent concurrent execution
type TaskLocker interface {
	// Lock attempts to acquire a lock for the specified task
	// Returns a function to unlock when successful, or an error if lock cannot be acquired
	Lock(ctx context.Context, taskID string) (unlock func(), err error)
	// Close gracefully shuts down the task locker
	Close() error
}

// TaskStatusError allows errors to define custom task status for more precise error handling
type TaskStatusError interface {
	error
	ToTaskStatus() a2a.TaskStatus
}

// AgentService combines Agent execution with Storage capabilities
// It dynamically determines AgentCard capabilities based on Storage interface implementations
type AgentService struct {
	Storage Storage
	Agent   Agent

	// Streaming configuration
	StreamingPollInterval time.Duration // Public field for polling interval in streaming operations

	// Job processing configuration
	HeartbeatInterval        time.Duration // Public field for heartbeat interval during job execution
	CancelMonitoringInterval time.Duration // Public field for task cancellation monitoring interval

	// Logging
	Logger *slog.Logger // Public field for logging configuration

	// JobQueue for asynchronous agent execution
	JobQueue JobQueue // Public field for background job processing

	// TaskLocker for preventing concurrent task execution (optional)
	TaskLocker TaskLocker // Public field for task locking

	// DisableStreaming disables streaming capabilities when set to true
	DisableStreaming bool // Public field for disabling streaming features

	// DisablePushNotifications disables push notification capabilities when set to true
	DisablePushNotifications bool // Public field for disabling push notification features

	// PushNotifier for sending push notifications
	PushNotifier PushNotifier // Public field for push notification delivery

	// BaseEndpoint of the agent service
	BaseEndpoint string // Public field for agent base endpoint (defaults to transport.PlaceholderURL)

	// IDGenerator management with lazy initialization
	mu          sync.RWMutex
	idGenerator IDGenerator

	// Worker management
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWg     sync.WaitGroup
}

// NewAgentService creates a new AgentService with default IDGenerator
func NewAgentService(storage Storage, agent Agent) *AgentService {
	return &AgentService{
		Storage:                  storage,
		Agent:                    agent,
		StreamingPollInterval:    time.Second,              // Default polling interval: 1 second
		HeartbeatInterval:        30 * time.Second,         // Default heartbeat interval: 30 seconds
		CancelMonitoringInterval: 1 * time.Second,          // Default cancel monitoring interval: 1 second
		Logger:                   slog.Default(),           // Default logger
		JobQueue:                 NewInMemoryJobQueue(100), // Default in-memory job queue
		PushNotifier:             NewDefaultPushNotifier(), // Default push notifier
		BaseEndpoint:             transport.PlaceholderURL, // Default placeholder base endpoint
		idGenerator:              &DefaultIDGenerator{},
	}
}

// SetIDGenerator sets a custom IDGenerator for the AgentService
// This replaces the default DefaultIDGenerator set in NewAgentService
func (s *AgentService) SetIDGenerator(idGen IDGenerator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idGenerator = idGen
}

// getIDGenerator returns the IDGenerator
func (s *AgentService) getIDGenerator() IDGenerator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idGenerator
}

// Start initiates background workers for processing jobs from the JobQueue
// This method should be called for long-running processes (ECS/Server)
func (s *AgentService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.workerCancel != nil {
		return errors.New("AgentService is already started")
	}

	s.workerCtx, s.workerCancel = context.WithCancel(ctx)

	// Start a single worker (can be extended to multiple workers later)
	s.workerWg.Add(1)
	go s.workerLoop(s.workerCtx)

	return nil
}

// Close gracefully shuts down the AgentService and its workers
func (s *AgentService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.workerCancel != nil {
		s.workerCancel()
		s.workerCancel = nil
	}

	// Wait for workers to finish
	s.workerWg.Wait()

	// Close the push notifier
	if s.PushNotifier != nil {
		if err := s.PushNotifier.Close(); err != nil {
			s.Logger.Error("Failed to close push notifier", "error", err)
		}
	}

	// Close the job queue
	if s.JobQueue != nil {
		return s.JobQueue.Close()
	}

	return nil
}

// statusTracker wraps a TaskHandle to track status updates made by the Agent
// This allows ProcessJob to determine if implicit status updates should be applied
type statusTracker struct {
	TaskHandle
	lastStatusUpdate *a2a.TaskStatus // Tracks the last status update made by Agent
	mu               sync.RWMutex    // Protects lastStatusUpdate
}

func wrapStatusTracker(handle TaskHandle) *statusTracker {
	return &statusTracker{
		TaskHandle:       handle,
		lastStatusUpdate: nil,
	}
}

// UpdateStatus overrides the wrapped TaskHandle's UpdateStatus to track status changes
func (w *statusTracker) UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Call the underlying UpdateStatus
	status, err := w.TaskHandle.UpdateStatus(ctx, state, parts, optFns...)
	if err != nil {
		return a2a.TaskStatus{}, err
	}
	w.lastStatusUpdate = &status
	return status, nil
}

func (w *statusTracker) getLastStatusUpdate() *a2a.TaskStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastStatusUpdate
}

func (w *statusTracker) hasStatusUpdate() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastStatusUpdate != nil
}

// ProcessJob processes a single job - used for FaaS environments
// This method can be called directly from Lambda handlers
func (s *AgentService) ProcessJob(ctx context.Context, job *Job) error {
	// Handle task locking if TaskLocker is configured
	var unlock func()
	if s.TaskLocker != nil {
		var err error
		unlock, err = s.acquireTaskLockWithRetry(ctx, job.TaskID)
		if err != nil {
			s.Logger.Error("Failed to acquire task lock", "error", err, "taskID", job.TaskID)
			if failErr := job.FailFunc(); failErr != nil {
				s.Logger.Error("Failed to mark job as failed", "error", failErr, "taskID", job.TaskID)
			}
			return err
		}
		defer unlock()
	}

	// Get task to capture initial status and verify state
	task, _, err := s.Storage.GetTask(ctx, job.TaskID, 10)
	if err != nil {
		s.Logger.Error("Failed to get task for processing", "error", err, "taskID", job.TaskID)
		if failErr := job.FailFunc(); failErr != nil {
			s.Logger.Error("Failed to mark job as failed", "error", failErr, "taskID", job.TaskID)
		}
		return err
	}

	// Check if task is already in terminal state
	if task.Status.State.IsTerminal() {
		s.Logger.Debug("Task is already in terminal state, skipping processing", "taskID", job.TaskID, "state", task.Status.State)
		return nil // Not an error - task was completed or cancelled by another process
	}

	// Capture initial status before changing to working
	initialStatus := task.Status
	var incomingMessage a2a.Message
	for _, msg := range task.History {
		if msg.MessageID == job.IncomingMessageID {
			incomingMessage = msg
			break
		}
	}

	// Set task to working state before processing
	if _, err := s.updateStatus(ctx, job.TaskID, a2a.TaskStateWorking, nil); err != nil {
		s.Logger.Error("Failed to set task to working state", "error", err, "taskID", job.TaskID)
		if failErr := job.FailFunc(); failErr != nil {
			s.Logger.Error("Failed to mark job as failed", "error", failErr, "taskID", job.TaskID)
		}
		return err
	}
	h := s.NewTaskHandle(ctx, TaskHandleParams{
		ContextID:           job.ContextID,
		TaskID:              job.TaskID,
		InitialStatus:       initialStatus,
		AcceptedOutputModes: job.AcceptedOutputModes,
		IncomingMessage:     incomingMessage,
		HTTPHeaders:         job.HTTPHeaders,
	})
	// Create wrapped handle to track status updates made by the Agent
	wrappedHandle := wrapStatusTracker(h)

	// Create context for agent execution with cancellation monitoring
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	// Start background monitoring goroutine for heartbeat and cancellation
	var backgroundWg sync.WaitGroup
	backgroundCtx, backgroundCancel := context.WithCancel(ctx)
	defer backgroundCancel()

	backgroundWg.Add(1)
	go s.processJobMonitoring(backgroundCtx, &backgroundWg, job, agentCancel)

	// Execute agent with wrapped handle to track status updates
	resultMessage, agentErr := s.Agent.Execute(agentCtx, wrappedHandle)

	// Stop background goroutines
	backgroundCancel()
	backgroundWg.Wait()

	if agentErr != nil {
		// Check if error is due to context cancellation (task cancellation)
		if errors.Is(agentErr, context.Canceled) {
			s.Logger.Debug("Agent execution cancelled due to task cancellation", "taskID", job.TaskID, "contextID", job.ContextID)
			// Don't update task status - it's already CANCELED
			// Don't mark job as failed - this is an expected cancellation
			return agentErr
		}

		// Handle agent execution error with status update logic
		var failedStatus a2a.TaskStatus
		shouldApplyFailedStatus := true

		if statusErr, ok := agentErr.(TaskStatusError); ok {
			// Error provides custom task status
			failedStatus = statusErr.ToTaskStatus()
		} else {
			// Check if Agent made explicit status updates before failing
			if wrappedHandle.hasStatusUpdate() {
				lastStatus := wrappedHandle.getLastStatusUpdate()
				s.Logger.Debug("Agent made status updates before error", "status", lastStatus.State, "taskID", job.TaskID, "contextID", job.ContextID)

				// If Agent set a terminal status before error, respect it
				if lastStatus.State.IsTerminal() {
					s.Logger.Debug("Agent set terminal status before error, not applying implicit Failed", "agentStatus", lastStatus.State, "taskID", job.TaskID, "contextID", job.ContextID)
					shouldApplyFailedStatus = false
				}
			}

			if shouldApplyFailedStatus {
				// Regular error - log and set to failed
				s.Logger.Warn("Agent execution failed", "error", agentErr.Error(), "taskID", job.TaskID, "contextID", job.ContextID)
				failedStatus = a2a.TaskStatus{
					State:   a2a.TaskStateFailed,
					Message: nil, // Error details logged only
				}
			}
		}

		if shouldApplyFailedStatus {
			var parts []a2a.Part
			var optFns []func(*a2a.MessageOptions)
			if failedStatus.Message != nil {
				parts = failedStatus.Message.Parts
				optFns = append(optFns, func(mo *a2a.MessageOptions) {
					mo.Extensions = failedStatus.Message.Extensions
					mo.Metadata = failedStatus.Message.Metadata
					mo.ReferenceTaskIDs = failedStatus.Message.ReferenceTaskIDs
				})
			}
			if _, updateErr := s.updateStatus(ctx, job.TaskID, failedStatus.State, parts, optFns...); updateErr != nil {
				s.Logger.Error("Failed to update task status after agent error",
					"error", updateErr, "taskID", job.TaskID, "contextID", job.ContextID)
			}
		}

		// Mark job as failed
		if failErr := job.FailFunc(); failErr != nil {
			s.Logger.Error("Failed to mark job as failed", "error", failErr, "taskID", job.TaskID)
		}

		return agentErr
	}

	// Agent execution succeeded - apply implicit status update logic
	if wrappedHandle.hasStatusUpdate() {
		// Agent made explicit status updates - use the last one set by Agent
		lastStatus := wrappedHandle.getLastStatusUpdate()
		s.Logger.Debug("Agent set explicit final status", "status", lastStatus.State, "taskID", job.TaskID, "contextID", job.ContextID)

		// Check if Agent set a terminal status, if not, we may need to apply implicit Completed
		if !lastStatus.State.IsTerminal() {
			s.Logger.Debug("Agent did not set terminal status, applying implicit Completed", "agentStatus", lastStatus.State, "taskID", job.TaskID, "contextID", job.ContextID)

			// Apply implicit completed status with result message if available
			var parts []a2a.Part
			var optFns []func(*a2a.MessageOptions)
			if resultMessage != nil {
				parts = resultMessage.Parts
				optFns = append(optFns, func(mo *a2a.MessageOptions) {
					mo.Extensions = resultMessage.Extensions
					mo.Metadata = resultMessage.Metadata
					mo.ReferenceTaskIDs = resultMessage.ReferenceTaskIDs
				})
			}

			if _, updateErr := s.updateStatus(ctx, job.TaskID, a2a.TaskStateCompleted, parts, optFns...); updateErr != nil {
				s.Logger.Error("Failed to apply implicit completed status", "error", updateErr, "taskID", job.TaskID, "contextID", job.ContextID)
				if failErr := job.FailFunc(); failErr != nil {
					s.Logger.Error("Failed to mark job as failed after status update error", "error", failErr, "taskID", job.TaskID)
				}
				return updateErr
			}
		}
	} else {
		// Agent made no explicit status updates - apply implicit Completed with result message
		s.Logger.Debug("Agent made no explicit status updates, applying implicit Completed", "taskID", job.TaskID, "contextID", job.ContextID)

		var parts []a2a.Part
		var optFns []func(*a2a.MessageOptions)
		if resultMessage != nil {
			parts = resultMessage.Parts
			optFns = append(optFns, func(mo *a2a.MessageOptions) {
				mo.Extensions = resultMessage.Extensions
				mo.Metadata = resultMessage.Metadata
				mo.ReferenceTaskIDs = resultMessage.ReferenceTaskIDs
			})
		}

		if _, updateErr := s.updateStatus(ctx, job.TaskID, a2a.TaskStateCompleted, parts, optFns...); updateErr != nil {
			s.Logger.Error("Failed to apply implicit completed status", "error", updateErr, "taskID", job.TaskID, "contextID", job.ContextID)
			if failErr := job.FailFunc(); failErr != nil {
				s.Logger.Error("Failed to mark job as failed after status update error", "error", failErr, "taskID", job.TaskID)
			}
			return updateErr
		}
	}

	// Mark job as completed
	if completeErr := job.CompleteFunc(); completeErr != nil {
		s.Logger.Error("Failed to mark job as completed", "error", completeErr, "taskID", job.TaskID)
		return completeErr
	}

	return nil
}

// acquireTaskLockWithRetry attempts to acquire a task lock with retry logic
func (s *AgentService) acquireTaskLockWithRetry(ctx context.Context, taskID string) (func(), error) {
	const retryInterval = 1 * time.Second
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		unlock, err := s.TaskLocker.Lock(ctx, taskID)
		if err == nil {
			return unlock, nil
		}

		s.Logger.Debug("Failed to acquire task lock, retrying", "error", err, "taskID", taskID)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Continue to next retry
		}
	}
}

// workerLoop is the main loop for background workers
func (s *AgentService) workerLoop(ctx context.Context) {
	defer s.workerWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			job, err := s.JobQueue.Dequeue(ctx)
			if err != nil {
				if err == ErrJobQueueClosed {
					return
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				s.Logger.Error("Failed to dequeue job", "error", err)
				continue
			}

			// Process the job
			if err := s.ProcessJob(ctx, job); err != nil {
				s.Logger.Error("Job processing failed", "error", err, "taskID", job.TaskID, "contextID", job.ContextID)
			}
		}
	}
}

// processJobMonitoring handles both heartbeat and cancellation monitoring in a single goroutine
func (s *AgentService) processJobMonitoring(ctx context.Context, wg *sync.WaitGroup, job *Job, agentCancel context.CancelFunc) {
	defer wg.Done()

	// Prepare heartbeat function - use dummy if nil
	extendTimeoutFunc := job.ExtendTimeoutFunc
	if extendTimeoutFunc == nil {
		extendTimeoutFunc = func(context.Context, time.Duration) error { return nil }
	}

	heartbeatTicker := time.NewTicker(s.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	cancelTicker := time.NewTicker(s.CancelMonitoringInterval)
	defer cancelTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeatTicker.C:
			if job.ExtendTimeoutFunc != nil { // Only send heartbeat if function is provided
				if err := extendTimeoutFunc(ctx, s.HeartbeatInterval*2); err != nil {
					s.Logger.Error("Heartbeat failed - cancelling agent execution",
						"error", err, "taskID", job.TaskID, "contextID", job.ContextID)
					agentCancel()
					return
				}
				s.Logger.Debug("Heartbeat sent successfully", "taskID", job.TaskID, "contextID", job.ContextID)
			}

		case <-cancelTicker.C:
			if s.checkAndHandleTaskCancellation(ctx, job.TaskID, agentCancel) {
				return // Task was cancelled
			}
		}
	}
}

// checkAndHandleTaskCancellation checks if task is cancelled and handles it
// Returns true if task was cancelled and agent execution should stop
func (s *AgentService) checkAndHandleTaskCancellation(ctx context.Context, taskID string, agentCancel context.CancelFunc) bool {
	task, _, err := s.Storage.GetTask(ctx, taskID, 1) // Only need latest status
	if err != nil {
		s.Logger.Debug("Failed to check task status for cancellation", "error", err, "taskID", taskID)
		// Continue monitoring on transient errors - don't stop agent execution
		// Critical: if we can't check task status, assume task is still running
		return false
	}

	if task.Status.State == a2a.TaskStateCanceled {
		s.Logger.Debug("Task cancellation detected - cancelling agent execution", "taskID", taskID)
		agentCancel()
		return true
	}

	// Also check for other terminal states that should stop processing
	if task.Status.State.IsTerminal() {
		s.Logger.Debug("Task is in terminal state - cancelling agent execution", "taskID", taskID, "state", task.Status.State)
		agentCancel()
		return true
	}

	return false
}

// Capability detection based on Storage interface implementations
func (s *AgentService) pushNotificationsEnabled() bool {
	return !s.DisablePushNotifications && s.PushNotifier != nil
}

// SendMessage implements the core A2A message sending workflow
func (s *AgentService) SendMessage(ctx context.Context, params a2a.MessageSendParams) (*a2a.SendMessageResult, error) {
	// Process message using common logic
	taskID, contextID, shouldExecuteAgent, err := s.processMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	// Execute agent if needed - always enqueue for unified architecture
	if shouldExecuteAgent {
		// Determine configuration from MessageSendConfiguration
		var acceptedOutputModes []string
		if params.Configuration != nil {
			acceptedOutputModes = params.Configuration.AcceptedOutputModes
		}

		jobConfig := JobConfig{
			TaskID:              taskID,
			ContextID:           contextID,
			AcceptedOutputModes: acceptedOutputModes,
			HTTPHeaders:         transport.GetHTTPHeaders(ctx),
		}

		if err := s.JobQueue.Enqueue(ctx, jobConfig); err != nil {
			s.Logger.Error("Failed to enqueue agent job", "error", err, "taskID", taskID, "contextID", contextID)
			return nil, fmt.Errorf("failed to enqueue agent job: %w", err)
		}

		// Check blocking configuration
		blocking := true // Default to blocking (synchronous)
		if params.Configuration != nil {
			blocking = params.Configuration.Blocking
		}

		// Wait for completion if blocking mode
		if blocking {
			if err := s.waitForTaskCompletion(ctx, contextID, taskID); err != nil {
				return nil, fmt.Errorf("failed to wait for task completion: %w", err)
			}
		}
	}

	// Retrieve final task state
	historyLength := HistoryLengthAll
	if params.Configuration != nil && params.Configuration.HistoryLength != nil {
		historyLength = *params.Configuration.HistoryLength
	}
	finalTask, _, err := s.Storage.GetTask(ctx, taskID, historyLength)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve final task: %w", err)
	}

	// Create and return SendMessageResult
	result := &a2a.SendMessageResult{
		Task: finalTask,
	}

	return result, nil
}

func (s *AgentService) SendStreamingMessage(ctx context.Context, params a2a.MessageSendParams) (<-chan a2a.StreamResponse, error) {
	// Check if streaming is disabled
	if s.DisableStreaming {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeUnsupportedOperation, nil)
	}

	// Check if Blocking mode is requested - not allowed for streaming
	if params.Configuration != nil && params.Configuration.Blocking {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeBlockingNotAllowed, nil)
	}

	// Process message using common logic
	taskID, contextID, shouldExecuteAgent, err := s.processMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	// Return streaming channel using common logic
	return s.streamTaskEvents(ctx, contextID, taskID, shouldExecuteAgent, params.Configuration), nil
}

// GetTask retrieves the current state of a task from the agent
func (s *AgentService) GetTask(ctx context.Context, params a2a.TaskQueryParams) (*a2a.Task, error) {
	historyLength := HistoryLengthAll
	if params.HistoryLength != nil {
		historyLength = *params.HistoryLength
	}
	task, _, err := s.Storage.GetTask(ctx, params.ID, historyLength)
	if err != nil {
		// Check if this is a "not found" error
		if errors.Is(err, ErrTaskNotFound) {
			return nil, a2a.NewJSONRPCTaskNotFoundError(params.ID)
		}
		return nil, fmt.Errorf("failed to get task %s: %w", params.ID, err)
	}

	return task, nil
}

func (s *AgentService) CancelTask(ctx context.Context, params a2a.TaskIDParams) (*a2a.Task, error) {
	// Get current task
	task, _, err := s.Storage.GetTask(ctx, params.ID, 1) // Only need current status
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			return nil, a2a.NewJSONRPCTaskNotFoundError(params.ID)
		}
		return nil, fmt.Errorf("failed to get task %s: %w", params.ID, err)
	}

	// Check if task can be cancelled
	if !task.Status.State.CanCancel() {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeTaskNotCancelable, map[string]string{"taskId": params.ID})
	}

	// Update task status to cancelled
	if _, err := s.updateStatus(ctx, params.ID, a2a.TaskStateCanceled, []a2a.Part{
		a2a.NewTextPart("Task cancelled by user"),
	}); err != nil {
		return nil, fmt.Errorf("failed to cancel task %s: %w", params.ID, err)
	}

	// Return updated task
	updatedTask, _, err := s.Storage.GetTask(ctx, params.ID, HistoryLengthAll)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cancelled task %s: %w", params.ID, err)
	}

	return updatedTask, nil
}

func (s *AgentService) TaskResubscription(ctx context.Context, params a2a.TaskIDParams) (<-chan a2a.StreamResponse, error) {
	// Check if streaming is disabled
	if s.DisableStreaming {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeUnsupportedOperation, nil)
	}

	// Verify task exists and get its contextID
	task, _, err := s.Storage.GetTask(ctx, params.ID, 1) // Only need basic info
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			return nil, a2a.NewJSONRPCTaskNotFoundError(params.ID)
		}
		return nil, fmt.Errorf("failed to get task %s: %w", params.ID, err)
	}

	// Return streaming channel for existing task (no agent execution needed)
	// For TaskResubscription, no configuration is needed since no new message is being sent
	return s.streamTaskEvents(ctx, task.ContextID, params.ID, false, nil), nil
}

// processExistingTask handles adding messages to existing tasks
func (s *AgentService) processExistingTask(ctx context.Context, params a2a.MessageSendParams) (taskID, contextID string, shouldExecuteAgent bool, err error) {
	taskID = params.Message.TaskID

	// Retrieve existing task
	existingTask, _, err := s.Storage.GetTask(ctx, taskID, HistoryLengthAll)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			return "", "", false, a2a.NewJSONRPCTaskNotFoundError(taskID)
		}
		return "", "", false, fmt.Errorf("failed to retrieve existing task %s: %w", taskID, err)
	}

	contextID = existingTask.ContextID

	// Check if task is in terminal state - A2A Specification (PR #608)
	// Terminal tasks cannot be restarted, client should create new task
	if existingTask.Status.State.IsTerminal() {
		return "", "", false, a2a.NewJSONRPCError(a2a.ErrorCodeInvalidParams, map[string]string{
			"reason":       "Cannot add messages to terminal task. Create new task with same contextId for follow-up.",
			"currentState": string(existingTask.Status.State),
			"suggestion":   "Use referenceTaskIds to reference previous task",
		})
	}

	// Add user message to existing task using event sourcing
	if _, err := s.addMessage(ctx, taskID, params.Message.MessageID, a2a.RoleUser, params.Message.Parts, func(mo *a2a.MessageOptions) {
		mo.Extensions = params.Message.Extensions
		mo.Metadata = params.Message.Metadata
		mo.ReferenceTaskIDs = params.Message.ReferenceTaskIDs
	}); err != nil {
		return "", "", false, fmt.Errorf("failed to add message to existing task: %w", err)
	}

	// Only interrupted tasks (input-required) can restart agent execution
	shouldExecuteAgent = existingTask.Status.State.IsInterrupted()

	return taskID, contextID, shouldExecuteAgent, nil
}

// createNewTaskFromMessage creates a new task from message parameters
func (s *AgentService) createNewTaskFromMessage(ctx context.Context, taskID, contextID string, params a2a.MessageSendParams) error {
	userMessage := a2a.NewMessage(
		params.Message.MessageID,
		a2a.RoleUser,
		params.Message.Parts,
	)
	userMessage.ReferenceTaskIDs = params.Message.ReferenceTaskIDs // Copy reference task IDs if any
	userMessage.Extensions = params.Message.Extensions             // Copy extensions if any
	userMessage.Metadata = params.Message.Metadata                 // Copy metadata if any

	// Create new task using TaskUpdater
	_, err := s.createNewTask(ctx, taskID, contextID, userMessage)
	return err
}

// processMessage handles common message processing logic for both SendMessage and SendStreamingMessage
func (s *AgentService) processMessage(ctx context.Context, params a2a.MessageSendParams) (taskID, contextID string, shouldExecuteAgent bool, err error) {
	// Get IDGenerator (lazy initialization)
	idGen := s.getIDGenerator()

	// Validate message role
	if params.Message.Role != "" && params.Message.Role != a2a.RoleUser {
		return "", "", false, a2a.NewJSONRPCError(a2a.ErrorCodeInvalidParams, map[string]string{
			"reason": "Message role must be 'user'",
			"role":   params.Message.Role.String(),
		})
	}

	// Ensure message has proper ID if not set
	if params.Message.MessageID == "" {
		params.Message.MessageID = idGen.GenerateMessageID()
	}

	// TaskID explicitly specified → add to existing task
	if params.Message.TaskID != "" {
		return s.processExistingTask(ctx, params)
	}

	// ContextID specified or generate new one → always create new task
	if params.Message.ContextID != "" {
		contextID = params.Message.ContextID
	} else {
		contextID = idGen.GenerateContextID()
	}

	// Generate new task ID and create task
	taskID = idGen.GenerateTaskID()

	if err := s.createNewTaskFromMessage(ctx, taskID, contextID, params); err != nil {
		return "", "", false, fmt.Errorf("failed to create new task: %w", err)
	}

	// New tasks always trigger agent execution
	shouldExecuteAgent = true

	return taskID, contextID, shouldExecuteAgent, nil
}

// waitForTaskCompletion waits for task completion using event streaming (blocking mode)
func (s *AgentService) waitForTaskCompletion(ctx context.Context, contextID, taskID string) error {
	var lastVersion uint64 = 0
	ticker := time.NewTicker(s.StreamingPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Poll for new events
			events, nextVersion, err := s.Storage.Load(ctx, contextID, taskID, lastVersion, 100)
			if err != nil {
				// Continue polling on error
				continue
			}

			// Check for task completion
			for _, event := range events {
				if event.Status != nil && event.Status.Final {
					return nil // Task completed
				}
			}

			// Update version for next poll
			if nextVersion > lastVersion {
				lastVersion = nextVersion
			}
		}
	}
}

// streamTaskEvents streams task events using event streaming (streaming mode)
func (s *AgentService) streamTaskEvents(ctx context.Context, contextID, taskID string, shouldExecuteAgent bool, config *a2a.MessageSendConfiguration) <-chan a2a.StreamResponse {
	streamChan := make(chan a2a.StreamResponse, 10) // Buffered channel

	go func() {
		defer close(streamChan)

		// Execute agent if needed - always async for streaming
		if shouldExecuteAgent {
			// Extract AcceptedOutputModes from config
			var acceptedOutputModes []string
			if config != nil {
				acceptedOutputModes = config.AcceptedOutputModes
			}

			// Enqueue job for asynchronous execution
			jobConfig := JobConfig{
				TaskID:              taskID,
				ContextID:           contextID,
				AcceptedOutputModes: acceptedOutputModes,
				HTTPHeaders:         transport.GetHTTPHeaders(ctx),
			}
			if err := s.JobQueue.Enqueue(ctx, jobConfig); err != nil {
				s.Logger.Error("Failed to enqueue agent job for streaming", "error", err, "taskID", taskID, "contextID", contextID)
				// Don't return error - continue with streaming, job will just not be processed
			}
		}

		// Start polling for updates
		var lastVersion uint64 = 0
		ticker := time.NewTicker(s.StreamingPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled, stop streaming
				return
			case <-ticker.C:
				// Poll for new events
				events, nextVersion, err := s.Storage.Load(ctx, contextID, taskID, lastVersion, 100) // Limit to 100 events per poll
				if err != nil {
					// Continue polling on error
					continue
				}

				// Send events to stream
				for _, event := range events {
					// Apply historyLength filtering for Task events
					filteredEvent := event
					if event.Task != nil && config != nil && config.HistoryLength != nil {
						historyLength := *config.HistoryLength

						// Only filter if historyLength >= 0 (negative values mean all history)
						if historyLength >= 0 {
							// Create a copy of the task to avoid modifying the original
							taskCopy := *event.Task

							// Apply history filtering based on historyLength
							if historyLength == 0 {
								// Empty history
								taskCopy.History = []a2a.Message{}
							} else if historyLength < len(event.Task.History) {
								// Return the last historyLength messages
								taskCopy.History = event.Task.History[len(event.Task.History)-historyLength:]
							}
							// If historyLength >= len(History), no filtering needed (keep all history)

							filteredEvent = event
							filteredEvent.Task = &taskCopy
						}
					}

					select {
					case streamChan <- filteredEvent:
						// Event sent successfully
					case <-ctx.Done():
						return
					}
				}

				// Update version for next poll
				if nextVersion > lastVersion {
					lastVersion = nextVersion
				}

				// Check if task is complete
				if len(events) > 0 {
					// Check the last event to see if task is terminal
					lastEvent := events[len(events)-1]
					if lastEvent.Status != nil && lastEvent.Status.Final {
						// Task is complete, stop streaming
						return
					}
				}
			}
		}
	}()

	return streamChan
}

func (s *AgentService) SetTaskPushNotificationConfig(ctx context.Context, config a2a.TaskPushNotificationConfig) (*a2a.TaskPushNotificationConfig, error) {
	// Check if push notifications are enabled
	if !s.pushNotificationsEnabled() {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodePushNotificationNotSupported, nil)
	}

	// Generate ID for the push notification config (server always generates, ignore client-provided ID)
	idGen := s.getIDGenerator()
	config.PushNotificationConfig.ID = idGen.GeneratePushNotificationConfigID()

	// Validate endpoint before saving
	if err := s.PushNotifier.ValidateEndpoint(ctx, config.PushNotificationConfig); err != nil {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeInvalidParams, map[string]string{"validation_error": err.Error()})
	}

	// Save the push notification configuration
	if err := s.Storage.SaveTaskPushNotificationConfig(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to save push notification config: %w", err)
	}

	// Return the saved configuration
	return &config, nil
}

func (s *AgentService) GetTaskPushNotificationConfig(ctx context.Context, params a2a.GetTaskPushNotificationConfigParams) (*a2a.TaskPushNotificationConfig, error) {
	// Check if push notifications are enabled
	if !s.pushNotificationsEnabled() {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodePushNotificationNotSupported, nil)
	}

	// Get the push notification configuration
	config, err := s.Storage.GetTaskPushNotificationConfig(ctx, params.ID, params.PushNotificationConfigID)
	if err != nil {
		if errors.Is(err, ErrPushNotificationConfigNotFound) {
			return nil, a2a.NewJSONRPCError(a2a.ErrorCodeInvalidParams, map[string]string{"error": "push notification config not found"})
		}
		return nil, fmt.Errorf("failed to get push notification config for task %s: %w", params.ID, err)
	}

	return &config, nil
}

func (s *AgentService) SupportedOutputModes(ctx context.Context) ([]string, error) {
	// Check if the Agent supports output modes
	if s.Agent == nil {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeUnsupportedOperation, nil)
	}

	// Get supported output modes from Agent metadata
	meta, err := s.Agent.GetMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent metadata: %w", err)
	}
	outputModes := make([]string, 0, len(meta.DefaultOutputModes))
	outputModes = append(outputModes, meta.DefaultOutputModes...)
	for _, skil := range meta.Skills {
		if skil.OutputModes != nil {
			outputModes = append(outputModes, skil.OutputModes...)
		}
	}
	slices.Sort(outputModes)
	return slices.Compact(outputModes), nil
}

// GetAgentServiceFromContext retrieves the AgentService from the given context.
// If extensionURIs are provided, returns an AgentService with those extensions applied.
// Returns error if no AgentService is found in the context or extension processing fails.
func GetAgentServiceFromContext(ctx context.Context, extensionURIs ...string) (transport.AgentService, error) {
	svc, ok := ctx.Value(agentServiceContextKey).(*AgentService)
	if !ok {
		return nil, errors.New("no AgentService found in context")
	}

	// If no extensions requested, return the base service
	if len(extensionURIs) == 0 {
		return svc, nil
	}

	// Get extensions from context
	extensions, ok := ctx.Value(extensionsContextKey).([]transport.Extension)
	if !ok || len(extensions) == 0 {
		// No extensions available, return base service
		return svc, nil
	}

	// Use transport.WrapAgentServiceWithExtensions to create extension-aware service
	return transport.WrapAgentServiceWithExtensions(svc, extensions, extensionURIs)
}

func (s *AgentService) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	meta, err := s.Agent.GetMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent metadata: %w", err)
	}
	card := &a2a.AgentCard{
		Name:               meta.Name,
		Version:            meta.Version, // Configurable version from AgentService
		Description:        meta.Description,
		URL:                s.BaseEndpoint,      // Configurable base endpoint from AgentService
		ProtocolVersion:    a2a.ProtocolVersion, // Required A2A protocol version
		Skills:             meta.Skills,
		DefaultInputModes:  meta.DefaultInputModes,
		DefaultOutputModes: meta.DefaultOutputModes,
		Provider:           meta.Provider,
	}
	if card.Version == "" {
		card.Version = "v0.0.0"
	}
	card.Capabilities.Streaming = !s.DisableStreaming
	card.Capabilities.PushNotifications = s.pushNotificationsEnabled()
	card.Capabilities.StateTransitionHistory = false // Permanently disabled - A2A 0.3.x placeholder feature
	return card, nil
}

func (s *AgentService) ListTaskPushNotificationConfig(ctx context.Context, params a2a.TaskIDParams) ([]a2a.TaskPushNotificationConfig, error) {
	// Check if push notifications are enabled
	if !s.pushNotificationsEnabled() {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodePushNotificationNotSupported, nil)
	}

	// List all push notification configurations for the task
	configs, err := s.Storage.ListTaskPushNotificationConfig(ctx, params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list push notification configs for task %s: %w", params.ID, err)
	}

	return configs, nil
}

// Server provides a high-level interface for running an A2A agent service
// with sensible defaults and easy configuration, similar to http.Server
type Server struct {
	// Addr specifies the TCP address for the server to listen on,
	// in the form "host:port". If empty, ":80" is used.
	Addr string

	// RPCPath specifies the base path for the JSON-RPC API.
	RPCPath string

	// AgentCardPath specifies the path for the agent card endpoint.
	AgentCardPath string

	// Storage specifies the storage backend for tasks and events.
	// If nil, a FileSystemStorage with default location is used.
	Storage Storage

	// JobQueue specifies the job queue for asynchronous agent execution.
	// If nil, an InMemoryJobQueue with default size is used.
	JobQueue JobQueue

	// Agent specifies the agent implementation.
	// This field is required and cannot be nil.
	Agent Agent

	// Authenticator specifies the authentication provider for the server.
	// If nil, no authentication is required.
	Authenticator transport.Authenticator

	// Extensions specifies the extensions to be used by the server.
	// Extensions are applied to the transport layer.
	Extensions []transport.Extension

	LambdaOptions []lambda.Option // Options for AWS Lambda integration

	// Internal fields
	agentService   *AgentService
	httpServer     *http.Server
	mux            *http.ServeMux                    // HTTP request multiplexer
	customHandlers map[string]http.Handler           // Custom handlers stored before initialization
	middlewares    []func(http.Handler) http.Handler // HTTP middlewares applied to all requests
	mu             sync.Mutex
}

// AddExtension adds A2A protocol extensions to the server.
// Extensions are applied to the transport layer for request/response processing.
func (s *Server) AddExtension(extensions ...transport.Extension) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Extensions == nil {
		s.Extensions = make([]transport.Extension, 0)
	}
	s.Extensions = append(s.Extensions, extensions...)
}

// Use adds HTTP middlewares to the server.
// Middlewares are applied to all HTTP requests in the order they are added.
func (s *Server) Use(middlewares ...func(http.Handler) http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middlewares = append(s.middlewares, middlewares...)
}

// Run starts the server and blocks until the server shuts down.
func (s *Server) Run() error {
	return s.RunWithContext(context.Background())
}

// RunWithContext starts the server with the given context and blocks until
// the server shuts down or the context is cancelled.
func (s *Server) RunWithContext(ctx context.Context) error {
	if err := s.initialize(); err != nil {
		return err
	}

	if ridge.OnLambdaRuntime() {
		// If running on AWS Lambda, use the Lambda handler
		return s.runOnLambdaRuntime(ctx)
	}

	// Start the agent service
	if err := s.agentService.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent service: %w", err)
	}

	// Start HTTP server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		// Context cancelled, shutdown gracefully
		return s.Shutdown(context.Background())
	case err := <-errChan:
		// Server error
		return err
	}
}

type EventParser interface {
	// ParseEvent parses an event and returns a JobConfig for it
	ParseEvent(ctx context.Context, event json.RawMessage) (*Job, error)
}

type eventParserFunc func(ctx context.Context, event json.RawMessage) (*Job, error)

func (f eventParserFunc) ParseEvent(ctx context.Context, event json.RawMessage) (*Job, error) {
	return f(ctx, event)
}

var (
	ErrSkipEvent = errors.New("skip event") // Special error to skip processing an event
)

func (s *Server) runOnLambdaRuntime(ctx context.Context) error {
	// If running on AWS Lambda, use the Lambda handler
	eventParser, ok := s.JobQueue.(EventParser)
	if !ok {
		slog.WarnContext(ctx, "JobQueue does not implement JobQueueWithEventParser, ignore no HTTP event")
		eventParser = eventParserFunc(func(ctx context.Context, event json.RawMessage) (*Job, error) {
			slog.DebugContext(ctx, "No event parser available, ignoring HTTP event", "payload", string(event))
			return nil, ErrSkipEvent
		})
	}
	card, err := s.agentService.GetAgentCard(ctx) // Ensure agent card is initialized
	if err != nil {
		return fmt.Errorf("failed to get agent card: %w", err)
	}
	opts := append([]lambda.Option{
		lambda.WithContext(ctx),
	}, s.LambdaOptions...)
	lambda.StartWithOptions(
		func(ctx context.Context, event json.RawMessage) (interface{}, error) {
			if req, err := ridge.NewRequest(event); err == nil && req.Method != "" && req.URL.Path != "" {
				if card.Capabilities.Streaming {
					w := ridge.NewStreamingResponseWriter()
					go func() {
						defer func() {
							if r := recover(); r != nil {
								slog.ErrorContext(ctx, "Panic in streaming handler", "panic", r)
							}
							w.Close()
						}()
						s.mux.ServeHTTP(w, req.WithContext(ctx))
					}()
					w.Wait()
					return w.Response(), nil
				}
				w := ridge.NewResponseWriter()
				s.mux.ServeHTTP(w, req.WithContext(ctx))
				return w.Response(), nil
			}
			job, err := eventParser.ParseEvent(ctx, event)
			if err != nil {
				if errors.Is(err, ErrSkipEvent) {
					slog.DebugContext(ctx, "Skipping event processing", "error", err)
					return json.RawMessage(`"skipped"`), nil
				}
				slog.ErrorContext(ctx, "Failed to parse event", "error", err, "payload", string(event))
				return nil, fmt.Errorf("failed to parse event: %w", err)
			}
			if err := s.agentService.ProcessJob(ctx, job); err != nil {
				slog.ErrorContext(ctx, "Failed to process job", "error", err)
				return nil, fmt.Errorf("failed to process job: %w", err)
			}
			return json.RawMessage(`"job processed"`), nil
		},
		opts...,
	)
	return nil
}

// Shutdown gracefully shuts down the server without interrupting any
// active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	// Shutdown HTTP server
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown error: %w", err))
		}
	}

	// Close agent service
	if s.agentService != nil {
		if err := s.agentService.Close(); err != nil {
			errs = append(errs, fmt.Errorf("agent service close error: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// initialize sets up the server with default values if not configured
func (s *Server) initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Agent is required
	if s.Agent == nil {
		return errors.New("Agent field is required and cannot be nil")
	}

	// Set default address
	if s.Addr == "" {
		s.Addr = ":80"
	}

	// Set default storage if not provided
	if s.Storage == nil {
		storageDir := "/tmp/a2a"
		if envDir := os.Getenv("A2A_STORAGE_DIR"); envDir != "" {
			storageDir = envDir
		}

		var err error
		s.Storage, err = NewFileSystemStorage(storageDir)
		if err != nil {
			return fmt.Errorf("failed to create default storage: %w", err)
		}
	}

	// Set default job queue if not provided
	if s.JobQueue == nil {
		s.JobQueue = NewInMemoryJobQueue(100)
	}

	// Create agent service if not already initialized
	if s.agentService == nil {
		s.agentService = NewAgentService(s.Storage, s.Agent)
		s.agentService.JobQueue = s.JobQueue
	}

	// Automatically inject AgentService into request context for custom handlers
	// This middleware is prepended to ensure it runs before any user-defined middlewares
	s.middlewares = append([]func(http.Handler) http.Handler{
		s.injectAgentServiceMiddleware,
	}, s.middlewares...)
	if s.RPCPath == "" {
		s.RPCPath = transport.DefaultRPCPath
	}
	if s.AgentCardPath == "" {
		s.AgentCardPath = transport.DefaultAgentCardPath
	}
	if s.mux == nil {
		handlerOptions := []transport.HandlerOption{
			transport.WithRPCPath(s.RPCPath),
			transport.WithAgentCardPath(s.AgentCardPath),
		}
		if s.Authenticator != nil {
			handlerOptions = append(handlerOptions, transport.WithAuthenticator(s.Authenticator))
		}
		if len(s.Extensions) > 0 {
			handlerOptions = append(handlerOptions, transport.WithExtensions(s.Extensions...))
		}
		handler := transport.NewHandler(s.agentService, handlerOptions...)
		s.mux = http.NewServeMux()
		s.mux.Handle(s.RPCPath, handler)
		s.mux.Handle(s.AgentCardPath, handler)

		// Register custom handlers
		for pattern, customHandler := range s.customHandlers {
			s.mux.Handle(pattern, customHandler)
		}
		s.customHandlers = nil // Clear to free memory
	}

	// Create HTTP server if not already initialized
	if s.httpServer == nil {
		// Apply middleware chain to the mux
		handler := s.applyMiddleware(s.mux)

		s.httpServer = &http.Server{
			Addr:              s.Addr,
			Handler:           handler,
			ReadHeaderTimeout: 30 * time.Second,
		}
	}

	return nil
}

// isProtectedPattern checks if the pattern conflicts with A2A endpoints
func (s *Server) isProtectedPattern(pattern string) bool {
	// Check against current RPC and AgentCard paths
	rpcPath := s.RPCPath
	if rpcPath == "" {
		rpcPath = transport.DefaultRPCPath
	}

	agentCardPath := s.AgentCardPath
	if agentCardPath == "" {
		agentCardPath = transport.DefaultAgentCardPath
	}

	return pattern == rpcPath || pattern == agentCardPath
}

// Handle registers a handler for the given pattern.
// It panics if the pattern is already registered or conflicts with A2A endpoints.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for A2A endpoint conflicts
	if s.isProtectedPattern(pattern) {
		panic(fmt.Sprintf("pattern %s conflicts with A2A endpoints", pattern))
	}

	// If mux is already initialized, register directly
	if s.mux != nil {
		s.mux.Handle(pattern, handler)
		return
	}

	// Store for later registration during initialization
	if s.customHandlers == nil {
		s.customHandlers = make(map[string]http.Handler)
	}

	// Check for duplicate patterns in custom handlers
	if _, exists := s.customHandlers[pattern]; exists {
		panic(fmt.Sprintf("http: multiple registrations for %s", pattern))
	}

	s.customHandlers[pattern] = handler
}

// HandleFunc registers a handler function for the given pattern.
// It panics if the pattern is already registered or conflicts with A2A endpoints.
func (s *Server) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.Handle(pattern, http.HandlerFunc(handler))
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler(r *http.Request) (http.Handler, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure server is initialized
	if s.mux == nil {
		if err := s.initialize(); err != nil {
			panic(fmt.Sprintf("failed to initialize server: %v", err))
		}
	}

	return s.mux.Handler(r)
}

func (s *AgentService) DeleteTaskPushNotificationConfig(ctx context.Context, params a2a.DeleteTaskPushNotificationConfigParams) error {
	// Check if push notifications are enabled
	if !s.pushNotificationsEnabled() {
		return a2a.NewJSONRPCError(a2a.ErrorCodePushNotificationNotSupported, nil)
	}

	// Delete the push notification configuration
	if err := s.Storage.DeleteTaskPushNotificationConfig(ctx, params.ID, params.PushNotificationConfigID); err != nil {
		if errors.Is(err, ErrPushNotificationConfigNotFound) {
			return a2a.NewJSONRPCError(a2a.ErrorCodeInvalidParams, map[string]string{"error": "push notification config not found"})
		}
		return fmt.Errorf("failed to delete push notification config for task %s: %w", params.ID, err)
	}

	return nil
}

// injectAgentServiceMiddleware injects the AgentService and Extensions into the request context
// This middleware is automatically applied to all requests to make AgentService and Extensions
// available to custom handlers via GetAgentServiceFromContext()
func (s *Server) injectAgentServiceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), agentServiceContextKey, s.agentService)
		ctx = context.WithValue(ctx, extensionsContextKey, s.Extensions)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// applyMiddleware applies all registered middlewares to the given handler
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// Apply middlewares in registration order (first registered wraps outermost)
	result := handler
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		result = s.middlewares[i](result)
	}
	return result
}
