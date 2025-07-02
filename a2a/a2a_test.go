package a2a

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// MESSAGE TESTS
// =============================================================================

func TestMessage_JSON(t *testing.T) {
	// Test basic message structure
	msg := NewMessage("msg-123", RoleUser, []Part{
		NewTextPart("Hello world"),
	})
	msg.ContextID = "ctx-456"

	// Marshal to JSON
	jsonData, err := json.Marshal(msg)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, string(KindMessage), jsonMap["kind"])
	assert.Equal(t, "msg-123", jsonMap["messageId"])
	assert.Equal(t, "ctx-456", jsonMap["contextId"])
	assert.Equal(t, string(RoleUser), jsonMap["role"])

	parts := jsonMap["parts"].([]interface{})
	require.Len(t, parts, 1)

	textPart := parts[0].(map[string]interface{})
	assert.Equal(t, string(KindTextPart), textPart["kind"])
	assert.Equal(t, "Hello world", textPart["text"])

	// Unmarshal back
	var unmarshaledMsg Message
	err = json.Unmarshal(jsonData, &unmarshaledMsg)
	require.NoError(t, err)
	assert.Equal(t, msg, unmarshaledMsg)
}

func TestMessage_WithFilePart(t *testing.T) {
	msg := NewMessage("msg-456", RoleAgent, []Part{
		NewTextPart("Here's your file:"),
		NewFilePart("https://example.com/document.pdf", "document.pdf", "application/pdf"),
	})

	// Marshal to JSON
	jsonData, err := json.Marshal(msg)
	require.NoError(t, err)

	// Verify JSON structure contains file part
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	parts := jsonMap["parts"].([]interface{})
	require.Len(t, parts, 2)

	filePart := parts[1].(map[string]interface{})
	assert.Equal(t, string(KindFilePart), filePart["kind"])

	file := filePart["file"].(map[string]interface{})
	assert.Equal(t, "https://example.com/document.pdf", file["uri"])
	assert.Equal(t, "document.pdf", file["name"])
	assert.Equal(t, "application/pdf", file["mimeType"])
}

func TestMessage_WithMetadata(t *testing.T) {
	msg := NewMessage("msg-meta", RoleUser, []Part{
		NewTextPart("Message with metadata"),
	})
	msg.Metadata = map[string]interface{}{
		"priority": "high",
		"source":   "web",
		"count":    42,
		"tags":     []string{"important", "user-request"},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(msg)
	require.NoError(t, err)

	// Verify metadata is preserved
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	metadata := jsonMap["metadata"].(map[string]interface{})
	assert.Equal(t, "high", metadata["priority"])
	assert.Equal(t, "web", metadata["source"])
	assert.Equal(t, float64(42), metadata["count"])

	tags := metadata["tags"].([]interface{})
	assert.Equal(t, "important", tags[0])
	assert.Equal(t, "user-request", tags[1])
}

// =============================================================================
// TASK TESTS
// =============================================================================

func TestTask_JSON(t *testing.T) {
	task := NewTask("task-789", "ctx-012", TaskStateSubmitted)
	task.History = []Message{
		NewMessage("msg-1", RoleUser, []Part{NewTextPart("Initial request")}),
	}

	// Set timestamp
	now := time.Now()
	task.Status.SetTimestamp(now)

	// Marshal to JSON
	jsonData, err := json.Marshal(task)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, string(KindTask), jsonMap["kind"])
	assert.Equal(t, "task-789", jsonMap["id"])
	assert.Equal(t, "ctx-012", jsonMap["contextId"])

	status := jsonMap["status"].(map[string]interface{})
	assert.Equal(t, string(TaskStateSubmitted), status["state"])
	assert.NotNil(t, status["timestamp"])

	history := jsonMap["history"].([]interface{})
	require.Len(t, history, 1)
}

func TestTaskStatusTimestamp(t *testing.T) {
	ts := &TaskStatus{State: TaskStateWorking}

	// Test setting timestamp
	now := time.Now()
	ts.SetTimestamp(now)

	// Test getting timestamp
	parsed, err := ts.GetTimestamp()
	require.NoError(t, err)
	require.NotNil(t, parsed)

	// Should be equal within second precision (due to RFC3339 format)
	assert.WithinDuration(t, now, *parsed, time.Second)
}

// =============================================================================
// JSON-RPC TESTS
// =============================================================================

