package atlasic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
)

// ExecutableTool represents a tool that can be executed by LLMAgent
type ExecutableTool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args map[string]any) (a2a.Part, error)
}

// toModelTool converts ExecutableTool to model.Tool for LLM
func toModelTool(tool ExecutableTool) model.Tool {
	return model.Tool{
		Name:        tool.Name(),
		Description: tool.Description(),
		Schema:      tool.Schema(),
	}
}

// StopTool ends the ReAct loop with final status
type StopTool struct {
	handle TaskHandle
}

// NewStopTool creates a new StopTool instance
func NewStopTool(handle TaskHandle) *StopTool {
	return &StopTool{handle: handle}
}

func (t *StopTool) Name() string {
	return "stop"
}

func (t *StopTool) Description() string {
	return "End the ReAct loop with final status. Use 'completed' when task is done, 'input_required' when waiting for user input/authentication/clarification."
}

func (t *StopTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"state": map[string]any{
				"type":        "string",
				"enum":        []string{"completed", "input_required"},
				"description": "Final state of the task",
			},
			"state_message": map[string]any{
				"type":        "string",
				"description": "System status message explaining the current state (e.g., 'Task completed successfully', 'Please provide your member ID', 'Authentication required for service X')",
			},
			"response_message": map[string]any{
				"type":        "string",
				"description": "Optional direct response message to user (e.g., order information, search results, final answer)",
			},
		},
		"required": []string{"state", "response_message"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *StopTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	stateStr, ok := args["state"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("state must be a string")
	}

	var state a2a.TaskState
	switch stateStr {
	case "completed":
		state = a2a.TaskStateCompleted
	case "input_required":
		state = a2a.TaskStateInputRequired
	default:
		return a2a.Part{}, fmt.Errorf("invalid state: %s", stateStr)
	}

	var parts []a2a.Part

	// Add state message if provided
	if stateMsg, ok := args["state_message"].(string); ok && stateMsg != "" {
		parts = append(parts, a2a.NewTextPart(stateMsg))
	}

	// Add response message to TaskHistory as a normal message if provided
	if responseMsg, ok := args["response_message"].(string); ok && responseMsg != "" {
		_, err := t.handle.AddMessage(ctx, []a2a.Part{a2a.NewTextPart(responseMsg)})
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to add response message: %w", err)
		}
	}

	_, err := t.handle.UpdateStatus(ctx, state, parts)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to update status: %w", err)
	}

	// StopToolの場合は特別なmetadataでstopシグナルを送る
	part := a2a.NewTextPart(fmt.Sprintf("Task completed with state: %s", state))
	part.Metadata = map[string]any{
		"final_state": state,
		"should_stop": true,
	}
	return part, nil
}

// UpdateArtifactTool creates or updates artifacts in the task
type UpdateArtifactTool struct {
	handle TaskHandle
}

// NewUpdateArtifactTool creates a new UpdateArtifactTool instance
func NewUpdateArtifactTool(handle TaskHandle) *UpdateArtifactTool {
	return &UpdateArtifactTool{handle: handle}
}

func (t *UpdateArtifactTool) Name() string {
	return "update_artifact"
}

func (t *UpdateArtifactTool) Description() string {
	return "Create or update task artifacts. Use 'file' for code/documents, 'data' for structured data, 'result' for analysis results, 'report' for summaries, 'config' for settings, 'log' for process logs, 'output' for final deliverables."
}

func (t *UpdateArtifactTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name/path of the artifact",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content of the artifact",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Type of the artifact",
				"enum":        []string{"file", "data", "result", "report", "config", "log", "output"},
				"default":     "file",
			},
		},
		"required": []string{"name", "content"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *UpdateArtifactTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	name, ok := args["name"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("name must be a string")
	}

	content, ok := args["content"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("content must be a string")
	}

	artifactType := "file" // default
	if t, ok := args["type"].(string); ok && t != "" {
		artifactType = t
	}

	// Update artifact through TaskHandle
	artifact := a2a.Artifact{
		Name:        name,
		Description: fmt.Sprintf("Artifact of type: %s", artifactType),
		Parts:       []a2a.Part{a2a.NewTextPart(content)},
		Metadata: map[string]any{
			"type": artifactType,
		},
	}
	err := t.handle.UpsertArtifact(ctx, artifact)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to update artifact: %w", err)
	}

	response := fmt.Sprintf("Artifact '%s' (type: %s) updated successfully", name, artifactType)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"artifact_name": name,
		"artifact_type": artifactType,
		"operation":     "update",
	}
	return part, nil
}

