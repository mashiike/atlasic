# ATLASIC

[![Go Reference](https://pkg.go.dev/badge/github.com/mashiike/atlasic.svg)](https://pkg.go.dev/github.com/mashiike/atlasic)
[![Go Report Card](https://goreportcard.com/badge/github.com/mashiike/atlasic)](https://goreportcard.com/report/github.com/mashiike/atlasic)

ATLASIC (A2A Toolkit Library to build Agent Service for Infrastructure on Cloud) is a Go library implementing the Agent-to-Agent (A2A) communication protocol. It provides a complete toolkit for building HTTP-based agent services with standardized messaging, task management, and event streaming capabilities.

## Features

- **A2A Protocol Implementation**: Full compliance with Agent-to-Agent communication specification
- **HTTP Server**: Ready-to-use server with JSON-RPC and Server-Sent Events
- **Storage Backends**: Local filesystem and AWS S3 adapters
- **Job Queue**: In-memory and AWS SQS adapters for distributed processing
- **Event Sourcing**: Complete task history with optimistic concurrency control
- **Streaming Support**: Real-time agent communication with SSE
- **Content Negotiation**: Flexible output format handling

## Quick Start

```go
package main

import (
    "context"
    "log"
    
    "github.com/mashiike/atlasic"
    "github.com/mashiike/atlasic/a2a"
)

func main() {
    // Define your agent
    agent := atlasic.NewAgent(
        a2a.AgentMetadata{
            Provider: "example-org",
            Version:  "1.0.0",
        },
        func(ctx context.Context, handle atlasic.TaskHandle) error {
            // Add your agent logic here
            return handle.AddMessage(ctx, []a2a.Part{
                a2a.NewTextPart("Hello from my agent!"),
            })
        },
    )

    // Start server
    server := &atlasic.Server{
        Addr:  ":8080",
        Agent: agent,
    }
    
    log.Fatal(server.RunWithContext(context.Background()))
}
```

## Storage Options

### Local Development

```go
// Uses local filesystem storage (default)
server := &atlasic.Server{
    Addr:  ":8080",
    Agent: agent,
}
```

### AWS S3 Storage

```go
import "github.com/mashiike/atlasic/awsadp"

storage := awsadp.NewS3Storage(awsadp.S3StorageConfig{
    Client: s3Client,
    Bucket: "my-atlasic-bucket",
})

server := &atlasic.Server{
    Addr:    ":8080",
    Agent:   agent,
    Storage: storage,
}
```

### AWS SQS JobQueue

```go
import "github.com/mashiike/atlasic/awsadp"

jobQueue, _ := awsadp.NewSQSJobQueue(awsadp.SQSJobQueueConfig{
    Client:   sqsClient,
    QueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
})

server := &atlasic.Server{
    Addr:     ":8080",
    Agent:    agent,
    Storage:  storage,
    JobQueue: jobQueue,
}
```

## API Usage

### Send Message

```bash
curl -X POST http://localhost:8080/message/send \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "messageId": "msg-1",
      "role": "user",
      "parts": [{"kind": "text", "text": "Hello"}]
    }
  }'
```

### Stream Messages

```bash
curl -N http://localhost:8080/message/stream \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "messageId": "msg-2", 
      "role": "user",
      "parts": [{"kind": "text", "text": "Hello"}]
    }
  }'
```

## Development

### Prerequisites

- [Go 1.21+](https://golang.org/dl/)
- [Task](https://taskfile.dev/installation/) (optional, for simplified commands)
- [Docker](https://www.docker.com/) (for integration tests)

### Quick Development Setup

```bash
# Install dependencies
task deps

# Run unit tests
task test

# Run integration tests (includes S3Storage + SQSJobQueue)
task test:integration

# Run all quality checks
task check

# Generate coverage report
task test:coverage
```

### Available Commands

```bash
# Core development
task build          # Build the project
task test            # Run unit tests
task test:integration # Run integration tests (starts Docker services)
task test:coverage   # Run tests with coverage report

# Code quality
task fmt             # Format code
task lint            # Run linter
task check           # Run all quality checks

# Docker services (for integration tests)
task docker:up       # Start minio + ElasticMQ
task docker:down     # Stop services
task docker:restart  # Restart services

# Cleanup
task clean           # Clean build artifacts
```

### Manual Testing (without Task)

```bash
# Unit tests only
go test -short ./...

# Integration tests (requires Docker services)
docker-compose up -d
go test ./awsadp -v
docker-compose down
```

## License

MIT License. See [LICENSE](LICENSE) for details.