func TestJSONRPCRequest(t *testing.T) {
	params := MessageSendParams{
		Message: NewMessage("msg-123", RoleUser, []Part{
			NewTextPart("Test message"),
		}),
		Configuration: &MessageSendConfiguration{
			AcceptedOutputModes: []string{"text/plain"},
			Blocking:            true,
		},
	}

	req := NewJSONRPCRequest(MethodSendMessage, params, "req-1")

	// Marshal to JSON
	jsonData, err := json.Marshal(req)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "2.0", jsonMap["jsonrpc"])
	assert.Equal(t, MethodSendMessage, jsonMap["method"])
	assert.Equal(t, "req-1", jsonMap["id"])
	assert.NotNil(t, jsonMap["params"])
}

func TestJSONRPCResponse(t *testing.T) {
	result := SendMessageResult{
		Task: &Task{
			Kind:      KindTask,
			ID:        "task-123",
			ContextID: "ctx-456",
			Status: TaskStatus{
				State: TaskStateSubmitted,
			},
		},
	}

	resp := NewJSONRPCResponse(result, "req-1")

	// Marshal to JSON
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "2.0", jsonMap["jsonrpc"])
	assert.Equal(t, "req-1", jsonMap["id"])
	assert.NotNil(t, jsonMap["result"])
	assert.Nil(t, jsonMap["error"])
}

func TestJSONRPCErrorResponse(t *testing.T) {
	resp := NewTaskNotFoundError("req-1")

	// Marshal to JSON
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)

	// Verify JSON structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonData, &jsonMap)
	require.NoError(t, err)

	assert.Equal(t, "2.0", jsonMap["jsonrpc"])
	assert.Equal(t, "req-1", jsonMap["id"])
	assert.Nil(t, jsonMap["result"])

	errorObj := jsonMap["error"].(map[string]interface{})
	assert.Equal(t, float64(ErrorCodeTaskNotFound), errorObj["code"])
	assert.Equal(t, ErrorCodeText(ErrorCodeTaskNotFound), errorObj["message"])
}

// =============================================================================
// HELPER FUNCTION TESTS
// =============================================================================

func TestHelperFunctions(t *testing.T) {
	t.Run("NewTextPart", func(t *testing.T) {
		part := NewTextPart("Hello")
		assert.Equal(t, KindTextPart, part.Kind)
		assert.Equal(t, "Hello", part.Text)
		assert.Nil(t, part.File)
		assert.Nil(t, part.Data)
	})

	t.Run("NewFilePart", func(t *testing.T) {
		part := NewFilePart("https://example.com/file.pdf", "file.pdf", "application/pdf")
		assert.Equal(t, KindFilePart, part.Kind)
		assert.Equal(t, "", part.Text)
		require.NotNil(t, part.File)
		assert.Equal(t, "https://example.com/file.pdf", part.File.URI)
		assert.Equal(t, "file.pdf", part.File.Name)
		assert.Equal(t, "application/pdf", part.File.MimeType)
	})

	t.Run("NewFilePartWithBytes", func(t *testing.T) {
		part := NewFilePartWithBytes("base64data", "file.txt", "text/plain")
		assert.Equal(t, KindFilePart, part.Kind)
		require.NotNil(t, part.File)
		assert.Equal(t, "base64data", part.File.Bytes)
		assert.Equal(t, "", part.File.URI)
	})

	t.Run("NewDataPart", func(t *testing.T) {
		data := map[string]interface{}{"key": "value"}
		part := NewDataPart(data)
		assert.Equal(t, KindDataPart, part.Kind)
		assert.Equal(t, data, part.Data)
	})
}

// =============================================================================
// VALIDATION TESTS
// =============================================================================

func TestMessage_Validate(t *testing.T) {
	t.Run("Valid message", func(t *testing.T) {
		msg := NewMessage("msg-123", RoleUser, []Part{
			NewTextPart("Hello world"),
		})
		assert.NoError(t, msg.Validate())
	})

	t.Run("Invalid kind", func(t *testing.T) {
		msg := NewMessage("msg-123", RoleUser, []Part{
			NewTextPart("Hello world"),
		})
		msg.Kind = Kind("invalid")
		assert.Error(t, msg.Validate())
	})

	t.Run("Missing messageId", func(t *testing.T) {
		msg := NewMessage("", RoleUser, []Part{
			NewTextPart("Hello world"),
		})
		assert.Error(t, msg.Validate())
	})

	t.Run("Invalid role", func(t *testing.T) {
		msg := NewMessage("msg-123", "invalid", []Part{
			NewTextPart("Hello world"),
		})
		assert.Error(t, msg.Validate())
	})

	t.Run("Empty parts", func(t *testing.T) {
		msg := NewMessage("msg-123", RoleUser, []Part{})
		assert.Error(t, msg.Validate())
	})

	t.Run("Invalid part", func(t *testing.T) {
		msg := NewMessage("msg-123", RoleUser, []Part{
			{Kind: Kind("invalid")},
		})
		assert.Error(t, msg.Validate())
	})
}

