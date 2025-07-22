package atlasic

import (
	"context"
	"fmt"
	"sync"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

// RemoteAgent implements Agent interface using transport.Client to communicate with remote agents
type RemoteAgent struct {
	client *transport.Client

	// Cached metadata from GetAgentCard
	cachedMetadata *AgentMetadata
	metadataMu     sync.RWMutex
}

// NewRemoteAgent creates a new RemoteAgent that communicates with a remote agent via HTTP
func NewRemoteAgent(baseURL string, opts ...transport.ClientOption) *RemoteAgent {
	client := transport.NewClient(baseURL, opts...)
	return &RemoteAgent{
		client: client,
	}
}

// GetMetadata retrieves and caches agent metadata from the remote agent
func (r *RemoteAgent) GetMetadata(ctx context.Context) (*AgentMetadata, error) {
	// Check if we have cached metadata
	r.metadataMu.RLock()
	if r.cachedMetadata != nil {
		cached := r.cachedMetadata
		r.metadataMu.RUnlock()
		return cached, nil
	}
	r.metadataMu.RUnlock()

	// Fetch agent card from remote agent
	agentCard, err := r.client.GetAgentCard(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent card from remote agent: %w", err)
	}

	// Convert AgentCard to AgentMetadata
	metadata := agentCardToMetadata(agentCard)

	// Cache the metadata
	r.metadataMu.Lock()
	r.cachedMetadata = metadata
	r.metadataMu.Unlock()

	return metadata, nil
}

// Execute executes a task on the remote agent
func (r *RemoteAgent) Execute(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
	// Get task from handle
	task, err := handle.GetTask(ctx, 10) // Get up to 10 history entries
	if err != nil {
		return nil, fmt.Errorf("failed to get task from handle: %w", err)
	}

	// Convert task to MessageSendParams
	params := taskToMessageParams(task)

	// Send message to remote agent
	result, err := r.client.SendMessage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to send message to remote agent: %w", err)
	}

	// Convert SendMessageResult to Message
	message := sendResultToMessage(result)
	return message, nil
}

// agentCardToMetadata converts a2a.AgentCard to AgentMetadata
func agentCardToMetadata(card *a2a.AgentCard) *AgentMetadata {
	if card == nil {
		return &AgentMetadata{
			Name:               "Unknown Remote Agent",
			Version:            "1.0.0",
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
		}
	}

	metadata := &AgentMetadata{
		Name:               card.Name,
		Description:        card.Description,
		Version:            card.Version,
		Provider:           card.Provider,
		Skills:             card.Skills,
		DefaultInputModes:  card.DefaultInputModes,
		DefaultOutputModes: card.DefaultOutputModes,
	}

	return metadata
}

// taskToMessageParams converts Task to a2a.MessageSendParams
func taskToMessageParams(task *a2a.Task) a2a.MessageSendParams {
	// Use the most recent message from task history
	var message a2a.Message
	if len(task.History) > 0 {
		message = task.History[0] // Most recent message
	} else {
		// Fallback: create a basic message
		message = a2a.NewMessage("", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Task execution request"),
		})
	}

	return a2a.MessageSendParams{
		Message: message,
		Configuration: &a2a.MessageSendConfiguration{
			Blocking: true, // Wait for completion
		},
	}
}

// sendResultToMessage converts a2a.SendMessageResult to a2a.Message
func sendResultToMessage(result *a2a.SendMessageResult) *a2a.Message {
	if result == nil {
		return &a2a.Message{
			MessageID: "remote-error",
			Role:      a2a.RoleAgent,
			Parts:     []a2a.Part{a2a.NewTextPart("No response from remote agent")},
		}
	}

	// If result has a Message, return it directly
	if result.Message != nil {
		return result.Message
	}

	// If result has a Task with completed status, extract the agent's response
	if result.Task != nil && result.Task.Status.State == a2a.TaskStateCompleted {
		if result.Task.Status.Message != nil {
			return result.Task.Status.Message
		}

		// Find the most recent agent message in history
		for i := len(result.Task.History) - 1; i >= 0; i-- {
			if result.Task.History[i].Role == a2a.RoleAgent {
				return &result.Task.History[i]
			}
		}
	}

	// Fallback: create a basic response message
	return &a2a.Message{
		MessageID: "remote-fallback",
		Role:      a2a.RoleAgent,
		Parts:     []a2a.Part{a2a.NewTextPart("Task completed on remote agent")},
	}
}
