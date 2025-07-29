package openai

import (
	"context"
	"fmt"

	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/model"
	"github.com/openai/openai-go"
)

// ImageModel implements the model.Model interface for OpenAI image generation
type ImageModel struct {
	modelID string
	client  OpenAIClient
}

// ID returns the model identifier
func (m *ImageModel) ID() string {
	return m.modelID
}

// InputModes returns the supported input modes for image generation
func (m *ImageModel) InputModes() []string {
	return []string{
		"text/plain", // Text prompts for generation
		"image/png",  // Input images for editing (up to 10 images)
		"image/jpeg", // Input images for editing
		"image/webp", // Input images for editing
	}
}

// OutputModes returns the supported output modes for image generation
func (m *ImageModel) OutputModes() []string {
	return []string{
		"image/png", // GPT Image 1 outputs PNG only
	}
}

// Generate generates or edits images based on the request
func (m *ImageModel) Generate(ctx context.Context, req *model.GenerateRequest) (*model.GenerateResponse, error) {
	// Extract input images from messages
	inputImages := m.extractInputImages(req.Messages)

	// Extract text prompt from messages
	textPrompt := m.extractTextPrompt(req.Messages)

	if textPrompt == "" {
		return nil, fmt.Errorf("text prompt is required for image generation")
	}

	// Determine mode based on input
	if len(inputImages) > 0 {
		// Mode 2: Text + Image → Image editing using Edit API
		return m.generateWithImages(ctx, textPrompt, inputImages)
	} else {
		// Mode 1: Text → Image generation using Generate API
		return m.generateFromText(ctx, textPrompt, req)
	}
}

// generateFromText generates images from text prompts (Mode 1) using Generate API
func (m *ImageModel) generateFromText(ctx context.Context, prompt string, req *model.GenerateRequest) (*model.GenerateResponse, error) {
	// Create OpenAI image generation request using official SDK
	params := openai.ImageGenerateParams{
		Prompt: prompt,
		Model:  m.modelID,
		N:      openai.Int(1),
	}

	// Apply extensions if provided
	if req != nil && req.Extensions != nil {
		if size, ok := req.Extensions["size"].(string); ok {
			params.Size = openai.ImageGenerateParamsSize(size)
		}
		if quality, ok := req.Extensions["quality"].(string); ok {
			params.Quality = openai.ImageGenerateParamsQuality(quality)
		}
		if style, ok := req.Extensions["style"].(string); ok {
			params.Style = openai.ImageGenerateParamsStyle(style)
		}
		if n, ok := req.Extensions["n"].(int); ok {
			params.N = openai.Int(int64(n))
		}
	}

	// Call OpenAI API using the Images service
	response, err := m.client.GetImages().Generate(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no images returned from OpenAI API")
	}

	// Get image data from response
	// The official SDK may return URL or base64 depending on format
	var imageData string
	if response.Data[0].B64JSON != "" {
		imageData = response.Data[0].B64JSON
	} else if response.Data[0].URL != "" {
		// Download image from URL and convert to base64
		var err error
		imageData, err = downloadImageFromURL(response.Data[0].URL)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no image data in response")
	}

	// Create file part for the generated image
	imagePart := a2a.NewFilePartWithBytes(imageData, "generated.png", "image/png")

	// Create response message
	responseMessage := a2a.NewMessage("openai-image", a2a.RoleAgent, []a2a.Part{imagePart})

	// Get resolution from generation params (default: 1024x1024)
	resolution := "1024x1024" // Default for GPT Image 1

	return &model.GenerateResponse{
		Message:    responseMessage,
		StopReason: model.StopReasonEndTurn,
		Usage: &model.Usage{
			// Image-specific usage information
			ImageRequests:   model.Ptr(1),
			ImageResolution: &resolution,
			ModelID:         m.modelID,
		},
		RawResponse: response, // Store raw OpenAI image response
	}, nil
}

// generateWithImages generates/edits images with text and image inputs (Mode 2) using Edit API
func (m *ImageModel) generateWithImages(ctx context.Context, prompt string, inputImages []imageData) (*model.GenerateResponse, error) {
	// Validate input images count (GPT Image 1 supports up to 10 images)
	if len(inputImages) > 10 {
		return nil, fmt.Errorf("too many input images: %d (maximum 10 supported)", len(inputImages))
	}

	// For image editing, we use the first image as the base image
	if len(inputImages) == 0 {
		return nil, fmt.Errorf("at least one input image is required for editing")
	}

	// Convert first image to io.Reader for Edit API
	firstImage := inputImages[0]
	imageReader, err := base64ToReader(firstImage.data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert image data: %w", err)
	}

	// Create OpenAI image edit request using proper Edit API
	params := openai.ImageEditParams{
		Image: openai.ImageEditParamsImageUnion{
			OfFile: imageReader,
		},
		Prompt:       prompt,
		Model:        openai.ImageModelGPTImage1, // Use GPT Image 1 for editing
		N:            openai.Int(1),
		OutputFormat: openai.ImageEditParamsOutputFormatPNG, // Default to PNG
		Quality:      openai.ImageEditParamsQualityAuto,     // Auto quality
		Background:   openai.ImageEditParamsBackgroundAuto,  // Auto background
	}

	// Call OpenAI Edit API
	response, err := m.client.GetImages().Edit(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to edit image: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no images returned from OpenAI Edit API")
	}

	// Get image data from response (GPT Image 1 always returns base64)
	imageData := response.Data[0].B64JSON
	if imageData == "" {
		return nil, fmt.Errorf("no base64 image data in edit response")
	}

	// Create file part for the generated image
	imagePart := a2a.NewFilePartWithBytes(imageData, "edited.png", "image/png")

	// Create response message
	responseMessage := a2a.NewMessage("openai-image-edit", a2a.RoleAgent, []a2a.Part{imagePart})

	// Extract usage information if available (GPT Image 1 provides usage data)
	usage := &model.Usage{
		ModelID:       m.modelID,
		ImageRequests: model.Ptr(len(inputImages)), // Number of images processed
	}

	if response.Usage.InputTokens > 0 || response.Usage.OutputTokens > 0 {
		usage.InputTokens = model.Ptr(int(response.Usage.InputTokens))
		usage.OutputTokens = model.Ptr(int(response.Usage.OutputTokens))
		usage.TotalTokens = model.Ptr(int(response.Usage.TotalTokens))
	}

	return &model.GenerateResponse{
		Message:     responseMessage,
		StopReason:  model.StopReasonEndTurn,
		Usage:       usage,
		RawResponse: response, // Store raw OpenAI edit response
	}, nil
}
