# Extension System Demo

This sample demonstrates the three types of ATLASIC Extension system in action.

## Extension Types

### 1. DataOnlyExtension
**Purpose**: Inject additional information into AgentCard  
**Implementation**: `SampleDataOnlyExtension`  
**Effect**: Additional fields like `customCapabilities` appear in `/.well-known/agent.json`

### 2. RPCMethodExtension  
**Purpose**: Add custom JSON-RPC methods  
**Implementation**: `SampleMethodExtension`  
**Added Method**: `sample/ping`  
**Effect**: `sample/ping` method is only available when extension is activated

### 3. ProfileExtension
**Purpose**: Pre-process requests with method-specific interfaces  
**Implementation**: `SampleProfileExtension` (implements `SendMessageProfileExtension`)  
**Effect**: Context is enriched with extension metadata before method execution

## Usage

### Start Server
```bash
cd _examples/extensions
go run main.go
```

### Testing Methods

#### 1. Check AgentCard (DataOnlyExtension)
```bash
curl http://localhost:8080/.well-known/agent.json | jq
```

Expected result:
```json
{
  "name": "Extension Demo Agent",
  "description": "Demonstration agent showing extension capabilities",
  "version": "1.0.0",
  "protocolVersion": "0.2.5",
  "customCapabilities": {
    "supportedLanguages": ["en", "ja", "es"],
    "maxFileSize": 2097152,
    "features": ["multilingual", "file-upload", "premium"]
  },
  "serviceLevel": "premium",
  "extensionVersion": "1.0.0"
}
```

#### 2. Method Call Without Extension
```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"sample/ping","params":{"message":"test"},"id":1}'
```

Expected result: Method Not Found error

#### 3. Method Call With Extension Enabled
```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -H "X-A2A-Extensions: https://example.com/extensions/sample-method/v1" \
  -d '{"jsonrpc":"2.0","method":"sample/ping","params":{"message":"test"},"id":1}'
```

Expected result:
```json
{
  "jsonrpc": "2.0",
  "result": {
    "response": "Pong: test",
    "timestamp": "2025-07-04T12:00:00Z",
    "method": "sample/ping"
  },
  "id": 1
}
```

#### 4. Method Call With ProfileExtension Enabled
```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -H "X-A2A-Extensions: https://example.com/extensions/sample-method/v1,https://example.com/extensions/sample-profile/v1" \
  -d '{"jsonrpc":"2.0","method":"sample/ping","params":{"message":"test"},"id":1}'
```

Expected result:
```json
{
  "jsonrpc": "2.0",
  "result": {
    "response": "Pong: test",
    "timestamp": "2025-07-04T12:00:00Z",
    "method": "sample/ping"
  },
  "id": 1
}
```

Note: ProfileExtension enriches the context during preprocessing but doesn't modify the response. The extension metadata is available in the request context during execution.

## Extension Activation

Extensions are activated using the `X-A2A-Extensions` header:

- **Single Extension**: `X-A2A-Extensions: https://example.com/extensions/sample-method/v1`
- **Multiple Extensions**: `X-A2A-Extensions: https://example.com/extensions/sample-method/v1,https://example.com/extensions/sample-profile/v1`

Activated extensions can be confirmed in the response header:
```
X-A2A-Extensions: https://example.com/extensions/sample-method/v1, https://example.com/extensions/sample-profile/v1
```

## Architecture

```
Handler
├── DataOnlyExtension → AgentCard enrichment
├── RPCMethodExtension → Custom JSON-RPC methods
└── ProfileExtension → Request/Response processing
```

Each extension operates independently and can be used in combination.