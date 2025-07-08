# LLM Agent Example

This example demonstrates a basic LLM agent with ReAct (Reasoning-Action-Observation) loop capabilities.

## Features

- **ReAct Loop**: The agent can reason about tasks, take actions using tools, and observe results
- **Built-in Tools**:
  - `add_message`: Add messages to the conversation
  - `final`: End the task with completion or input-required status
  - `delegate_to_agent`: Delegate to sub-agents (stub implementation)

## Running the Example

1. Start Ollama with a compatible model:
```bash
ollama pull llama3.2:1b
ollama serve
```

2. Run the example server:
```bash
cd _examples/llm_agent
go run main.go
```

3. Test with a message:
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "parts": [{"kind": "text", "text": "Hello! Can you help me write a simple greeting message?"}]
    }
  }'
```

## How it Works

1. **Reasoning**: The LLM analyzes the current task and conversation history
2. **Action**: Based on reasoning, the LLM chooses and executes appropriate tools
3. **Observation**: Results are added to the conversation and the loop continues
4. **Termination**: The loop ends when the `final` tool is called or max iterations are reached

The agent will automatically use tools to accomplish tasks and provide structured responses.