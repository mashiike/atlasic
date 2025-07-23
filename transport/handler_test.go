package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHandler_SendMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Expected request params
	params := a2a.MessageSendParams{
		Message: a2a.NewMessage("test-msg", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Hello, agent!"),
		}),
	}

	// Expected response
	expectedResult := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Kind:      a2a.KindTask,
			ID:        "task-123",
			ContextID: "context-456",
			Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		},
	}

	// Set up mock expectation
	mockService.EXPECT().SendMessage(gomock.Any(), params).Return(expectedResult, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest(a2a.MethodSendMessage, params, "req-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify JSON-RPC response structure
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "req-1", response.ID)
	assert.Nil(t, response.Error)

	// Verify result content
	resultBytes, err := json.Marshal(response.Result)
	require.NoError(t, err)

	var actualResult a2a.SendMessageResult
	err = json.Unmarshal(resultBytes, &actualResult)
	require.NoError(t, err)

	assert.Equal(t, "task-123", actualResult.Task.ID)
	assert.Equal(t, a2a.TaskStateCompleted, actualResult.Task.Status.State)
}

func TestHandler_SendStreamingMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Create test stream channel
	streamChan := make(chan a2a.StreamResponse, 2)
	go func() {
		defer close(streamChan)

		// Send status update
		streamChan <- a2a.StreamResponse{
			Status: &a2a.TaskStatusUpdateEvent{
				Kind:      a2a.KindStatusUpdate,
				TaskID:    "stream-task",
				ContextID: "stream-ctx",
				Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
				Final:     false,
			},
		}

		// Send completion
		streamChan <- a2a.StreamResponse{
			Status: &a2a.TaskStatusUpdateEvent{
				Kind:      a2a.KindStatusUpdate,
				TaskID:    "stream-task",
				ContextID: "stream-ctx",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Final:     true,
			},
		}
	}()

	// Expected request params
	params := a2a.MessageSendParams{
		Message: a2a.NewMessage("stream-msg", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Streaming test"),
		}),
	}

	// Set up mock expectation
	mockService.EXPECT().SendStreamingMessage(gomock.Any(), params).Return(streamChan, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest(a2a.MethodSendStreamingMessage, params, "stream-req-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request with SSE accept header
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response headers
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	// Parse SSE events
	responseBody := recorder.Body.String()
	events := strings.Split(strings.TrimSpace(responseBody), "\n\n")

	require.Len(t, events, 2)

	// Verify first event (working status)
	firstEventData := strings.TrimPrefix(events[0], "data: ")
	var firstResponse a2a.JSONRPCResponse
	err = json.Unmarshal([]byte(firstEventData), &firstResponse)
	require.NoError(t, err)

	assert.Equal(t, "2.0", firstResponse.JSONRpc)
	assert.Equal(t, "stream-req-1", firstResponse.ID)
	assert.Nil(t, firstResponse.Error)

	// Verify second event (completed status)
	secondEventData := strings.TrimPrefix(events[1], "data: ")
	var secondResponse a2a.JSONRPCResponse
	err = json.Unmarshal([]byte(secondEventData), &secondResponse)
	require.NoError(t, err)

	assert.Equal(t, "2.0", secondResponse.JSONRpc)
	assert.Equal(t, "stream-req-1", secondResponse.ID)
	assert.Nil(t, secondResponse.Error)
}

func TestHandler_MethodNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Create JSON-RPC request with unknown method
	jsonrpcReq := a2a.NewJSONRPCRequest("unknown/method", nil, "req-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code) // JSON-RPC errors are HTTP 200
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify error response
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "req-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeMethodNotFound, response.Error.Code)
	assert.Equal(t, a2a.ErrorCodeText(a2a.ErrorCodeMethodNotFound), response.Error.Message)
}

func TestHandler_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Create invalid JSON request
	invalidJSON := `{"jsonrpc": "2.0", "method": "message/send", "params":` // incomplete JSON

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(invalidJSON))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code) // JSON-RPC errors are HTTP 200
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify error response
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeParseError, response.Error.Code)
	assert.Equal(t, a2a.ErrorCodeText(a2a.ErrorCodeParseError), response.Error.Message)
}

