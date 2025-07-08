package atlasic

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mashiike/atlasic/a2a"
	"go.uber.org/mock/gomock"
)

func TestInMemoryJobQueue_EnqueueDequeue(t *testing.T) {
	queue := NewInMemoryJobQueue(10)
	defer queue.Close()

	ctx := context.Background()

	// Enqueue a job
	err := queue.Enqueue(ctx, JobConfig{TaskID: "task-001", ContextID: "context-001"})
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Dequeue the job
	job, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Failed to dequeue job: %v", err)
	}

	// Verify job content
	if job.TaskID != "task-001" {
		t.Errorf("Expected TaskID 'task-001', got %s", job.TaskID)
	}
	if job.ContextID != "context-001" {
		t.Errorf("Expected ContextID 'context-001', got %s", job.ContextID)
	}

	// Verify function fields are not nil
	if job.ExtendTimeoutFunc == nil {
		t.Error("ExtendTimeoutFunc should not be nil")
	}
	if job.CompleteFunc == nil {
		t.Error("CompleteFunc should not be nil")
	}
	if job.FailFunc == nil {
		t.Error("FailFunc should not be nil")
	}

	// Test function calls (should be no-ops)
	err = job.ExtendTimeoutFunc(ctx, time.Minute)
	if err != nil {
		t.Errorf("ExtendTimeoutFunc should not return error: %v", err)
	}

	err = job.CompleteFunc()
	if err != nil {
		t.Errorf("CompleteFunc should not return error: %v", err)
	}

	err = job.FailFunc()
	if err != nil {
		t.Errorf("FailFunc should not return error: %v", err)
	}
}

func TestInMemoryJobQueue_MultipleJobs(t *testing.T) {
	queue := NewInMemoryJobQueue(5)
	defer queue.Close()

	ctx := context.Background()

	// Enqueue multiple jobs
	expectedJobs := []struct {
		taskID    string
		contextID string
	}{
		{"task-001", "context-001"},
		{"task-002", "context-002"},
		{"task-003", "context-001"}, // Same context, different task
	}

	for _, expected := range expectedJobs {
		err := queue.Enqueue(ctx, JobConfig{TaskID: expected.taskID, ContextID: expected.contextID})
		if err != nil {
			t.Fatalf("Failed to enqueue job %s: %v", expected.taskID, err)
		}
	}

	// Dequeue and verify jobs (FIFO order)
	for i, expected := range expectedJobs {
		job, err := queue.Dequeue(ctx)
		if err != nil {
			t.Fatalf("Failed to dequeue job %d: %v", i, err)
		}

		if job.TaskID != expected.taskID {
			t.Errorf("Job %d: Expected TaskID %s, got %s", i, expected.taskID, job.TaskID)
		}
		if job.ContextID != expected.contextID {
			t.Errorf("Job %d: Expected ContextID %s, got %s", i, expected.contextID, job.ContextID)
		}
	}
}

func TestInMemoryJobQueue_ContextCancellation(t *testing.T) {
	queue := NewInMemoryJobQueue(1)
	defer queue.Close()

	// Fill the queue first
	ctx := context.Background()
	err := queue.Enqueue(ctx, JobConfig{TaskID: "task-000", ContextID: "context-000"})
	if err != nil {
		t.Fatalf("Failed to fill queue: %v", err)
	}

	// Test enqueue with cancelled context (should block then get cancelled)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = queue.Enqueue(cancelledCtx, JobConfig{TaskID: "task-001", ContextID: "context-001"})
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled for enqueue, got %v", err)
	}

	// Test dequeue with cancelled context on empty queue
	// First drain the queue
	job, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Failed to drain queue: %v", err)
	}
	if job.TaskID != "task-000" {
		t.Errorf("Expected drained job task-000, got %s", job.TaskID)
	}

	// Now test dequeue with cancelled context on empty queue
	cancelledCtx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	job, err = queue.Dequeue(cancelledCtx2)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled for dequeue, got %v", err)
	}
	if job != nil {
		t.Error("Job should be nil when dequeue fails")
	}
}

