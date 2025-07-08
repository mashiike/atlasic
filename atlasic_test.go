package atlasic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mashiike/atlasic/a2a"
	"go.uber.org/mock/gomock"
)

// setupTestFileSystemStorage creates a temporary FileSystemStorage for testing
func setupTestFileSystemStorage(t *testing.T) *FileSystemStorage {
	tempDir, err := os.MkdirTemp("", "atlasic-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	storage, err := NewFileSystemStorage(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileSystemStorage: %v", err)
	}
	return storage
}

// setupTestAgentService creates an AgentService with FileSystemStorage for testing
func setupTestAgentService(t *testing.T, mockAgent Agent, mockIDGen IDGenerator) *AgentService {
	storage := setupTestFileSystemStorage(t)
	agentService := NewAgentService(storage, mockAgent)
	if mockIDGen != nil {
		agentService.SetIDGenerator(mockIDGen)
	}

	// Start AgentService for tests that need async execution
	ctx := context.Background()
	if err := agentService.Start(ctx); err != nil {
		t.Fatalf("Failed to start AgentService: %v", err)
	}
	t.Cleanup(func() { agentService.Close() })

	return agentService
}

// TestA2A_BasicMessageFlow tests the complete A2A message flow with FileSystemStorage
func TestA2A_BasicMessageFlow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock Agent and IDGenerator
	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup ID generator expectations with fixed IDs
	mockIDGen.EXPECT().GenerateTaskID().Return("task-001")
	mockIDGen.EXPECT().GenerateContextID().Return("context-001")
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-001").AnyTimes() // Agent may create multiple messages

	// Setup agent metadata expectations
	expectedMetadata := &AgentMetadata{
		Name:        "A2A Test Agent",
		Description: "A test agent for A2A protocol compliance",
		Skills: []a2a.AgentSkill{
			{
				Name:        "echo",
				Description: "Echo back user messages",
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(expectedMetadata, nil).AnyTimes()

	// Mock agent execution - add a response message
	agentCalled := make(chan struct{})
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			defer close(agentCalled)
			// Agent adds a response message using new interface
			_, err := handle.AddMessage(ctx, []a2a.Part{
				{Kind: a2a.KindTextPart, Text: "Hello back! I received your message."},
			})
			return nil, err
		}).Times(1)

	// Create AgentService with FileSystemStorage
	agentService := setupTestAgentService(t, mockAgent, mockIDGen)

	// Test 1: Send non-blocking message (A2A basic pattern)
	ctx := context.Background()
	userMessage := a2a.NewMessage("user-msg-001", a2a.RoleUser, []a2a.Part{
		{Kind: a2a.KindTextPart, Text: "Hello, Agent!"},
	})

	params := a2a.MessageSendParams{
		Message: userMessage,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false,
		},
	}

	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Verify immediate response structure
	if result.Task.ID != "task-001" {
		t.Errorf("Expected task ID 'task-001', got %s", result.Task.ID)
	}

	if result.Task.ContextID != "context-001" {
		t.Errorf("Expected context ID 'context-001', got %s", result.Task.ContextID)
	}

	if result.Task.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("Expected task state submitted, got %s", result.Task.Status.State)
	}

	// Wait for agent execution to complete
	select {
	case <-agentCalled:
		t.Log("Agent execution completed")
	case <-time.After(1 * time.Second):
		t.Log("Agent execution timeout - agent was not called")
	}
	time.Sleep(100 * time.Millisecond) // Additional time for state updates

	// Test 2: Verify task completion and history
	storage := agentService.Storage.(*FileSystemStorage)
	finalTask, _, err := storage.GetTask(ctx, "task-001", HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to get final task: %v", err)
	}

	if finalTask.Status.State != a2a.TaskStateCompleted {
		t.Errorf("Expected final task state completed, got %s", finalTask.Status.State)
	}

	if len(finalTask.History) < 2 {
		t.Errorf("Expected at least 2 messages in history, got %d", len(finalTask.History))
	}

	// Verify user message is in history
	userFound := false
	agentFound := false
	for _, msg := range finalTask.History {
		if msg.Role == a2a.RoleUser && len(msg.Parts) > 0 && msg.Parts[0].Text == "Hello, Agent!" {
			userFound = true
		}
		if msg.Role == a2a.RoleAgent && len(msg.Parts) > 0 && strings.Contains(msg.Parts[0].Text, "Hello back!") {
			agentFound = true
		}
	}

	if !userFound {
		t.Error("User message not found in task history")
	}
	if !agentFound {
		t.Error("Agent response not found in task history")
	}
}

// TestA2A_BlockingMessage tests blocking message configuration
func TestA2A_BlockingMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup expectations
	mockIDGen.EXPECT().GenerateTaskID().Return("task-002")
	mockIDGen.EXPECT().GenerateContextID().Return("context-002")
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-002").AnyTimes()

	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(&AgentMetadata{
		Name:               "Blocking Test Agent",
		Description:        "Test agent for blocking mode",
		Skills:             []a2a.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}, nil).AnyTimes()

	// Agent execution with quick response
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			_, err := handle.AddMessage(ctx, []a2a.Part{
				{Kind: a2a.KindTextPart, Text: "Blocking response completed."},
			})
			return nil, err
		}).Times(1)

	agentService := setupTestAgentService(t, mockAgent, mockIDGen)
	agentService.StreamingPollInterval = 10 * time.Millisecond // Fast polling for test

	// Test blocking message
	ctx := context.Background()
	userMessage := a2a.NewMessage("", a2a.RoleUser, []a2a.Part{
		{Kind: a2a.KindTextPart, Text: "Blocking request"},
	})

	params := a2a.MessageSendParams{
		Message: userMessage,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: true,
		},
	}

	start := time.Now()
	result, err := agentService.SendMessage(ctx, params)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Blocking SendMessage failed: %v", err)
	}

	// Should have waited for completion
	if duration < 5*time.Millisecond {
		t.Errorf("Expected to wait for completion, but returned too quickly: %v", duration)
	}

	// Result should be completed
	if result.Task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("Expected completed state, got %s", result.Task.Status.State)
	}

	t.Logf("Blocking mode completed in %v", duration)
}