func TestHandler_GetAgentCard(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Add GetAgentCard method to the registry manually for this test
	handler := NewHandler(mockService)
	handler.methodRegistry["agent/card"] = methodDescriptor{
		method:    "agent/card",
		mediaType: "application/json",
		handler: func(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id interface{}) error {
			resp, err := mockService.GetAgentCard(ctx)
			if err != nil {
				return err
			}
			handler.writeSuccessResponse(w, id, resp)
			return nil
		},
	}

	// Expected response
	expectedCard := &a2a.AgentCard{
		Name:               "Test Agent",
		Version:            "1.0.0",
		ProtocolVersion:    a2a.ProtocolVersion,
		Description:        "A test agent for unit testing",
		URL:                "https://example.com/agent",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []a2a.AgentSkill{},
		Capabilities: a2a.AgentCapabilities{
			Streaming: true,
		},
	}

	// Set up mock expectation
	mockService.EXPECT().GetAgentCard(gomock.Any()).Return(expectedCard, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest("agent/card", nil, "card-req-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify JSON-RPC response structure
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "card-req-1", response.ID)
	assert.Nil(t, response.Error)

	// Verify result content
	resultBytes, err := json.Marshal(response.Result)
	require.NoError(t, err)

	var actualCard a2a.AgentCard
	err = json.Unmarshal(resultBytes, &actualCard)
	require.NoError(t, err)

	assert.Equal(t, "Test Agent", actualCard.Name)
	assert.Equal(t, "1.0.0", actualCard.Version)
	assert.Equal(t, a2a.ProtocolVersion, actualCard.ProtocolVersion)
	assert.True(t, actualCard.Capabilities.Streaming)
}

func TestHandler_GetTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Expected request params
	params := a2a.TaskQueryParams{
		ID: "test-task-456",
	}

	// Expected response
	expectedTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task-456",
		ContextID: "context-789",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
	}

	// Set up mock expectation
	mockService.EXPECT().GetTask(gomock.Any(), params).Return(expectedTask, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest(a2a.MethodGetTask, params, "get-task-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify JSON-RPC response structure
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "get-task-1", response.ID)
	assert.Nil(t, response.Error)

	// Verify result content
	resultBytes, err := json.Marshal(response.Result)
	require.NoError(t, err)

	var actualTask a2a.Task
	err = json.Unmarshal(resultBytes, &actualTask)
	require.NoError(t, err)

	assert.Equal(t, "test-task-456", actualTask.ID)
	assert.Equal(t, a2a.TaskStateCompleted, actualTask.Status.State)
}

func TestHandler_CancelTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Expected request params
	params := a2a.TaskIDParams{
		ID: "test-task-789",
	}

	// Expected response
	expectedTask := &a2a.Task{
		Kind:      a2a.KindTask,
		ID:        "test-task-789",
		ContextID: "context-123",
		Status:    a2a.TaskStatus{State: a2a.TaskStateCanceled},
	}

	// Set up mock expectation
	mockService.EXPECT().CancelTask(gomock.Any(), params).Return(expectedTask, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest(a2a.MethodCancelTask, params, "cancel-task-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify JSON-RPC response structure
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "cancel-task-1", response.ID)
	assert.Nil(t, response.Error)

	// Verify result content
	resultBytes, err := json.Marshal(response.Result)
	require.NoError(t, err)

	var actualTask a2a.Task
	err = json.Unmarshal(resultBytes, &actualTask)
	require.NoError(t, err)

	assert.Equal(t, "test-task-789", actualTask.ID)
	assert.Equal(t, a2a.TaskStateCanceled, actualTask.Status.State)
}