// DelegateToAgentTool delegates processing to a sub-agent
type DelegateToAgentTool struct {
	handle    TaskHandle
	subAgents []Agent
}

// NewDelegateToAgentTool creates a new DelegateToAgentTool instance
func NewDelegateToAgentTool(handle TaskHandle, subAgents ...Agent) *DelegateToAgentTool {
	return &DelegateToAgentTool{handle: handle, subAgents: subAgents}
}

func (t *DelegateToAgentTool) Name() string {
	return "delegate_to_agent"
}

func (t *DelegateToAgentTool) Description() string {
	return "Delegate specialized tasks to appropriate sub-agents (e.g., OrderAgent for order queries, PaymentAgent for billing issues). Use when task requires domain-specific expertise."
}

func (t *DelegateToAgentTool) Schema() json.RawMessage {
	// Extract agent names for enum
	var agentNames []string
	for _, agent := range t.subAgents {
		// Get metadata name if available, otherwise use clean type name
		if meta, err := agent.GetMetadata(context.Background()); err == nil && meta.Name != "" {
			agentNames = append(agentNames, meta.Name)
		} else {
			// Use clean type name without package and pointer indicators
			typeName := fmt.Sprintf("%T", agent)
			// Remove pointer indicator and package prefix
			typeName = strings.TrimPrefix(typeName, "*")
			if lastDot := strings.LastIndex(typeName, "."); lastDot != -1 {
				typeName = typeName[lastDot+1:]
			}
			agentNames = append(agentNames, typeName)
		}
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_name": map[string]any{
				"type":        "string",
				"description": "Name of the sub-agent to delegate to",
				"enum":        agentNames,
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why you are delegating to this specific agent (e.g., 'User asking about order status', 'Need payment processing expertise')",
			},
			"instruction": map[string]any{
				"type":        "string",
				"description": "Specific instruction or context for the sub-agent to focus on. This tells the sub-agent what to prioritize or how to approach the task (e.g., 'Focus on order ID ORD-12345', 'Process payment for premium subscription', 'Check inventory levels for product XYZ')",
			},
		},
		"required": []string{"agent_name", "reason", "instruction"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *DelegateToAgentTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	agentName, ok := args["agent_name"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("agent_name must be a string")
	}

	reason, ok := args["reason"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("reason must be a string")
	}

	// Extract required instruction for sub-agent
	delegationInstruction, ok := args["instruction"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("instruction must be a string")
	}
	if delegationInstruction == "" {
		return a2a.Part{}, fmt.Errorf("instruction cannot be empty")
	}

	// Find the sub-agent by name
	var targetAgent Agent
	for _, agent := range t.subAgents {
		// First try to match by metadata name
		if meta, err := agent.GetMetadata(context.Background()); err == nil && meta.Name == agentName {
			targetAgent = agent
			break
		}

		// Fallback: match by clean type name
		typeName := fmt.Sprintf("%T", agent)
		typeName = strings.TrimPrefix(typeName, "*")
		if lastDot := strings.LastIndex(typeName, "."); lastDot != -1 {
			typeName = typeName[lastDot+1:]
		}
		if typeName == agentName {
			targetAgent = agent
			break
		}
	}

	if targetAgent == nil {
		return a2a.Part{}, fmt.Errorf("sub-agent '%s' not found", agentName)
	}

	// Add reasoning message to task history if reason is provided
	if reason != "" {
		reasoningParts := model.MarkReasoning(a2a.NewTextPart(reason))
		_, err := t.handle.AddMessage(ctx, reasoningParts)
		if err != nil {
			return a2a.Part{}, fmt.Errorf("failed to add reasoning message: %w", err)
		}
	}

	// Create context with delegation instruction (now required)
	subCtx := WithDelegationMessage(ctx, delegationInstruction)

	// Execute the sub-agent with enriched context
	resultMessage, err := targetAgent.Execute(subCtx, t.handle)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("sub-agent execution failed: %w", err)
	}

	// Use result message if available, otherwise provide default delegation message
	if resultMessage != nil && len(resultMessage.Parts) > 0 {
		// Return the first part of the result message as tool result
		resultPart := resultMessage.Parts[0]
		// Add delegation metadata
		if resultPart.Metadata == nil {
			resultPart.Metadata = make(map[string]any)
		}
		resultPart.Metadata["delegated_agent"] = agentName
		resultPart.Metadata["delegation_reason"] = reason
		resultPart.Metadata["execution_status"] = "completed"
		return resultPart, nil
	}

	// Fallback: No result message, use default delegation response
	response := fmt.Sprintf("Successfully delegated to agent '%s' and completed execution", agentName)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"delegated_agent":   agentName,
		"delegation_reason": reason,
		"execution_status":  "completed",
	}
	return part, nil
}

