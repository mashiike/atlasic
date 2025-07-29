package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	_ "github.com/mashiike/atlasic/model/bedrock"
	_ "github.com/mashiike/atlasic/model/ollama"
)

func main() {
	var (
		useBedrock = false // Set to true to use Bedrock model
	)
	flag.BoolVar(&useBedrock, "bedrock", false, "Use Bedrock model instead of Ollama")
	flag.Parse()
	modelProvider := "ollama"
	modelID := "llama3.2"
	opts := make([]func(*model.GenerateRequest), 0)
	if useBedrock {
		modelProvider = "bedrock"
		modelID = "us.anthropic.claude-3-sonnet-20240229-v1:0"
		opts = append(opts, func(req *model.GenerateRequest) {
			req.Extensions = map[string]any{
				"thinking": map[string]any{
					"type":          "enabled",
					"budget_tokens": 10000,
				},
			}
		})
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	m, err := model.GetModel(ctx, modelProvider, modelID)
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

	model.RegisterTool("weather", func(ctx context.Context, toolUse *model.ToolUse) (*model.ToolResult, error) {
		// Implement weather tool logic here
		return &model.ToolResult{
			Parts: []a2a.Part{
				a2a.NewTextPart(fmt.Sprintf("Current weather in %s is sunny with a temperature of 25°C", toolUse.Arguments["location"])),
			},
		}, nil
	})
	agentFunc := func(ctx context.Context, h atlasic.TaskHandle) (*a2a.Message, error) {
		task, err := h.GetTask(ctx, atlasic.HistoryLengthAll)
		if err != nil {
			return nil, fmt.Errorf("failed to get task: %w", err)
		}
		runner := model.NewRunner(m, &model.GenerateRequest{
			System: strings.Join([]string{
				"Your echo agent, Please reply user's message.",
				"For example, if the user says 'Hello Agent!', you should respond with 'Hello User!'.",
			}, "\n"),
			Messages: task.History,
			Tools: []model.Tool{
				{
					Name:        "weather",
					Description: "Get the current weather",
					Schema:      json.RawMessage(`{"type": "object", "properties": {"location": {"type": "string", "description": "The location to get the weather for"}}}`),
				},
			},
		})
		for step, err := range runner.Steps(ctx) {
			if err != nil {
				return nil, fmt.Errorf("failed to run step %d: %w", step.Index, err)
			}
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

	server := &atlasic.Server{
		Addr:  ":8080",
		Agent: agent,
	}

	// Add custom endpoints using Handle/HandleFunc
	server.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"atlasic-agent"}`))
	})

	server.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version":"1.0.0","agent":"Local Agent"}`))
	})

	// Example: Custom handler using AgentService via Context
	server.HandleFunc("/custom/tasks", func(w http.ResponseWriter, r *http.Request) {
		// Get AgentService from context (automatically injected by middleware)
		agentSvc := atlasic.GetAgentServiceFromContext(r.Context())
		if agentSvc == nil {
			http.Error(w, "AgentService not available", http.StatusInternalServerError)
			return
		}

		switch r.Method {
		case http.MethodPost:
			// Create a new task
			params := a2a.MessageSendParams{
				Message: a2a.Message{
					Parts: []a2a.Part{
						a2a.NewTextPart("Hello from custom handler!"),
					},
				},
			}

			result, err := agentSvc.SendMessage(r.Context(), params)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to send message: %v", err), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"task":    result.Task,
			})

		case http.MethodGet:
			// List available output modes
			outputModes, err := agentSvc.SupportedOutputModes(r.Context())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to get output modes: %v", err), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"supportedOutputModes": outputModes,
			})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Example: Get specific task by ID
	server.HandleFunc("/custom/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract task ID from URL path
		taskID := strings.TrimPrefix(r.URL.Path, "/custom/tasks/")
		if taskID == "" {
			http.Error(w, "Task ID required", http.StatusBadRequest)
			return
		}

		// Get AgentService from context
		agentSvc := atlasic.GetAgentServiceFromContext(r.Context())
		if agentSvc == nil {
			http.Error(w, "AgentService not available", http.StatusInternalServerError)
			return
		}

		// Get task
		task, err := agentSvc.GetTask(r.Context(), a2a.TaskQueryParams{
			ID: taskID,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get task: %v", err), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	})

	// Add HTTP middlewares using Use
	server.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			duration := time.Since(start)
			slog.Info("HTTP Request", "method", r.Method, "path", r.URL.Path, "duration", duration)
		})
	})

	server.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Server", "atlasic-agent")
			next.ServeHTTP(w, r)
		})
	})

	slog.Info("Starting server with custom endpoints", "addr", server.Addr,
		"endpoints", []string{"/health", "/version", "/custom/tasks", "/custom/tasks/{id}", "/", "/.well-known/agent.json"})
	if err := server.RunWithContext(ctx); err != nil {
		slog.Error("Server error", "error", err)
	}
}
