package atlasictest

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	// Create a simple test agent
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:        "Test Agent",
		Description: "A simple test agent",
		Version:     "1.0.0",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Hello from test agent!"),
			},
		}, nil
	})

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Verify server is running
	require.NotEmpty(t, server.URL())
	require.NotNil(t, server.AgentService)
	require.Equal(t, agent, server.Agent)
}

func TestNewServer_WithTempDir(t *testing.T) {
	// Create a simple test agent
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:    "Temp Dir Agent",
		Version: "1.0.0",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Response from temp dir agent"),
			},
		}, nil
	})

	// NewServer automatically creates temp directory
	server := NewServer(t, agent)
	defer server.Close()

	// Verify server is running
	require.NotEmpty(t, server.URL())
	require.NotNil(t, server.AgentService)
}

func TestTestServer_Client(t *testing.T) {
	// Create a test agent
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name: "Client Test Agent",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Client test response"),
			},
		}, nil
	})

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Get client from server
	client := server.Client()
	require.NotNil(t, client)

	// Test basic connectivity by getting agent card
	ctx := context.Background()
	agentCard, err := client.GetAgentCard(ctx)
	require.NoError(t, err)
	require.Equal(t, "Client Test Agent", agentCard.Name)
}

func TestTestServer_Integration(t *testing.T) {
	// Create a more complex test agent
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:        "Integration Test Agent",
		Description: "An agent for integration testing",
		Version:     "2.0.0",
		Skills: []a2a.AgentSkill{
			{
				ID:          "test-skill",
				Name:        "testing",
				Description: "A skill for testing",
			},
		},
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		// Get the task to see what was sent
		task, err := handle.GetTask(ctx, 10) // Get up to 10 history entries
		if err != nil {
			return nil, err
		}

		// Echo back the received message with modification
		var receivedText string
		if len(task.History) > 0 && len(task.History[0].Parts) > 0 {
			receivedText = task.History[0].Parts[0].Text
		} else {
			receivedText = "no message"
		}

		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Received: " + receivedText),
			},
		}, nil
	})

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Create client and test full message flow
	client := server.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send a test message
	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Role: a2a.RoleUser,
			Parts: []a2a.Part{
				a2a.NewTextPart("Hello integration test!"),
			},
		},
	}

	result, err := client.SendMessage(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check if result has Message before accessing it
	if result.Message != nil {
		require.Equal(t, a2a.RoleAgent, result.Message.Role)
		require.Len(t, result.Message.Parts, 1)
		require.Equal(t, "Received: Hello integration test!", result.Message.Parts[0].Text)
	} else if result.Task != nil {
		// In non-blocking mode, we might get a Task instead
		require.NotNil(t, result.Task)
		t.Logf("Got Task instead of Message: %+v", result.Task)
	} else {
		t.Fatalf("Result has neither Message nor Task: %+v", result)
	}
}

func TestNewServer_SubdirectoryCreation(t *testing.T) {
	// Create a simple test agent
	agent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:    "Directory Test Agent",
		Version: "1.0.0",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Directory test response"),
			},
		}, nil
	})

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Verify server is running
	require.NotEmpty(t, server.URL())
	require.NotNil(t, server.AgentService)

	// Verify that the storage is working by creating a test file
	require.NotNil(t, server.AgentService.Storage)

	// Test that we can create a test file in the storage to verify subdirectory is working
	ctx := context.Background()
	testData := []byte("test content")

	// Use OpenContextFile to create a test file
	file, err := server.AgentService.Storage.OpenContextFile(ctx, "test-context", "test-file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer file.Close()

	// Write test data - need to assert that fs.File implements io.Writer
	writer, ok := file.(io.Writer)
	require.True(t, ok, "file should implement io.Writer")

	n, err := writer.Write(testData)
	require.NoError(t, err)
	require.Equal(t, len(testData), n)

	// Verify we can read it back
	readFile, err := server.AgentService.Storage.OpenContextFile(ctx, "test-context", "test-file.txt", os.O_RDONLY, 0)
	require.NoError(t, err)
	defer readFile.Close()

	readData := make([]byte, len(testData))
	n, err = readFile.Read(readData)
	require.NoError(t, err)
	require.Equal(t, len(testData), n)
	require.Equal(t, testData, readData)
}