// TaskFileWriteTool writes content to a task-scoped file
type TaskFileWriteTool struct {
	handle TaskHandle
}

// NewTaskFileWriteTool creates a new TaskFileWriteTool instance
func NewTaskFileWriteTool(handle TaskHandle) *TaskFileWriteTool {
	return &TaskFileWriteTool{handle: handle}
}

func (t *TaskFileWriteTool) Name() string {
	return "write_task_file"
}

func (t *TaskFileWriteTool) Description() string {
	return "Write content to a task-scoped file"
}

func (t *TaskFileWriteTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to task scope",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *TaskFileWriteTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	content, ok := args["content"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("content must be a string")
	}

	err := t.handle.PutTaskFile(ctx, path, []byte(content))
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to write task file: %w", err)
	}

	response := fmt.Sprintf("Successfully wrote task file: %s", path)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "write_task_file",
		"path":      path,
		"size":      len(content),
	}
	return part, nil
}

// TaskFileReadTool reads content from a task-scoped file
type TaskFileReadTool struct {
	handle TaskHandle
}

// NewTaskFileReadTool creates a new TaskFileReadTool instance
func NewTaskFileReadTool(handle TaskHandle) *TaskFileReadTool {
	return &TaskFileReadTool{handle: handle}
}

func (t *TaskFileReadTool) Name() string {
	return "read_task_file"
}

func (t *TaskFileReadTool) Description() string {
	return "Read content from a task-scoped file"
}

func (t *TaskFileReadTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to task scope",
			},
		},
		"required": []string{"path"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *TaskFileReadTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	data, err := t.handle.GetTaskFile(ctx, path)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to read task file: %w", err)
	}

	response := fmt.Sprintf("Content of task file '%s':\n\n%s", path, string(data))
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "read_task_file",
		"path":      path,
		"size":      len(data),
	}
	return part, nil
}

// TaskFileListTool lists task-scoped files
type TaskFileListTool struct {
	handle TaskHandle
}

// NewTaskFileListTool creates a new TaskFileListTool instance
func NewTaskFileListTool(handle TaskHandle) *TaskFileListTool {
	return &TaskFileListTool{handle: handle}
}

func (t *TaskFileListTool) Name() string {
	return "list_task_files"
}

func (t *TaskFileListTool) Description() string {
	return "List task-scoped files with optional path prefix filter"
}

func (t *TaskFileListTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path_prefix": map[string]any{
				"type":        "string",
				"description": "Optional path prefix to filter files",
				"default":     "",
			},
		},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *TaskFileListTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	pathPrefix := ""
	if prefix, ok := args["path_prefix"].(string); ok {
		pathPrefix = prefix
	}

	files, err := t.handle.ListTaskFiles(ctx, pathPrefix)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to list task files: %w", err)
	}

	var response strings.Builder
	if pathPrefix == "" {
		fmt.Fprintf(&response, "Task files (%d total):\n", len(files))
	} else {
		fmt.Fprintf(&response, "Task files with prefix '%s' (%d total):\n", pathPrefix, len(files))
	}

	for _, file := range files {
		fmt.Fprintf(&response, "- %s\n", file)
	}

	part := a2a.NewTextPart(response.String())
	part.Metadata = map[string]any{
		"operation":   "list_task_files",
		"path_prefix": pathPrefix,
		"count":       len(files),
		"files":       files,
	}
	return part, nil
}

// TaskFileDeleteTool deletes a task-scoped file
type TaskFileDeleteTool struct {
	handle TaskHandle
}

// NewTaskFileDeleteTool creates a new TaskFileDeleteTool instance
func NewTaskFileDeleteTool(handle TaskHandle) *TaskFileDeleteTool {
	return &TaskFileDeleteTool{handle: handle}
}

func (t *TaskFileDeleteTool) Name() string {
	return "delete_task_file"
}

func (t *TaskFileDeleteTool) Description() string {
	return "Delete a task-scoped file"
}

func (t *TaskFileDeleteTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to task scope",
			},
		},
		"required": []string{"path"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *TaskFileDeleteTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	err := t.handle.DeleteTaskFile(ctx, path)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to delete task file: %w", err)
	}

	response := fmt.Sprintf("Successfully deleted task file: %s", path)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "delete_task_file",
		"path":      path,
	}
	return part, nil
}

// ContextFileWriteTool writes content to a context-scoped file
type ContextFileWriteTool struct {
	handle TaskHandle
}

