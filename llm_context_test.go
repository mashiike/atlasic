package atlasic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"go.uber.org/mock/gomock"
)

// mockTool for testing
type mockTool struct {
	name        string
	description string
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object"}`)
}

func (m *mockTool) Execute(ctx context.Context, args map[string]any) (a2a.Part, error) {
	return a2a.NewTextPart("mock tool executed"), nil
}

// mockAgent for testing
type mockAgent struct {
	name        string
	description string
	skills      []a2a.AgentSkill
}

func (m *mockAgent) GetMetadata(ctx context.Context) (*AgentMetadata, error) {
	return &AgentMetadata{
		Name:        m.name,
		Description: m.description,
		Skills:      m.skills,
		Version:     "1.0.0",
	}, nil
}

func (m *mockAgent) Execute(ctx context.Context, handle TaskHandle) (*a2a.Message, error) {
	return &a2a.Message{
		Role:  a2a.RoleAgent,
		Parts: []a2a.Part{a2a.NewTextPart("mock agent executed")},
	}, nil
}

func TestWithSubAgents(t *testing.T) {
	ctx := context.Background()

	agent1 := &mockAgent{name: "agent1", description: "test agent 1"}
	agent2 := &mockAgent{name: "agent2", description: "test agent 2"}

	// Test adding single agent
	ctx1 := WithSubAgents(ctx, agent1)
	agents := GetSubAgentsFromContext(ctx1)

	if len(agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(agents))
	}
	if agents[0] != agent1 {
		t.Errorf("Expected agent1, got different agent")
	}

	// Test adding multiple agents
	ctx2 := WithSubAgents(ctx1, agent2)
	agents = GetSubAgentsFromContext(ctx2)

	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}
}

func TestWithTools(t *testing.T) {
	ctx := context.Background()

	tool1 := &mockTool{name: "tool1", description: "test tool 1"}
	tool2 := &mockTool{name: "tool2", description: "test tool 2"}

	// Test adding single tool
	ctx1 := WithTools(ctx, tool1)
	tools := GetToolsFromContext(ctx1)

	if len(tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(tools))
	}
	if tools[0] != tool1 {
		t.Errorf("Expected tool1, got different tool")
	}

	// Test adding multiple tools
	ctx2 := WithTools(ctx1, tool2)
	tools = GetToolsFromContext(ctx2)

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}
}

func TestAppendSubAgent(t *testing.T) {
	ctx := context.Background()

	agent1 := &mockAgent{name: "agent1", description: "test agent 1"}
	agent2 := &mockAgent{name: "agent2", description: "test agent 2"}

	ctx1 := AppendSubAgent(ctx, agent1)
	ctx2 := AppendSubAgent(ctx1, agent2)

	agents := GetSubAgentsFromContext(ctx2)

	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}
}

func TestAppendTool(t *testing.T) {
	ctx := context.Background()

	tool1 := &mockTool{name: "tool1", description: "test tool 1"}
	tool2 := &mockTool{name: "tool2", description: "test tool 2"}

	ctx1 := AppendTool(ctx, tool1)
	ctx2 := AppendTool(ctx1, tool2)

	tools := GetToolsFromContext(ctx2)

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}
}

func TestLLMAgent_GetAllSubAgents(t *testing.T) {
	ctx := context.Background()

	staticAgent := &mockAgent{name: "static", description: "static agent"}
	dynamicAgent := &mockAgent{name: "dynamic", description: "dynamic agent"}

	agent := &LLMAgent{
		SubAgents: []Agent{staticAgent},
	}

	// Test with only static agents
	allAgents := agent.GetAllSubAgents(ctx)
	if len(allAgents) != 1 {
		t.Errorf("Expected 1 agent (static only), got %d", len(allAgents))
	}

	// Test with static + dynamic agents
	ctx1 := WithSubAgents(ctx, dynamicAgent)
	allAgents = agent.GetAllSubAgents(ctx1)
	if len(allAgents) != 2 {
		t.Errorf("Expected 2 agents (static + dynamic), got %d", len(allAgents))
	}
}

