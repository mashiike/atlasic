# Local A2A Agent Example

This example demonstrates how to quickly set up a local A2A server using ATLASIC's simplified Server API with Ollama.

## Prerequisites

- Go 1.21+
- Ollama with llama3.2 model installed

## Quick Start

1. Install Ollama and pull the model:
```bash
ollama pull llama3.2
```

2. Run the server:
```bash
go run main.go
```

The server will start on `:8080` with default settings:
- **Storage**: `/tmp/a2a` (configurable via `A2A_STORAGE_DIR` environment variable)
- **Job Queue**: In-memory queue with 100 capacity
- **Graceful shutdown**: Handles `Ctrl+C` properly

3. Test the server:

### Get Agent Card
```bash
curl http://localhost:8080/.well-known/agent.json
```

### Send a message
```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "message/send",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"kind": "text", "text": "Hello, Agent!"}]
      }
    },
    "id": 1
  }'
```

### Stream messages
```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "method": "message/stream",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"kind": "text", "text": "Tell me a story"}]
      }
    },
    "id": 1
  }'
```

## Features

- **Simplified Server API**: Uses ATLASIC's new `Server` type for minimal configuration
- **Local LLM**: Uses Ollama for running models locally
- **A2A Protocol**: Full compliance with Agent-to-Agent protocol
- **Event Sourcing**: All task events are stored and can be replayed
- **Auto Configuration**: Storage and job queue configured automatically
- **Graceful Shutdown**: Proper cleanup on `Ctrl+C` signal
- **Simple Echo Agent**: Responds to user messages with friendly replies

## Code Overview

The example showcases the simplified ATLASIC Server API:

```go
agent := atlasic.NewAgent(metadata, agentFunc)

server := &atlasic.Server{
    Addr:  ":8080",
    Agent: agent,
    // Storage and JobQueue use sensible defaults
}

server.RunWithContext(ctx) // Blocks until shutdown
```

This replaces the previous manual setup of `AgentService`, `transport.Handler`, and `http.Server`.

## Storage

The example uses FileSystemStorage with the following structure:
- `/tmp/a2a/tasks/` - Task snapshots (default location)
- `/tmp/a2a/events/` - Event streams for each context

Override the storage location with:
```bash
A2A_STORAGE_DIR=/path/to/storage go run main.go
```

This allows for full task history and event replay capabilities.

## Customization

For advanced use cases, you can still configure individual components:

```go
server := &atlasic.Server{
    Addr:     ":8080",
    Agent:    agent,
    Storage:  customStorage,   // Optional: custom storage backend
    JobQueue: customJobQueue,  // Optional: custom job queue
}
```