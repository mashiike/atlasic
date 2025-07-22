package atlasic_test

import (
	"context"
	"testing"
	"time"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/atlasictest"
	"github.com/stretchr/testify/require"
)

func TestRemoteAgent_GetMetadata(t *testing.T) {
	// Create a test agent with known metadata
	testAgent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:        "Remote Test Agent",
		Description: "An agent for testing remote communication",
		Version:     "2.1.0",
		Skills: []a2a.AgentSkill{
			{
				ID:          "remote-skill",
				Name:        "remote processing",
				Description: "Processes requests remotely",
			},
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain"},
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Remote processing completed"),
			},
		}, nil
	})

	// Start test server
	server := atlasictest.NewServer(t, testAgent)
	defer server.Close()

	// Create RemoteAgent pointing to test server
	remoteAgent := atlasic.NewRemoteAgent(server.URL())

	// Test GetMetadata
	ctx := context.Background()
	metadata, err := remoteAgent.GetMetadata(ctx)
	require.NoError(t, err)
	require.NotNil(t, metadata)

	// Verify metadata matches
	require.Equal(t, "Remote Test Agent", metadata.Name)
	require.Equal(t, "An agent for testing remote communication", metadata.Description)
	require.Equal(t, "2.1.0", metadata.Version)
	require.Len(t, metadata.Skills, 1)
	require.Equal(t, "remote-skill", metadata.Skills[0].ID)
	require.Equal(t, "remote processing", metadata.Skills[0].Name)
	require.Equal(t, []string{"text/plain", "application/json"}, metadata.DefaultInputModes)
	require.Equal(t, []string{"text/plain"}, metadata.DefaultOutputModes)
}

func TestRemoteAgent_GetMetadata_Caching(t *testing.T) {
	// Create a simple test agent
	testAgent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name:    "Caching Test Agent",
		Version: "1.0.0",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Response"),
			},
		}, nil
	})

	// Start test server
	server := atlasictest.NewServer(t, testAgent)
	defer server.Close()

	// Create RemoteAgent
	remoteAgent := atlasic.NewRemoteAgent(server.URL())
	ctx := context.Background()

	// First call should fetch from server
	metadata1, err := remoteAgent.GetMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, "Caching Test Agent", metadata1.Name)

	// Second call should return cached version (test by ensuring same pointer)
	metadata2, err := remoteAgent.GetMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, metadata1, metadata2) // Should be identical
}

func TestRemoteAgent_Execute(t *testing.T) {
	// Create a test agent that echoes the received message
	testAgent := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name: "Echo Remote Agent",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		// Get the task to see what was sent
		task, err := handle.GetTask(ctx, 10)
		if err != nil {
			return nil, err
		}

		// Echo back the received message with prefix
		var receivedText string
		if len(task.History) > 0 && len(task.History[0].Parts) > 0 {
			receivedText = task.History[0].Parts[0].Text
		} else {
			receivedText = "no message"
		}

		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Echo: " + receivedText),
			},
		}, nil
	})

	// Start test server
	server := atlasictest.NewServer(t, testAgent)
	defer server.Close()

	// Create RemoteAgent
	remoteAgent := atlasic.NewRemoteAgent(server.URL())

	// Create a simple task using TaskCapture - no local server needed!
	task := &a2a.Task{
		ID:        "execute-test-task",
		ContextID: "execute-test-context",
		History: []a2a.Message{
			{
				Role: a2a.RoleUser,
				Parts: []a2a.Part{
					a2a.NewTextPart("Hello remote agent!"),
				},
			},
		},
	}
	taskHandle := atlasictest.NewCapture(task)
	defer taskHandle.Cleanup()

	// Execute remote agent with the task handle
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := remoteAgent.Execute(ctx, taskHandle)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, a2a.RoleAgent, response.Role)
	require.Len(t, response.Parts, 1)
	require.Equal(t, "Echo: Hello remote agent!", response.Parts[0].Text)
}

func TestRemoteAgent_Execute_ErrorHandling(t *testing.T) {
	// Create RemoteAgent pointing to invalid URL
	remoteAgent := atlasic.NewRemoteAgent("http://invalid-url-that-does-not-exist:12345")

	// Create a simple task using TaskCapture - no server needed!
	task := &a2a.Task{
		ID:        "error-test-task",
		ContextID: "error-test-context",
		History: []a2a.Message{
			{
				Role: a2a.RoleUser,
				Parts: []a2a.Part{
					a2a.NewTextPart("Test message"),
				},
			},
		},
	}
	taskHandle := atlasictest.NewCapture(task)
	defer taskHandle.Cleanup()

	// Execute should fail due to invalid URL
	ctx := context.Background()
	_, err := remoteAgent.Execute(ctx, taskHandle)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to send message to remote agent")
}

func TestRemoteAgent_IntegrationWithRealCommunication(t *testing.T) {
	// Create two different agents
	agent1 := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name: "Agent One",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Response from Agent One"),
			},
		}, nil
	})

	agent2 := atlasic.NewAgent(&atlasic.AgentMetadata{
		Name: "Agent Two",
	}, func(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
		return &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{
				a2a.NewTextPart("Response from Agent Two"),
			},
		}, nil
	})

	// Start both as test servers
	server1 := atlasictest.NewServer(t, agent1)
	defer server1.Close()

	server2 := atlasictest.NewServer(t, agent2)
	defer server2.Close()

	// Create RemoteAgents pointing to each server
	remoteAgent1 := atlasic.NewRemoteAgent(server1.URL())
	remoteAgent2 := atlasic.NewRemoteAgent(server2.URL())

	// Test metadata retrieval from both
	ctx := context.Background()

	metadata1, err := remoteAgent1.GetMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, "Agent One", metadata1.Name)

	metadata2, err := remoteAgent2.GetMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, "Agent Two", metadata2.Name)

	// Test that they are different agents
	require.NotEqual(t, metadata1.Name, metadata2.Name)
}
