# A2A Client Example

This example demonstrates how to use ATLASIC's `transport.Client` to communicate with A2A servers. It provides a comprehensive test of the A2A protocol implementation.

## Prerequisites

- Go 1.21+
- Running A2A server (see `../local` for setup)

## Quick Start

### With Docker Compose Server
```bash
# Start server (from ../local directory)
cd ../local && docker compose up -d

# Run client test
cd ../client
go run main.go
```

### With Manual Server
```bash
# Start server manually (from ../local directory)
cd ../local && go run main.go

# Run client test (in another terminal)
cd ../client
go run main.go
```

## What it does

The client performs a comprehensive A2A protocol test:

1. **Agent Card Retrieval**: Fetches server information from `/.well-known/agent.json`
2. **Message Sending**: Sends a user message using `message/send` JSON-RPC method
3. **Task Monitoring**: Polls task status using `tasks/get` method
4. **History Display**: Shows complete conversation history
5. **Status Reporting**: Provides detailed logging of all operations

## Sample Output

```
time=2025-07-03T01:55:50.933+09:00 level=INFO msg="A2A Client starting" serverURL=http://localhost:8080
time=2025-07-03T01:55:50.933+09:00 level=INFO msg="Step 1: Fetching agent card..."
time=2025-07-03T01:55:50.938+09:00 level=INFO msg="Agent card retrieved" name="Local Agent" version=1.0.0 description="An agent that runs tasks locally" skills=1
time=2025-07-03T01:55:50.938+09:00 level=INFO msg="Step 2: Sending message to create new task..."
time=2025-07-03T01:55:53.942+09:00 level=INFO msg="Message sent successfully" taskID=0197cc10-eb58-7b08-b0b3-a70fbea0d7ca contextID=example-context-1751475350 taskState=completed
time=2025-07-03T01:55:55.946+09:00 level=INFO msg="Task completed!" finalState=completed
time=2025-07-03T01:55:55.946+09:00 level=INFO msg="=== Conversation History ==="
time=2025-07-03T01:55:55.946+09:00 level=INFO msg=Message index=0 role=user messageID=user-msg-1751475350938361000 text="[Hello, Agent! Please echo this message back to me.]"
time=2025-07-03T01:55:55.946+09:00 level=INFO msg=Message index=1 role=agent messageID=0197cc10-f5f8-7866-b69b-4fb21a01b421 text="[Hello, User!]"
time=2025-07-03T01:55:55.946+09:00 level=INFO msg="✅ Client-Server communication test completed successfully!"
```

## Configuration

The client can be configured via environment variables:

- `A2A_SERVER_URL`: Server URL (default: `http://localhost:8080`)

Example:
```bash
A2A_SERVER_URL=http://localhost:9000 go run main.go
```

## Code Overview

### transport.Client Usage

```go
// Create client
client := transport.NewClient(serverURL,
    transport.WithClientLogger(slog.Default()),
    transport.WithUserAgent("ATLASIC-Example-Client/1.0"),
)

// Get agent card
agentCard, err := client.GetAgentCard(ctx)

// Send message
sendResp, err := client.SendMessage(ctx, a2a.MessageSendParams{
    Message: a2a.Message{
        Kind:      a2a.KindMessage,
        MessageID: messageID,
        ContextID: contextID,
        Role:      a2a.RoleUser,
        Parts:     []a2a.Part{a2a.NewTextPart("Hello, Agent!")},
    },
})

// Get task with full history
task, err := client.GetTask(ctx, a2a.TaskQueryParams{
    ID:            taskID,
    HistoryLength: &historyLengthAll,
})
```

### Supported Methods

The client implements all major A2A protocol methods:

- `GetAgentCard()`: Retrieve agent information
- `SendMessage()`: Send messages to agents
- `GetTask()`: Retrieve task status and history
- `CancelTask()`: Cancel running tasks
- `SetTaskPushNotificationConfig()`: Configure push notifications
- `GetTaskPushNotificationConfig()`: Get push notification settings

## Features

- **Complete A2A Protocol Support**: All major JSON-RPC methods implemented
- **Content Negotiation**: Proper Accept/Content-Type handling
- **Error Handling**: JSON-RPC error parsing and reporting
- **Debug Logging**: Detailed request/response logging
- **Timeout Handling**: Configurable request timeouts
- **Context Support**: Proper cancellation handling
- **Type Safety**: Full Go type safety with A2A protocol structures

## Development Use Cases

This client example is useful for:

1. **Protocol Testing**: Verify A2A server implementations
2. **Integration Testing**: Test server-client communication
3. **RemoteAgent Development**: Foundation for building agents that call other agents
4. **API Exploration**: Learn the A2A protocol interactively
5. **Performance Testing**: Measure request/response times
6. **Debugging**: Inspect server behavior with detailed logging

## Error Handling

The client handles various error scenarios:

- **Connection Errors**: Server unavailable or network issues
- **JSON-RPC Errors**: Protocol-level errors from the server
- **Timeout Errors**: Request timeout handling
- **Parse Errors**: Invalid response format handling

Example error output:
```
time=2025-07-03T01:55:50.933+09:00 level=ERROR msg="Failed to get agent card" error="HTTP request failed: dial tcp [::1]:8080: connect: connection refused"
```

## Extending the Client

To add new functionality:

1. **Add Methods**: Implement additional JSON-RPC methods in `transport.Client`
2. **Custom Configuration**: Add new client options via `ClientOption` functions
3. **Specialized Clients**: Create domain-specific clients that wrap `transport.Client`
4. **Middleware**: Add request/response middleware for logging, metrics, etc.

Example custom method:
```go
func (c *Client) CustomMethod(ctx context.Context, params CustomParams) (*CustomResult, error) {
    resp, err := c.sendJSONRPCRequest(ctx, "custom/method", params)
    if err != nil {
        return nil, err
    }
    
    var result CustomResult
    if err := json.Unmarshal(resp.Result, &result); err != nil {
        return nil, err
    }
    
    return &result, nil
}
```

## RemoteAgent Foundation

This client provides the foundation for implementing RemoteAgent, which would:

1. **Agent Discovery**: Use `GetAgentCard()` to discover remote agents
2. **Task Delegation**: Use `SendMessage()` to delegate tasks to remote agents
3. **Progress Monitoring**: Use `GetTask()` to monitor remote task progress
4. **Result Retrieval**: Extract results from completed remote tasks
5. **Error Recovery**: Handle remote agent failures and retries

The transport.Client abstraction makes it easy to build higher-level agent coordination systems.