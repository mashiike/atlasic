package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

func main() {
	// Setup logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Server URL (assumes server is running on localhost:8080)
	serverURL := "http://localhost:8080"
	if url := os.Getenv("A2A_SERVER_URL"); url != "" {
		serverURL = url
	}

	// Create A2A client
	client := transport.NewClient(serverURL,
		transport.WithClientLogger(slog.Default()),
		transport.WithUserAgent("ATLASIC-Example-Client/1.0"),
	)

	slog.Info("A2A Client starting", "serverURL", serverURL)

	// Step 1: Get Agent Card
	slog.Info("Step 1: Fetching agent card...")
	agentCard, err := client.GetAgentCard(ctx)
	if err != nil {
		slog.Error("Failed to get agent card", "error", err)
		os.Exit(1)
	}
	slog.Info("Agent card retrieved",
		"name", agentCard.Name,
		"version", agentCard.Version,
		"description", agentCard.Description,
		"skills", len(agentCard.Skills))

	// Step 2: Send a message to create a new task
	slog.Info("Step 2: Sending message to create new task...")
	contextID := fmt.Sprintf("example-context-%d", time.Now().Unix())
	
	sendParams := a2a.MessageSendParams{
		Message: a2a.Message{
			Kind:      a2a.KindMessage,
			MessageID: fmt.Sprintf("user-msg-%d", time.Now().UnixNano()),
			ContextID: contextID,
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				a2a.NewTextPart("Hello, Agent! Please echo this message back to me."),
			},
		},
	}

	sendResp, err := client.SendMessage(ctx, sendParams)
	if err != nil {
		slog.Error("Failed to send message", "error", err)
		os.Exit(1)
	}
	slog.Info("Message sent successfully",
		"taskID", sendResp.Task.ID,
		"contextID", sendResp.Task.ContextID,
		"taskState", sendResp.Task.Status.State)

	taskID := sendResp.Task.ID

	// Step 3: Wait for task completion and get result
	slog.Info("Step 3: Waiting for task completion...")
	
	maxRetries := 10
	retryDelay := 2 * time.Second
	
	for i := 0; i < maxRetries; i++ {
		time.Sleep(retryDelay)
		
		historyLengthAll := atlasic.HistoryLengthAll
		getParams := a2a.TaskQueryParams{
			ID:            taskID,
			HistoryLength: &historyLengthAll, // Get all message history
		}
		
		task, err := client.GetTask(ctx, getParams)
		if err != nil {
			slog.Error("Failed to get task", "error", err, "retry", i+1)
			continue
		}
		
		slog.Info("Task status",
			"state", task.Status.State,
			"historyCount", len(task.History))
		
		// Check if task is completed
		if task.Status.State.IsTerminal() {
			slog.Info("Task completed!", "finalState", task.Status.State)
			
			// Show conversation history
			slog.Info("=== Conversation History ===")
			for i, msg := range task.History {
				var textParts []string
				for _, part := range msg.Parts {
					if part.Kind == a2a.KindTextPart {
						textParts = append(textParts, part.Text)
					}
				}
				slog.Info("Message",
					"index", i,
					"role", msg.Role,
					"messageID", msg.MessageID,
					"text", fmt.Sprintf("%v", textParts))
			}
			
			if task.Status.State == a2a.TaskStateCompleted {
				slog.Info("✅ Client-Server communication test completed successfully!")
			} else {
				slog.Warn("⚠️ Task completed with non-success state", "state", task.Status.State)
			}
			return
		}
		
		slog.Info("Task still in progress, waiting...", "currentState", task.Status.State)
	}
	
	slog.Error("Task did not complete within timeout")
	os.Exit(1)
}