package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/awsadp"
	"github.com/mashiike/atlasic/model"
	_ "github.com/mashiike/atlasic/model/bedrock"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	model.RegisterTool("weather", func(ctx context.Context, toolUse *model.ToolUse) (*model.ToolResult, error) {
		// Implement weather tool logic here
		return &model.ToolResult{
			Parts: []a2a.Part{
				a2a.NewTextPart(fmt.Sprintf("Current weather in %s is sunny with a temperature of 25°C", toolUse.Arguments["location"])),
			},
		}, nil
	})
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("Failed to load AWS config", "error", err)
		return
	}
	queueURL := os.Getenv("ATLASIC_QUEUE_URL")
	if queueURL == "" {
		slog.Error("ATLASIC_QUEUE_URL environment variable is not set")
		return
	}

	jobQueue, err := awsadp.NewSQSJobQueue(awsadp.SQSJobQueueConfig{
		Client:   sqs.NewFromConfig(awsCfg),
		QueueURL: queueURL,
	})
	if err != nil {
		slog.Error("Failed to create job queue", "error", err)
		return
	}

	bucketName := os.Getenv("ATLASIC_STORAGE_BUCKET")
	if bucketName == "" {
		slog.Error("ATLASIC_STORAGE_BUCKET environment variable is not set")
		return
	}

	storage := awsadp.NewS3Storage(awsadp.S3StorageConfig{
		Client: s3.NewFromConfig(awsCfg),
		Bucket: bucketName,
		Prefix: "atlasic-example/tasks/",
	})

	agent, err := newAgent(ctx)
	if err != nil {
		slog.Error("Failed to create agent", "error", err)
		return
	}

	server := &atlasic.Server{
		Addr:     ":8080",
		Agent:    agent,
		JobQueue: jobQueue,
		Storage:  storage,
	}

	slog.Info("Starting server", "addr", server.Addr)
	if err := server.RunWithContext(ctx); err != nil {
		slog.Error("Server error", "error", err)
	}
}

const (
	modelProvider = "bedrock"
	modelID       = "apac.anthropic.claude-sonnet-4-20250514-v1:0"
)

func newAgent(ctx context.Context) (atlasic.Agent, error) {
	m, err := model.GetModel(ctx, modelProvider, modelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}
	metadata := &atlasic.AgentMetadata{
		Name:        "Converse Agent",
		Description: "An agent that converses with the user",
		Skills: []a2a.AgentSkill{
			{
				ID:          "greeting",
				Name:        "greet user",
				Description: "Greets the user with a friendly message",
				Examples: []string{
					"Hello, Agent!",
				},
			},
			{
				ID:          "weather",
				Name:        "get weather",
				Description: "Fetches the current weather for a given location",
				Examples: []string{
					"What's the weather like in Tokyo?",
				},
			},
		},
		DefaultInputModes:  m.InputModes(),
		DefaultOutputModes: m.OutputModes(),
	}
	slog.InfoContext(ctx, "Creating agent", "modelID", m.ID())
	agentFunc := func(ctx context.Context, h atlasic.TaskHandle) (*a2a.Message, error) {
		task, err := h.GetTask(ctx, atlasic.HistoryLengthAll)
		if err != nil {
			return nil, fmt.Errorf("failed to get task: %w", err)
		}
		runner := model.NewRunner(m, &model.GenerateRequest{
			System: strings.Join([]string{
				"Your converse agent, Please reply user's message.",
				"For example, if the user says 'Hello Agent!', you should respond with 'Hello User!'.",
				"If the user asks about the weather, you should provide the current weather information.",
			}, "\n"),
			Messages: task.History,
			Tools: []model.Tool{
				{
					Name:        "weather",
					Description: "Get the current weather",
					Schema:      json.RawMessage(`{"type": "object", "properties": {"location": {"type": "string", "description": "The location to get the weather for"}}}`),
				},
			},
			Extensions: map[string]any{
				"thinking": map[string]any{
					"type":          "enabled",
					"budget_tokens": 10000,
				},
			},
		})
		for step, err := range runner.Steps(ctx) {
			if err != nil {
				return nil, fmt.Errorf("failed to run step %d: %w", step.Index, err)
			}
			slog.InfoContext(ctx, "Running step", "stepIndex", step.Index, "stopReason", step.Response.StopReason, "taskID", h.GetTaskID(), "contextID", h.GetContextID(), "modelID", m.ID())
		}
		msgID, err := h.AddMessage(ctx, runner.Response().Message.Parts)
		if err != nil {
			return nil, fmt.Errorf("failed to add message: %w", err)
		}
		slog.Info("Agent response added", "messageID", msgID, "taskID", h.GetTaskID(), "contextID", h.GetContextID())
		return &a2a.Message{
			Parts: []a2a.Part{
				a2a.NewTextPart("Successfully processed the task"),
			},
		}, nil
	}
	agent := atlasic.NewAgent(metadata, agentFunc)
	return agent, nil
}