// NewContextFileWriteTool creates a new ContextFileWriteTool instance
func NewContextFileWriteTool(handle TaskHandle) *ContextFileWriteTool {
	return &ContextFileWriteTool{handle: handle}
}

func (t *ContextFileWriteTool) Name() string {
	return "write_context_file"
}

func (t *ContextFileWriteTool) Description() string {
	return "Write content to a context-scoped file (shared across tasks)"
}

func (t *ContextFileWriteTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to context scope",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *ContextFileWriteTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	content, ok := args["content"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("content must be a string")
	}

	err := t.handle.PutContextFile(ctx, path, []byte(content))
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to write context file: %w", err)
	}

	response := fmt.Sprintf("Successfully wrote context file: %s", path)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "write_context_file",
		"path":      path,
		"size":      len(content),
	}
	return part, nil
}

// ContextFileReadTool reads content from a context-scoped file
type ContextFileReadTool struct {
	handle TaskHandle
}

// NewContextFileReadTool creates a new ContextFileReadTool instance
func NewContextFileReadTool(handle TaskHandle) *ContextFileReadTool {
	return &ContextFileReadTool{handle: handle}
}

func (t *ContextFileReadTool) Name() string {
	return "read_context_file"
}

func (t *ContextFileReadTool) Description() string {
	return "Read content from a context-scoped file (shared across tasks)"
}

func (t *ContextFileReadTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to context scope",
			},
		},
		"required": []string{"path"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *ContextFileReadTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	data, err := t.handle.GetContextFile(ctx, path)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to read context file: %w", err)
	}

	response := fmt.Sprintf("Content of context file '%s':\n\n%s", path, string(data))
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "read_context_file",
		"path":      path,
		"size":      len(data),
	}
	return part, nil
}

// ContextFileListTool lists context-scoped files
type ContextFileListTool struct {
	handle TaskHandle
}

// NewContextFileListTool creates a new ContextFileListTool instance
func NewContextFileListTool(handle TaskHandle) *ContextFileListTool {
	return &ContextFileListTool{handle: handle}
}

func (t *ContextFileListTool) Name() string {
	return "list_context_files"
}

func (t *ContextFileListTool) Description() string {
	return "List context-scoped files with optional path prefix filter (shared across tasks)"
}

func (t *ContextFileListTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path_prefix": map[string]any{
				"type":        "string",
				"description": "Optional path prefix to filter files",
				"default":     "",
			},
		},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *ContextFileListTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	pathPrefix := ""
	if prefix, ok := args["path_prefix"].(string); ok {
		pathPrefix = prefix
	}

	files, err := t.handle.ListContextFiles(ctx, pathPrefix)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to list context files: %w", err)
	}

	var response strings.Builder
	if pathPrefix == "" {
		fmt.Fprintf(&response, "Context files (%d total):\n", len(files))
	} else {
		fmt.Fprintf(&response, "Context files with prefix '%s' (%d total):\n", pathPrefix, len(files))
	}

	for _, file := range files {
		fmt.Fprintf(&response, "- %s\n", file)
	}

	part := a2a.NewTextPart(response.String())
	part.Metadata = map[string]any{
		"operation":   "list_context_files",
		"path_prefix": pathPrefix,
		"count":       len(files),
		"files":       files,
	}
	return part, nil
}

// ContextFileDeleteTool deletes a context-scoped file
type ContextFileDeleteTool struct {
	handle TaskHandle
}

// NewContextFileDeleteTool creates a new ContextFileDeleteTool instance
func NewContextFileDeleteTool(handle TaskHandle) *ContextFileDeleteTool {
	return &ContextFileDeleteTool{handle: handle}
}

func (t *ContextFileDeleteTool) Name() string {
	return "delete_context_file"
}

func (t *ContextFileDeleteTool) Description() string {
	return "Delete a context-scoped file (shared across tasks)"
}

func (t *ContextFileDeleteTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to context scope",
			},
		},
		"required": []string{"path"},
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func (t *ContextFileDeleteTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	path, ok := args["path"].(string)
	if !ok {
		return a2a.Part{}, fmt.Errorf("path must be a string")
	}

	err := t.handle.DeleteContextFile(ctx, path)
	if err != nil {
		return a2a.Part{}, fmt.Errorf("failed to delete context file: %w", err)
	}

	response := fmt.Sprintf("Successfully deleted context file: %s", path)
	part := a2a.NewTextPart(response)
	part.Metadata = map[string]any{
		"operation": "delete_context_file",
		"path":      path,
	}
	return part, nil
}
