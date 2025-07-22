package atlasictest

import (
	"context"
	"io"
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
