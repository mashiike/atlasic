package openai

import (
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsImageMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/webp", true},
		{"text/plain", false},
		{"application/json", false},
		{"", false},
	}

	for _, test := range tests {
		result := isImageMimeType(test.mimeType)
		assert.Equal(t, test.expected, result, "mimeType: %s", test.mimeType)
	}
}

func TestBase64ToReader(t *testing.T) {
	// Test valid base64 data
	testData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChAI9aANII0AAAABJRU5ErkJggg=="

	reader, err := base64ToReader(testData)
	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Check if it implements the required methods
	if imageReader, ok := reader.(*imageReader); ok {
		assert.Equal(t, "image/png", imageReader.ContentType())
		assert.Equal(t, "input.png", imageReader.Filename())
	} else {
		t.Error("Reader should be of type *imageReader")
	}
}

func TestExtractTextPrompt(t *testing.T) {
	model := &ImageModel{}

	messages := []a2a.Message{
		a2a.NewMessage("user1", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Hello"),
			a2a.NewTextPart("World"),
		}),
		a2a.NewMessage("agent1", a2a.RoleAgent, []a2a.Part{
			a2a.NewTextPart("This should be ignored"),
		}),
		a2a.NewMessage("user2", a2a.RoleUser, []a2a.Part{
			a2a.NewTextPart("Test"),
		}),
	}

	result := model.extractTextPrompt(messages)
	assert.Equal(t, "Hello World Test", result)
}
