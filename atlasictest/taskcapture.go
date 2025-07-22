package atlasictest

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mashiike/atlasic/a2a"
)

// TaskCapture implements the TaskHandle interface for testing purposes.
// It captures and records all interactions during agent execution,
// similar to how httptest.ResponseRecorder works for HTTP handlers.
type TaskCapture struct {
	// Original task data
	task *a2a.Task

	// Captured interactions
	addedMessages     []CapturedMessage
	statusUpdates     []CapturedStatusUpdate
	upsertedArtifacts []a2a.Artifact

	// File operations (in-memory or temp directory)
	tempDir string
	fileOps []CapturedFileOp

	// Mutex for thread safety
	mu sync.RWMutex
}

// CapturedMessage represents a message that was added during execution
type CapturedMessage struct {
	MessageID string
	Parts     []a2a.Part
	Options   a2a.MessageOptions
	Timestamp time.Time
}

// CapturedStatusUpdate represents a status update during execution
type CapturedStatusUpdate struct {
	State     a2a.TaskState
	Parts     []a2a.Part
	Options   a2a.MessageOptions
	Status    a2a.TaskStatus
	Timestamp time.Time
}

// CapturedFileOp represents a file operation during execution
type CapturedFileOp struct {
	Operation string // "open", "list", "delete"
	Path      string
	Flag      int
	Perm      os.FileMode
	IsContext bool // true for context files, false for task files
	Timestamp time.Time
}

// NewCapture creates a new TaskCapture from the given task.
// It fills in sensible defaults for missing or incomplete task data.
func NewCapture(task *a2a.Task) *TaskCapture {
	// Create a copy of the task to avoid modifying the original
	taskCopy := *task

	// Fill in defaults for missing fields
	if taskCopy.ID == "" {
		taskCopy.ID = uuid.New().String()
	}
	if taskCopy.ContextID == "" {
		taskCopy.ContextID = uuid.New().String()
	}
	if taskCopy.Kind == "" {
		taskCopy.Kind = "task"
	}
	if taskCopy.Status.State == "" {
		taskCopy.Status.State = a2a.TaskStateSubmitted
		nowStr := time.Now().Format(time.RFC3339)
		taskCopy.Status.Timestamp = &nowStr
	}
	if taskCopy.History == nil {
		// Add a default user message if no history exists
		taskCopy.History = []a2a.Message{
			{
				Kind:      "message",
				MessageID: uuid.New().String(),
				Role:      a2a.RoleUser,
				Parts: []a2a.Part{
					a2a.NewTextPart("Test task execution"),
				},
			},
		}
	}
	if taskCopy.Artifacts == nil {
		taskCopy.Artifacts = []a2a.Artifact{}
	}
	if taskCopy.Metadata == nil {
		taskCopy.Metadata = make(map[string]interface{})
	}

	// Create temporary directory for file operations
	tempDir, err := os.MkdirTemp("", "atlasictest-capture-*")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp directory for TaskCapture: %v", err))
	}

	return &TaskCapture{
		task:              &taskCopy,
		addedMessages:     make([]CapturedMessage, 0),
		statusUpdates:     make([]CapturedStatusUpdate, 0),
		upsertedArtifacts: make([]a2a.Artifact, 0),
		tempDir:           tempDir,
		fileOps:           make([]CapturedFileOp, 0),
	}
}

// Cleanup removes the temporary directory used for file operations.
// This should be called when the TaskCapture is no longer needed.
func (tc *TaskCapture) Cleanup() error {
	if tc.tempDir != "" {
		return os.RemoveAll(tc.tempDir)
	}
	return nil
}

// GetContextID implements TaskHandle interface
func (tc *TaskCapture) GetContextID() string {
	return tc.task.ContextID
}

// GetTaskID implements TaskHandle interface
func (tc *TaskCapture) GetTaskID() string {
	return tc.task.ID
}

