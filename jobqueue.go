package atlasic

import (
	"context"
	"errors"
	"sync"
	"time"
)

// JobQueue error variables
var (
	// ErrJobQueueClosed is returned when attempting to use a closed job queue
	ErrJobQueueClosed = errors.New("job queue is closed")
)

// JobConfig represents the configuration for creating a job
type JobConfig struct {
	TaskID              string
	ContextID           string
	IncomingMessageID   string   // ID of the incoming message that triggered this job
	AcceptedOutputModes []string // Accepted output modes from MessageSendConfiguration
}

// Job represents a background job for agent execution
type Job struct {
	TaskID              string
	ContextID           string
	IncomingMessageID   string   // ID of the incoming message that triggered this job
	AcceptedOutputModes []string // Accepted output modes from MessageSendConfiguration

	// Function fields for queue operations
	ExtendTimeoutFunc func(context.Context, time.Duration) error
	CompleteFunc      func() error
	FailFunc          func() error
}

// JobQueue provides an interface for background job management
type JobQueue interface {
	// Enqueue adds a new job to the queue
	Enqueue(ctx context.Context, config JobConfig) error

	// Dequeue retrieves a job from the queue, blocking until one is available
	Dequeue(ctx context.Context) (*Job, error)

	// Close gracefully shuts down the queue
	Close() error
}

// InMemoryJobQueue is a simple in-memory implementation of JobQueue using channels
type InMemoryJobQueue struct {
	jobs   chan *Job
	closed bool
	mu     sync.RWMutex
}

// NewInMemoryJobQueue creates a new in-memory job queue with the specified buffer size
func NewInMemoryJobQueue(size int) *InMemoryJobQueue {
	return &InMemoryJobQueue{
		jobs: make(chan *Job, size),
	}
}

// Enqueue adds a new job to the queue
func (q *InMemoryJobQueue) Enqueue(ctx context.Context, config JobConfig) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return ErrJobQueueClosed
	}

	job := &Job{
		TaskID:              config.TaskID,
		ContextID:           config.ContextID,
		AcceptedOutputModes: config.AcceptedOutputModes,
		IncomingMessageID:   config.IncomingMessageID,
		// In-memory queue doesn't need actual timeout/completion handling
		ExtendTimeoutFunc: func(context.Context, time.Duration) error { return nil },
		CompleteFunc:      func() error { return nil },
		FailFunc:          func() error { return nil },
	}

	select {
	case q.jobs <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dequeue retrieves a job from the queue, blocking until one is available
func (q *InMemoryJobQueue) Dequeue(ctx context.Context) (*Job, error) {
	select {
	case job, ok := <-q.jobs:
		if !ok {
			return nil, ErrJobQueueClosed
		}
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close gracefully shuts down the queue
func (q *InMemoryJobQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		close(q.jobs)
		q.closed = true
	}
	return nil
}
