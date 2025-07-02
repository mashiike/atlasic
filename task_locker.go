package atlasic

import (
	"context"
	"errors"
	"sync"
)

// TaskLocker error variables
var (
	// ErrTaskLockAlreadyAcquired is returned when a task lock is already held
	ErrTaskLockAlreadyAcquired = errors.New("task lock already acquired")
	// ErrTaskLockerClosed is returned when attempting to use a closed task locker
	ErrTaskLockerClosed = errors.New("task locker is closed")
)

// InMemoryTaskLocker is a simple in-memory implementation of TaskLocker
type InMemoryTaskLocker struct {
	locks  map[string]bool
	mu     sync.RWMutex
	closed bool
}

// NewInMemoryTaskLocker creates a new in-memory task locker
func NewInMemoryTaskLocker() *InMemoryTaskLocker {
	return &InMemoryTaskLocker{
		locks: make(map[string]bool),
	}
}

// Lock attempts to acquire a lock for the specified task
func (l *InMemoryTaskLocker) Lock(ctx context.Context, taskID string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil, ErrTaskLockerClosed
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Check if lock already exists
	if locked, exists := l.locks[taskID]; exists && locked {
		// Lock is held by someone else
		return nil, ErrTaskLockAlreadyAcquired
	}

	// Acquire the lock
	l.locks[taskID] = true

	// Return unlock function
	unlock := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if !l.closed {
			delete(l.locks, taskID)
		}
	}

	return unlock, nil
}

// Close gracefully shuts down the task locker
func (l *InMemoryTaskLocker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.closed {
		l.closed = true
		// Clear all locks
		l.locks = make(map[string]bool)
	}
	return nil
}