func TestInMemoryJobQueue_Close(t *testing.T) {
	queue := NewInMemoryJobQueue(5)

	ctx := context.Background()

	// Enqueue a job
	err := queue.Enqueue(ctx, JobConfig{TaskID: "task-001", ContextID: "context-001"})
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Close the queue
	err = queue.Close()
	if err != nil {
		t.Fatalf("Failed to close queue: %v", err)
	}

	// Try to enqueue after closing
	err = queue.Enqueue(ctx, JobConfig{TaskID: "task-002", ContextID: "context-002"})
	if err != ErrJobQueueClosed {
		t.Errorf("Expected ErrJobQueueClosed for enqueue after close, got %v", err)
	}

	// Dequeue should still return the existing job first
	job, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("First dequeue after close should return existing job: %v", err)
	}
	if job.TaskID != "task-001" {
		t.Errorf("Expected existing job task-001, got %s", job.TaskID)
	}

	// Second dequeue should get ErrJobQueueClosed (channel is closed and empty)
	_, err = queue.Dequeue(ctx)
	if err != ErrJobQueueClosed {
		t.Errorf("Expected ErrJobQueueClosed for second dequeue, got %v", err)
	}

	// Multiple closes should be safe
	err = queue.Close()
	if err != nil {
		t.Errorf("Multiple closes should not return error: %v", err)
	}
}

func TestInMemoryJobQueue_Blocking(t *testing.T) {
	queue := NewInMemoryJobQueue(1)
	defer queue.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try to dequeue from empty queue (should block then timeout)
	start := time.Now()
	job, err := queue.Dequeue(ctx)
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
	if job != nil {
		t.Error("Job should be nil on timeout")
	}

	// Should have waited close to the timeout duration
	if duration < 90*time.Millisecond {
		t.Errorf("Expected to wait ~100ms, waited %v", duration)
	}
}

// ====================
// ProcessJob Tests (Enterprise Features)
// ====================

func TestJobQueue_ProcessJobHeartbeat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Setup storage expectations
	mockStorage.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(2), nil).AnyTimes()
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockStorage.EXPECT().GetTask(gomock.Any(), "test-task", gomock.Any()).Return(
		&a2a.Task{
			ID:        "test-task",
			ContextID: "test-context",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
			History:   []a2a.Message{},
		}, uint64(1), nil).AnyTimes()

	// Setup agent execution with delay to allow heartbeat
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			// Simulate long-running agent execution
			select {
			case <-time.After(150 * time.Millisecond): // Longer than heartbeat interval
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

	// Create agent service with short heartbeat interval for testing
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true // Disable for test focus
	agentService.HeartbeatInterval = 50 * time.Millisecond
	agentService.CancelMonitoringInterval = 10 * time.Millisecond

	// Track heartbeat calls
	var heartbeatCount int64
	extendTimeoutFunc := func(ctx context.Context, duration time.Duration) error {
		atomic.AddInt64(&heartbeatCount, 1)
		t.Logf("Heartbeat called, extending timeout by %v", duration)
		return nil
	}

	// Create job with heartbeat function
	job := &Job{
		TaskID:            "test-task",
		ContextID:         "test-context",
		ExtendTimeoutFunc: extendTimeoutFunc,
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Process job
	err := agentService.ProcessJob(ctx, job)
	if err != nil {
		t.Fatalf("ProcessJob failed: %v", err)
	}

	// Verify heartbeat was called at least once
	finalHeartbeatCount := atomic.LoadInt64(&heartbeatCount)
	if finalHeartbeatCount < 1 {
		t.Errorf("Expected at least 1 heartbeat call, got %d", finalHeartbeatCount)
	}
	t.Logf("Total heartbeat calls: %d", finalHeartbeatCount)
}

func TestJobQueue_ProcessJobCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Setup storage expectations - initially working, then cancelled
	callCount := 0
	mockStorage.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(2), nil).AnyTimes()
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockStorage.EXPECT().GetTask(gomock.Any(), "test-task", gomock.Any()).DoAndReturn(
		func(ctx context.Context, taskID string, historyLength int) (*a2a.Task, uint64, error) {
			callCount++
			var status a2a.TaskState
			if callCount <= 2 {
				status = a2a.TaskStateWorking // Initially working
			} else {
				status = a2a.TaskStateCanceled // Then cancelled
			}

			task := &a2a.Task{
				ID:        "test-task",
				ContextID: "test-context",
				Status:    a2a.TaskStatus{State: status},
				History:   []a2a.Message{},
			}
			return task, uint64(1), nil
		}).AnyTimes()

	// Setup agent execution that should be cancelled
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			// Wait for cancellation
			select {
			case <-time.After(500 * time.Millisecond):
				t.Error("Agent execution should have been cancelled")
				return nil, nil
			case <-ctx.Done():
				t.Log("Agent execution cancelled as expected")
				return nil, ctx.Err()
			}
		})

	// Create agent service with short monitoring interval for testing
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true       // Disable for test focus
	agentService.HeartbeatInterval = 100 * time.Second // Long interval to avoid heartbeat interference
	agentService.CancelMonitoringInterval = 20 * time.Millisecond

	// Create job without heartbeat function
	job := &Job{
		TaskID:            "test-task",
		ContextID:         "test-context",
		ExtendTimeoutFunc: nil, // No heartbeat
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Process job - should detect cancellation and cancel agent
	start := time.Now()
	err := agentService.ProcessJob(ctx, job)
	duration := time.Since(start)

	// Agent should have been cancelled due to task cancellation
	if err == nil {
		t.Error("Expected error due to cancellation")
	}

	// Should have cancelled quickly (much less than the 500ms timeout in agent)
	if duration > 200*time.Millisecond {
		t.Errorf("Expected quick cancellation, took %v", duration)
	}

	t.Logf("Cancellation detected and processed in %v", duration)
}

