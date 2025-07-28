package openai

import (
	"context"
	"testing"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelProviderBasic(t *testing.T) {
	provider := &ModelProvider{}

	t.Run("gpt-image-1 model creation", func(t *testing.T) {
		// Set a mock client to avoid real API calls
		mockClient := &mockClient{}
		provider.SetClient(mockClient)

		model, err := provider.GetModel(context.Background(), "gpt-image-1")
		require.NoError(t, err)
		assert.Equal(t, "gpt-image-1", model.ID())

		// Check InputModes
		inputModes := model.InputModes()
		expectedInputModes := []string{"text/plain", "image/png", "image/jpeg", "image/webp"}
		assert.Equal(t, expectedInputModes, inputModes)

		// Check OutputModes
		outputModes := model.OutputModes()
		expectedOutputModes := []string{"image/png"} // GPT Image 1 outputs PNG only
		assert.Equal(t, expectedOutputModes, outputModes)
	})

	t.Run("dall-e-3 model creation", func(t *testing.T) {
		mockClient := &mockClient{}
		provider.SetClient(mockClient)

		model, err := provider.GetModel(context.Background(), "dall-e-3")
		require.NoError(t, err)
		assert.Equal(t, "dall-e-3", model.ID())
	})

	t.Run("unknown model defaults to TextModel", func(t *testing.T) {
		mockClient := &mockClient{}
		provider.SetClient(mockClient)

		model, err := provider.GetModel(context.Background(), "unknown-model")
		require.NoError(t, err)
		assert.Equal(t, "unknown-model", model.ID())
		// Should be TextModel (not ImageModel)
		_, isTextModel := model.(*TextModel)
		assert.True(t, isTextModel, "Unknown model should be TextModel")
	})

	t.Run("empty model ID", func(t *testing.T) {
		mockClient := &mockClient{}
		provider.SetClient(mockClient)

		_, err := provider.GetModel(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model ID cannot be empty")
	})
}

// Simple mock client for basic testing
type mockClient struct{}

func (m *mockClient) GetImages() *openai.ImageService {
	return nil
}

func (m *mockClient) GetChat() *openai.ChatService {
	return nil
}