// GetTask implements TaskHandle interface
func (tc *TaskCapture) GetTask(ctx context.Context, historyLength int) (*a2a.Task, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	// Create a copy to avoid external modifications
	taskCopy := *tc.task

	// Limit history if requested
	if historyLength > 0 && len(taskCopy.History) > historyLength {
		taskCopy.History = taskCopy.History[:historyLength]
	}

	return &taskCopy, nil
}

// GetInitialStatus implements TaskHandle interface
func (tc *TaskCapture) GetInitialStatus() a2a.TaskStatus {
	return tc.task.Status
}

// GetAcceptedOutputModes implements TaskHandle interface
func (tc *TaskCapture) GetAcceptedOutputModes() []string {
	// Return sensible defaults for testing
	return []string{"text/plain", "application/json"}
}

// GetIncomingMessage implements TaskHandle interface
func (tc *TaskCapture) GetIncomingMessage() a2a.Message {
	if len(tc.task.History) > 0 {
		return tc.task.History[0]
	}
	// Return empty message if no history
	return a2a.Message{}
}

func (tc *TaskCapture) GetHTTPHeaders() http.Header {
	// TaskCapture for testing returns empty headers by default
	// In real scenarios, this would be populated from the original HTTP request
	return http.Header{}
}

// AddMessage implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) AddMessage(ctx context.Context, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (string, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	messageID := uuid.New().String()

	// Apply options
	var opts a2a.MessageOptions
	for _, fn := range optFns {
		fn(&opts)
	}

	// Capture the interaction
	captured := CapturedMessage{
		MessageID: messageID,
		Parts:     make([]a2a.Part, len(parts)),
		Options:   opts,
		Timestamp: time.Now(),
	}
	copy(captured.Parts, parts)
	tc.addedMessages = append(tc.addedMessages, captured)

	// Add to task history
	newMessage := a2a.Message{
		Kind:      "message",
		MessageID: messageID,
		Role:      a2a.RoleAgent,
		Parts:     parts,
	}
	tc.task.History = append(tc.task.History, newMessage)

	return messageID, nil
}

// UpdateStatus implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) UpdateStatus(ctx context.Context, state a2a.TaskState, parts []a2a.Part, optFns ...func(*a2a.MessageOptions)) (a2a.TaskStatus, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Apply options
	var opts a2a.MessageOptions
	for _, fn := range optFns {
		fn(&opts)
	}

	// Update task status
	nowStr := time.Now().Format(time.RFC3339)
	tc.task.Status.State = state
	tc.task.Status.Timestamp = &nowStr

	if len(parts) > 0 {
		// Create status message
		statusMessage := &a2a.Message{
			Kind:      "message",
			MessageID: uuid.New().String(),
			Role:      a2a.RoleAgent,
			Parts:     parts,
		}
		tc.task.Status.Message = statusMessage
	}

	// Capture the interaction
	captured := CapturedStatusUpdate{
		State:     state,
		Parts:     make([]a2a.Part, len(parts)),
		Options:   opts,
		Status:    tc.task.Status,
		Timestamp: time.Now(),
	}
	copy(captured.Parts, parts)
	tc.statusUpdates = append(tc.statusUpdates, captured)

	return tc.task.Status, nil
}

// UpsertArtifact implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) UpsertArtifact(ctx context.Context, artifact a2a.Artifact) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Find existing artifact or add new one
	found := false
	for i, existing := range tc.task.Artifacts {
		if existing.Name == artifact.Name {
			tc.task.Artifacts[i] = artifact
			found = true
			break
		}
	}

	if !found {
		tc.task.Artifacts = append(tc.task.Artifacts, artifact)
	}

	// Capture the artifact
	tc.upsertedArtifacts = append(tc.upsertedArtifacts, artifact)

	return nil
}

// OpenContextFile implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) OpenContextFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error) {
	tc.mu.Lock()

	// Capture the file operation
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "open",
		Path:      path,
		Flag:      flag,
		Perm:      perm,
		IsContext: true,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	// Create context-specific directory
	contextDir := filepath.Join(tc.tempDir, "context", tc.task.ContextID)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create context directory: %w", err)
	}

	filePath := filepath.Join(contextDir, path)

	// Create directory for file if necessary
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create file directory: %w", err)
	}

	return os.OpenFile(filePath, flag, perm)
}