func TestHandler_TaskResubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Create test stream channel
	streamChan := make(chan a2a.StreamResponse, 1)
	go func() {
		defer close(streamChan)

		// Send status update
		streamChan <- a2a.StreamResponse{
			Status: &a2a.TaskStatusUpdateEvent{
				Kind:      a2a.KindStatusUpdate,
				TaskID:    "resubscribe-task",
				ContextID: "resubscribe-ctx",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Final:     true,
			},
		}
	}()

	// Expected request params
	params := a2a.TaskIDParams{
		ID: "test-task-456",
	}

	// Set up mock expectation
	mockService.EXPECT().TaskResubscription(gomock.Any(), params).Return(streamChan, nil)

	// Create JSON-RPC request
	jsonrpcReq := a2a.NewJSONRPCRequest(a2a.MethodTaskResubscription, params, "resub-req-1")
	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request with SSE accept header
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response headers
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	// Parse SSE events
	responseBody := recorder.Body.String()
	assert.Contains(t, responseBody, "data:")
	assert.Contains(t, responseBody, "resubscribe-task")
}

func TestHandler_PushNotificationConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	testCases := []struct {
		name        string
		method      string
		params      interface{}
		setupMock   func()
		description string
	}{
		{
			name:   "SetTaskPushNotificationConfig",
			method: a2a.MethodSetTaskPushNotificationConfig,
			params: a2a.TaskPushNotificationConfig{
				TaskID: "test-123",
			},
			setupMock: func() {
				mockService.EXPECT().
					SetTaskPushNotificationConfig(gomock.Any(), gomock.Any()).
					Return(&a2a.TaskPushNotificationConfig{
						TaskID: "test-123",
						PushNotificationConfig: a2a.PushNotificationConfig{
							ID:  "test-config",
							URL: "https://webhook.example.com",
						},
					}, nil).
					Times(1)
			},
			description: "Should handle push notification config creation",
		},
		{
			name:   "GetTaskPushNotificationConfig",
			method: a2a.MethodGetTaskPushNotificationConfig,
			params: a2a.TaskIDParams{
				ID: "test-123",
			},
			setupMock: func() {
				mockService.EXPECT().
					GetTaskPushNotificationConfig(gomock.Any(), gomock.Any()).
					Return(&a2a.TaskPushNotificationConfig{
						TaskID: "test-123",
						PushNotificationConfig: a2a.PushNotificationConfig{
							ID:  "test-config",
							URL: "https://webhook.example.com",
						},
					}, nil).
					Times(1)
			},
			description: "Should handle push notification config retrieval",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupMock()

			// Create JSON-RPC request
			jsonrpcReq := a2a.NewJSONRPCRequest(tc.method, tc.params, "push-notification-req")
			reqBody, err := json.Marshal(jsonrpcReq)
			require.NoError(t, err)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Handle request
			handler.ServeHTTP(recorder, req)

			// Verify response
			assert.Equal(t, http.StatusOK, recorder.Code, tc.description)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

			// Parse response
			var response a2a.JSONRPCResponse
			err = json.Unmarshal(recorder.Body.Bytes(), &response)
			require.NoError(t, err)

			// Verify JSON-RPC response structure
			assert.Equal(t, "2.0", response.JSONRpc)
			assert.Equal(t, "push-notification-req", response.ID)
			assert.Nil(t, response.Error, tc.description)
			assert.NotNil(t, response.Result, tc.description)
		})
	}
}

func TestHandler_InvalidHTTPMethod(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Test GET method (not allowed)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestHandler_InvalidParams(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Create JSON-RPC request with invalid params structure
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  a2a.MethodSendMessage,
		Params:  json.RawMessage(`"invalid params structure"`), // Should be object, not string
		ID:      "invalid-params-req",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code) // JSON-RPC errors are HTTP 200
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify error response
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "invalid-params-req", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInvalidParams, response.Error.Code)
}

func TestHandler_InvalidRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	testCases := []struct {
		name          string
		requestBody   string
		expectedError int
		description   string
	}{
		{
			name:          "Invalid JSON-RPC version",
			requestBody:   `{"jsonrpc":"1.0","method":"test","id":"1"}`,
			expectedError: a2a.ErrorCodeInvalidRequest,
			description:   "Should reject non-2.0 JSON-RPC versions",
		},
		{
			name:          "Missing JSONRPC field",
			requestBody:   `{"method":"test","id":"1"}`,
			expectedError: a2a.ErrorCodeInvalidRequest,
			description:   "Should reject requests without jsonrpc field",
		},
		{
			name:          "Missing method field",
			requestBody:   `{"jsonrpc":"2.0","id":"1"}`,
			expectedError: a2a.ErrorCodeMethodNotFound,
			description:   "Should reject requests without method field",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create HTTP request
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.requestBody))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Handle request
			handler.ServeHTTP(recorder, req)

			// Verify response
			assert.Equal(t, http.StatusOK, recorder.Code, tc.description) // JSON-RPC errors are HTTP 200
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

			// Parse response
			var response a2a.JSONRPCResponse
			err := json.Unmarshal(recorder.Body.Bytes(), &response)
			require.NoError(t, err)

			// Verify error response
			assert.Equal(t, "2.0", response.JSONRpc)
			assert.Nil(t, response.Result)
			require.NotNil(t, response.Error, "Expected JSON-RPC error")
			assert.Equal(t, tc.expectedError, response.Error.Code, tc.description)
		})
	}
}

