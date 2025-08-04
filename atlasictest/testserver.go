// Package atlasictest provides testing utilities for ATLASIC A2A agents.
// It offers httptest-like functionality specifically for A2A protocol testing.
package atlasictest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mashiike/atlasic"
	"github.com/mashiike/atlasic/transport"
)

// TestServer wraps httptest.Server to provide A2A protocol testing capabilities.
// It automatically sets up an AgentService and HTTP handler for testing A2A agents.
type TestServer struct {
	*httptest.Server

	// AgentService is the underlying service that handles A2A requests
	AgentService *atlasic.AgentService

	// Agent is the agent being tested
	Agent atlasic.Agent
}

// NewServer creates a new test server for the given agent.
// It sets up the necessary AgentService and HTTP handlers automatically.
// Uses FileSystemStorage with a temporary directory from testing.TB.TempDir().
//
// Example usage:
//
//	agent := atlasic.NewAgent(metadata, executeFunc)
//	server := atlasictest.NewServer(t, agent)
//	defer server.Close()
//
//	client := transport.NewClient(server.URL())
//	result, err := client.SendMessage(ctx, params)
func NewServer(tb testing.TB, agent atlasic.Agent) *TestServer {
	// Create temp directory for storage using testing.TB.TempDir()
	// Create a subdirectory for better organization
	baseTempDir := tb.TempDir()
	tempDir := filepath.Join(baseTempDir, "atlasictest-storage")

	// Create the subdirectory
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		panic("failed to create storage directory: " + err.Error())
	}

	// Create FileSystemStorage
	storage, err := atlasic.NewFileSystemStorage(tempDir)
	if err != nil {
		panic("failed to create FileSystemStorage: " + err.Error())
	}

	// Create AgentService using the proper constructor
	agentService := atlasic.NewAgentService(storage, agent)

	// Start JobQueue worker (essential for processing jobs)
	ctx := context.Background()
	if err := agentService.Start(ctx); err != nil {
		panic("failed to start AgentService worker: " + err.Error())
	}

	// Create HTTP handler
	handler := transport.NewHandler(agentService)

	// Create httptest server
	httpServer := httptest.NewServer(handler)

	return &TestServer{
		Server:       httpServer,
		AgentService: agentService,
		Agent:        agent,
	}
}

// URL returns the base URL of the test server.
// This can be used with transport.NewClient to create a client for testing.
func (s *TestServer) URL() string {
	return s.Server.URL
}

// Close shuts down the test server.
// This should be called when the test is complete, typically in a defer statement.
func (s *TestServer) Close() {
	s.Server.Close()
}

// Client creates a new transport.Client configured to communicate with this test server.
// This is a convenience method that creates a client with the correct URL.
func (s *TestServer) Client(opts ...transport.ClientOption) *transport.Client {
	return transport.NewClient(s.URL(), opts...)
}

// ClientWithHeaders creates a transport.Client that automatically adds specified HTTP headers to all requests.
// This is useful for testing scenarios where you need to verify that HTTP headers are properly preserved
// and passed to the Agent through TaskHandle.GetHTTPHeaders().
//
// Example usage:
//
//	headers := http.Header{
//		"User-Agent":    []string{"TestClient/1.0"},
//		"X-Request-ID":  []string{"test-123"},
//		"Authorization": []string{"Bearer token"},
//	}
//	client := server.ClientWithHeaders(headers)
//	result, err := client.SendMessage(ctx, params)
func (s *TestServer) ClientWithHeaders(headers http.Header, opts ...transport.ClientOption) *transport.Client {
	// Create a custom transport that adds headers to every request
	headerTransport := &headerAddingTransport{
		base:    http.DefaultTransport,
		headers: headers,
	}

	// Add custom transport to client options
	httpClient := &http.Client{Transport: headerTransport}
	opts = append(opts, transport.WithHTTPClient(httpClient))

	return transport.NewClient(s.URL(), opts...)
}

// headerAddingTransport is a custom http.RoundTripper that automatically adds headers to requests
type headerAddingTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t *headerAddingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	newReq := req.Clone(req.Context())

	// Set headers to the cloned request (overwrite any existing headers)
	for key, values := range t.headers {
		// Delete existing header first to ensure clean override
		newReq.Header.Del(key)
		for _, value := range values {
			newReq.Header.Add(key, value)
		}
	}

	return t.base.RoundTrip(newReq)
}
