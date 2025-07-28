package openai

import (
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextModel_Basic(t *testing.T) {
	textModel := &TextModel{
		modelID: "gpt-4o",
		client:  nil, // We'll test basic functionality without client
	}

	// Test ID method
	assert.Equal(t, "gpt-4o", textModel.ID())

	// Test InputModes
	inputModes := textModel.InputModes()
	assert.Contains(t, inputModes, "text/plain")
	assert.Contains(t, inputModes, "image/png")
	assert.Contains(t, inputModes, "image/jpeg")
	assert.Contains(t, inputModes, "image/webp")

	// Test OutputModes
	outputModes := textModel.OutputModes()
	assert.Contains(t, outputModes, "text/plain")
}

func TestTextModel_ConvertMessage(t *testing.T) {
	textModel := &TextModel{
		modelID: "gpt-4o",
		client:  nil,
	}

	t.Run("user message with text", func(t *testing.T) {
		msg := a2a.NewMessage("user1", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Hello, how are you?"),
		})

		openaiMsgs, err := textModel.convertMessage(msg)
		require.NoError(t, err)
		require.Len(t, openaiMsgs, 1)

		// Extract user message from union
		userMsg := openaiMsgs[0].OfUser
		require.NotNil(t, userMsg)
		assert.Equal(t, "Hello, how are you?", userMsg.Content.OfString.Value)
	})

	t.Run("assistant message with text", func(t *testing.T) {
		msg := a2a.NewMessage("agent1", a2a.RoleAgent, []a2a.Part{
			a2a.NewTextPart("I'm doing well, thank you!"),
		})

		openaiMsgs, err := textModel.convertMessage(msg)
		require.NoError(t, err)
		require.Len(t, openaiMsgs, 1)

		// Extract assistant message from union
		assistantMsg := openaiMsgs[0].OfAssistant
		require.NotNil(t, assistantMsg)
		assert.Equal(t, "I'm doing well, thank you!", assistantMsg.Content.OfString.Value)
	})

	t.Run("assistant message with tool use", func(t *testing.T) {
		toolUsePart := model.NewToolUsePart("call_123", "get_weather", map[string]interface{}{
			"location": "Tokyo",
		})

		msg := a2a.NewMessage("agent1", a2a.RoleAgent, []a2a.Part{
			a2a.NewTextPart("Let me check the weather for you."),
			toolUsePart,
		})

		openaiMsgs, err := textModel.convertMessage(msg)
		require.NoError(t, err)
		require.Len(t, openaiMsgs, 1)

		// Extract assistant message from union
		assistantMsg := openaiMsgs[0].OfAssistant
		require.NotNil(t, assistantMsg)
		assert.Equal(t, "Let me check the weather for you.", assistantMsg.Content.OfString.Value)
		assert.Len(t, assistantMsg.ToolCalls, 1)

		toolCall := assistantMsg.ToolCalls[0]
		assert.Equal(t, "call_123", toolCall.ID)
		assert.Equal(t, "get_weather", toolCall.Function.Name)
		assert.Contains(t, toolCall.Function.Arguments, "Tokyo")
	})

	t.Run("user message with tool result", func(t *testing.T) {
		toolResultPart := model.NewToolResultPart("call_123", "get_weather", []a2a.Part{
			a2a.NewTextPart("The weather in Tokyo is sunny, 25°C"),
		})

		msg := a2a.NewMessage("user1", a2a.RoleUser, []a2a.Part{
			toolResultPart,
		})

		openaiMsgs, err := textModel.convertMessage(msg)
		require.NoError(t, err)
		require.Len(t, openaiMsgs, 1)

		// Extract tool message from union
		toolMsg := openaiMsgs[0].OfTool
		require.NotNil(t, toolMsg)
		assert.Equal(t, "The weather in Tokyo is sunny, 25°C", toolMsg.Content.OfString.Value)
		assert.Equal(t, "call_123", toolMsg.ToolCallID)
	})

	t.Run("user message with text and image (placeholder)", func(t *testing.T) {
		imagePart := a2a.NewFilePartWithBytes("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChAI9aANII0AAAABJRU5ErkJggg==", "test.png", "image/png")

		msg := a2a.NewMessage("user1", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("What's in this image?"),
			imagePart,
		})

		openaiMsgs, err := textModel.convertMessage(msg)
		require.NoError(t, err)
		require.Len(t, openaiMsgs, 1)

		// Currently converts to simple text message with image placeholder
		// TODO: Update when proper vision support is implemented
		userMsg := openaiMsgs[0].OfUser
		require.NotNil(t, userMsg)

		// Should contain both text and image placeholder
		content := userMsg.Content.OfString
		require.NotNil(t, content)
		assert.Contains(t, content.Value, "What's in this image?")
		assert.Contains(t, content.Value, "[Image: test.png]")
	})
}
