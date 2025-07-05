package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

// SampleDataOnlyExtension demonstrates DataOnlyExtension implementation
type SampleDataOnlyExtension struct {
	supportedLanguages []string
	maxFileSize        int64
}

func (e *SampleDataOnlyExtension) GetMetadata() a2a.AgentExtension {
	return a2a.AgentExtension{
		URI:         "https://example.com/extensions/sample-data/v1",
		Description: "Sample data-only extension that adds custom capabilities",
		Required:    false,
		Params: map[string]any{
			"supportedLanguages": e.supportedLanguages,
			"maxFileSize":        e.maxFileSize,
		},
	}
}

func (e *SampleDataOnlyExtension) EnrichAgentCard(builder *transport.AgentCardBuilder) error {
	builder.AddExtensionField("customCapabilities", map[string]any{
		"supportedLanguages": e.supportedLanguages,
		"maxFileSize":        e.maxFileSize,
		"features":           []string{"multilingual", "file-upload", "premium"},
	})
	
	builder.AddExtensionField("serviceLevel", "premium")
	builder.AddExtensionField("extensionVersion", "1.0.0")
	
	return nil
}

// SampleMethodExtension demonstrates RPCMethodExtension implementation
type SampleMethodExtension struct{}

func (e *SampleMethodExtension) GetMetadata() a2a.AgentExtension {
	return a2a.AgentExtension{
		URI:         "https://example.com/extensions/sample-method/v1",
		Description: "Sample method extension that adds custom JSON-RPC methods",
		Required:    false,
	}
}

func (e *SampleMethodExtension) MethodName() string {
	return "sample/ping"
}

func (e *SampleMethodExtension) MethodHandler() transport.JSONRPCMethodHandler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var pingParams struct {
			Message string `json:"message,omitempty"`
		}
		
		if len(params) > 0 {
			if err := json.Unmarshal(params, &pingParams); err != nil {
				return nil, &a2a.JSONRPCError{
					Code:    a2a.ErrorCodeInvalidParams,
					Message: a2a.ErrorCodeText(a2a.ErrorCodeInvalidParams),
					Data:    err.Error(),
				}
			}
		}
		
		message := pingParams.Message
		if message == "" {
			message = "default"
		}
		
		return map[string]any{
			"response":  fmt.Sprintf("Pong: %s", message),
			"timestamp": "2025-07-04T12:00:00Z",
			"method":    "sample/ping",
		}, nil
	}
}

// SampleProfileExtension demonstrates ProfileExtension implementation
type SampleProfileExtension struct {
	transport.BaseProfileExtension
}

func (e *SampleProfileExtension) GetMetadata() a2a.AgentExtension {
	return a2a.AgentExtension{
		URI:         "https://example.com/extensions/sample-profile/v1",
		Description: "Sample profile extension that modifies request/response flow",
		Required:    false,
	}
}

func (e *SampleProfileExtension) PrepareContext(ctx context.Context, method string, params json.RawMessage) (context.Context, error) {
	// Add extension metadata to context
	ctx = context.WithValue(ctx, "extensionTimestamp", "2025-07-04T12:00:00Z")
	ctx = context.WithValue(ctx, "extensionMethod", method)
	ctx = context.WithValue(ctx, "extensionActive", true)
	
	return ctx, nil
}

// Implement SendMessageProfileExtension for method-specific preprocessing
func (e *SampleProfileExtension) PrepareSendMessage(ctx context.Context, params *a2a.MessageSendParams) (context.Context, error) {
	// Add message-specific metadata to context
	ctx = context.WithValue(ctx, "messageExtensionActive", true)
	ctx = context.WithValue(ctx, "messageType", "send")
	ctx = context.WithValue(ctx, "messageCount", len(params.Message.Parts))
	
	return ctx, nil
}

func main() {
	fmt.Println("🚀 ATLASIC Extension System Demo")
	fmt.Println("=================================")
	
	// Create sample extensions
	dataOnlyExt := &SampleDataOnlyExtension{
		supportedLanguages: []string{"en", "ja", "es"},
		maxFileSize:        2097152, // 2MB
	}
	
	methodExt := &SampleMethodExtension{}
	profileExt := &SampleProfileExtension{}
	
	// Create agent using the simplified agent function
	agent := atlasic.NewAgent(
		&atlasic.AgentMetadata{
			Name:        "Extension Demo Agent",
			Description: "Demonstration agent showing extension capabilities",
			Version:     "1.0.0",
		},
		func(ctx context.Context, handle atlasic.TaskHandle) error {
			// Simple echo implementation
			task, err := handle.GetTask(ctx, atlasic.HistoryLengthAll)
			if err != nil {
				return fmt.Errorf("failed to get task: %w", err)
			}
			
			if len(task.History) == 0 {
				return fmt.Errorf("no message found in task history")
			}
			
			// Get the last user message
			var lastMessage *a2a.Message
			for i := len(task.History) - 1; i >= 0; i-- {
				if task.History[i].Role == a2a.RoleUser {
					lastMessage = &task.History[i]
					break
				}
			}
			
			if lastMessage == nil || len(lastMessage.Parts) == 0 {
				return fmt.Errorf("no user message found")
			}

			response := fmt.Sprintf("Echo: %s", lastMessage.Parts[0].Text)
			_, err = handle.AddMessage(ctx, []a2a.Part{
				{Kind: a2a.KindTextPart, Text: response},
			})
			return err
		},
	)

	// Create server with extensions
	server := &atlasic.Server{
		Addr:  ":8080",
		Agent: agent,
	}

	// Add extensions using the Use method
	server.Use(dataOnlyExt, methodExt, profileExt)
	
	fmt.Println("🎯 Demo Server Configuration:")
	fmt.Println("  • DataOnly Extension: Custom capabilities in AgentCard")
	fmt.Println("  • Method Extension: Custom 'sample/ping' JSON-RPC method")
	fmt.Println("  • Profile Extension: Request/response processing")
	fmt.Println("")
	fmt.Println("📡 Available Endpoints:")
	fmt.Println("  GET  /.well-known/agent.json")
	fmt.Println("       → Agent Card with DataOnly extension data")
	fmt.Println("  POST /")
	fmt.Println("       → JSON-RPC endpoint")
	fmt.Println("")
	fmt.Println("🧪 Testing Commands:")
	fmt.Println("# Get Agent Card (shows DataOnly extension)")
	fmt.Println("curl http://localhost:8080/.well-known/agent.json")
	fmt.Println("")
	fmt.Println("# Test sample/ping method WITHOUT extension activation")
	fmt.Println(`curl -X POST http://localhost:8080/ \`)
	fmt.Println(`  -H "Content-Type: application/json" \`)
	fmt.Println(`  -d '{"jsonrpc":"2.0","method":"sample/ping","params":{"message":"test"},"id":1}'`)
	fmt.Println("")
	fmt.Println("# Test sample/ping method WITH extension activation")
	fmt.Println(`curl -X POST http://localhost:8080/ \`)
	fmt.Println(`  -H "Content-Type: application/json" \`)
	fmt.Println(`  -H "X-A2A-Extensions: https://example.com/extensions/sample-method/v1,https://example.com/extensions/sample-profile/v1" \`)
	fmt.Println(`  -d '{"jsonrpc":"2.0","method":"sample/ping","params":{"message":"test"},"id":1}'`)
	fmt.Println("")
	fmt.Println("🌐 Starting server on http://localhost:8080...")

	if err := server.Run(); err != nil {
		log.Fatal("❌ Server failed:", err)
	}
}