func TestJobQueue_ProcessJobHeartbeatFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Setup storage expectations
	mockStorage.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(2), nil).AnyTimes()
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockStorage.EXPECT().GetTask(gomock.Any(), "test-task", gomock.Any()).Return(
		&a2a.Task{
			ID:        "test-task",
			ContextID: "test-context",
			Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
			History:   []a2a.Message{},
		}, uint64(1), nil).AnyTimes()

	// Setup agent execution that should be cancelled due to heartbeat failure
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			// Wait for cancellation due to heartbeat failure
			select {
			case <-time.After(500 * time.Millisecond):
				t.Error("Agent execution should have been cancelled due to heartbeat failure")
				return nil, nil
			case <-ctx.Done():
				t.Log("Agent execution cancelled due to heartbeat failure as expected")
				return nil, ctx.Err()
			}
		})

	// Create agent service with short heartbeat interval for testing
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true // Disable for test focus
	agentService.HeartbeatInterval = 30 * time.Millisecond
	agentService.CancelMonitoringInterval = 100 * time.Second // Long interval to avoid cancellation monitoring interference

	// Heartbeat function that fails after first call
	var heartbeatCount int64
	extendTimeoutFunc := func(ctx context.Context, duration time.Duration) error {
		count := atomic.AddInt64(&heartbeatCount, 1)
		if count > 1 {
			// Fail heartbeat after first success
			return context.DeadlineExceeded
		}
		return nil
	}

	// Create job with failing heartbeat function
	job := &Job{
		TaskID:            "test-task",
		ContextID:         "test-context",
		ExtendTimeoutFunc: extendTimeoutFunc,
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Process job - should fail due to heartbeat failure
	start := time.Now()
	err := agentService.ProcessJob(ctx, job)
	duration := time.Since(start)

	// Agent should have been cancelled due to heartbeat failure
	if err == nil {
		t.Error("Expected error due to heartbeat failure")
	}

	// Should have cancelled after heartbeat failure (around 60ms for two heartbeat intervals)
	if duration > 150*time.Millisecond {
		t.Errorf("Expected quick cancellation after heartbeat failure, took %v", duration)
	}

	finalHeartbeatCount := atomic.LoadInt64(&heartbeatCount)
	if finalHeartbeatCount < 2 {
		t.Errorf("Expected at least 2 heartbeat attempts, got %d", finalHeartbeatCount)
	}

	t.Logf("Heartbeat failure detected and agent cancelled in %v (heartbeat attempts: %d)", duration, finalHeartbeatCount)
}

func TestInMemoryJobQueue_BufferFull(t *testing.T) {
	queue := NewInMemoryJobQueue(2) // Small buffer
	defer queue.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Fill the buffer
	err := queue.Enqueue(ctx, JobConfig{TaskID: "task-001", ContextID: "context-001"})
	if err != nil {
		t.Fatalf("Failed to enqueue first job: %v", err)
	}

	err = queue.Enqueue(ctx, JobConfig{TaskID: "task-002", ContextID: "context-002"})
	if err != nil {
		t.Fatalf("Failed to enqueue second job: %v", err)
	}

	// Try to enqueue when buffer is full (should block then timeout)
	start := time.Now()
	err = queue.Enqueue(ctx, JobConfig{TaskID: "task-003", ContextID: "context-003"})
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error when buffer is full")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}

	// Should have waited close to the timeout duration
	if duration < 90*time.Millisecond {
		t.Errorf("Expected to wait ~100ms, waited %v", duration)
	}
}
