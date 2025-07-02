package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	_ "github.com/mashiike/atlasic/model/ollama"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	m, err := model.GetModel(ctx, "ollama", "llama3.2")
	if err != nil {
		panic(fmt.Errorf("failed to get model: %w", err))
	}

	metadata := &atlasic.AgentMetadata{
		Name:        "Local Agent",
		Description: "An agent that runs tasks locally",
		Skills: []a2a.AgentSkill{
			{
				ID:          "echo_message",
				Name:        "echo message",
				Description: "Echoes the input text",
				Examples: []string{
					"Hello, Agent!",
				},
			},
		},
		DefaultInputModes:  m.InputModes(),
		DefaultOutputModes: m.OutputModes(),
	}

	agentFunc := func(ctx context.Context, h atlasic.TaskHandle) error {
		task, err := h.GetTask(ctx, atlasic.HistoryLengthAll)
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		resp, err := m.Generate(ctx, &model.GenerateRequest{
			System: strings.Join([]string{
				"Your echo agent, Please reply user's message.",
				"For example, if the user says 'Hello Agent!', you should respond with 'Hello User!'.",
			}, "\n"),
			Messages: task.History,
		})
		if err != nil {
			return fmt.Errorf("failed to generate response: %w", err)
		}
		if resp.StopReason != model.StopReasonEndTurn {
			return fmt.Errorf("unexpected stop reason: %s", resp.StopReason)
		}
		msgID, err := h.AddMessage(ctx, resp.Message.Parts)
		if err != nil {
			return fmt.Errorf("failed to add message: %w", err)
		}
		slog.Info("Agent response added", "messageID", msgID, "taskID", h.GetTaskID(), "contextID", h.GetContextID())
		return nil
	}

	agent := atlasic.NewAgent(metadata, agentFunc)

	// New simplified Server API
	server := &atlasic.Server{
		Addr:  ":8080",
		Agent: agent,
	}

	slog.Info("Starting server", "addr", server.Addr)
	if err := server.RunWithContext(ctx); err != nil {
		slog.Error("Server error", "error", err)
	}
}