func TestHandler_WellKnownAgentCard(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Expected agent card
	expectedCard := &a2a.AgentCard{
		Name:               "Test Agent",
		Version:            "1.0.0",
		ProtocolVersion:    a2a.ProtocolVersion,
		Description:        "A test agent",
		URL:                "https://example.com/api",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []a2a.AgentSkill{},
		Capabilities:       a2a.AgentCapabilities{},
	}

	// Set up mock expectation
	mockService.EXPECT().GetAgentCard(gomock.Any()).Return(expectedCard, nil)

	// Create HTTP request for agent card
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=3600", recorder.Header().Get("Cache-Control"))

	// Parse response
	var response a2a.AgentCard
	err := json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify agent card
	assert.Equal(t, expectedCard.Name, response.Name)
	assert.Equal(t, expectedCard.Version, response.Version)
	assert.Equal(t, expectedCard.ProtocolVersion, response.ProtocolVersion)
	assert.Equal(t, expectedCard.Description, response.Description)
}

func TestHandler_WithCustomPaths(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Create handler with custom paths
	handler := NewHandler(mockService,
		WithRPCPath("/api/a2a"),
		WithAgentCardPath("/agent-info"),
		WithAgentCardCacheMaxAge(7200),
	)

	// Test custom agent card path
	expectedCard := &a2a.AgentCard{
		Name:               "Custom Agent",
		Version:            "2.0.0",
		ProtocolVersion:    a2a.ProtocolVersion,
		Description:        "Custom path agent",
		URL:                "https://example.com/api/a2a",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []a2a.AgentSkill{},
		Capabilities:       a2a.AgentCapabilities{},
	}

	mockService.EXPECT().GetAgentCard(gomock.Any()).Return(expectedCard, nil)

	// Test custom agent card endpoint
	req := httptest.NewRequest(http.MethodGet, "/agent-info", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=7200", recorder.Header().Get("Cache-Control"))

	// Test that default path no longer works
	req2 := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	recorder2 := httptest.NewRecorder()
	handler.ServeHTTP(recorder2, req2)

	assert.Equal(t, http.StatusNotFound, recorder2.Code)
}

func TestA2AMiddleware(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Create a custom handler for non-A2A requests
	customHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Custom response"))
	})

	// Create middleware
	middleware := A2AMiddleware(mockService, WithRPCPath("/api/a2a"))
	finalHandler := middleware(customHandler)

	// Test A2A agent card request (should be handled by middleware)
	expectedCard := &a2a.AgentCard{
		Name:               "Middleware Agent",
		Version:            "1.0.0",
		ProtocolVersion:    a2a.ProtocolVersion,
		Description:        "Middleware test agent",
		URL:                "https://example.com/api/a2a",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []a2a.AgentSkill{},
		Capabilities:       a2a.AgentCapabilities{},
	}

	mockService.EXPECT().GetAgentCard(gomock.Any()).Return(expectedCard, nil)

	req1 := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	recorder1 := httptest.NewRecorder()
	finalHandler.ServeHTTP(recorder1, req1)

	assert.Equal(t, http.StatusOK, recorder1.Code)
	assert.Equal(t, "application/json", recorder1.Header().Get("Content-Type"))

	// Test custom endpoint (should be handled by next handler)
	req2 := httptest.NewRequest(http.MethodGet, "/custom", nil)
	recorder2 := httptest.NewRecorder()
	finalHandler.ServeHTTP(recorder2, req2)

	assert.Equal(t, http.StatusOK, recorder2.Code)
	assert.Equal(t, "text/plain", recorder2.Header().Get("Content-Type"))
	assert.Equal(t, "Custom response", recorder2.Body.String())
}

