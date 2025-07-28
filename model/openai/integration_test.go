package openai

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveImageToFile saves base64 image data to testdata/dist directory and returns the path
func saveImageToFile(t *testing.T, base64Data, filename string) string {
	// Create testdata/dist directory
	distDir := filepath.Join("testdata", "dist")
	err := os.MkdirAll(distDir, 0755)
	require.NoError(t, err, "Failed to create testdata/dist directory")

	// Create subdirectory with sanitized test name and timestamp
	testDir := filepath.Join(distDir, "create_edit_workflow_"+fmt.Sprintf("%d", time.Now().Unix()))
	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test subdirectory")

	imagePath := filepath.Join(testDir, filename)

	imageBytes, err := base64.StdEncoding.DecodeString(base64Data)
	require.NoError(t, err, "Failed to decode base64 image")

	err = os.WriteFile(imagePath, imageBytes, 0644)
	require.NoError(t, err, "Failed to write image file")

	return imagePath
}

func TestOpenAIImageGeneration_Integration(t *testing.T) {
	// Skip if OPENAI_API_KEY is not set
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY environment variable not set, skipping integration test")
	}

	// Create a real provider (no mock)
	provider := &ModelProvider{}

	t.Run("Create → Edit workflow test", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		imageModel, err := provider.GetModel(ctx, "gpt-image-1")
		require.NoError(t, err)

		// Step 1: Create initial image
		t.Logf("Step 1: Generating initial image...")
		createMessages := []a2a.Message{
			a2a.NewMessage("user-create", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("A simple blue square on white background"),
			}),
		}

		createResponse, err := imageModel.Generate(ctx, &model.GenerateRequest{
			Messages: createMessages,
		})
		require.NoError(t, err, "Failed to create initial image")
		require.Len(t, createResponse.Message.Parts, 1)

		// Validate and save created image
		createdImagePart := createResponse.Message.Parts[0]
		require.Equal(t, a2a.KindFilePart, createdImagePart.Kind)
		require.NotNil(t, createdImagePart.File)
		require.Equal(t, "image/png", createdImagePart.File.MimeType)

		createdImagePath := saveImageToFile(t, createdImagePart.File.Bytes, "01_created.png")
		t.Logf("✅ Created image saved to: %s", createdImagePath)

		// Step 2: Edit the created image
		t.Logf("Step 2: Editing the created image...")
		editMessages := []a2a.Message{
			a2a.NewMessage("user-edit", a2a.RoleUser, []a2a.Part{
				a2a.NewTextPart("add a red circle in the center of this image"),
				a2a.NewFilePartWithBytes(createdImagePart.File.Bytes, "input.png", "image/png"),
			}),
		}

		editResponse, err := imageModel.Generate(ctx, &model.GenerateRequest{
			Messages: editMessages,
		})
		require.NoError(t, err, "Failed to edit image")
		require.Len(t, editResponse.Message.Parts, 1)

		// Validate and save edited image
		editedImagePart := editResponse.Message.Parts[0]
		require.Equal(t, a2a.KindFilePart, editedImagePart.Kind)
		require.NotNil(t, editedImagePart.File)
		require.Equal(t, "image/png", editedImagePart.File.MimeType)
		require.Equal(t, "edited.png", editedImagePart.File.Name)

		editedImagePath := saveImageToFile(t, editedImagePart.File.Bytes, "02_edited.png")
		t.Logf("✅ Edited image saved to: %s", editedImagePath)

		// Verify images are different
		assert.NotEqual(t, createdImagePart.File.Bytes, editedImagePart.File.Bytes, "Edited image should be different from original")

		t.Logf("🎉 Create → Edit workflow completed successfully!")
		t.Logf("📂 Check the images at:")
		t.Logf("   Original: %s", createdImagePath)
		t.Logf("   Edited:   %s", editedImagePath)
	})
}