// ListContextFiles implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) ListContextFiles(ctx context.Context, pathPrefix string) ([]string, error) {
	tc.mu.Lock()
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "list",
		Path:      pathPrefix,
		IsContext: true,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	contextDir := filepath.Join(tc.tempDir, "context", tc.task.ContextID)
	searchPath := filepath.Join(contextDir, pathPrefix)

	var files []string
	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, just like the real implementation might
		}
		if !d.IsDir() {
			relPath, _ := filepath.Rel(contextDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return files, nil
}

// DeleteContextFile implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) DeleteContextFile(ctx context.Context, path string) error {
	tc.mu.Lock()
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "delete",
		Path:      path,
		IsContext: true,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	contextDir := filepath.Join(tc.tempDir, "context", tc.task.ContextID)
	filePath := filepath.Join(contextDir, path)

	return os.Remove(filePath)
}

// OpenTaskFile implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) OpenTaskFile(ctx context.Context, path string, flag int, perm os.FileMode) (fs.File, error) {
	tc.mu.Lock()
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "open",
		Path:      path,
		Flag:      flag,
		Perm:      perm,
		IsContext: false,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	taskDir := filepath.Join(tc.tempDir, "task", tc.task.ID)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create task directory: %w", err)
	}

	filePath := filepath.Join(taskDir, path)

	// Create directory for file if necessary
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create file directory: %w", err)
	}

	return os.OpenFile(filePath, flag, perm)
}

// ListTaskFiles implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) ListTaskFiles(ctx context.Context, pathPrefix string) ([]string, error) {
	tc.mu.Lock()
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "list",
		Path:      pathPrefix,
		IsContext: false,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	taskDir := filepath.Join(tc.tempDir, "task", tc.task.ID)
	searchPath := filepath.Join(taskDir, pathPrefix)

	var files []string
	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !d.IsDir() {
			relPath, _ := filepath.Rel(taskDir, path)
			files = append(files, relPath)
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return files, nil
}

// DeleteTaskFile implements TaskHandle interface and captures the interaction
func (tc *TaskCapture) DeleteTaskFile(ctx context.Context, path string) error {
	tc.mu.Lock()
	tc.fileOps = append(tc.fileOps, CapturedFileOp{
		Operation: "delete",
		Path:      path,
		IsContext: false,
		Timestamp: time.Now(),
	})
	tc.mu.Unlock()

	taskDir := filepath.Join(tc.tempDir, "task", tc.task.ID)
	filePath := filepath.Join(taskDir, path)

	return os.Remove(filePath)
}

// Capture access methods for testing assertions

// Messages returns all messages that were added during execution
func (tc *TaskCapture) Messages() []CapturedMessage {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]CapturedMessage, len(tc.addedMessages))
	copy(result, tc.addedMessages)
	return result
}

// StatusUpdates returns all status updates that occurred during execution
func (tc *TaskCapture) StatusUpdates() []CapturedStatusUpdate {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]CapturedStatusUpdate, len(tc.statusUpdates))
	copy(result, tc.statusUpdates)
	return result
}

// Artifacts returns all artifacts that were upserted during execution
func (tc *TaskCapture) Artifacts() []a2a.Artifact {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]a2a.Artifact, len(tc.upsertedArtifacts))
	copy(result, tc.upsertedArtifacts)
	return result
}

// FileOperations returns all file operations that occurred during execution
func (tc *TaskCapture) FileOperations() []CapturedFileOp {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	result := make([]CapturedFileOp, len(tc.fileOps))
	copy(result, tc.fileOps)
	return result
}

// Task returns the current state of the task (including any modifications)
func (tc *TaskCapture) Task() *a2a.Task {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	// Return a copy to prevent external modification
	taskCopy := *tc.task
	return &taskCopy
}
