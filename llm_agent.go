package atlasic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"github.com/mashiike/atlasic/prompt"
)

// ReActLog represents an entry in the ReAct execution log
type ReActLog struct {
	Iteration int       `json:"iteration"`
	Type      string    `json:"type"` // "llm_request", "llm_response", "tool_execution", "error"
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// LLMAgent implements Agent interface with ReAct loop capability
type LLMAgent struct {
	Name            string
	Description     string           // Agent description for metadata
	ModelProvider   string           `json:"model_provider"`
	ModelID         string           `json:"model_id"`
	Instructions    string           `json:"instructions"`
	Skills          []a2a.AgentSkill `json:"skills"`  // Agent skills for metadata
	Version         string           `json:"version"` // Agent version for metadata
	Tools           []ExecutableTool `json:"-"`       // Additional tools beyond defaults
	MaxIterations   int              `json:"max_iterations"`
	TaskLogFilePath *string          `json:"task_log_file_path"` // ReAct internal logs output path (JSONL format), defaults to "llm_agent_log.jsonl"
	Logger          *slog.Logger     `json:"-"`
	SubAgents       []Agent          `json:"-"` // Available sub-agents for delegation

	// Customizable prompt builders
	SystemPromptBuilder func(ctx context.Context, handle TaskHandle, agent *LLMAgent, summary string) (string, error)
	MessagesBuilder     func(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error)

	// RequestOptions allows customization of model.GenerateRequest
	RequestOptions []func(*model.GenerateRequest) `json:"-"`

	// Summarizer for managing ReAct execution logs
	Summarizer Summarizer `json:"-"`

	// Summarizer trigger settings
	SummarizerTriggerIterations int `json:"summarizer_trigger_iterations"` // Trigger summarization after N iterations (default: 5)
	SummarizerMaxTokens         int `json:"summarizer_max_tokens"`         // Trigger summarization when input tokens exceed threshold (default: 8000)
	SummarizerMaxHistory        int `json:"summarizer_max_history"`        // Keep last N messages after summarization (default: 2)
}

// GetMetadata implements Agent interface
func (a *LLMAgent) GetMetadata(ctx context.Context) (*AgentMetadata, error) {
	// Get model to extract supported modes
	m, err := model.GetModel(ctx, a.ModelProvider, a.ModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}

	// Use Skills if provided, otherwise aggregate SubAgent skills
	var skills []a2a.AgentSkill
	if len(a.Skills) > 0 {
		skills = a.Skills
	} else {
		// Aggregate skills from SubAgents
		for _, subAgent := range a.SubAgents {
			meta, err := subAgent.GetMetadata(ctx)
			if err == nil {
				skills = append(skills, meta.Skills...)
			}
		}
	}

	// Use agent's version if specified, otherwise default to "1.0.0"
	version := a.Version
	if version == "" {
		version = "1.0.0"
	}
	name := a.Name
	if name == "" {
		name = fmt.Sprintf("LLM Agent (%s/%s)", a.ModelProvider, a.ModelID)
	}
	if a.Description == "" {
		a.Description = fmt.Sprintf("LLM-powered agent with ReAct capabilities. %s", a.Instructions)
	}

	return &AgentMetadata{
		Name:               name,
		Description:        a.Description,
		Skills:             skills,
		DefaultInputModes:  m.InputModes(),
		DefaultOutputModes: m.OutputModes(),
		Version:            version,
	}, nil
}

// fillDefaults initializes default values for the LLMAgent
func (a *LLMAgent) fillDefaults() {
	if a.Logger == nil {
		a.Logger = slog.Default()
	}
	if a.MaxIterations <= 0 {
		a.MaxIterations = 50
	}
	if a.SystemPromptBuilder == nil {
		a.SystemPromptBuilder = defaultSystemPromptBuilder
	}
	if a.MessagesBuilder == nil {
		a.MessagesBuilder = defaultMessagesBuilder
	}
	if a.SummarizerTriggerIterations <= 0 {
		a.SummarizerTriggerIterations = 5
	}
	if a.SummarizerMaxTokens <= 0 {
		a.SummarizerMaxTokens = 8000
	}
	if a.SummarizerMaxHistory <= 0 {
		a.SummarizerMaxHistory = 2
	}
}

// buildAllTools builds the complete tool set with deduplication
// Priority order (first wins in case of name conflicts):
// 1. Builtin tools (highest priority)
// 2. Context dynamic tools
// 3. Agent static tools (LLMAgent specific)
// 4. Additional tools (lowest priority)
func (a *LLMAgent) buildAllTools(ctx context.Context, handle TaskHandle, additionalTools []ExecutableTool) []ExecutableTool {
	var allTools []ExecutableTool

	// Get all sub-agents (static + dynamic from context)
	allSubAgents := a.GetAllSubAgents(ctx)

	// Add default builtin tools (highest priority)
	builtinTools := []ExecutableTool{
		NewUpdateArtifactTool(handle),
		NewStopTool(handle),
		NewDelegateToAgentTool(handle, allSubAgents...),
		// Only provide read-only file tools to prevent unwanted file manipulation
		NewTaskFileReadTool(handle),
		NewTaskFileListTool(handle),
		NewContextFileReadTool(handle),
		NewContextFileListTool(handle),
	}
	allTools = append(allTools, builtinTools...)

	// Add dynamic tools from context (with deduplication)
	dynamicTools := GetToolsFromContext(ctx)
	for _, dynamicTool := range dynamicTools {
		duplicate := false
		for _, existing := range allTools {
			if existing.Name() == dynamicTool.Name() {
				duplicate = true
				break
			}
		}
		if !duplicate {
			allTools = append(allTools, dynamicTool)
		}
	}

	// Add agent's static tools (LLMAgent specific, with deduplication)
	for _, agentTool := range a.Tools {
		duplicate := false
		for _, existing := range allTools {
			if existing.Name() == agentTool.Name() {
				duplicate = true
				break
			}
		}
		if !duplicate {
			allTools = append(allTools, agentTool)
		}
	}

	// Add additional tools (with deduplication)
	for _, addTool := range additionalTools {
		duplicate := false
		for _, existing := range allTools {
			if existing.Name() == addTool.Name() {
				duplicate = true
				break
			}
		}
		if !duplicate {
			allTools = append(allTools, addTool)
		}
	}

	return allTools
}

// Execute implements Agent interface with ReAct loop
func (a *LLMAgent) Execute(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
	// Fill in defaults before execution
	a.fillDefaults()

	// Get model for generation
	m, err := model.GetModel(ctx, a.ModelProvider, a.ModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}

	a.Logger.Info("Starting ReAct loop", "taskID", handle.GetTaskID(), "contextID", handle.GetContextID())

	// Initialize log state
	var logEncoder *json.Encoder
	var inMemoryLogs []ReActLog

	// Setup log file for sequential JSONL writing
	logPath := "llm_agent_log.jsonl"
	if a.TaskLogFilePath != nil && *a.TaskLogFilePath != "" {
		logPath = *a.TaskLogFilePath
	}

	logFile, err := handle.OpenTaskFile(ctx, logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		a.Logger.Warn("Failed to open log file, logging disabled", "error", err, "path", logPath)
	} else {
		defer logFile.Close()
		if writer, ok := logFile.(io.Writer); ok {
			logEncoder = json.NewEncoder(writer)
		} else {
			a.Logger.Warn("Log file does not implement io.Writer, logging disabled", "path", logPath)
		}
	}

	// Initialize summarization state
	var currentSummary string

	// Create initial request
	req, err := a.newGenerateRequestWithSummary(ctx, handle, currentSummary)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial request: %w", err)
	}

	var finalResponse *a2a.Message
	var exitReason string

	// ReAct loop
	for iteration := 0; iteration < a.MaxIterations; iteration++ {
		a.Logger.Debug("ReAct iteration starting", "iteration", iteration+1, "max", a.MaxIterations)

		// Check and perform summarization if needed
		newSummary, newReq, newLogs, err := a.checkAndPerformSummarization(ctx, handle, iteration, req, currentSummary, inMemoryLogs)
		if err != nil {
			return nil, err
		}
		if newSummary != currentSummary {
			currentSummary = newSummary
		}
		if newReq != nil {
			req = newReq
		}
		if newLogs != nil {
			inMemoryLogs = newLogs
		}

		// Log LLM request
		log := ReActLog{
			Iteration: iteration + 1,
			Type:      "llm_request",
			Timestamp: time.Now(),
			Data: map[string]any{
				"system":   req.System,
				"messages": len(req.Messages),
				"tools":    len(req.Tools),
			},
		}
		if logEncoder != nil {
			if err := logEncoder.Encode(log); err != nil {
				a.Logger.Warn("Failed to write log entry", "error", err)
			}
		}
		inMemoryLogs = append(inMemoryLogs, log)

		// Step 1: Reasoning - Generate response with tool usage
		response, err := m.Generate(ctx, req)
		if err != nil {
			log := ReActLog{
				Iteration: iteration + 1,
				Type:      "error",
				Timestamp: time.Now(),
				Data:      map[string]any{"error": err.Error(), "stage": "llm_generation"},
			}
			if logEncoder != nil {
				if err := logEncoder.Encode(log); err != nil {
					a.Logger.Warn("Failed to write log entry", "error", err)
				}
			}
			inMemoryLogs = append(inMemoryLogs, log)
			return nil, fmt.Errorf("generation failed at iteration %d: %w", iteration+1, err)
		}

		// Log LLM response
		log = ReActLog{
			Iteration: iteration + 1,
			Type:      "llm_response",
			Timestamp: time.Now(),
			Data: map[string]any{
				"message":     response.Message,
				"stop_reason": response.StopReason,
				"usage":       response.Usage,
			},
		}
		if logEncoder != nil {
			if err := logEncoder.Encode(log); err != nil {
				a.Logger.Warn("Failed to write log entry", "error", err)
			}
		}
		inMemoryLogs = append(inMemoryLogs, log)

		// Add agent response to request for next iteration
		req.Messages = append(req.Messages, response.Message)

		// Step 2: Action - Extract and execute tools
		_, finalMsg, newExitReason, shouldReturn, err := a.handleToolExecution(ctx, handle, response.Message, iteration+1, req, logEncoder, &inMemoryLogs)
		if shouldReturn {
			if err != nil {
				return nil, err
			}
			finalResponse = finalMsg
			exitReason = newExitReason
			break
		}

		// Step 3: Observation - Task state is updated through TaskHandle
	}

	// Handle different exit conditions
	switch exitReason {
	case "tool_stop":
		// Normal completion via stop tool
		a.Logger.Info("ReAct loop completed successfully", "reason", exitReason)
		return finalResponse, nil
	case "":
		// Max iterations reached without stop tool
		if finalResponse == nil {
			exitReason = "max_iterations_no_stop"
		} else {
			exitReason = "max_iterations_with_response"
		}
		fallthrough
	default:
		// Log specific exit reason
		a.Logger.Warn("ReAct loop ended without proper completion",
			"reason", exitReason,
			"maxIterations", a.MaxIterations,
			"hasFinalResponse", finalResponse != nil)

		// Update task status based on exit reason
		var taskState a2a.TaskState
		var statusMessage string

		switch exitReason {
		case "max_iterations_no_stop":
			taskState = a2a.TaskStateFailed
			statusMessage = fmt.Sprintf("Agent reached maximum iterations (%d) without using stop tool", a.MaxIterations)
		case "max_iterations_with_response":
			taskState = a2a.TaskStateInputRequired
			statusMessage = fmt.Sprintf("Agent reached maximum iterations (%d) but has pending response", a.MaxIterations)
		default:
			taskState = a2a.TaskStateFailed
			statusMessage = fmt.Sprintf("Agent exited unexpectedly: %s", exitReason)
		}

		_, err = handle.UpdateStatus(ctx, taskState, []a2a.Part{
			a2a.NewTextPart(statusMessage),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update status after %s: %w", exitReason, err)
		}

		// Return response if available, otherwise nil
		return finalResponse, nil
	}
}

// newGenerateRequestWithSummary creates generate request with system prompt (including summary), messages and tools
func (a *LLMAgent) newGenerateRequestWithSummary(ctx context.Context, handle TaskHandle, summary string) (*model.GenerateRequest, error) {
	// Build system prompt with summary
	systemPrompt, err := a.SystemPromptBuilder(ctx, handle, a, summary)
	if err != nil {
		return nil, fmt.Errorf("failed to build system prompt: %w", err)
	}

	// Build messages
	messages, additionalTools, err := a.MessagesBuilder(ctx, handle, a)
	if err != nil {
		return nil, fmt.Errorf("failed to build messages: %w", err)
	}

	// Build all available tools with deduplication
	allTools := a.buildAllTools(ctx, handle, additionalTools)

	// Convert tools to model.Tools
	var modelTools []model.Tool
	for _, tool := range allTools {
		modelTools = append(modelTools, toModelTool(tool))
	}

	// Create request
	req := &model.GenerateRequest{}

	// Apply custom request options first
	for _, opt := range a.RequestOptions {
		opt(req)
	}

	// Apply defaults (won't override if already set)
	if req.Options == nil {
		req.Options = &model.GenerationOptions{}
	}
	if req.Options.Temperature == nil {
		req.Options.Temperature = model.Ptr(float32(0.7))
	}
	if req.ToolChoice == nil {
		req.ToolChoice = &model.ToolChoice{
			Type: model.ToolChoiceRequired,
		}
	}

	// Protect core fields - always use LLMAgent's values
	req.System = systemPrompt
	req.Messages = messages
	req.Tools = modelTools

	return req, nil
}

// executeToolsAndCollectLogs executes tools, collects logs, and handles stop message
func (a *LLMAgent) executeToolsAndCollectLogs(ctx context.Context, handle TaskHandle, toolUses []*model.ToolUse, iteration int, logEncoder *json.Encoder, inMemoryLogs *[]ReActLog) (bool, a2a.Message, *a2a.Message, error) {
	// Build available tools
	allTools := a.buildAllTools(ctx, handle, nil)

	shouldStop := false
	var results []a2a.Part
	var stopMessage *a2a.Message

	// Execute each tool
	for _, toolUse := range toolUses {
		a.Logger.Debug("Executing tool", "toolName", toolUse.ToolName, "arguments", toolUse.Arguments)

		// Find tool by name
		var tool ExecutableTool
		var found bool
		for _, t := range allTools {
			if t.Name() == toolUse.ToolName {
				tool = t
				found = true
				break
			}
		}

		if !found {
			log := ReActLog{
				Iteration: iteration,
				Type:      "error",
				Timestamp: time.Now(),
				Data:      map[string]any{"error": fmt.Sprintf("unknown tool: %s", toolUse.ToolName), "tool_name": toolUse.ToolName},
			}
			if logEncoder != nil {
				if err := logEncoder.Encode(log); err != nil {
					a.Logger.Warn("Failed to write log entry", "error", err)
				}
			}
			*inMemoryLogs = append(*inMemoryLogs, log)
			return false, a2a.Message{}, nil, fmt.Errorf("unknown tool: %s", toolUse.ToolName)
		}

		// Log tool execution start
		log := ReActLog{
			Iteration: iteration,
			Type:      "tool_execution",
			Timestamp: time.Now(),
			Data: map[string]any{
				"tool_name": toolUse.ToolName,
				"arguments": toolUse.Arguments,
				"stage":     "start",
			},
		}
		if logEncoder != nil {
			if err := logEncoder.Encode(log); err != nil {
				a.Logger.Warn("Failed to write log entry", "error", err)
			}
		}
		*inMemoryLogs = append(*inMemoryLogs, log)

		result, err := tool.Execute(ctx, toolUse.Arguments)
		if err != nil {
			log := ReActLog{
				Iteration: iteration,
				Type:      "error",
				Timestamp: time.Now(),
				Data: map[string]any{
					"error":     err.Error(),
					"tool_name": toolUse.ToolName,
					"stage":     "execution",
				},
			}
			if logEncoder != nil {
				if err := logEncoder.Encode(log); err != nil {
					a.Logger.Warn("Failed to write log entry", "error", err)
				}
			}
			*inMemoryLogs = append(*inMemoryLogs, log)
			return false, a2a.Message{}, nil, fmt.Errorf("tool %s execution failed: %w", toolUse.ToolName, err)
		}

		// Log tool execution result
		log = ReActLog{
			Iteration: iteration,
			Type:      "tool_execution",
			Timestamp: time.Now(),
			Data: map[string]any{
				"tool_name": toolUse.ToolName,
				"result":    result,
				"stage":     "completed",
			},
		}
		if logEncoder != nil {
			if err := logEncoder.Encode(log); err != nil {
				a.Logger.Warn("Failed to write log entry", "error", err)
			}
		}
		*inMemoryLogs = append(*inMemoryLogs, log)

		// Add tool result to results
		results = append(results, model.NewToolResultPart(toolUse.ID, toolUse.ToolName, []a2a.Part{result}))

		// Check metadata for stop signal
		if stopFlag, exists := result.Metadata["should_stop"].(bool); exists && stopFlag {
			shouldStop = true
			// Extract final message from stop tool for user response
			if toolUse.ToolName == "stop" {
				if message, ok := toolUse.Arguments["message"].(string); ok && message != "" {
					stopMessage = &a2a.Message{
						Role:  a2a.RoleAgent,
						Parts: []a2a.Part{a2a.NewTextPart(message)},
					}
				}
			}
		}
	}

	// Do NOT save tool results to task history - keep task clean for user
	// Only log tool execution internally

	// Create user message with tool results for next LLM iteration
	userMessage := a2a.NewMessage("tool-results", a2a.RoleUser, results)
	return shouldStop, userMessage, stopMessage, nil
}

// defaultSystemPromptBuilder creates the default system prompt using English markdown format
func defaultSystemPromptBuilder(ctx context.Context, handle TaskHandle, agent *LLMAgent, summary string) (string, error) {
	var sb strings.Builder

	// Header
	sb.WriteString("# LLM Agent Instructions\n\n")
	sb.WriteString("## Your Role\n")
	sb.WriteString("You are an intelligent agent operating within the A2A (Agent-to-Agent) task management system. ")
	sb.WriteString("Your goal is to help users accomplish their tasks efficiently through a ReAct (Reasoning and Acting) loop.\n\n")

	// Add execution summary if available
	if summary != "" {
		sb.WriteString("## Previous Execution Summary\n")
		sb.WriteString("Based on your previous ReAct loop executions, here's a summary of key insights and patterns:\n\n")
		sb.WriteString(summary)
		sb.WriteString("\n\n")
		sb.WriteString("Use this context to inform your decision-making and avoid repeating past mistakes or unnecessary actions.\n\n")
	}

	// User instructions
	sb.WriteString("## Instructions\n")
	sb.WriteString(agent.Instructions)
	sb.WriteString("\n\n")

	// Sub-agent delegation
	if len(agent.SubAgents) > 0 {
		sb.WriteString("## Available Sub-Agents\n")
		for _, subAgent := range agent.SubAgents {
			meta, err := subAgent.GetMetadata(ctx)
			if err == nil {
				fmt.Fprintf(&sb, "### %s\n", meta.Name)
				fmt.Fprintf(&sb, "%s\n\n", meta.Description)

				if len(meta.Skills) > 0 {
					sb.WriteString("**Skills:**\n")
					for _, skill := range meta.Skills {
						fmt.Fprintf(&sb, "- **%s**: %s\n", skill.Name, skill.Description)
					}
					sb.WriteString("\n")
				}
			}
		}
	}

	// Guidelines
	sb.WriteString("## Sub-Agent Delegation Guidelines\n")
	sb.WriteString("Use `delegate_to_agent` when:\n")
	sb.WriteString("- The task requires specialized knowledge outside your expertise\n")
	sb.WriteString("- Complex multi-step processes would benefit from dedicated agent handling\n")
	sb.WriteString("- The user explicitly requests interaction with a specific agent type\n\n")

	sb.WriteString("## A2A Task Management\n")
	sb.WriteString("Use `update_artifact` to:\n")
	sb.WriteString("- Save intermediate results and working data\n")
	sb.WriteString("- Create files or documents as requested\n")
	sb.WriteString("- Store analysis results, code, or other outputs\n")
	sb.WriteString("- Enable persistence across conversation turns\n\n")

	sb.WriteString("## File Access Guidelines\n")
	sb.WriteString("Use file reading tools (`read_task_file`, `list_task_files`, etc.) only when:\n")
	sb.WriteString("- A sub-agent specifically requests file information\n")
	sb.WriteString("- The user explicitly asks for file content\n")
	sb.WriteString("- You need to verify file existence for a specific purpose\n")
	sb.WriteString("**Do NOT use file tools for exploratory purposes or general investigation.**\n\n")

	sb.WriteString("## ReAct Loop Operation\n")
	sb.WriteString("1. **Reason**: Think step-by-step internally. Analyze the current situation, consider available options, and plan your next action methodically\n")
	sb.WriteString("2. **Act**: Execute one tool call to make progress\n")
	sb.WriteString("3. **Observe**: Review the results and determine next steps\n")
	sb.WriteString("4. **Repeat**: Continue until task completion\n\n")

	sb.WriteString("**IMPORTANT**: You MUST use a tool in every response. Do not provide text-only responses.\n")
	sb.WriteString("If you need to communicate with the user, use the `stop` tool with an appropriate message.\n\n")

	sb.WriteString("Use `stop` tool when:\n")
	sb.WriteString("- Task is fully completed (state: \"completed\")\n")
	sb.WriteString("- You need additional user input to proceed (state: \"input_required\")\n")
	sb.WriteString("- You want to provide a final response to the user\n")

	return sb.String(), nil
}

// defaultMessagesBuilder creates the default messages using prompt.Builder with file reference tools
func defaultMessagesBuilder(ctx context.Context, handle TaskHandle, agent *LLMAgent) ([]a2a.Message, []ExecutableTool, error) {
	task, err := handle.GetTask(ctx, HistoryLengthAll)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get task: %w", err)
	}

	// Use prompt.Builder to construct messages with file tools
	builder := prompt.NewBuilder()

	// Add task history with file support (last 20 messages to keep context manageable)
	// Incoming message is already included in task history
	builder.WriteTask(task,
		prompt.WithFormat("markdown"),
		prompt.WithMaxHistory(20),
		prompt.WithFileParts(), // Enable file parts for file reference
		prompt.WithArtifacts(), // Include artifacts
	)

	// Get file reference tool if generated
	var additionalTools []ExecutableTool
	if taskDataTool := builder.GetTaskDataTool(); taskDataTool != nil {
		additionalTools = append(additionalTools, taskDataTool)
	}

	return builder.Messages(), additionalTools, nil
}

