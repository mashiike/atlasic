package atlasic

import "context"

// Context keys for dynamic LLMAgent extension
type llmContextKey string

const (
	// contextKeySubAgents stores additional sub-agents for LLMAgent (private)
	contextKeySubAgents = llmContextKey("llm_agent_sub_agents")

	// contextKeyTools stores additional tools for LLMAgent (private)
	contextKeyTools = llmContextKey("llm_agent_tools")

	// contextKeyDelegationMessage stores message for sub-agent delegation (private)
	contextKeyDelegationMessage = llmContextKey("llm_agent_delegation_message")
)

// WithSubAgents adds sub-agents to the context for dynamic LLMAgent extension
func WithSubAgents(ctx context.Context, agents ...Agent) context.Context {
	existing := GetSubAgentsFromContext(ctx)
	combined := append(existing, agents...)
	return context.WithValue(ctx, contextKeySubAgents, combined)
}

// WithTools adds tools to the context for dynamic LLMAgent extension
func WithTools(ctx context.Context, tools ...ExecutableTool) context.Context {
	existing := GetToolsFromContext(ctx)
	combined := append(existing, tools...)
	return context.WithValue(ctx, contextKeyTools, combined)
}

// GetSubAgentsFromContext retrieves sub-agents from the context
func GetSubAgentsFromContext(ctx context.Context) []Agent {
	if agents, ok := ctx.Value(contextKeySubAgents).([]Agent); ok {
		return agents
	}
	return nil
}

// GetToolsFromContext retrieves tools from the context
func GetToolsFromContext(ctx context.Context) []ExecutableTool {
	if tools, ok := ctx.Value(contextKeyTools).([]ExecutableTool); ok {
		return tools
	}
	return nil
}

// WithDelegationMessage adds a delegation message to the context for sub-agent communication
func WithDelegationMessage(ctx context.Context, message string) context.Context {
	return context.WithValue(ctx, contextKeyDelegationMessage, message)
}

// GetDelegationMessage retrieves the delegation message from the context
func GetDelegationMessage(ctx context.Context) (string, bool) {
	if message, ok := ctx.Value(contextKeyDelegationMessage).(string); ok {
		return message, true
	}
	return "", false
}

// AppendSubAgent appends a single sub-agent to existing context sub-agents
func AppendSubAgent(ctx context.Context, agent Agent) context.Context {
	return WithSubAgents(ctx, agent)
}

// AppendTool appends a single tool to existing context tools
func AppendTool(ctx context.Context, tool ExecutableTool) context.Context {
	return WithTools(ctx, tool)
}

// GetAllSubAgents combines static and dynamic sub-agents
func (a *LLMAgent) GetAllSubAgents(ctx context.Context) []Agent {
	var allAgents []Agent

	// Add static sub-agents from LLMAgent
	allAgents = append(allAgents, a.SubAgents...)

	// Add dynamic sub-agents from context
	dynamicAgents := GetSubAgentsFromContext(ctx)
	allAgents = append(allAgents, dynamicAgents...)

	return allAgents
}

// GetAllTools combines static and dynamic tools
func (a *LLMAgent) GetAllTools(ctx context.Context) []ExecutableTool {
	var allTools []ExecutableTool

	// Add static tools from LLMAgent
	allTools = append(allTools, a.Tools...)

	// Add dynamic tools from context
	dynamicTools := GetToolsFromContext(ctx)
	allTools = append(allTools, dynamicTools...)

	return allTools
}