func TestHandler_AcceptHeaderHandling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	testCases := []struct {
		name         string
		method       string
		acceptHeader string
		expectSSE    bool
		expectStatus int
		description  string
	}{
		{
			name:         "Invalid method with Accept */*",
			method:       "invalid/method",
			acceptHeader: "*/*",
			expectSSE:    false,
			expectStatus: http.StatusOK, // JSON-RPC errors are still HTTP 200
			description:  "Should return JSON error for invalid method even with Accept */*",
		},
		{
			name:         "Invalid method with Accept text/*",
			method:       "invalid/method",
			acceptHeader: "text/*",
			expectSSE:    false,
			expectStatus: http.StatusOK,
			description:  "Should return JSON error for invalid method even with Accept text/*",
		},
		{
			name:         "Invalid method with Accept text/event-stream",
			method:       "invalid/method",
			acceptHeader: "text/event-stream",
			expectSSE:    true,
			expectStatus: http.StatusOK,
			description:  "Should return SSE error for invalid method with explicit text/event-stream",
		},
		{
			name:         "Valid SSE method with Accept */*",
			method:       "message/stream",
			acceptHeader: "*/*",
			expectSSE:    true,
			expectStatus: http.StatusOK,
			description:  "Should accept SSE method with Accept */*",
		},
		{
			name:         "Valid SSE method with Accept text/*",
			method:       "message/stream",
			acceptHeader: "text/*",
			expectSSE:    true,
			expectStatus: http.StatusOK,
			description:  "Should accept SSE method with Accept text/*",
		},
		{
			name:         "Valid SSE method with Accept application/json",
			method:       "message/stream",
			acceptHeader: "application/json",
			expectSSE:    false,
			expectStatus: http.StatusOK,
			description:  "Should reject SSE method with incompatible Accept header",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mock for valid methods
			if tc.method == "message/stream" {
				params := a2a.MessageSendParams{
					Message: a2a.NewMessage("test-msg", a2a.RoleUser, []a2a.Part{
						a2a.NewTextPart("Hello"),
					}),
				}

				if tc.expectSSE && tc.acceptHeader != "application/json" {
					// Mock successful SSE response
					streamChan := make(chan a2a.StreamResponse, 1)
					streamChan <- a2a.StreamResponse{
						Task: &a2a.Task{
							Kind:      a2a.KindTask,
							ID:        "task-123",
							ContextID: "context-456",
							Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
						},
					}
					close(streamChan)
					mockService.EXPECT().SendStreamingMessage(gomock.Any(), params).Return(streamChan, nil)
				}
			}

			// Create JSON-RPC request
			jsonrpcReq := a2a.JSONRPCRequest{
				JSONRpc: "2.0",
				Method:  tc.method,
				Params: map[string]interface{}{
					"message": map[string]interface{}{
						"kind":      "message",
						"messageId": "test-msg",
						"role":      "user",
						"parts": []map[string]interface{}{
							{"kind": "text", "text": "Hello"},
						},
					},
				},
				ID: "req-1",
			}

			reqBody, err := json.Marshal(jsonrpcReq)
			require.NoError(t, err)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", tc.acceptHeader)

			// Create response recorder
			recorder := httptest.NewRecorder()

			// Handle request
			handler.ServeHTTP(recorder, req)

			// Verify response
			assert.Equal(t, tc.expectStatus, recorder.Code, tc.description)

			if tc.expectSSE {
				assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"), tc.description)
			} else {
				assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"), tc.description)
			}
		})
	}
}

func TestHandler_RegisterMethod(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Register custom JSON-RPC method
	handler.RegisterMethod("custom/calculate", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var req struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params")
		}
		return map[string]int{"sum": req.A + req.B}, nil
	})

	// Test the custom method
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "custom/calculate",
		Params: map[string]interface{}{
			"a": 5,
			"b": 3,
		},
		ID: "calc-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	// Handle request
	handler.ServeHTTP(recorder, req)

	// Verify response
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Parse response
	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify result
	assert.Equal(t, "2.0", response.JSONRpc)
	assert.Equal(t, "calc-1", response.ID)
	assert.Nil(t, response.Error)
	require.NotNil(t, response.Result)

	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(8), result["sum"]) // JSON numbers are float64
}