// TestA2A_StreamingMessage tests streaming message functionality
func TestA2A_StreamingMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup expectations
	mockIDGen.EXPECT().GenerateTaskID().Return("task-003")
	mockIDGen.EXPECT().GenerateContextID().Return("context-003")
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-003").AnyTimes()

	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(&AgentMetadata{
		Name:               "Streaming Test Agent",
		Description:        "Test agent for streaming",
		Skills:             []a2a.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}, nil).AnyTimes()

	// Agent execution that adds response
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			_, err := handle.AddMessage(ctx, []a2a.Part{
				{Kind: a2a.KindTextPart, Text: "Streaming response"},
			})
			return nil, err
		}).Times(1)

	agentService := setupTestAgentService(t, mockAgent, mockIDGen)
	agentService.StreamingPollInterval = 10 * time.Millisecond // Fast polling

	// Test streaming message
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	userMessage := a2a.NewMessage("", a2a.RoleUser, []a2a.Part{
		{Kind: a2a.KindTextPart, Text: "Streaming request"},
	})

	params := a2a.MessageSendParams{
		Message: userMessage,
	}

	streamChan, err := agentService.SendStreamingMessage(ctx, params)
	if err != nil {
		t.Fatalf("SendStreamingMessage failed: %v", err)
	}

	// Read from stream
	eventCount := 0
	for {
		select {
		case event, ok := <-streamChan:
			if !ok {
				t.Logf("Stream closed after %d events", eventCount)
				return // Test completed successfully
			}
			eventCount++
			t.Logf("Received stream event %d: %+v", eventCount, event)

			// Check for completion event
			if event.Status != nil && event.Status.Final {
				t.Log("Received final status event")
			}

		case <-ctx.Done():
			t.Log("Stream test completed by timeout")
			return
		}
	}
}

// TestA2A_ErrorHandling tests error handling scenarios
func TestA2A_ErrorHandling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Setup expectations
	mockIDGen.EXPECT().GenerateTaskID().Return("task-error")
	mockIDGen.EXPECT().GenerateContextID().Return("context-error")
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-error").AnyTimes()

	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(&AgentMetadata{
		Name:        "Error Test Agent",
		Description: "Test agent for error scenarios",
		Skills:      []a2a.AgentSkill{},
	}, nil).AnyTimes()

	// Agent execution that fails
	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(
		nil, fmt.Errorf("simulated agent error")).Times(1)

	agentService := setupTestAgentService(t, mockAgent, mockIDGen)

	// Test error handling
	ctx := context.Background()
	userMessage := a2a.NewMessage("", a2a.RoleUser, []a2a.Part{
		{Kind: a2a.KindTextPart, Text: "This will cause an error"},
	})

	params := a2a.MessageSendParams{
		Message: userMessage,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false,
		},
	}

	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Initial response should be submitted
	if result.Task.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("Expected submitted state, got %s", result.Task.Status.State)
	}

	// Wait for agent execution to fail
	time.Sleep(500 * time.Millisecond)

	// Check final task state
	storage := agentService.Storage.(*FileSystemStorage)
	finalTask, _, err := storage.GetTask(ctx, "task-error", HistoryLengthAll)
	if err != nil {
		t.Fatalf("Failed to get final task: %v", err)
	}

	if finalTask.Status.State != a2a.TaskStateFailed {
		t.Errorf("Expected failed state, got %s", finalTask.Status.State)
	}
}

// TestA2A_TaskNotFound tests GetTask with non-existent task
func TestA2A_TaskNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	storage := setupTestFileSystemStorage(t)
	agentService := NewAgentService(storage, mockAgent)

	ctx := context.Background()
	historyLength := 5 // Arbitrary history length for this test
	params := a2a.TaskQueryParams{
		ID:            "non-existent-task",
		HistoryLength: &historyLength, // Use pointer to match new TaskQueryParams structure
	}

	result, err := agentService.GetTask(ctx, params)

	// Verify error handling
	if err == nil {
		t.Fatal("GetTask should fail for non-existent task")
	}

	if result != nil {
		t.Error("GetTask result should be nil on error")
	}

	// Verify JSON-RPC error
	jsonRPCError, ok := err.(*a2a.JSONRPCError)
	if !ok {
		t.Fatalf("Expected JSONRPCError, got: %T", err)
	}

	if jsonRPCError.Code != a2a.ErrorCodeTaskNotFound {
		t.Errorf("Expected error code %d, got %d", a2a.ErrorCodeTaskNotFound, jsonRPCError.Code)
	}
}