func TestPart_Validate(t *testing.T) {
	t.Run("Valid text part", func(t *testing.T) {
		part := NewTextPart("Hello world")
		assert.NoError(t, part.Validate())
	})

	t.Run("Valid file part with URI", func(t *testing.T) {
		part := NewFilePart("https://example.com/file.pdf", "file.pdf", "application/pdf")
		assert.NoError(t, part.Validate())
	})

	t.Run("Valid file part with bytes", func(t *testing.T) {
		part := NewFilePartWithBytes("base64data", "file.txt", "text/plain")
		assert.NoError(t, part.Validate())
	})

	t.Run("Valid data part", func(t *testing.T) {
		part := NewDataPart(map[string]interface{}{"key": "value"})
		assert.NoError(t, part.Validate())
	})

	t.Run("Missing kind", func(t *testing.T) {
		part := Part{Text: "Hello"}
		assert.Error(t, part.Validate())
	})

	t.Run("Invalid kind", func(t *testing.T) {
		part := Part{Kind: Kind("invalid")}
		assert.Error(t, part.Validate())
	})

	t.Run("Text part without text", func(t *testing.T) {
		part := Part{Kind: KindTextPart}
		assert.Error(t, part.Validate())
	})

	t.Run("Text part with file field", func(t *testing.T) {
		part := Part{
			Kind: KindTextPart,
			Text: "Hello",
			File: &FilePart{URI: "test"},
		}
		assert.Error(t, part.Validate())
	})

	t.Run("File part without file", func(t *testing.T) {
		part := Part{Kind: KindFilePart}
		assert.Error(t, part.Validate())
	})

	t.Run("Data part without data", func(t *testing.T) {
		part := Part{Kind: KindDataPart}
		assert.Error(t, part.Validate())
	})
}

func TestFilePart_Validate(t *testing.T) {
	t.Run("Valid with URI", func(t *testing.T) {
		file := &FilePart{URI: "https://example.com/file.pdf"}
		assert.NoError(t, file.Validate())
	})

	t.Run("Valid with bytes", func(t *testing.T) {
		file := &FilePart{Bytes: "base64data"}
		assert.NoError(t, file.Validate())
	})

	t.Run("Both URI and bytes", func(t *testing.T) {
		file := &FilePart{
			URI:   "https://example.com/file.pdf",
			Bytes: "base64data",
		}
		assert.Error(t, file.Validate())
	})

	t.Run("Neither URI nor bytes", func(t *testing.T) {
		file := &FilePart{}
		assert.Error(t, file.Validate())
	})
}

func TestTask_Validate(t *testing.T) {
	t.Run("Valid task", func(t *testing.T) {
		task := NewTask("task-123", "ctx-456", TaskStateSubmitted)
		assert.NoError(t, task.Validate())
	})

	t.Run("Invalid kind", func(t *testing.T) {
		task := NewTask("task-123", "ctx-456", TaskStateSubmitted)
		task.Kind = Kind("invalid")
		assert.Error(t, task.Validate())
	})

	t.Run("Missing ID", func(t *testing.T) {
		task := NewTask("", "ctx-456", TaskStateSubmitted)
		assert.Error(t, task.Validate())
	})

	t.Run("Missing contextId", func(t *testing.T) {
		task := NewTask("task-123", "", TaskStateSubmitted)
		assert.Error(t, task.Validate())
	})

	t.Run("Invalid status", func(t *testing.T) {
		task := NewTask("task-123", "ctx-456", "invalid")
		assert.Error(t, task.Validate())
	})
}