func TestClientWithHeaders_HTTPHeaders_PassedToAgent(t *testing.T) {
	// Track headers received by the agent
	var receivedHeaders http.Header

	// Create an agent that captures headers from TaskHandle
	agent := atlasic.NewAgent(
		&atlasic.AgentMetadata{
			Name:        "HTTP Headers Test Agent",
			Description: "Agent that captures HTTP headers for testing",
		},
		func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
			// Capture headers from TaskHandle
			receivedHeaders = handle.GetHTTPHeaders()

			// Return response
			return &a2a.Message{
				MessageID: "agent-response",
				Role:      a2a.RoleAgent,
				Parts:     []a2a.Part{a2a.NewTextPart("Headers received and captured")},
			}, nil
		},
	)

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Define test headers with multiple values
	testHeaders := http.Header{
		"User-Agent":    []string{"ATLASIC-Test/1.0"},
		"X-Request-ID":  []string{"req-12345"},
		"X-Trace-ID":    []string{"trace-67890"},
		"Accept":        []string{"application/json"},
		"Authorization": []string{"Bearer test-token"},
		"Custom-Header": []string{"value1", "value2"}, // Test multi-value header
	}

	// Create client with custom headers
	client := server.ClientWithHeaders(testHeaders)

	// Send message
	ctx := context.Background()
	params := a2a.MessageSendParams{
		Message: a2a.NewMessage("test-msg", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Testing HTTP headers"),
		}),
	}

	result, err := client.SendMessage(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Task)

	// Verify that the agent received HTTP headers
	require.NotNil(t, receivedHeaders, "Agent should receive HTTP headers")

	// Debug: log all received headers
	t.Logf("All received headers: %v", receivedHeaders)

	// Verify specific headers
	require.Equal(t, []string{"ATLASIC-Test/1.0"}, receivedHeaders["User-Agent"])
	require.Equal(t, []string{"req-12345"}, receivedHeaders["X-Request-Id"])
	require.Equal(t, []string{"trace-67890"}, receivedHeaders["X-Trace-Id"])
	require.Equal(t, []string{"application/json"}, receivedHeaders["Accept"])
	require.Equal(t, []string{"Bearer test-token"}, receivedHeaders["Authorization"])

	// Verify multi-value header
	require.Equal(t, []string{"value1", "value2"}, receivedHeaders["Custom-Header"])

	t.Logf("Agent successfully received %d HTTP headers", len(receivedHeaders))
}

func TestClientWithHeaders_EmptyHeaders(t *testing.T) {
	// Track headers received by the agent
	var receivedHeaders http.Header

	// Create an agent that captures headers from TaskHandle
	agent := atlasic.NewAgent(
		&atlasic.AgentMetadata{
			Name:        "Empty Headers Test Agent",
			Description: "Agent that tests empty headers scenario",
		},
		func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
			// Capture headers from TaskHandle
			receivedHeaders = handle.GetHTTPHeaders()

			// Return response
			return &a2a.Message{
				MessageID: "agent-response",
				Role:      a2a.RoleAgent,
				Parts:     []a2a.Part{a2a.NewTextPart("No custom headers test")},
			}, nil
		},
	)

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Create client without custom headers (only default HTTP headers)
	client := server.Client()

	// Send message
	ctx := context.Background()
	params := a2a.MessageSendParams{
		Message: a2a.NewMessage("test-msg", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Testing without custom headers"),
		}),
	}

	result, err := client.SendMessage(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Task)

	// Verify that the agent still receives some headers (default HTTP headers)
	require.NotNil(t, receivedHeaders, "Agent should receive HTTP headers even if empty custom headers")

	// Should have some default headers like Content-Type, User-Agent from HTTP client
	require.True(t, len(receivedHeaders) > 0, "Should have at least some default HTTP headers")

	t.Logf("Agent received %d default HTTP headers", len(receivedHeaders))
}

func TestClientWithHeaders_JobQueuePreservation(t *testing.T) {
	// Track headers received by the agent
	var receivedHeaders http.Header

	// Create an agent that captures headers from TaskHandle
	agent := atlasic.NewAgent(
		&atlasic.AgentMetadata{
			Name:        "JobQueue Headers Test Agent",
			Description: "Agent that tests headers preservation through JobQueue",
		},
		func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
			// Capture headers from TaskHandle
			receivedHeaders = handle.GetHTTPHeaders()

			// Return response
			return &a2a.Message{
				MessageID: "agent-response",
				Role:      a2a.RoleAgent,
				Parts:     []a2a.Part{a2a.NewTextPart("JobQueue headers preserved")},
			}, nil
		},
	)

	// Create test server
	server := NewServer(t, agent)
	defer server.Close()

	// Define test headers
	testHeaders := http.Header{
		"X-Session-Id":     []string{"session-abc123"},
		"X-Client-Version": []string{"2.1.0"},
		"Accept-Language":  []string{"en-US,en;q=0.9"},
	}

	// Create client with custom headers
	client := server.ClientWithHeaders(testHeaders)

	// Send message with blocking configuration to ensure JobQueue processing
	ctx := context.Background()
	params := a2a.MessageSendParams{
		Message: a2a.NewMessage("test-msg", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Testing JobQueue header preservation"),
		}),
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: true, // Still blocking for test simplicity, but goes through JobQueue
		},
	}

	result, err := client.SendMessage(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Task)

	// Verify that the agent received HTTP headers through JobQueue
	require.NotNil(t, receivedHeaders, "Agent should receive HTTP headers through JobQueue")

	// Verify specific headers are preserved through JobQueue processing
	require.Equal(t, []string{"session-abc123"}, receivedHeaders["X-Session-Id"])
	require.Equal(t, []string{"2.1.0"}, receivedHeaders["X-Client-Version"])
	require.Equal(t, []string{"en-US,en;q=0.9"}, receivedHeaders["Accept-Language"])

	t.Logf("JobQueue preserved %d HTTP headers correctly", len(receivedHeaders))
}