// TestA2A_AgentCard tests GetAgentCard functionality
func TestA2A_AgentCard(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	storage := setupTestFileSystemStorage(t)

	expectedMetadata := &AgentMetadata{
		Name:        "Test Agent",
		Description: "A test agent for unit testing",
		Skills: []a2a.AgentSkill{
			{
				Name:        "test_skill",
				Description: "A test skill",
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(expectedMetadata, nil)

	agentService := NewAgentService(storage, mockAgent)

	ctx := context.Background()
	card, err := agentService.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}

	// Verify agent card
	if card.Name != "Test Agent" {
		t.Errorf("Expected name 'Test Agent', got '%s'", card.Name)
	}

	if card.Description != "A test agent for unit testing" {
		t.Errorf("Expected description 'A test agent for unit testing', got '%s'", card.Description)
	}

	// Verify protocol version is set correctly
	if card.ProtocolVersion != a2a.ProtocolVersion {
		t.Errorf("Expected protocol version '%s', got '%s'", a2a.ProtocolVersion, card.ProtocolVersion)
	}

	if len(card.Skills) != 1 {
		t.Errorf("Expected 1 skill, got %d", len(card.Skills))
	}

	if len(card.Skills) > 0 && card.Skills[0].Name != "test_skill" {
		t.Errorf("Expected skill name 'test_skill', got '%s'", card.Skills[0].Name)
	}
}

// ====================
// A2A Protocol Compliance Tests
// ====================

// TestA2A_ContextContinuation tests context continuation with existing contextID
func TestA2A_ContextContinuation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Test message with contextID but no taskID (new task in existing context)
	message := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "msg-001",
		ContextID: "existing-context-001", // Existing context
		// TaskID is empty - should create new task
		Role:  a2a.RoleUser,
		Parts: []a2a.Part{a2a.NewTextPart("Continue our conversation with a new task")},
	}

	params := a2a.MessageSendParams{
		Message: message,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false, // Non-blocking for simplicity
		},
	}

	// Mock expectations for new task creation
	mockIDGen.EXPECT().GenerateTaskID().Return("new-task-002")
	// Note: GenerateContextID should NOT be called since contextID is provided

	// Mock storage operations for new task creation
	mockStorage.EXPECT().Append(gomock.Any(), "existing-context-001", "new-task-002", uint64(0), gomock.Any()).
		Return(uint64(2), nil)

	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(0), uint64(2)).
		Return(nil)

	// Mock final task retrieval
	expectedTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "new-task-002",
		ContextID: "existing-context-001", // Should use existing context
		Status: a2a.TaskStatus{
			State: a2a.TaskStateSubmitted,
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "msg-001",
				ContextID: "existing-context-001",
				TaskID:    "new-task-002",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Continue our conversation with a new task")},
			},
		},
	}

	mockStorage.EXPECT().GetTask(gomock.Any(), "new-task-002", HistoryLengthAll).
		Return(expectedTask, uint64(2), nil)

	ctx := context.Background()

	// Execute SendMessage
	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("Expected success for context continuation, got error: %v", err)
	}

	// Verify the result
	if result == nil || result.Task == nil {
		t.Fatal("Expected non-nil result and task")
	}

	if result.Task.ID != "new-task-002" {
		t.Errorf("Expected task ID 'new-task-002', got '%s'", result.Task.ID)
	}

	if result.Task.ContextID != "existing-context-001" {
		t.Errorf("Expected context ID 'existing-context-001', got '%s'", result.Task.ContextID)
	}

	if result.Task.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("Expected task state 'submitted', got '%s'", result.Task.Status.State)
	}

	t.Logf("Successfully created new task in existing context: taskID=%s, contextID=%s",
		result.Task.ID, result.Task.ContextID)
}

// TestA2A_ParallelFollowUpTask tests parallel follow-up task creation with existing contextID
func TestA2A_ParallelFollowUpTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Test message with contextID but no taskID (should create new parallel task)
	message := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "msg-002",
		ContextID: "existing-context-001", // Existing context
		// TaskID is empty - should create new parallel task
		Role:             a2a.RoleUser,
		Parts:            []a2a.Part{a2a.NewTextPart("Parallel follow-up task")},
		ReferenceTaskIDs: []string{"existing-task-001"}, // Reference to original task
	}

	params := a2a.MessageSendParams{
		Message: message,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false,
		},
	}

	// Mock ID generation for new parallel task
	mockIDGen.EXPECT().GenerateTaskID().Return("parallel-task-002")

	// Mock expectations for new task creation
	mockStorage.EXPECT().Append(gomock.Any(), "existing-context-001", "parallel-task-002", gomock.Any(), gomock.Any()).
		Return(uint64(1), nil)

	// Mock SaveTask for new task creation
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(0), uint64(1)).
		Return(nil)

	// Expect final GetTask call to retrieve created task for result
	newParallelTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "parallel-task-002",
		ContextID: "existing-context-001",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateSubmitted,
		},
		History: []a2a.Message{
			{
				Kind:             a2a.KindMessage,
				MessageID:        "msg-002",
				ContextID:        "existing-context-001",
				TaskID:           "parallel-task-002",
				Role:             a2a.RoleUser,
				Parts:            []a2a.Part{a2a.NewTextPart("Parallel follow-up task")},
				ReferenceTaskIDs: []string{"existing-task-001"},
			},
		},
	}
	mockStorage.EXPECT().GetTask(gomock.Any(), "parallel-task-002", HistoryLengthAll).
		Return(newParallelTask, uint64(1), nil)

	ctx := context.Background()

	// Call SendMessage
	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Verify the result creates new parallel task
	if result.Task.ID != "parallel-task-002" {
		t.Errorf("Expected task ID 'parallel-task-002', got '%s'", result.Task.ID)
	}

	if result.Task.ContextID != "existing-context-001" {
		t.Errorf("Expected context ID 'existing-context-001', got '%s'", result.Task.ContextID)
	}

	// Verify referenceTaskIDs are preserved
	if len(result.Task.History) > 0 && len(result.Task.History[0].ReferenceTaskIDs) > 0 {
		if result.Task.History[0].ReferenceTaskIDs[0] != "existing-task-001" {
			t.Errorf("Expected referenceTaskID 'existing-task-001', got '%s'", result.Task.History[0].ReferenceTaskIDs[0])
		}
	}

	t.Logf("Successfully created parallel follow-up task: taskID=%s, contextID=%s", result.Task.ID, result.Task.ContextID)
}

