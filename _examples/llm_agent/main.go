package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	_ "github.com/mashiike/atlasic/model/bedrock"
	_ "github.com/mashiike/atlasic/model/ollama"
)

// OrderAgent is a specialized agent that handles order-related queries
type OrderAgent struct{}

// GetMetadata returns metadata for the OrderAgent
func (o *OrderAgent) GetMetadata(ctx context.Context) (*atlasic.AgentMetadata, error) {
	return &atlasic.AgentMetadata{
		Name:        "OrderAgent",
		Description: "Specialized agent for handling order information and inquiries",
		Skills: []a2a.AgentSkill{
			{
				ID:          "get_order_info",
				Name:        "Get Order Information",
				Description: "Retrieve order details including status, items, and tracking",
			},
			{
				ID:          "order_history",
				Name:        "Order History",
				Description: "Access customer order history and past purchases",
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Version:            "1.0.0",
	}, nil
}

// Execute handles order-related requests with hardcoded sample data
func (o *OrderAgent) Execute(ctx context.Context, handle atlasic.TaskHandle) (*a2a.Message, error) {
	// For this example, we'll return hardcoded order information
	// In a real implementation, this would query a database or API
	orderInfo := `📦 **Order Information**

**Order ID:** ORD-2024-001
**Status:** Shipped
**Tracking Number:** TRK123456789

**Items Ordered:**
- 🎧 Wireless Bluetooth Headphones - $99.99
- 📱 Phone Case (Blue) - $24.99
- 🔌 USB-C Cable (2m) - $19.99

**Total:** $144.97
**Shipping Address:** 123 Main St, Anytown, AN 12345
**Estimated Delivery:** December 20, 2024

**Order History:**
- Dec 15, 2024 - Order placed
- Dec 16, 2024 - Payment confirmed
- Dec 17, 2024 - Items picked and packed
- Dec 18, 2024 - Shipped via Express Delivery

Your order is on its way! You can expect delivery by December 20th.`

	// Return the order information directly as the result message
	// Do NOT add to task history - let the delegating agent handle user communication
	resultMessage := &a2a.Message{
		Role: a2a.RoleAgent,
		Parts: []a2a.Part{
			a2a.NewTextPart(orderInfo),
		},
	}

	return resultMessage, nil
}

func main() {
	var (
		useBedrock = false // Set to true to use Bedrock model
	)
	flag.BoolVar(&useBedrock, "bedrock", false, "Use Bedrock model instead of Ollama")
	flag.Parse()
	modelProvider := "ollama"
	modelID := "llama3.2"
	if useBedrock {
		modelProvider = "bedrock"
		modelID = "us.anthropic.claude-3-sonnet-20240229-v1:0"
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Create OrderAgent
	orderAgent := &OrderAgent{}

	// Create LLM agent with ReAct capabilities and order delegation
	agent := &atlasic.LLMAgent{
		Name:          "CustomerServiceAgent",
		Description:   "A customer service agent that handles general inquiries and delegates order-related questions",
		ModelProvider: modelProvider,
		ModelID:       modelID,
		Instructions: `You are a helpful customer service assistant for an e-commerce company.

IMPORTANT: For ANY question about orders, shipping, tracking, or order history:
1. IMMEDIATELY use delegate_to_agent with "OrderAgent"
2. Do NOT attempt to read files or use other tools first
3. Provide a friendly summary after receiving the OrderAgent's response
4. Do Not include AgentName in final response. Generate Your answer.

Your main responsibilities:
- Help customers with general inquiries
- Delegate order-related questions to OrderAgent as the FIRST action
- Provide friendly and helpful responses
- Summarize information received from specialized agents

Always be polite and helpful in your responses.`,
		Version: "1.0.0",
	}

	// Set up sub-agents for delegation
	agent.SubAgents = []atlasic.Agent{orderAgent}

	// Create server with LLM agent
	server := &atlasic.Server{
		Addr:  ":8080",
		Agent: agent,
	}

	slog.Info("Starting Customer Service Agent server with order delegation", "addr", server.Addr)
	slog.Info("Try asking: 'What's the status of my order?' or 'Can you check my order history?'")

	if err := server.RunWithContext(ctx); err != nil {
		slog.Error("Server error", "error", err)
	}
}
