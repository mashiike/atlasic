package openai

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

// extractTextPrompt extracts text content from messages to create a prompt
func (m *ImageModel) extractTextPrompt(messages []a2a.Message) string {
	var prompt strings.Builder

	for _, msg := range messages {
		// Focus on user messages for prompts
		if msg.Role == a2a.RoleUser {
			for _, part := range msg.Parts {
				if part.Kind == a2a.KindTextPart {
					if prompt.Len() > 0 {
						prompt.WriteString(" ")
					}
					prompt.WriteString(part.Text)
				}
			}
		}
	}

	return prompt.String()
}

// extractInputImages extracts image data from messages for editing
func (m *ImageModel) extractInputImages(messages []a2a.Message) []imageData {
	var images []imageData

	for _, msg := range messages {
		// Look for images in user messages
		if msg.Role == a2a.RoleUser {
			for _, part := range msg.Parts {
				if part.Kind == a2a.KindFilePart && part.File != nil {
					if isImageMimeType(part.File.MimeType) {
						// Convert base64 to bytes
						// Note: part.File.Bytes is already base64 encoded string
						images = append(images, imageData{
							data:     part.File.Bytes, // Keep as base64 string for consistency
							mimeType: part.File.MimeType,
							name:     part.File.Name,
						})
					}
				}
			}
		}
	}

	return images
}

// imageData represents image data for processing
type imageData struct {
	data     string // base64 encoded image data
	mimeType string
	name     string
}

// isImageMimeType checks if the mime type is supported for image input
func isImageMimeType(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/jpg", "image/webp":
		return true
	default:
		return false
	}
}

// downloadImageFromURL downloads an image from URL and returns base64 encoded data
func downloadImageFromURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	imageBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image data: %w", err)
	}

	return base64.StdEncoding.EncodeToString(imageBytes), nil
}

// imageReader wraps bytes.Reader with ContentType method for OpenAI SDK
type imageReader struct {
	*bytes.Reader
	contentType string
	filename    string
}

// ContentType returns the MIME type for multipart uploads
func (r *imageReader) ContentType() string {
	return r.contentType
}

// Filename returns the filename for multipart uploads
func (r *imageReader) Filename() string {
	return r.filename
}

// base64ToReader converts base64 string to io.Reader with proper MIME type for OpenAI API
func base64ToReader(base64Data string) (io.Reader, error) {
	imageBytes, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 image data: %w", err)
	}

	return &imageReader{
		Reader:      bytes.NewReader(imageBytes),
		contentType: "image/png", // GPT Image 1 works with PNG
		filename:    "input.png",
	}, nil
}