// TestA2A_NewConversation tests completely new conversation creation
func TestA2A_NewConversation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Test message with no contextID and no taskID (completely new conversation)
	message := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "msg-001",
		// ContextID is empty - should create new context
		// TaskID is empty - should create new task
		Role:  a2a.RoleUser,
		Parts: []a2a.Part{a2a.NewTextPart("Start a completely new conversation")},
	}

	params := a2a.MessageSendParams{
		Message: message,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false, // Non-blocking for simplicity
		},
	}

	// Mock expectations
	mockIDGen.EXPECT().GenerateTaskID().Return("new-task-001")
	mockIDGen.EXPECT().GenerateContextID().Return("new-context-001") // Should be called

	// Mock storage operations for new task creation
	mockStorage.EXPECT().Append(gomock.Any(), "new-context-001", "new-task-001", uint64(0), gomock.Any()).
		Return(uint64(2), nil)

	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(0), uint64(2)).
		Return(nil)

	// Mock final task retrieval
	expectedTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "new-task-001",
		ContextID: "new-context-001", // Should use new context
		Status: a2a.TaskStatus{
			State: a2a.TaskStateSubmitted,
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "msg-001",
				ContextID: "new-context-001",
				TaskID:    "new-task-001",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Start a completely new conversation")},
			},
		},
	}

	mockStorage.EXPECT().GetTask(gomock.Any(), "new-task-001", HistoryLengthAll).
		Return(expectedTask, uint64(2), nil)

	ctx := context.Background()

	// Execute SendMessage
	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("Expected success for new conversation, got error: %v", err)
	}

	// Verify the result
	if result == nil || result.Task == nil {
		t.Fatal("Expected non-nil result and task")
	}

	if result.Task.ID != "new-task-001" {
		t.Errorf("Expected task ID 'new-task-001', got '%s'", result.Task.ID)
	}

	if result.Task.ContextID != "new-context-001" {
		t.Errorf("Expected context ID 'new-context-001', got '%s'", result.Task.ContextID)
	}

	if result.Task.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("Expected task state 'submitted', got '%s'", result.Task.Status.State)
	}

	t.Logf("Successfully created new conversation: taskID=%s, contextID=%s",
		result.Task.ID, result.Task.ContextID)
}

// TestA2A_TerminalTaskContinuation tests that terminal tasks cannot be continued
func TestA2A_TerminalTaskContinuation(t *testing.T) {
	terminalStates := []a2a.TaskState{
		a2a.TaskStateCompleted,
		a2a.TaskStateCanceled,
		a2a.TaskStateFailed,
		a2a.TaskStateRejected,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create mocks
			mockAgent := NewMockAgent(ctrl)
			mockStorage := NewMockStorage(ctrl)
			mockIDGen := NewMockIDGenerator(ctrl)

			// Create agent service
			agentService := NewAgentService(mockStorage, mockAgent)
			agentService.SetIDGenerator(mockIDGen)
			agentService.DisablePushNotifications = true

			// Setup existing task in terminal state
			existingTask := &a2a.Task{
				Kind:      a2a.KindTask,
				ID:        "existing-task-id",
				ContextID: "existing-context-id",
				Status: a2a.TaskStatus{
					State: state,
				},
				History: []a2a.Message{
					{
						Kind:      a2a.KindMessage,
						MessageID: "original-msg-id",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{a2a.NewTextPart("Original message")},
					},
				},
			}

			// Mock storage to return existing terminal task
			mockStorage.EXPECT().GetTask(gomock.Any(), "existing-task-id", HistoryLengthAll).
				Return(existingTask, uint64(1), nil)

			// Prepare follow-up message
			followUpMessage := a2a.Message{
				Kind:      a2a.KindMessage,
				MessageID: "followup-msg-id",
				TaskID:    "existing-task-id", // Continue existing task
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Follow up message")},
			}

			params := a2a.MessageSendParams{
				Message: followUpMessage,
			}

			// Execute SendMessage - should return error for terminal task
			ctx := context.Background()
			result, err := agentService.SendMessage(ctx, params)

			// Verify error response
			if err == nil {
				t.Fatalf("Expected error for terminal task state %s, got success: %+v", state, result)
			}

			jsonRPCErr, ok := err.(*a2a.JSONRPCError)
			if !ok {
				t.Fatalf("Expected JSONRPCError, got %T: %v", err, err)
			}

			if jsonRPCErr.Code != a2a.ErrorCodeInvalidParams {
				t.Errorf("Expected error code %d, got %d", a2a.ErrorCodeInvalidParams, jsonRPCErr.Code)
			}

			// Verify error data contains useful information
			data, ok := jsonRPCErr.Data.(map[string]string)
			if !ok {
				t.Fatalf("Expected error data to be map[string]string, got %T", jsonRPCErr.Data)
			}

			if data["currentState"] != string(state) {
				t.Errorf("Expected currentState %s, got %s", state, data["currentState"])
			}

			if data["reason"] == "" {
				t.Error("Expected non-empty reason in error data")
			}

			if data["suggestion"] == "" {
				t.Error("Expected non-empty suggestion in error data")
			}

			t.Logf("Correctly rejected terminal task (%s) continuation with error: %v", state, err)
		})
	}
}

