package atlasic

import (
	"context"
	"testing"
	"time"

	"github.com/mashiike/atlasic/a2a"
	"go.uber.org/mock/gomock"
)

func TestInMemoryTaskLocker_BasicLocking(t *testing.T) {
	locker := NewInMemoryTaskLocker()
	defer locker.Close()

	ctx := context.Background()
	taskID := "test-task-001"

	// Acquire lock
	unlock, err := locker.Lock(ctx, taskID)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Try to acquire same lock (should fail)
	_, err = locker.Lock(ctx, taskID)
	if err != ErrTaskLockAlreadyAcquired {
		t.Errorf("Expected ErrTaskLockAlreadyAcquired, got %v", err)
	}

	// Release lock
	unlock()

	// Try to acquire lock again (should succeed)
	unlock2, err := locker.Lock(ctx, taskID)
	if err != nil {
		t.Fatalf("Failed to acquire lock after release: %v", err)
	}
	unlock2()
}

func TestInMemoryTaskLocker_MultipleTasks(t *testing.T) {
	locker := NewInMemoryTaskLocker()
	defer locker.Close()

	ctx := context.Background()

	// Acquire locks for different tasks
	unlock1, err := locker.Lock(ctx, "task-001")
	if err != nil {
		t.Fatalf("Failed to acquire lock for task-001: %v", err)
	}

	unlock2, err := locker.Lock(ctx, "task-002")
	if err != nil {
		t.Fatalf("Failed to acquire lock for task-002: %v", err)
	}

	// Try to acquire existing locks (should fail)
	_, err = locker.Lock(ctx, "task-001")
	if err != ErrTaskLockAlreadyAcquired {
		t.Errorf("Expected ErrTaskLockAlreadyAcquired for task-001, got %v", err)
	}

	_, err = locker.Lock(ctx, "task-002")
	if err != ErrTaskLockAlreadyAcquired {
		t.Errorf("Expected ErrTaskLockAlreadyAcquired for task-002, got %v", err)
	}

	// Release one lock
	unlock1()

	// Should be able to acquire the released lock
	unlock3, err := locker.Lock(ctx, "task-001")
	if err != nil {
		t.Fatalf("Failed to acquire lock for task-001 after release: %v", err)
	}

	// But not the other one
	_, err = locker.Lock(ctx, "task-002")
	if err != ErrTaskLockAlreadyAcquired {
		t.Errorf("Expected ErrTaskLockAlreadyAcquired for task-002, got %v", err)
	}

	// Clean up
	unlock2()
	unlock3()
}

func TestInMemoryTaskLocker_ContextCancellation(t *testing.T) {
	locker := NewInMemoryTaskLocker()
	defer locker.Close()

	ctx, cancel := context.WithCancel(context.Background())
	taskID := "test-task-cancel"

	// Acquire lock first
	unlock, err := locker.Lock(ctx, taskID)
	if err != nil {
		t.Fatalf("Failed to acquire initial lock: %v", err)
	}

	// Cancel context
	cancel()

	// Try to acquire lock with cancelled context
	_, err = locker.Lock(ctx, taskID)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	// Clean up
	unlock()
}

func TestInMemoryTaskLocker_Close(t *testing.T) {
	locker := NewInMemoryTaskLocker()

	ctx := context.Background()
	taskID := "test-task-close"

	// Acquire lock
	unlock, err := locker.Lock(ctx, taskID)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Close locker
	err = locker.Close()
	if err != nil {
		t.Fatalf("Failed to close locker: %v", err)
	}

	// Try to acquire lock after closing
	_, err = locker.Lock(ctx, taskID)
	if err != ErrTaskLockerClosed {
		t.Errorf("Expected ErrTaskLockerClosed, got %v", err)
	}

	// Unlock should not panic
	unlock()
}

func TestAgentService_ProcessJobWithTaskLocker(t *testing.T) {
	// This test verifies that ProcessJob works with TaskLocker
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup storage expectations
	mockStorage.EXPECT().GetTask(gomock.Any(), "test-task", gomock.Any()).Return(
		&a2a.Task{
			ID:        "test-task",
			ContextID: "test-context",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateSubmitted,
			},
			History: []a2a.Message{},
		}, uint64(1), nil).AnyTimes()

	mockStorage.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(2), nil).AnyTimes()
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Setup agent execution
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	// Create agent service with TaskLocker
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true // Disable for test focus
	agentService.DisablePushNotifications = true // Disable for JobQueue test focus
	agentService.SetIDGenerator(mockIDGen)
	agentService.TaskLocker = NewInMemoryTaskLocker()
	defer agentService.TaskLocker.Close()

	// Create job
	job := &Job{
		TaskID:            "test-task",
		ContextID:         "test-context",
		ExtendTimeoutFunc: func(context.Context, time.Duration) error { return nil },
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Process job
	err := agentService.ProcessJob(ctx, job)
	if err != nil {
		t.Fatalf("ProcessJob failed: %v", err)
	}
}

func TestAgentService_ProcessJobWithoutTaskLocker(t *testing.T) {
	// This test verifies that ProcessJob still works without TaskLocker (backward compatibility)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup storage expectations - GetTask still called by updateStatus in ProcessJob
	mockStorage.EXPECT().GetTask(gomock.Any(), "test-task", gomock.Any()).Return(
		&a2a.Task{
			ID:        "test-task",
			ContextID: "test-context",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateSubmitted,
			},
			History: []a2a.Message{},
		}, uint64(1), nil).AnyTimes()
	mockStorage.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(2), nil).AnyTimes()
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Setup agent execution
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	// Create agent service without TaskLocker
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true // Disable for test focus
	agentService.DisablePushNotifications = true // Disable for JobQueue test focus
	agentService.SetIDGenerator(mockIDGen)
	// TaskLocker is nil by default

	// Create job
	job := &Job{
		TaskID:            "test-task",
		ContextID:         "test-context",
		ExtendTimeoutFunc: func(context.Context, time.Duration) error { return nil },
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Process job
	err := agentService.ProcessJob(ctx, job)
	if err != nil {
		t.Fatalf("ProcessJob failed: %v", err)
	}
}
