package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/mashiike/atlasic/a2a"
)

// ClientOption defines configuration option for Client
type ClientOption func(*clientConfig)

// clientConfig holds internal configuration for Client
type clientConfig struct {
	httpClient           *http.Client
	rpcPath              string
	agentCardPath        string
	logger               *slog.Logger
	userAgent            string
	defaultRequestTimeout time.Duration
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *clientConfig) {
		c.httpClient = client
	}
}

// WithClientRPCPath sets the JSON-RPC endpoint path (default: "/")
func WithClientRPCPath(path string) ClientOption {
	return func(c *clientConfig) {
		c.rpcPath = path
	}
}

// WithClientAgentCardPath sets the agent card endpoint path (default: "/.well-known/agent.json")
func WithClientAgentCardPath(path string) ClientOption {
	return func(c *clientConfig) {
		c.agentCardPath = path
	}
}

// WithClientLogger sets an optional logger for debug output
func WithClientLogger(logger *slog.Logger) ClientOption {
	return func(c *clientConfig) {
		c.logger = logger
	}
}

// WithUserAgent sets the User-Agent header
func WithUserAgent(userAgent string) ClientOption {
	return func(c *clientConfig) {
		c.userAgent = userAgent
	}
}

// WithDefaultRequestTimeout sets default timeout for requests (default: 30s)
func WithDefaultRequestTimeout(timeout time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.defaultRequestTimeout = timeout
	}
}

// Client provides A2A JSON-RPC client functionality
type Client struct {
	baseURL string
	config  clientConfig
}

// NewClient creates a new A2A JSON-RPC client
func NewClient(baseURL string, options ...ClientOption) *Client {
	config := clientConfig{
		httpClient:            &http.Client{Timeout: 30 * time.Second},
		rpcPath:               "/",
		agentCardPath:         "/.well-known/agent.json",
		logger:                slog.Default(),
		userAgent:             "ATLASIC-Client/1.0",
		defaultRequestTimeout: 30 * time.Second,
	}

	for _, option := range options {
		option(&config)
	}

	return &Client{
		baseURL: baseURL,
		config:  config,
	}
}

// buildURL constructs URL for specific endpoint
func (c *Client) buildURL(endpoint string) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	baseURL.Path = path.Join(baseURL.Path, endpoint)
	return baseURL.String(), nil
}

// sendJSONRPCRequest sends a JSON-RPC request and returns the raw response
func (c *Client) sendJSONRPCRequest(ctx context.Context, method string, params interface{}) (*a2a.JSONRPCResponse, error) {
	// Build request URL
	reqURL, err := c.buildURL(c.config.rpcPath)
	if err != nil {
		return nil, err
	}

	// Create JSON-RPC request
	req := a2a.JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      fmt.Sprintf("req-%d", time.Now().UnixNano()),
	}

	// Marshal request to JSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	c.config.logger.Debug("Sending JSON-RPC request", 
		"method", method, 
		"url", reqURL,
		"id", req.ID)

	// Create HTTP request with context
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.config.userAgent != "" {
		httpReq.Header.Set("User-Agent", c.config.userAgent)
	}

	// Send request
	httpResp, err := c.config.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.config.logger.Debug("Received JSON-RPC response", 
		"status", httpResp.StatusCode,
		"contentType", httpResp.Header.Get("Content-Type"),
		"bodySize", len(respBody))

	// Parse JSON-RPC response
	var jsonrpcResp a2a.JSONRPCResponse
	if err := json.Unmarshal(respBody, &jsonrpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC response: %w", err)
	}

	// Check for JSON-RPC error
	if jsonrpcResp.Error != nil {
		return nil, &a2a.JSONRPCError{
			Code:    jsonrpcResp.Error.Code,
			Message: jsonrpcResp.Error.Message,
			Data:    jsonrpcResp.Error.Data,
		}
	}

	return &jsonrpcResp, nil
}

// SendMessage sends a message using message/send method
func (c *Client) SendMessage(ctx context.Context, params a2a.MessageSendParams) (*a2a.SendMessageResult, error) {
	resp, err := c.sendJSONRPCRequest(ctx, a2a.MethodSendMessage, params)
	if err != nil {
		return nil, err
	}

	// resp.Result is interface{}, need to marshal/unmarshal for type conversion
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result a2a.SendMessageResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse message send response: %w", err)
	}

	return &result, nil
}

// GetTask retrieves a task using tasks/get method
func (c *Client) GetTask(ctx context.Context, params a2a.TaskQueryParams) (*a2a.Task, error) {
	resp, err := c.sendJSONRPCRequest(ctx, a2a.MethodGetTask, params)
	if err != nil {
		return nil, err
	}

	// resp.Result is interface{}, need to marshal/unmarshal for type conversion
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result a2a.Task
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse task response: %w", err)
	}

	return &result, nil
}

// CancelTask cancels a task using tasks/cancel method
func (c *Client) CancelTask(ctx context.Context, params a2a.TaskIDParams) (*a2a.TaskStatus, error) {
	resp, err := c.sendJSONRPCRequest(ctx, a2a.MethodCancelTask, params)
	if err != nil {
		return nil, err
	}

	// resp.Result is interface{}, need to marshal/unmarshal for type conversion
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result a2a.TaskStatus
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse cancel task response: %w", err)
	}

	return &result, nil
}

// GetAgentCard retrieves the agent card
func (c *Client) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	// Build agent card URL
	cardURL, err := c.buildURL(c.config.agentCardPath)
	if err != nil {
		return nil, err
	}

	c.config.logger.Debug("Fetching agent card", "url", cardURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Accept", "application/json")
	if c.config.userAgent != "" {
		httpReq.Header.Set("User-Agent", c.config.userAgent)
	}

	// Send request
	httpResp, err := c.config.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card request failed with status %d", httpResp.StatusCode)
	}

	// Parse response
	var agentCard a2a.AgentCard
	if err := json.NewDecoder(httpResp.Body).Decode(&agentCard); err != nil {
		return nil, fmt.Errorf("failed to parse agent card: %w", err)
	}

	return &agentCard, nil
}

// SetTaskPushNotificationConfig sets push notification config for a task
func (c *Client) SetTaskPushNotificationConfig(ctx context.Context, params a2a.TaskPushNotificationConfig) (*a2a.TaskPushNotificationConfig, error) {
	resp, err := c.sendJSONRPCRequest(ctx, a2a.MethodSetTaskPushNotificationConfig, params)
	if err != nil {
		return nil, err
	}

	// resp.Result is interface{}, need to marshal/unmarshal for type conversion
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result a2a.TaskPushNotificationConfig
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse set push notification config response: %w", err)
	}

	return &result, nil
}

// GetTaskPushNotificationConfig gets push notification config for a task
func (c *Client) GetTaskPushNotificationConfig(ctx context.Context, params a2a.GetTaskPushNotificationConfigParams) (*a2a.TaskPushNotificationConfig, error) {
	resp, err := c.sendJSONRPCRequest(ctx, a2a.MethodGetTaskPushNotificationConfig, params)
	if err != nil {
		return nil, err
	}

	// resp.Result is interface{}, need to marshal/unmarshal for type conversion
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result a2a.TaskPushNotificationConfig
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse get push notification config response: %w", err)
	}

	return &result, nil
}