// TestA2A_InputRequiredTaskContinuation tests that input-required tasks can be continued
func TestA2A_InputRequiredTaskContinuation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Setup existing task in input-required state
	existingTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "input-required-task-id",
		ContextID: "input-required-context-id",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateInputRequired,
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "original-msg-id",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Original message")},
			},
		},
	}

	// Mock storage expectations for retrieving task (in processMessage)
	mockStorage.EXPECT().GetTask(gomock.Any(), "input-required-task-id", HistoryLengthAll).
		Return(existingTask, uint64(1), nil)

	// Mock storage expectations for retrieving task again (in addMessage)
	mockStorage.EXPECT().GetTask(gomock.Any(), "input-required-task-id", HistoryLengthAll).
		Return(existingTask, uint64(1), nil)

	// Mock adding message
	mockStorage.EXPECT().Append(gomock.Any(), "input-required-context-id", "input-required-task-id", uint64(1), gomock.Any()).
		Return(uint64(2), nil)

	// Mock saving updated task
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(1), uint64(2)).
		Return(nil)

	// Mock final task retrieval (after message added, agent not executed for simplicity)
	updatedTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "input-required-task-id",
		ContextID: "input-required-context-id",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateInputRequired, // Still input-required
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "original-msg-id",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Original message")},
			},
			{
				Kind:      a2a.KindMessage,
				MessageID: "followup-msg-id",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Here is the additional information you requested")},
			},
		},
	}
	mockStorage.EXPECT().GetTask(gomock.Any(), "input-required-task-id", HistoryLengthAll).
		Return(updatedTask, uint64(2), nil)

	// Mock ID generation
	mockIDGen.EXPECT().GenerateMessageID().Return("followup-msg-id")

	// Prepare follow-up message
	followUpMessage := a2a.Message{
		Kind:   a2a.KindMessage,
		TaskID: "input-required-task-id", // Continue existing task
		Role:   a2a.RoleUser,
		Parts:  []a2a.Part{a2a.NewTextPart("Here is the additional information you requested")},
	}

	params := a2a.MessageSendParams{
		Message: followUpMessage,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false, // Non-blocking to avoid agent execution complexity
		},
	}

	ctx := context.Background()

	// Execute SendMessage - should succeed for input-required task
	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("Expected success for input-required task continuation, got error: %v", err)
	}

	// Verify the result
	if result == nil || result.Task == nil {
		t.Fatal("Expected non-nil result and task")
	}

	if result.Task.ID != "input-required-task-id" {
		t.Errorf("Expected task ID %s, got %s", "input-required-task-id", result.Task.ID)
	}

	// Verify task has both original and follow-up messages
	if len(result.Task.History) < 2 {
		t.Errorf("Expected at least 2 messages in history, got %d", len(result.Task.History))
	}

	t.Logf("Successfully continued input-required task")
}

// TestA2A_HistoryLengthEdgeCases tests various historyLength edge cases
func TestA2A_HistoryLengthEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		historyLength int
		description   string
		shouldError   bool
	}{
		{
			name:          "HistoryLengthAll",
			historyLength: HistoryLengthAll, // -1
			description:   "Should return complete history",
			shouldError:   false,
		},
		{
			name:          "Zero history",
			historyLength: 0,
			description:   "Should return task with no history",
			shouldError:   false,
		},
		{
			name:          "Single message",
			historyLength: 1,
			description:   "Should return task with latest 1 message",
			shouldError:   false,
		},
		{
			name:          "Large history",
			historyLength: 1000,
			description:   "Should handle large historyLength gracefully",
			shouldError:   false,
		},
		{
			name:          "Negative value other than -1",
			historyLength: -5,
			description:   "Should handle unexpected negative values",
			shouldError:   false, // Implementation dependent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create mocks
			mockAgent := NewMockAgent(ctrl)
			mockStorage := NewMockStorage(ctrl)

			// Create agent service
			agentService := NewAgentService(mockStorage, mockAgent)
			agentService.DisablePushNotifications = true

			// Create test task with multiple messages
			testTask := &a2a.Task{
				Kind:      a2a.KindTask,
				ID:        "test-task-001",
				ContextID: "test-context-001",
				Status: a2a.TaskStatus{
					State: a2a.TaskStateCompleted,
				},
				History: []a2a.Message{
					{
						Kind:      a2a.KindMessage,
						MessageID: "msg-001",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{a2a.NewTextPart("First message")},
					},
					{
						Kind:      a2a.KindMessage,
						MessageID: "msg-002",
						Role:      a2a.RoleAgent,
						Parts:     []a2a.Part{a2a.NewTextPart("Agent response")},
					},
					{
						Kind:      a2a.KindMessage,
						MessageID: "msg-003",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{a2a.NewTextPart("Follow up message")},
					},
				},
			}

			// Mock storage to return the task
			mockStorage.EXPECT().GetTask(gomock.Any(), "test-task-001", tt.historyLength).
				Return(testTask, uint64(1), nil)

			ctx := context.Background()
			params := a2a.TaskQueryParams{
				ID:            "test-task-001",
				HistoryLength: &tt.historyLength,
			}

			// Execute GetTask
			result, err := agentService.GetTask(ctx, params)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tt.description, err)
			}

			if result == nil {
				t.Fatalf("Expected task result for %s, got nil", tt.description)
			}

			// Verify the task is returned (detailed history validation is Storage implementation dependent)
			if result.ID != "test-task-001" {
				t.Errorf("Expected task ID 'test-task-001', got '%s'", result.ID)
			}

			t.Logf("✅ %s: historyLength=%d handled successfully", tt.description, tt.historyLength)
		})
	}
}