func TestLLMAgent_GetAllTools(t *testing.T) {
	ctx := context.Background()

	staticTool := &mockTool{name: "static", description: "static tool"}
	dynamicTool := &mockTool{name: "dynamic", description: "dynamic tool"}

	agent := &LLMAgent{
		Tools: []ExecutableTool{staticTool},
	}

	// Test with only static tools
	allTools := agent.GetAllTools(ctx)
	if len(allTools) != 1 {
		t.Errorf("Expected 1 tool (static only), got %d", len(allTools))
	}

	// Test with static + dynamic tools
	ctx1 := WithTools(ctx, dynamicTool)
	allTools = agent.GetAllTools(ctx1)
	if len(allTools) != 2 {
		t.Errorf("Expected 2 tools (static + dynamic), got %d", len(allTools))
	}
}

func TestGetFromEmptyContext(t *testing.T) {
	ctx := context.Background()

	// Test empty context returns nil
	agents := GetSubAgentsFromContext(ctx)
	if agents != nil {
		t.Errorf("Expected nil agents from empty context, got %v", agents)
	}

	tools := GetToolsFromContext(ctx)
	if tools != nil {
		t.Errorf("Expected nil tools from empty context, got %v", tools)
	}
}

func TestToolPriorityOrder(t *testing.T) {
	ctx := context.Background()

	// Create tools with same name to test priority
	dynamicTool := &mockTool{name: "samename", description: "dynamic"}
	staticTool := &mockTool{name: "samename", description: "static"}
	additionalTool := &mockTool{name: "samename", description: "additional"}

	agent := &LLMAgent{
		Tools: []ExecutableTool{staticTool},
	}

	// Add dynamic tool to context
	ctx1 := WithTools(ctx, dynamicTool)

	// Create a simple mock TaskHandle implementation for testing
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := NewMockTaskHandle(ctrl)
	mockHandle.EXPECT().GetTaskID().Return("test-task").AnyTimes()
	mockHandle.EXPECT().GetContextID().Return("test-context").AnyTimes()

	// Build tools with additional tool
	allTools := agent.buildAllTools(ctx1, mockHandle, []ExecutableTool{additionalTool})

	// Find the tool with name "samename" - should be from highest priority source
	var foundTool ExecutableTool
	for _, tool := range allTools {
		if tool.Name() == "samename" {
			foundTool = tool
			break
		}
	}

	if foundTool == nil {
		t.Errorf("Expected to find tool with name 'samename'")
		return
	}

	// Expected priority: Builtin > Dynamic > Static > Additional
	// Since no builtin tools have "samename", dynamic should win
	if foundTool.Description() != "dynamic" {
		t.Errorf("Expected dynamic tool to have priority, got tool with description: %s", foundTool.Description())
	}
}

func TestDelegationMessageContext(t *testing.T) {
	ctx := context.Background()

	// Test empty context
	if message, exists := GetDelegationMessage(ctx); exists {
		t.Errorf("Expected no delegation message in empty context, got %s", message)
	}

	// Test with delegation message
	testMessage := "Please process this user request"
	ctx1 := WithDelegationMessage(ctx, testMessage)

	message, exists := GetDelegationMessage(ctx1)
	if !exists {
		t.Error("Expected delegation message to exist")
	}
	if message != testMessage {
		t.Errorf("Expected message %s, got %s", testMessage, message)
	}

	// Test overwriting message
	newMessage := "Updated delegation message"
	ctx2 := WithDelegationMessage(ctx1, newMessage)

	message, exists = GetDelegationMessage(ctx2)
	if !exists {
		t.Error("Expected delegation message to exist after update")
	}
	if message != newMessage {
		t.Errorf("Expected updated message %s, got %s", newMessage, message)
	}

	// Test original context unchanged
	originalMessage, exists := GetDelegationMessage(ctx1)
	if !exists {
		t.Error("Expected original context to still have delegation message")
	}
	if originalMessage != testMessage {
		t.Errorf("Expected original message %s, got %s", testMessage, originalMessage)
	}
}
