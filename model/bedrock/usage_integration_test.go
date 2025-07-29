package bedrock

import (
	"context"
	"os"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	atlasicmodel "github.com/mashiike/atlasic/model"
	"github.com/stretchr/testify/require"
)

func TestModel_UsageMetrics_Integration(t *testing.T) {
	// Check if AWS integration test is enabled
	if os.Getenv("AWS_INTEGRATION_TEST") != "true" {
		t.Skip("AWS_INTEGRATION_TEST not set to 'true', skipping integration test")
	}

	ctx := context.Background()
	provider := &ModelProvider{}

	// Use Claude 4 Sonnet with US cross-region interface
	model, err := provider.GetModel(ctx, "us.anthropic.claude-sonnet-4-20250514-v1:0")
	require.NoError(t, err)

	req := &atlasicmodel.GenerateRequest{
		Messages: []a2a.Message{
			a2a.NewMessage("test-1", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("Say 'Hello World' in exactly two words."),
			}),
		},
	}

	resp, err := model.Generate(ctx, req)
	require.NoError(t, err)

	// Verify Usage metrics
	require.NotNil(t, resp.Usage)
	require.Equal(t, "us.anthropic.claude-sonnet-4-20250514-v1:0", resp.Usage.ModelID)
	require.NotNil(t, resp.Usage.InputTokens)
	require.NotNil(t, resp.Usage.OutputTokens)
	require.NotNil(t, resp.Usage.TotalTokens)
	require.Greater(t, *resp.Usage.InputTokens, 0)
	require.Greater(t, *resp.Usage.OutputTokens, 0)
	require.Greater(t, *resp.Usage.TotalTokens, 0)
	require.Equal(t, *resp.Usage.InputTokens+*resp.Usage.OutputTokens, *resp.Usage.TotalTokens)

	// Verify raw response is stored
	require.NotNil(t, resp.RawResponse)
	require.NotNil(t, resp.Usage.ProviderUsage)

	t.Logf("Bedrock Usage metrics: Input=%d, Output=%d, Total=%d",
		*resp.Usage.InputTokens, *resp.Usage.OutputTokens, *resp.Usage.TotalTokens)
	t.Logf("Response: %s", resp.Message.Parts[0].Text)
}