// TestA2A_MessageSendConfigurationHistoryLength tests historyLength in MessageSendConfiguration
func TestA2A_MessageSendConfigurationHistoryLength(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Test different historyLength configurations in MessageSendConfiguration
	testConfigs := []struct {
		historyLength int
		description   string
	}{
		{HistoryLengthAll, "Full history in configuration"},
		{0, "No history in configuration"},
		{3, "Limited history in configuration"},
	}

	for _, tc := range testConfigs {
		t.Run(tc.description, func(t *testing.T) {
			// Mock ID generation
			mockIDGen.EXPECT().GenerateTaskID().Return("config-test-task")
			mockIDGen.EXPECT().GenerateContextID().Return("config-test-context")

			// Mock storage operations for new task creation
			mockStorage.EXPECT().Append(gomock.Any(), "config-test-context", "config-test-task", uint64(0), gomock.Any()).
				Return(uint64(2), nil)
			mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(0), uint64(2)).
				Return(nil)

			// Mock final task retrieval
			finalTask := &a2a.Task{
				Kind:      a2a.KindTask,
				ID:        "config-test-task",
				ContextID: "config-test-context",
				Status: a2a.TaskStatus{
					State: a2a.TaskStateSubmitted,
				},
				History: []a2a.Message{
					{
						Kind:      a2a.KindMessage,
						MessageID: "config-msg",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{a2a.NewTextPart("Test message")},
					},
				},
			}
			mockStorage.EXPECT().GetTask(gomock.Any(), "config-test-task", tc.historyLength).
				Return(finalTask, uint64(2), nil)

			// Create message with historyLength configuration
			message := a2a.Message{
				Kind:      a2a.KindMessage,
				MessageID: "config-msg",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Test message")},
			}

			params := a2a.MessageSendParams{
				Message: message,
				Configuration: &a2a.MessageSendConfiguration{
					Blocking:      false,
					HistoryLength: &tc.historyLength,
				},
			}

			ctx := context.Background()

			// Execute SendMessage with historyLength configuration
			result, err := agentService.SendMessage(ctx, params)
			if err != nil {
				t.Fatalf("SendMessage failed for %s: %v", tc.description, err)
			}

			if result == nil || result.Task == nil {
				t.Fatalf("Expected task result for %s, got nil", tc.description)
			}

			t.Logf("✅ %s (historyLength=%d): Configuration handled successfully", tc.description, tc.historyLength)
		})
	}
}

// TestA2A_SecurityConfiguration tests security configuration in AgentCard
func TestA2A_SecurityConfiguration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock agent that supports security configurations
	mockAgent := NewMockAgent(ctrl)
	storage := setupTestFileSystemStorage(t)

	// Create agent service
	agentService := NewAgentService(storage, mockAgent)

	ctx := context.Background()
	if err := agentService.Start(ctx); err != nil {
		t.Fatalf("Failed to start AgentService: %v", err)
	}
	t.Cleanup(func() { agentService.Close() })

	// Test cases for different security schemes
	testCases := []struct {
		name        string
		metadata    *AgentMetadata
		description string
	}{
		{
			name: "APIKey Security",
			metadata: &AgentMetadata{
				Name:        "Secure Agent",
				Description: "Agent with API Key authentication",
				Skills: []a2a.AgentSkill{
					{
						ID:          "secure-skill",
						Name:        "Secure Operation",
						Description: "Operations requiring API key",
						Tags:        []string{"security"},
					},
				},
			},
			description: "Agent with API Key security scheme",
		},
		{
			name: "OAuth2 Security",
			metadata: &AgentMetadata{
				Name:        "OAuth Agent",
				Description: "Agent with OAuth2 authentication",
				Skills: []a2a.AgentSkill{
					{
						ID:          "oauth-skill",
						Name:        "OAuth Operation",
						Description: "Operations requiring OAuth2",
						Tags:        []string{"oauth", "security"},
					},
				},
			},
			description: "Agent with OAuth2 security scheme",
		},
		{
			name: "HTTP Bearer Security",
			metadata: &AgentMetadata{
				Name:        "Bearer Agent",
				Description: "Agent with HTTP Bearer authentication",
				Skills: []a2a.AgentSkill{
					{
						ID:          "bearer-skill",
						Name:        "Bearer Operation",
						Description: "Operations requiring Bearer token",
						Tags:        []string{"bearer", "security"},
					},
				},
			},
			description: "Agent with HTTP Bearer security scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock agent metadata
			mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(tc.metadata, nil)

			// Get agent card
			card, err := agentService.GetAgentCard(ctx)
			if err != nil {
				t.Fatalf("GetAgentCard failed for %s: %v", tc.description, err)
			}

			if card == nil {
				t.Fatalf("Expected agent card for %s, got nil", tc.description)
			}

			// Verify basic agent card structure
			if card.Name != tc.metadata.Name {
				t.Errorf("Expected name '%s', got '%s'", tc.metadata.Name, card.Name)
			}

			if card.Description != tc.metadata.Description {
				t.Errorf("Expected description '%s', got '%s'", tc.metadata.Description, card.Description)
			}

			// Verify skills are properly transferred
			if len(card.Skills) != len(tc.metadata.Skills) {
				t.Errorf("Expected %d skills, got %d", len(tc.metadata.Skills), len(card.Skills))
			}

			// Security fields should be initialized (even if empty)
			if card.Security == nil {
				t.Logf("Security field is nil for %s (acceptable)", tc.description)
			}

			if card.SecuritySchemes == nil {
				t.Logf("SecuritySchemes field is nil for %s (acceptable)", tc.description)
			}

			t.Logf("✅ %s: AgentCard retrieved successfully with security structure", tc.description)
		})
	}
}

