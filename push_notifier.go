package atlasic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mashiike/atlasic/a2a"
)

// PushNotifier handles push notification delivery for task events
type PushNotifier interface {
	// Notify sends a push notification for the given event (at-least-once delivery)
	Notify(ctx context.Context, config a2a.PushNotificationConfig, event a2a.StreamResponse) error

	// ValidateEndpoint checks if the notification endpoint is valid and reachable
	ValidateEndpoint(ctx context.Context, config a2a.PushNotificationConfig) error

	// Close gracefully shuts down the notifier
	Close() error
}

// DefaultPushNotifier implements PushNotifier using standard HTTP client
type DefaultPushNotifier struct {
	Client *http.Client
}

// NewDefaultPushNotifier creates a new DefaultPushNotifier with default HTTP client
func NewDefaultPushNotifier() *DefaultPushNotifier {
	return &DefaultPushNotifier{
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Notify sends HTTP POST request to the configured URL with the event data
func (n *DefaultPushNotifier) Notify(ctx context.Context, config a2a.PushNotificationConfig, event a2a.StreamResponse) error {
	// Marshal event to JSON
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	// Set content type
	req.Header.Set("Content-Type", "application/json")

	// Add authentication if provided
	if config.Authentication != nil && config.Authentication.Credentials != "" {
		// Simple bearer token authentication
		req.Header.Set("Authorization", "Bearer "+config.Authentication.Credentials)
	}

	// Add token header if provided
	if config.Token != "" {
		req.Header.Set("X-A2A-Notification-Token", config.Token)
	}

	// Send request
	resp, err := n.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check for successful status codes (2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &PushNotificationError{
			StatusCode: resp.StatusCode,
			URL:        config.URL,
		}
	}

	return nil
}

// ValidateEndpoint performs no validation - always returns nil (no-check implementation)
func (n *DefaultPushNotifier) ValidateEndpoint(ctx context.Context, config a2a.PushNotificationConfig) error {
	// No validation in default implementation
	return nil
}

// Close gracefully shuts down the notifier
func (n *DefaultPushNotifier) Close() error {
	// Nothing to clean up for default implementation
	return nil
}

// PushNotificationError represents an error during push notification delivery
type PushNotificationError struct {
	StatusCode int
	URL        string
}

func (e *PushNotificationError) Error() string {
	return fmt.Sprintf("push notification failed: HTTP %d for URL %s", e.StatusCode, e.URL)
}
