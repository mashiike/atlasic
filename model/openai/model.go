// Package openai provides integration with OpenAI for image generation using GPT Image 1.
package openai

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/mashiike/atlasic/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func init() {
	p := &ModelProvider{}
	model.Register("openai", p)
}

// OpenAIClient interface for dependency injection and testing
type OpenAIClient interface {
	GetImages() *openai.ImageService
	GetChat() *openai.ChatService
}

// ClientAdapter wraps the official OpenAI client to implement our interface
type ClientAdapter struct {
	client *openai.Client
}

func (a *ClientAdapter) GetImages() *openai.ImageService {
	return &a.client.Images
}

func (a *ClientAdapter) GetChat() *openai.ChatService {
	return &a.client.Chat
}

type ModelProvider struct {
	once    sync.Once
	client  OpenAIClient
	loadErr error
}

// SetClient sets the OpenAI client for dependency injection
func (p *ModelProvider) SetClient(client OpenAIClient) {
	p.once.Do(func() {
		p.client = client
	})
}

// GetClient returns the OpenAI client, initializing it if necessary
func (p *ModelProvider) GetClient() (OpenAIClient, error) {
	p.once.Do(func() {
		if p.client != nil {
			return
		}

		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			p.loadErr = errors.New("OPENAI_API_KEY environment variable is required")
			return
		}

		realClient := openai.NewClient(option.WithAPIKey(apiKey))
		p.client = &ClientAdapter{client: &realClient}
	})

	if p.loadErr != nil {
		return nil, p.loadErr
	}
	if p.client == nil {
		return nil, errors.New("openai client is not initialized")
	}
	return p.client, nil
}

// GetModel returns a model instance for the given model ID
func (p *ModelProvider) GetModel(ctx context.Context, modelID string) (model.Model, error) {
	if modelID == "" {
		return nil, errors.New("model ID cannot be empty")
	}

	client, err := p.GetClient()
	if err != nil {
		return nil, err
	}

	// Support different model types
	switch modelID {
	case "gpt-image-1", "dall-e-3":
		return &ImageModel{
			modelID: modelID,
			client:  client,
		}, nil
	default:
		// Default to TextModel for all other models
		return &TextModel{
			modelID: modelID,
			client:  client,
		}, nil
	}
}
