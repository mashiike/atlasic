# Local A2A Agent Example

This example demonstrates how to quickly set up a local A2A server using ATLASIC's simplified Server API with Ollama. It includes both Docker Compose setup and manual installation options.

## Quick Start with Docker Compose (Recommended)

The easiest way to run the complete A2A server with Ollama integration:

### Prerequisites
- Docker & Docker Compose
- Go 1.21+ (for client testing)

### Setup & Run
```bash
# Start server in background
docker compose up -d

# Check status
docker compose ps

# Pull lightweight model (will happen automatically)
docker exec atlasic-ollama ollama pull llama3.2:1b

# Test with integrated client
cd ../client
go run main.go

# Stop services when done
cd ../local
docker compose down
```

### What's included:
- **Ollama Service**: LLM server with llama3.2:1b model
- **ATLASIC Server**: A2A protocol server on port 8080
- **Automatic Model Setup**: Downloads required models
- **Health Checks**: Ensures services are ready before startup
- **Persistent Storage**: Data preserved across restarts

## Manual Setup (Alternative)

If you prefer to run without Docker:

### Prerequisites
- Go 1.21+
- Ollama with llama3.2:1b model installed

### Setup & Run
```bash
# Install and start Ollama
ollama serve &
ollama pull llama3.2:1b

# Run the server
go run main.go
```

## Testing the Server

### Option 1: Integrated Client (Recommended)
```bash
cd ../client
go run main.go
```

This runs a comprehensive test that:
- Fetches the agent card
- Sends a message to create a task
- Waits for completion
- Displays the conversation history

### Option 2: Manual API Testing

#### Get Agent Card
```bash
curl http://localhost:8080/.well-known/agent.json
```

#### Send a message
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

#### Stream messages
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

## Architecture Overview

### Docker Compose Services
- **ollama**: Ollama LLM server (port 11434)
- **atlasic-server**: ATLASIC A2A server (port 8080)

### Configuration
The server configuration is optimized for Docker environments:
- **Storage**: `/tmp/a2a` inside container
- **Ollama Host**: `http://ollama:11434` (container networking)
- **Job Queue**: In-memory queue with 100 capacity
- **Model**: llama3.2:1b (lightweight, 1.3GB)

## Features

- **Simplified Server API**: Uses ATLASIC's new `Server` type for minimal configuration
- **Docker Integration**: Complete environment setup with one command
- **Local LLM**: Uses Ollama for running models locally
- **A2A Protocol**: Full compliance with Agent-to-Agent protocol
- **Event Sourcing**: All task events are stored and can be replayed
- **Auto Configuration**: Storage and job queue configured automatically
- **Graceful Shutdown**: Proper cleanup on signals
- **Memory Optimized**: Uses lightweight llama3.2:1b model
- **Health Checks**: Ensures all services are ready
- **Client Integration**: Comprehensive testing with transport.Client

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
- `/tmp/a2a/tasks/` - Task snapshots
- `/tmp/a2a/events/` - Event streams for each context

In Docker, data is persisted using named volumes.

## Environment Variables

- `A2A_STORAGE_DIR`: Override storage location (default: `/tmp/a2a`)
- `OLLAMA_HOST`: Ollama API endpoint (default: `http://ollama:11434` in Docker)

## Troubleshooting

### Memory Issues
If you encounter "model requires more system memory" errors:
```bash
# Use the lighter model
docker exec atlasic-ollama ollama pull llama3.2:1b
# Restart server
docker compose restart atlasic-server
```

### Service Health
Check service status:
```bash
docker compose ps
docker compose logs ollama
docker compose logs atlasic-server
```

### Model List
Check available models:
```bash
docker exec atlasic-ollama ollama list
```

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

## Client Development

The `../client` directory contains a complete example of using `transport.Client` for A2A communication. This demonstrates:

- Agent card retrieval
- Message sending with JSON-RPC
- Task status monitoring
- Conversation history access
- Error handling and retry logic

This client can be used as a foundation for building RemoteAgent implementations or other A2A clients.