// checkAndPerformSummarization checks if summarization should be triggered and performs it if needed
func (a *LLMAgent) checkAndPerformSummarization(ctx context.Context, handle TaskHandle, iteration int, req *model.GenerateRequest, currentSummary string, inMemoryLogs []ReActLog) (string, *model.GenerateRequest, []ReActLog, error) {
	// Check if summarization should be triggered
	shouldSummarize := false
	if a.Summarizer != nil {
		// Trigger by iteration count
		if iteration > 0 && iteration%a.SummarizerTriggerIterations == 0 {
			shouldSummarize = true
			a.Logger.Debug("Summarization triggered by iteration count", "iteration", iteration+1, "trigger", a.SummarizerTriggerIterations)
		}
		// Trigger by token count (approximate)
		if !shouldSummarize && len(req.Messages) > 0 {
			totalTokens := len(req.System) + len(fmt.Sprintf("%v", req.Messages)) // Rough approximation
			if totalTokens > a.SummarizerMaxTokens {
				shouldSummarize = true
				a.Logger.Debug("Summarization triggered by token count", "estimated_tokens", totalTokens, "threshold", a.SummarizerMaxTokens)
			}
		}
	}

	// Perform summarization if triggered
	if shouldSummarize && len(inMemoryLogs) > 0 {
		a.Logger.Info("Starting summarization", "logs_count", len(inMemoryLogs), "iteration", iteration+1)

		newSummary, err := a.Summarizer.Summarize(ctx, handle, currentSummary, inMemoryLogs)
		if err != nil {
			a.Logger.Warn("Summarization failed, continuing without summary", "error", err)
			return currentSummary, req, inMemoryLogs, nil
		}

		a.Logger.Info("Summarization completed", "summary_length", len(newSummary))

		// Clear in-memory logs after successful summarization
		clearedLogs := []ReActLog{}

		// Rebuild request with new summary and reduced message history
		newReq, err := a.newGenerateRequestWithSummary(ctx, handle, newSummary)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to rebuild request with summary: %w", err)
		}

		// Limit message history to keep only recent messages
		if len(newReq.Messages) > a.SummarizerMaxHistory {
			totalBefore := len(newReq.Messages)
			newReq.Messages = newReq.Messages[len(newReq.Messages)-a.SummarizerMaxHistory:]
			a.Logger.Debug("Limited message history after summarization", "kept_messages", len(newReq.Messages), "total_before", totalBefore)
		}

		return newSummary, newReq, clearedLogs, nil
	}

	return currentSummary, req, inMemoryLogs, nil
}

