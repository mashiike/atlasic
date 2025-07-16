package atlasic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummarizerInterface(t *testing.T) {
	// Test that LLMSummarizer satisfies the interface
	var _ Summarizer = (*LLMSummarizer)(nil)

	// Test constructor function
	llmSummarizer := NewLLMSummarizer("test-provider", "test-model")
	require.NotNil(t, llmSummarizer)
	require.Equal(t, "test-provider", llmSummarizer.ModelProvider)
	require.Equal(t, "test-model", llmSummarizer.ModelID)
	require.Equal(t, 500, llmSummarizer.MaxTokens) // default value
}
