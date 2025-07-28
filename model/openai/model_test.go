package openai_test

import (
	"context"
	"os"
	"testing"

	"github.com/mashiike/atlasic/model"
	_ "github.com/mashiike/atlasic/model/openai" // Import to trigger registration
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIProviderRegistration(t *testing.T) {
	ctx := context.Background()

	t.Run("provider is registered", func(t *testing.T) {
		providers := model.ListProviders()
		assert.Contains(t, providers, "openai", "OpenAI provider should be registered")
	})

	t.Run("can get provider", func(t *testing.T) {
		provider, err := model.GetProvider("openai")
		require.NoError(t, err, "Should be able to get OpenAI provider")
		assert.NotNil(t, provider, "Provider should not be nil")
	})

	t.Run("image model creation requires API key", func(t *testing.T) {
		// Skip this test if API key is already set (provider already initialized)
		if os.Getenv("OPENAI_API_KEY") != "" {
			t.Skip("OPENAI_API_KEY is set, cannot test error behavior")
		}

		// Without API key, model creation should fail with appropriate error
		_, err := model.GetModel(ctx, "openai", "gpt-image-1")
		assert.Error(t, err, "Should error without API key")
		if err != nil {
			assert.Contains(t, err.Error(), "OPENAI_API_KEY environment variable is required")
		}
	})

	t.Run("text model creation requires API key", func(t *testing.T) {
		// Skip this test if API key is already set (provider already initialized)
		if os.Getenv("OPENAI_API_KEY") != "" {
			t.Skip("OPENAI_API_KEY is set, cannot test error behavior")
		}

		// Without API key, text model creation should also fail
		_, err := model.GetModel(ctx, "openai", "gpt-4o")
		assert.Error(t, err, "Should error without API key")
		if err != nil {
			assert.Contains(t, err.Error(), "OPENAI_API_KEY environment variable is required")
		}
	})

	t.Run("unknown model defaults to text model and requires API key", func(t *testing.T) {
		// Skip this test if API key is already set (provider already initialized)
		if os.Getenv("OPENAI_API_KEY") != "" {
			t.Skip("OPENAI_API_KEY is set, cannot test error behavior")
		}

		// Without API key, unknown models should default to TextModel and still require API key
		_, err := model.GetModel(ctx, "openai", "gpt-5-unknown")
		assert.Error(t, err, "Should error without API key")
		if err != nil {
			assert.Contains(t, err.Error(), "OPENAI_API_KEY environment variable is required")
		}
	})
}