func TestHandler_RegisterMethodError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Register custom method that returns error
	handler.RegisterMethod("custom/error", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("custom error")
	})

	// Test error case
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "custom/error",
		Params:  map[string]interface{}{},
		ID:      "error-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	// Should return internal error
	assert.Equal(t, http.StatusOK, recorder.Code) // JSON-RPC errors are HTTP 200
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "error-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInternalError, response.Error.Code)
}

func TestHandler_JSONRPCErrorHandling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Register method that returns JSONRPCError directly
	handler.RegisterMethod("custom/jsonrpc-error", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var req struct {
			ErrorType string `json:"errorType"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse request")
		}

		switch req.ErrorType {
		case "task-not-found":
			return nil, a2a.NewJSONRPCTaskNotFoundError("task-123")
		case "custom-internal":
			return nil, a2a.NewJSONRPCInternalError("Custom internal error", map[string]string{
				"detail": "Something went wrong",
			})
		default:
			return map[string]string{"result": "success"}, nil
		}
	})

	testCases := []struct {
		name         string
		errorType    string
		expectedCode int
		expectedData interface{}
	}{
		{
			name:         "Task not found error",
			errorType:    "task-not-found",
			expectedCode: a2a.ErrorCodeTaskNotFound,
			expectedData: map[string]interface{}{"taskId": "task-123"},
		},
		{
			name:         "Custom internal error",
			errorType:    "custom-internal",
			expectedCode: a2a.ErrorCodeInternalError,
			expectedData: map[string]interface{}{"detail": "Something went wrong"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonrpcReq := a2a.JSONRPCRequest{
				JSONRpc: "2.0",
				Method:  "custom/jsonrpc-error",
				Params: map[string]interface{}{
					"errorType": tc.errorType,
				},
				ID: "error-test-1",
			}

			reqBody, err := json.Marshal(jsonrpcReq)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

			var response a2a.JSONRPCResponse
			err = json.Unmarshal(recorder.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "error-test-1", response.ID)
			assert.Nil(t, response.Result)
			require.NotNil(t, response.Error)
			assert.Equal(t, tc.expectedCode, response.Error.Code)

			if tc.expectedData != nil {
				assert.Equal(t, tc.expectedData, response.Error.Data)
			}
		})
	}
}

func TestHandler_RegularErrorToInternalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService)

	// Register method that returns regular Go error
	handler.RegisterMethod("custom/regular-error", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("this is a regular Go error")
	})

	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "custom/regular-error",
		Params:  map[string]interface{}{},
		ID:      "regular-error-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "regular-error-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInternalError, response.Error.Code)
	assert.Equal(t, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), response.Error.Message)
	assert.Nil(t, response.Error.Data)
}

func TestHandler_NewDefaultInternalErrorBehavior(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Create handler without custom internal error builder (uses default behavior)
	handler := NewHandler(mockService)

	// Register a method that returns a regular error
	handler.RegisterMethod("default/error-test", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("test error for default behavior")
	})

	// Create request
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "default/error-test",
		Params:  map[string]interface{}{},
		ID:      "default-error-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "default-error-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInternalError, response.Error.Code)
	assert.Equal(t, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), response.Error.Message)

	// Check that default behavior has data=nil
	assert.Nil(t, response.Error.Data)
}

func TestHandler_NewCustomInternalErrorBuilder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Create handler with custom internal error builder
	handler := NewHandler(mockService, WithInternalErrorBuilder(func(ctx context.Context, id interface{}, originalError error) (int, string, interface{}) {
		return a2a.ErrorCodeInternalError, "Custom Internal Error", map[string]interface{}{
			"message":  "Custom error processing",
			"original": originalError.Error(),
			"type":     "internal_error",
		}
	}))

	// Register a method that returns a regular error
	handler.RegisterMethod("custom/error-test", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("test error for custom builder")
	})

	// Create request
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "custom/error-test",
		Params:  map[string]interface{}{},
		ID:      "custom-error-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var response a2a.JSONRPCResponse
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "custom-error-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInternalError, response.Error.Code)
	assert.Equal(t, "Custom Internal Error", response.Error.Message)

	// Check that custom error builder was used
	dataMap, ok := response.Error.Data.(map[string]interface{})
	require.True(t, ok, "Expected data to be a map")
	assert.Equal(t, "Custom error processing", dataMap["message"])
	assert.Equal(t, "test error for custom builder", dataMap["original"])
	assert.Equal(t, "internal_error", dataMap["type"])
}

func TestHandler_NewCustomInternalErrorBuilder_SSE(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)

	// Create handler with custom internal error builder
	handler := NewHandler(mockService, WithInternalErrorBuilder(func(ctx context.Context, id interface{}, originalError error) (int, string, interface{}) {
		return a2a.ErrorCodeInternalError, "SSE Internal Error", map[string]interface{}{
			"error_code": "SSE_INTERNAL_ERROR",
			"details":    originalError.Error(),
		}
	}))

	// Register a streaming method that returns an error
	handler.methodRegistry["custom/sse-error"] = methodDescriptor{
		method:    "custom/sse-error",
		mediaType: "text/event-stream",
		handler: func(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id interface{}) error {
			return fmt.Errorf("SSE error for testing")
		},
	}

	// Create request
	jsonrpcReq := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  "custom/sse-error",
		Params:  map[string]interface{}{},
		ID:      "sse-error-1",
	}

	reqBody, err := json.Marshal(jsonrpcReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))

	// Parse SSE response
	responseBody := recorder.Body.String()
	assert.True(t, strings.HasPrefix(responseBody, "data: "))

	// Extract JSON from SSE format
	jsonData := strings.TrimPrefix(responseBody, "data: ")
	jsonData = strings.TrimSuffix(jsonData, "\n\n")

	var response a2a.JSONRPCResponse
	err = json.Unmarshal([]byte(jsonData), &response)
	require.NoError(t, err)

	assert.Equal(t, "sse-error-1", response.ID)
	assert.Nil(t, response.Result)
	require.NotNil(t, response.Error)
	assert.Equal(t, a2a.ErrorCodeInternalError, response.Error.Code)
	assert.Equal(t, "SSE Internal Error", response.Error.Message)

	// Check that custom error builder was used for SSE
	dataMap, ok := response.Error.Data.(map[string]interface{})
	require.True(t, ok, "Expected data to be a map")
	assert.Equal(t, "SSE_INTERNAL_ERROR", dataMap["error_code"])
	assert.Equal(t, "SSE error for testing", dataMap["details"])
}

func TestHandler_NewBuildInternalError_DefaultBehavior(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService) // No custom builder

	ctx := context.Background()
	originalError := fmt.Errorf("test error message")

	code, message, data := handler.buildInternalError(ctx, "test-id", originalError)

	assert.Equal(t, a2a.ErrorCodeInternalError, code)
	assert.Equal(t, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), message)
	assert.Nil(t, data)
}

func TestHandler_NewBuildInternalError_CustomBuilder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockAgentService(ctrl)
	handler := NewHandler(mockService, WithInternalErrorBuilder(func(ctx context.Context, id interface{}, originalError error) (int, string, interface{}) {
		return 5000, "Custom Error Message", map[string]interface{}{
			"custom":    true,
			"message":   originalError.Error(),
			"timestamp": "2024-01-01T00:00:00Z",
		}
	}))

	ctx := context.Background()
	originalError := fmt.Errorf("custom test error")

	code, message, data := handler.buildInternalError(ctx, "custom-id", originalError)

	assert.Equal(t, 5000, code)
	assert.Equal(t, "Custom Error Message", message)

	dataMap, ok := data.(map[string]interface{})
	require.True(t, ok, "Expected data to be a map")
	assert.Equal(t, true, dataMap["custom"])
	assert.Equal(t, "custom test error", dataMap["message"])
	assert.Equal(t, "2024-01-01T00:00:00Z", dataMap["timestamp"])
}