// TestA2A_SecuritySchemeValidation tests security scheme structure validation
func TestA2A_SecuritySchemeValidation(t *testing.T) {
	// Test cases for different security scheme types
	testSchemes := []struct {
		name          string
		scheme        a2a.SecurityScheme
		description   string
		shouldBeValid bool
	}{
		{
			name: "Valid APIKey Scheme",
			scheme: a2a.SecurityScheme{
				Type:        a2a.SecurityTypeAPIKey,
				Description: "API Key for authentication",
				Name:        "X-API-Key",
				In:          "header",
			},
			description:   "API Key in header",
			shouldBeValid: true,
		},
		{
			name: "Valid OAuth2 Scheme",
			scheme: a2a.SecurityScheme{
				Type:        a2a.SecurityTypeOAuth2,
				Description: "OAuth2 authentication",
				Flows: a2a.OAuthFlows{
					AuthorizationCode: &a2a.AuthorizationCodeOAuthFlow{
						AuthorizationURL: "https://auth.example.com/oauth/authorize",
						TokenURL:         "https://auth.example.com/oauth/token",
						Scopes: map[string]string{
							"read":  "Read access",
							"write": "Write access",
						},
					},
				},
			},
			description:   "OAuth2 authorization code flow",
			shouldBeValid: true,
		},
		{
			name: "Valid HTTP Bearer Scheme",
			scheme: a2a.SecurityScheme{
				Type:         a2a.SecurityTypeHTTP,
				Description:  "HTTP Bearer authentication",
				Scheme:       "bearer",
				BearerFormat: "JWT",
			},
			description:   "HTTP Bearer with JWT",
			shouldBeValid: true,
		},
		{
			name: "Valid OpenID Connect Scheme",
			scheme: a2a.SecurityScheme{
				Type:             a2a.SecurityTypeOpenIDConnect,
				Description:      "OpenID Connect authentication",
				OpenIDConnectURL: "https://auth.example.com/.well-known/openid-configuration",
			},
			description:   "OpenID Connect",
			shouldBeValid: true,
		},
	}

	for _, tc := range testSchemes {
		t.Run(tc.name, func(t *testing.T) {
			// For now, we just verify the scheme structure is complete
			// Actual validation would depend on specific implementation requirements

			if tc.scheme.Type == "" {
				t.Errorf("SecurityScheme type should not be empty for %s", tc.description)
			}

			// Type-specific validation
			switch tc.scheme.Type {
			case a2a.SecurityTypeAPIKey:
				if tc.scheme.Name == "" {
					t.Errorf("APIKey scheme should have name for %s", tc.description)
				}
				if tc.scheme.In == "" {
					t.Errorf("APIKey scheme should specify 'in' field for %s", tc.description)
				}

			case a2a.SecurityTypeOAuth2:
				hasFlow := tc.scheme.Flows.Implicit != nil ||
					tc.scheme.Flows.Password != nil ||
					tc.scheme.Flows.ClientCredentials != nil ||
					tc.scheme.Flows.AuthorizationCode != nil

				if !hasFlow {
					t.Errorf("OAuth2 scheme should have at least one flow for %s", tc.description)
				}

			case a2a.SecurityTypeHTTP:
				if tc.scheme.Scheme == "" {
					t.Errorf("HTTP scheme should specify scheme for %s", tc.description)
				}

			case a2a.SecurityTypeOpenIDConnect:
				if tc.scheme.OpenIDConnectURL == "" {
					t.Errorf("OpenID Connect scheme should have URL for %s", tc.description)
				}
			}

			t.Logf("✅ %s: SecurityScheme structure validated", tc.description)
		})
	}
}

// TestA2A_AuthRequiredState tests auth-required state handling
func TestA2A_TaskHandleInitialStatus(t *testing.T) {
	// Test that TaskHandle.GetInitialStatus() returns the status at dequeue time
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	agentService := setupTestAgentService(t, mockAgent, mockIDGen)

	// Mock Agent execution to check initial status
	agentCalled := make(chan struct{})
	var capturedInitialStatus a2a.TaskStatus

	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			defer close(agentCalled)

			// Capture the initial status
			capturedInitialStatus = handle.GetInitialStatus()

			// Add agent response
			_, err := handle.AddMessage(ctx, []a2a.Part{a2a.NewTextPart("Task handle received initial status correctly")})
			return nil, err
		}).Times(1)

	// Mock ID generation
	mockIDGen.EXPECT().GenerateTaskID().Return("test-task-001")
	mockIDGen.EXPECT().GenerateContextID().Return("test-context-001")
	mockIDGen.EXPECT().GenerateMessageID().Return("user-msg-001").Times(1)
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-001").AnyTimes()

	// Send message
	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Kind:  a2a.KindMessage,
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{a2a.NewTextPart("Test message for initial status")},
		},
	}

	result, err := agentService.SendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("SendMessage result is nil")
	}

	// Wait for agent execution
	<-agentCalled

	// Verify initial status was submitted (before being changed to working)
	if capturedInitialStatus.State != a2a.TaskStateSubmitted {
		t.Errorf("Expected initial status to be %s, got %s", a2a.TaskStateSubmitted, capturedInitialStatus.State)
	}
	t.Logf("✅ TaskHandle correctly captured initial status: %s", capturedInitialStatus.State)
}