func TestTaskStatus_Validate(t *testing.T) {
	t.Run("Valid status", func(t *testing.T) {
		status := TaskStatus{State: TaskStateWorking}
		assert.NoError(t, status.Validate())
	})

	t.Run("Missing state", func(t *testing.T) {
		status := TaskStatus{}
		assert.Error(t, status.Validate())
	})

	t.Run("Invalid state", func(t *testing.T) {
		status := TaskStatus{State: "invalid"}
		assert.Error(t, status.Validate())
	})

	t.Run("Valid with message", func(t *testing.T) {
		status := TaskStatus{
			State: TaskStateWorking,
			Message: &Message{
				Kind:      KindMessage,
				MessageID: "msg-123",
				Role:      RoleAgent,
				Parts:     []Part{NewTextPart("Status update")},
			},
		}
		assert.NoError(t, status.Validate())
	})
}

func TestJSONRPCRequest_Validate(t *testing.T) {
	t.Run("Valid request", func(t *testing.T) {
		req := NewJSONRPCRequest(MethodSendMessage, nil, "req-1")
		assert.NoError(t, req.Validate())
	})

	t.Run("Invalid jsonrpc version", func(t *testing.T) {
		req := JSONRPCRequest{
			JSONRpc: "1.0",
			Method:  MethodSendMessage,
			ID:      "req-1",
		}
		assert.Error(t, req.Validate())
	})

	t.Run("Missing method", func(t *testing.T) {
		req := JSONRPCRequest{
			JSONRpc: "2.0",
			ID:      "req-1",
		}
		assert.Error(t, req.Validate())
	})

	t.Run("Invalid method", func(t *testing.T) {
		req := JSONRPCRequest{
			JSONRpc: "2.0",
			Method:  "invalid/method",
			ID:      "req-1",
		}
		assert.Error(t, req.Validate())
	})

	t.Run("Missing ID", func(t *testing.T) {
		req := JSONRPCRequest{
			JSONRpc: "2.0",
			Method:  MethodSendMessage,
		}
		assert.Error(t, req.Validate())
	})
}

// =============================================================================
// ENUM VALIDATION TESTS
// =============================================================================

func TestTaskStateHelpers(t *testing.T) {
	t.Run("IsTerminal", func(t *testing.T) {
		assert.True(t, TaskStateCompleted.IsTerminal())
		assert.True(t, TaskStateFailed.IsTerminal())
		assert.True(t, TaskStateCanceled.IsTerminal())
		assert.True(t, TaskStateRejected.IsTerminal())

		assert.False(t, TaskStateSubmitted.IsTerminal())
		assert.False(t, TaskStateWorking.IsTerminal())
		assert.False(t, TaskStateInputRequired.IsTerminal())
		assert.False(t, TaskStateAuthRequired.IsTerminal())
	})

	t.Run("IsInterrupted", func(t *testing.T) {
		assert.True(t, TaskStateInputRequired.IsInterrupted())

		assert.False(t, TaskStateSubmitted.IsInterrupted())
		assert.False(t, TaskStateWorking.IsInterrupted())
		assert.False(t, TaskStateCompleted.IsInterrupted())
	})

	t.Run("CanCancel", func(t *testing.T) {
		assert.True(t, TaskStateSubmitted.CanCancel())
		assert.True(t, TaskStateWorking.CanCancel())
		assert.True(t, TaskStateInputRequired.CanCancel())
		assert.True(t, TaskStateAuthRequired.CanCancel())

		assert.False(t, TaskStateCompleted.CanCancel())
		assert.False(t, TaskStateFailed.CanCancel())
		assert.False(t, TaskStateCanceled.CanCancel())
		assert.False(t, TaskStateRejected.CanCancel())
	})
}

func TestValidationHelpers(t *testing.T) {
	t.Run("Role.IsValid", func(t *testing.T) {
		assert.True(t, RoleUser.IsValid())
		assert.True(t, RoleAgent.IsValid())
		assert.False(t, Role("invalid").IsValid())
	})

	t.Run("isValidPartKind", func(t *testing.T) {
		assert.True(t, isValidPartKind(KindTextPart))
		assert.True(t, isValidPartKind(KindFilePart))
		assert.True(t, isValidPartKind(KindDataPart))
		assert.False(t, isValidPartKind(Kind("invalid")))
	})

	t.Run("TaskState.IsValid", func(t *testing.T) {
		assert.True(t, TaskStateSubmitted.IsValid())
		assert.True(t, TaskStateWorking.IsValid())
		assert.True(t, TaskStateCompleted.IsValid())
		assert.False(t, TaskState("invalid").IsValid())
	})

}