// handleToolExecution handles tool execution and returns whether to stop the loop
func (a *LLMAgent) handleToolExecution(ctx context.Context, handle TaskHandle, responseMessage a2a.Message, iteration int, req *model.GenerateRequest, logEncoder *json.Encoder, inMemoryLogs *[]ReActLog) (bool, *a2a.Message, string, bool, error) {
	toolUses := model.GetToolUseParts(responseMessage)
	if len(toolUses) > 0 {
		shouldStop, userMessage, stopMessage, err := a.executeToolsAndCollectLogs(ctx, handle, toolUses, iteration, logEncoder, inMemoryLogs)
		if err != nil {
			// Check if error is ErrInterrupted from sub-agent
			if errors.Is(err, ErrInterrupted) {
				a.Logger.Info("ReAct loop ended due to sub-agent interruption", "iteration", iteration, "error", err)
				return false, nil, "", true, err
			}

			// If tool execution fails, add error to context and continue
			a.Logger.Warn("Tool execution failed, continuing with error feedback", "error", err, "iteration", iteration)

			log := ReActLog{
				Iteration: iteration,
				Type:      "error",
				Timestamp: time.Now(),
				Data:      map[string]any{"error": err.Error(), "stage": "tool_execution"},
			}
			if logEncoder != nil {
				if err := logEncoder.Encode(log); err != nil {
					a.Logger.Warn("Failed to write log entry", "error", err)
				}
			}
			*inMemoryLogs = append(*inMemoryLogs, log)

			// Add error message as user message for next iteration
			errorMsg := fmt.Sprintf("Tool execution error: %s", err.Error())
			userErrorMessage := a2a.NewMessage("tool-error", a2a.RoleUser, []a2a.Part{a2a.NewTextPart(errorMsg)})
			req.Messages = append(req.Messages, userErrorMessage)
		} else {
			// Add tool results as user message
			req.Messages = append(req.Messages, userMessage)

			if shouldStop {
				a.Logger.Info("ReAct loop ended by tool", "iteration", iteration)
				return true, stopMessage, "tool_stop", true, nil
			}
		}
	} else {
		// No tools used - prompt to use tools or stop
		promptMessage := a2a.NewMessage("system-prompt", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Please use appropriate tools to complete the task, or call the 'stop' tool when finished."),
		})
		req.Messages = append(req.Messages, promptMessage)
	}

	return false, nil, "", false, nil
}