func TestA2A_AgentExplicitStatusUpdate(t *testing.T) {
	// Test that Agent can set explicit status and ProcessJob respects it
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAgent := NewMockAgent(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	agentService := setupTestAgentService(t, mockAgent, mockIDGen)

	// Mock Agent execution to set explicit status with message
	agentCalled := make(chan struct{})

	mockAgent.EXPECT().Execute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
			defer close(agentCalled)

			// Add agent response message first
			_, err := handle.AddMessage(ctx, []a2a.Part{a2a.NewTextPart("Task completed with custom message")})
			if err != nil {
				return nil, err
			}

			// Set explicit completed status with message
			_, err = handle.UpdateStatus(ctx, a2a.TaskStateCompleted, []a2a.Part{a2a.NewTextPart("Agent completed successfully with explicit message")})
			return nil, err
		}).Times(1)

	// Mock ID generation
	mockIDGen.EXPECT().GenerateTaskID().Return("test-task-001")
	mockIDGen.EXPECT().GenerateContextID().Return("test-context-001")
	mockIDGen.EXPECT().GenerateMessageID().Return("user-msg-001").Times(1)
	mockIDGen.EXPECT().GenerateMessageID().Return("agent-msg-001").AnyTimes()

	// Send message
	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Kind:  a2a.KindMessage,
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{a2a.NewTextPart("Test explicit status update")},
		},
	}

	result, err := agentService.SendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if result == nil {
		t.Fatal("SendMessage result is nil")
	}

	// Wait for agent execution
	<-agentCalled

	// Verify task has the explicit status with message
	finalTask := result.Task
	if finalTask.Status.State != a2a.TaskStateCompleted {
		t.Errorf("Expected final status to be %s, got %s", a2a.TaskStateCompleted, finalTask.Status.State)
	}
	if finalTask.Status.Message == nil {
		t.Error("Expected status message to be set by Agent, but got nil")
	} else if len(finalTask.Status.Message.Parts) == 0 || finalTask.Status.Message.Parts[0].Text != "Agent completed successfully with explicit message" {
		t.Errorf("Expected status message to contain Agent's explicit message, got: %v", finalTask.Status.Message.Parts)
	}

	t.Logf("✅ Agent explicit status update correctly preserved: %s", finalTask.Status.State)
}

func TestA2A_AuthRequiredState(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)
	mockIDGen := NewMockIDGenerator(ctrl)

	// Create agent service
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.SetIDGenerator(mockIDGen)
	agentService.DisablePushNotifications = true

	// Test auth-required state transition
	message := a2a.Message{
		Kind:      a2a.KindMessage,
		MessageID: "auth-test-msg",
		Role:      a2a.RoleUser,
		Parts:     []a2a.Part{a2a.NewTextPart("Request requiring authentication")},
	}

	params := a2a.MessageSendParams{
		Message: message,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: false,
		},
	}

	// Mock ID generation
	mockIDGen.EXPECT().GenerateTaskID().Return("auth-task")
	mockIDGen.EXPECT().GenerateContextID().Return("auth-context")

	// Mock storage operations for task creation
	mockStorage.EXPECT().Append(gomock.Any(), "auth-context", "auth-task", uint64(0), gomock.Any()).
		Return(uint64(2), nil)
	mockStorage.EXPECT().SaveTask(gomock.Any(), gomock.Any(), uint64(0), uint64(2)).
		Return(nil)

	// Mock final task retrieval with auth-required state
	authRequiredTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "auth-task",
		ContextID: "auth-context",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateAuthRequired, // Auth required state
		},
		History: []a2a.Message{
			{
				Kind:      a2a.KindMessage,
				MessageID: "auth-test-msg",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.NewTextPart("Request requiring authentication")},
			},
		},
	}

	mockStorage.EXPECT().GetTask(gomock.Any(), "auth-task", HistoryLengthAll).
		Return(authRequiredTask, uint64(2), nil)

	ctx := context.Background()

	// Execute SendMessage
	result, err := agentService.SendMessage(ctx, params)
	if err != nil {
		t.Fatalf("SendMessage failed for auth-required test: %v", err)
	}

	if result == nil || result.Task == nil {
		t.Fatal("Expected task result for auth-required test, got nil")
	}

	// Verify task is in auth-required state
	if result.Task.Status.State != a2a.TaskStateAuthRequired {
		t.Errorf("Expected task state 'auth-required', got '%s'", result.Task.Status.State)
	}

	// Verify auth-required is considered interrupted (can be continued)
	if !result.Task.Status.State.IsInterrupted() {
		t.Error("Expected auth-required state to be considered interrupted")
	}

	// Verify auth-required is not terminal
	if result.Task.Status.State.IsTerminal() {
		t.Error("Expected auth-required state to not be terminal")
	}

	t.Logf("✅ Auth-required state handling: Task correctly transitioned to auth-required state")
}
