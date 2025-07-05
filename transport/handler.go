package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/mashiike/atlasic/a2a"
)

// JSONRPCMethodHandler defines the signature for JSON-RPC method handlers
type JSONRPCMethodHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// HandlerOption defines configuration option for Handler
type HandlerOption func(*handlerConfig)

// handlerConfig holds internal configuration for Handler
type handlerConfig struct {
	rpcPath              string
	agentCardPath        string
	agentCardCacheMaxAge int
	logger               *slog.Logger // Optional logger for debug output
	authenticator        Authenticator
	authRequired         bool
	extensions           []Extension
}

// WithRPCPath sets the JSON-RPC endpoint path (default: "/")
func WithRPCPath(path string) HandlerOption {
	return func(c *handlerConfig) {
		c.rpcPath = path
	}
}

// WithAgentCardPath sets the agent card endpoint path (default: "/.well-known/agent.json")
func WithAgentCardPath(path string) HandlerOption {
	return func(c *handlerConfig) {
		c.agentCardPath = path
	}
}

// WithAgentCardCacheMaxAge sets cache max-age for agent card in seconds (default: 3600)
func WithAgentCardCacheMaxAge(seconds int) HandlerOption {
	return func(c *handlerConfig) {
		c.agentCardCacheMaxAge = seconds
	}
}

// WithAuthenticator sets the authenticator for the handler
func WithAuthenticator(auth Authenticator) HandlerOption {
	return func(c *handlerConfig) {
		c.authenticator = auth
		c.authRequired = true
	}
}

// WithLogger sets an optional logger for debug output
func WithLogger(logger *slog.Logger) HandlerOption {
	return func(c *handlerConfig) {
		c.logger = logger
	}
}

// WithExtensions adds extensions to the handler
func WithExtensions(extensions ...Extension) HandlerOption {
	return func(c *handlerConfig) {
		c.extensions = append(c.extensions, extensions...)
	}
}

// methodDescriptor represents an A2A method with its properties and handler
type methodDescriptor struct {
	method    string                                                                                 // Method name (e.g., "message/send")
	mediaType string                                                                                 // Response media type ("application/json" or "text/event-stream")
	handler   func(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id any) error // Handler function
}

// Handler wraps an AgentService and provides JSON-RPC over HTTP handling
type Handler struct {
	mu             sync.RWMutex
	service        AgentService
	methodRegistry map[string]methodDescriptor
	config         handlerConfig
	extensions     map[string]Extension
}

// NewHandler creates a new A2A JSON-RPC handler with options
func NewHandler(service AgentService, options ...HandlerOption) *Handler {
	config := handlerConfig{
		rpcPath:              "/",
		agentCardPath:        "/.well-known/agent.json",
		agentCardCacheMaxAge: 3600,
		logger:               slog.Default(), // Default logger
	}

	for _, option := range options {
		option(&config)
	}
	extensions := make(map[string]Extension)
	for _, ext := range config.extensions {
		if ext == nil {
			continue // Skip nil extensions
		}
		metadata := ext.GetMetadata()
		extensions[metadata.URI] = ext
	}
	config.extensions = nil // Clear the slice to avoid memory leak

	// Set extensions to AgentService if it supports ExtensionAware
	if extService, ok := service.(ExtensionAware); ok {
		extList := make([]Extension, 0, len(extensions))
		for _, ext := range extensions {
			extList = append(extList, ext)
		}
		extService.SetExtensions(extList)
	}

	h := &Handler{
		service:    service,
		config:     config,
		extensions: extensions,
	}
	h.initMethodRegistry()
	return h
}

// initMethodRegistry initializes the method registry with all supported A2A methods
func (h *Handler) initMethodRegistry() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.methodRegistry = make(map[string]methodDescriptor)

	// JSON methods (unified registration)
	h.registerJSONMethod(a2a.MethodSendMessage, h.handleSendMessage)
	h.registerJSONMethod(a2a.MethodGetTask, h.handleGetTask)
	h.registerJSONMethod(a2a.MethodCancelTask, h.handleCancelTask)
	h.registerJSONMethod(a2a.MethodSetTaskPushNotificationConfig, h.handleSetTaskPushNotificationConfig)
	h.registerJSONMethod(a2a.MethodGetTaskPushNotificationConfig, h.handleGetTaskPushNotificationConfig)

	// Streaming methods (internal only)
	h.registerStreamMethod(a2a.MethodSendStreamingMessage, h.handleSendStreamingMessage)
	h.registerStreamMethod(a2a.MethodTaskResubscription, h.handleTaskResubscribe)

	// Register extensions as methods
	for uri, ext := range h.extensions {
		rpcExt, ok := ext.(RPCMethodExtension)
		if !ok {
			h.config.logger.Debug("Skipping non-RPC extension", "uri", uri)
			continue // Skip non-RPC extensions
		}
		method := rpcExt.MethodName()
		baseHandler := rpcExt.MethodHandler()
		metadata := rpcExt.GetMetadata()
		handler := &rpcMethodExtensionHandler{
			handler:    baseHandler,
			methodName: method,
			metadata:   metadata,
		}
		h.registerJSONMethod(method, handler.ServeRPC)
	}
}

// RegisterMethod registers a JSON-RPC method with simplified handler
func (h *Handler) RegisterMethod(method string, handler JSONRPCMethodHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.methodRegistry[method]; exists {
		panic(fmt.Sprintf("method %s already registered", method))
	}
	h.registerJSONMethod(method, handler)
}

// registerJSONMethod registers a JSON-RPC method (internal use)
func (h *Handler) registerJSONMethod(method string, handler JSONRPCMethodHandler) {
	wrappedHandler := func(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id interface{}) error {
		result, err := handler(ctx, params)
		if err != nil {
			// Check if error is a JSONRPCError and handle it directly
			if jsonrpcErr, ok := err.(*a2a.JSONRPCError); ok {
				h.writeErrorResponse(w, id, jsonrpcErr.Code, jsonrpcErr.Message, jsonrpcErr.Data)
				return nil
			}
			// Convert regular errors to JSON-RPC Internal Server Error
			h.writeErrorResponse(w, id, a2a.ErrorCodeInternalError, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), err.Error())
			return nil
		}

		h.writeSuccessResponse(w, id, result)
		return nil
	}

	h.methodRegistry[method] = methodDescriptor{
		method:    method,
		mediaType: "application/json",
		handler:   wrappedHandler,
	}
}

// registerStreamMethod registers a streaming method (internal use only)
func (h *Handler) registerStreamMethod(method string, handler func(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id any) error) {
	h.methodRegistry[method] = methodDescriptor{
		method:    method,
		mediaType: "text/event-stream",
		handler:   handler,
	}
}

// clientAcceptsSSE determines if the client accepts Server-Sent Events
// If forValidSSEMethod is true, also accepts */* and text/* (for valid SSE methods)
// Otherwise, only accepts explicit text/event-stream (for error cases)
func clientAcceptsSSE(acceptHeader string, forValidSSEMethod bool) bool {
	// text/event-stream explicitly accepts SSE
	if strings.Contains(acceptHeader, "text/event-stream") {
		return true
	}

	// For valid SSE methods, also accept broader content types
	if forValidSSEMethod {
		// text/* accepts all text types including SSE
		if strings.Contains(acceptHeader, "text/*") {
			return true
		}

		// */* accepts everything including SSE
		if strings.Contains(acceptHeader, "*/*") {
			return true
		}
	}

	return false
}

// ServeHTTP implements http.Handler interface
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle Agent Card endpoint (no authentication required)
	if r.Method == http.MethodGet && r.URL.Path == h.config.agentCardPath {
		h.config.logger.Debug("Handling agent card request", "path", r.URL.Path)
		h.handleWellKnownAgentCard(w, r)
		return
	}

	// Handle JSON-RPC endpoint
	if r.URL.Path != h.config.rpcPath {
		http.NotFound(w, r)
		return
	}

	// Only accept POST requests for JSON-RPC
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authentication check for JSON-RPC requests
	if h.config.authenticator != nil {
		newReq, err := h.config.authenticator.Authenticate(r.Context(), r)
		if err != nil {
			h.writeAuthError(w, err)
			return
		}
		r = newReq
	}

	// Parse JSON-RPC request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeErrorResponse(w, nil, a2a.ErrorCodeParseError, a2a.ErrorCodeText(a2a.ErrorCodeParseError), nil)
		return
	}

	var req a2a.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeErrorResponse(w, nil, a2a.ErrorCodeParseError, a2a.ErrorCodeText(a2a.ErrorCodeParseError), err.Error())
		return
	}

	// Validate JSON-RPC version
	if req.JSONRpc != "2.0" {
		h.writeErrorResponse(w, req.ID, a2a.ErrorCodeInvalidRequest, a2a.ErrorCodeText(a2a.ErrorCodeInvalidRequest), nil)
		return
	}

	// Process Extension activation
	ctx := r.Context()
	extensionHeader := r.Header.Get("X-A2A-Extensions")
	requested := ParseExtensionHeader(extensionHeader)

	// Validate required extensions
	if err := ValidateRequiredExtensions(h.extensions, requested); err != nil {
		h.writeErrorResponse(w, req.ID, a2a.ErrorCodeInvalidRequest, err.Error(), nil)
		return
	}

	// Resolve active extensions
	activeExtensions := ResolveActiveExtensions(h.extensions, requested)
	ctx = WithActiveExtensions(ctx, activeExtensions)

	// Set response header for activated extensions
	if len(activeExtensions) > 0 {
		w.Header().Set("X-A2A-Extensions", FormatExtensionHeader(activeExtensions))
	}

	// Route method using the new registry approach
	h.routeMethodByRegistry(ctx, req, w, r.Header.Get("Accept"))
}

// routeMethodByRegistry routes the method using the method registry
func (h *Handler) routeMethodByRegistry(ctx context.Context, req a2a.JSONRPCRequest, w http.ResponseWriter, acceptHeader string) {
	// Look up method in registry first
	entity, exists := h.methodRegistry[req.Method]

	if !exists {
		// Method not found - only use SSE if explicitly requested (not for */* or text/*)
		clientWantsSSE := clientAcceptsSSE(acceptHeader, false)
		if clientWantsSSE {
			h.setupSSEHeaders(w)
			h.writeSSEError(w, req.ID, a2a.ErrorCodeMethodNotFound, a2a.ErrorCodeText(a2a.ErrorCodeMethodNotFound), nil)
		} else {
			h.writeErrorResponse(w, req.ID, a2a.ErrorCodeMethodNotFound, a2a.ErrorCodeText(a2a.ErrorCodeMethodNotFound), nil)
		}
		return
	}

	// Check if this is an SSE method and if client accepts it
	isSSEMethod := entity.mediaType == "text/event-stream"
	clientWantsSSE := clientAcceptsSSE(acceptHeader, isSSEMethod)

	if isSSEMethod && !clientWantsSSE {
		// SSE method but client doesn't accept SSE
		h.writeErrorResponse(w, req.ID, a2a.ErrorCodeMethodNotFound, a2a.ErrorCodeText(a2a.ErrorCodeMethodNotFound), nil)
		return
	}

	// Set appropriate headers for SSE methods
	if isSSEMethod {
		h.setupSSEHeaders(w)
	}

	// Convert params to json.RawMessage for handler
	paramsRaw, err := json.Marshal(req.Params)
	if err != nil {
		if isSSEMethod {
			h.writeSSEError(w, req.ID, a2a.ErrorCodeInvalidParams, a2a.ErrorCodeText(a2a.ErrorCodeInvalidParams), err.Error())
		} else {
			h.writeErrorResponse(w, req.ID, a2a.ErrorCodeInvalidParams, a2a.ErrorCodeText(a2a.ErrorCodeInvalidParams), err.Error())
		}
		return
	}

	// Apply ProfileExtension preprocessing
	ctx, err = h.applyProfileExtensionPreprocessing(ctx, req.Method, paramsRaw)
	if err != nil {
		if isSSEMethod {
			h.writeSSEError(w, req.ID, a2a.ErrorCodeInternalError, "Profile extension error", err.Error())
		} else {
			h.writeErrorResponse(w, req.ID, a2a.ErrorCodeInternalError, "Profile extension error", err.Error())
		}
		return
	}

	// Execute the handler
	err = entity.handler(ctx, paramsRaw, w, req.ID)
	if err != nil {
		// Check if error is a JSONRPCError and handle it directly
		if jsonrpcErr, ok := err.(*a2a.JSONRPCError); ok {
			if isSSEMethod {
				h.writeSSEError(w, req.ID, jsonrpcErr.Code, jsonrpcErr.Message, jsonrpcErr.Data)
			} else {
				h.writeErrorResponse(w, req.ID, jsonrpcErr.Code, jsonrpcErr.Message, jsonrpcErr.Data)
			}
			return
		}

		// Convert regular errors to JSON-RPC Internal Server Error
		if isSSEMethod {
			h.writeSSEError(w, req.ID, a2a.ErrorCodeInternalError, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), err.Error())
		} else {
			h.writeErrorResponse(w, req.ID, a2a.ErrorCodeInternalError, a2a.ErrorCodeText(a2a.ErrorCodeInternalError), err.Error())
		}
		return
	}

	// For non-SSE methods that return success, the handler should have already written the response
	// SSE methods handle their own streaming responses
}

// handleSendMessage handles message/send as JSONRPCMethodHandler
func (h *Handler) handleSendMessage(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req a2a.MessageSendParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse message send parameters")
	}

	// Perform content negotiation if AcceptedOutputModes is specified
	if req.Configuration != nil && len(req.Configuration.AcceptedOutputModes) > 0 {
		supportedModes, err := h.service.SupportedOutputModes(ctx)
		if err != nil {
			return nil, err
		}

		_, err = FindCompatibleOutputModes(req.Configuration.AcceptedOutputModes, supportedModes)
		if err != nil {
			return nil, err // This will be ContentTypeNotSupportedError (-32005)
		}
	}

	return h.service.SendMessage(ctx, req)
}

// handleGetTask handles tasks/get as JSONRPCMethodHandler
func (h *Handler) handleGetTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req a2a.TaskQueryParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse task query parameters")
	}
	return h.service.GetTask(ctx, req)
}

// handleCancelTask handles tasks/cancel as JSONRPCMethodHandler
func (h *Handler) handleCancelTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req a2a.TaskIDParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse task ID parameters")
	}
	return h.service.CancelTask(ctx, req)
}

// handleSetTaskPushNotificationConfig handles tasks/pushNotificationConfig/set as JSONRPCMethodHandler
func (h *Handler) handleSetTaskPushNotificationConfig(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req a2a.TaskPushNotificationConfig
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse push notification config parameters")
	}
	return h.service.SetTaskPushNotificationConfig(ctx, req)
}

// handleGetTaskPushNotificationConfig handles tasks/pushNotificationConfig/get as JSONRPCMethodHandler
func (h *Handler) handleGetTaskPushNotificationConfig(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req a2a.GetTaskPushNotificationConfigParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, a2a.NewJSONRPCInvalidParamsError("Failed to parse get task push notification config parameters")
	}
	return h.service.GetTaskPushNotificationConfig(ctx, req)
}

// handleSendStreamingMessage handles message/stream JSON-RPC method with SSE
func (h *Handler) handleSendStreamingMessage(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id interface{}) error {
	var req a2a.MessageSendParams
	if err := json.Unmarshal(params, &req); err != nil {
		return a2a.NewJSONRPCInvalidParamsError("Failed to parse message send parameters")
	}

	// Perform content negotiation if AcceptedOutputModes is specified
	if req.Configuration != nil && len(req.Configuration.AcceptedOutputModes) > 0 {
		supportedModes, err := h.service.SupportedOutputModes(ctx)
		if err != nil {
			return err
		}

		_, err = FindCompatibleOutputModes(req.Configuration.AcceptedOutputModes, supportedModes)
		if err != nil {
			return err // This will be ContentTypeNotSupportedError (-32005)
		}
	}

	// Call the streaming method to get the channel
	streamChan, err := h.service.SendStreamingMessage(ctx, req)
	if err != nil {
		return err
	}

	// Write SSE stream from the channel
	return writeSSEStream(w, id, streamChan)
}

// handleTaskResubscribe handles tasks/resubscribe JSON-RPC method with SSE
func (h *Handler) handleTaskResubscribe(ctx context.Context, params json.RawMessage, w http.ResponseWriter, id interface{}) error {
	var req a2a.TaskIDParams
	if err := json.Unmarshal(params, &req); err != nil {
		return a2a.NewJSONRPCInvalidParamsError("Failed to parse task ID parameters")
	}

	// Call the streaming method to get the channel
	streamChan, err := h.service.TaskResubscription(ctx, req)
	if err != nil {
		return err
	}

	// Write SSE stream from the channel
	return writeSSEStream(w, id, streamChan)
}

// handleWellKnownAgentCard handles GET requests for agent card endpoint
func (h *Handler) handleWellKnownAgentCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Try to get extended agent card first, fall back to standard
	var responseData any
	var err error

	if extService, ok := h.service.(ExtensionAware); ok {
		responseData, err = extService.GetExtendedAgentCard(ctx)
	} else {
		agentCard, cardErr := h.service.GetAgentCard(ctx)
		if cardErr != nil {
			err = cardErr
		} else {
			responseData = agentCard
		}
	}

	if err != nil {
		h.config.logger.Error("Failed to get agent card", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Handle URL and authentication for both standard and extended cards
	var agentCard *a2a.AgentCard
	if extCard, ok := responseData.(map[string]any); ok {
		// Extended card - extract base AgentCard for URL handling
		if baseCard, cardOk := h.extractBaseAgentCard(extCard); cardOk {
			agentCard = baseCard
		}
	} else if card, ok := responseData.(*a2a.AgentCard); ok {
		// Standard card
		agentCard = card
	}

	if agentCard != nil {
		// Apply URL and authentication handling
		h.applyAgentCardPostProcessing(agentCard, r)

		// Update the response data with modified card
		if extCard, ok := responseData.(map[string]any); ok {
			h.updateExtendedCardWithProcessedBase(extCard, agentCard)
		} else {
			responseData = agentCard
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if h.config.agentCardCacheMaxAge > 0 {
		w.Header().Set("Cache-Control",
			fmt.Sprintf("public, max-age=%d", h.config.agentCardCacheMaxAge))
	}

	if err := json.NewEncoder(w).Encode(responseData); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// buildRequestBaseEndpoint constructs base endpoint from HTTP request considering proxies
func (h *Handler) buildRequestBaseEndpoint(r *http.Request) string {
	// Check X-Forwarded-Proto first, then fallback to TLS detection
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}

	// Check X-Forwarded-Host first, then fallback to Host header
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}

	return fmt.Sprintf("%s://%s/", scheme, host)
}

// extractBaseAgentCard extracts a2a.AgentCard from extended card map
func (h *Handler) extractBaseAgentCard(extCard map[string]any) (*a2a.AgentCard, bool) {
	cardData, err := json.Marshal(extCard)
	if err != nil {
		return nil, false
	}

	var agentCard a2a.AgentCard
	if err := json.Unmarshal(cardData, &agentCard); err != nil {
		return nil, false
	}

	return &agentCard, true
}

// applyAgentCardPostProcessing applies URL and authentication post-processing
func (h *Handler) applyAgentCardPostProcessing(agentCard *a2a.AgentCard, r *http.Request) {
	// Ensure ProtocolVersion is always set to the current A2A protocol version
	agentCard.ProtocolVersion = a2a.ProtocolVersion

	// Replace placeholder URL with actual host if needed
	if agentCard.URL == PlaceholderURL {
		agentCard.URL = h.buildRequestBaseEndpoint(r)
	}

	// Always join the base endpoint with rpcPath to get the final URL
	if finalURL, err := url.JoinPath(agentCard.URL, h.config.rpcPath); err == nil {
		agentCard.URL = finalURL
	}

	// Add authentication information if authenticator is configured
	if h.config.authenticator != nil {
		agentCard.SecuritySchemes = h.config.authenticator.GetSecuritySchemes()
		agentCard.Security = h.config.authenticator.GetSecurityRequirements()
	}
}

// updateExtendedCardWithProcessedBase updates extended card with processed base fields
func (h *Handler) updateExtendedCardWithProcessedBase(extCard map[string]any, processedCard *a2a.AgentCard) {
	extCard["protocolVersion"] = processedCard.ProtocolVersion
	extCard["url"] = processedCard.URL
	if processedCard.SecuritySchemes != nil {
		extCard["securitySchemes"] = processedCard.SecuritySchemes
	}
	if processedCard.Security != nil {
		extCard["security"] = processedCard.Security
	}
}

// applyProfileExtensionPreprocessing applies ProfileExtension preprocessing
func (h *Handler) applyProfileExtensionPreprocessing(ctx context.Context, method string, params json.RawMessage) (context.Context, error) {
	activeExtensions := GetActiveExtensions(ctx)
	for _, ext := range activeExtensions {
		if profile, ok := ext.(ProfileExtension); ok {
			// Try method-specific interfaces first
			processed, err := h.tryMethodSpecificPreprocessing(ctx, method, params, ext)
			if err != nil {
				return ctx, fmt.Errorf("profile extension %s method-specific preprocessing failed: %w", ext.GetMetadata().URI, err)
			}
			if processed {
				continue // Skip generic preprocessing if method-specific was handled
			}

			// Fallback to generic preprocessing
			ctx, err = profile.PrepareContext(ctx, method, params)
			if err != nil {
				return ctx, fmt.Errorf("profile extension %s preprocessing failed: %w", ext.GetMetadata().URI, err)
			}
		}
	}
	return ctx, nil
}

// tryMethodSpecificPreprocessing tries method-specific preprocessing interfaces
func (h *Handler) tryMethodSpecificPreprocessing(ctx context.Context, method string, params json.RawMessage, ext Extension) (bool, error) {
	switch method {
	case a2a.MethodSendMessage:
		if msgExt, ok := ext.(SendMessageProfileExtension); ok {
			var msgParams a2a.MessageSendParams
			if err := json.Unmarshal(params, &msgParams); err != nil {
				return false, err
			}
			_, err := msgExt.PrepareSendMessage(ctx, &msgParams)
			return true, err
		}

	case a2a.MethodSendStreamingMessage:
		if streamExt, ok := ext.(SendStreamingMessageProfileExtension); ok {
			var msgParams a2a.MessageSendParams
			if err := json.Unmarshal(params, &msgParams); err != nil {
				return false, err
			}
			_, err := streamExt.PrepareSendStreamingMessage(ctx, &msgParams)
			return true, err
		}

	case a2a.MethodGetTask:
		if taskExt, ok := ext.(GetTaskProfileExtension); ok {
			var taskParams a2a.TaskQueryParams
			if err := json.Unmarshal(params, &taskParams); err != nil {
				return false, err
			}
			_, err := taskExt.PrepareGetTask(ctx, &taskParams)
			return true, err
		}

	case a2a.MethodCancelTask:
		if taskExt, ok := ext.(CancelTaskProfileExtension); ok {
			var taskParams a2a.TaskIDParams
			if err := json.Unmarshal(params, &taskParams); err != nil {
				return false, err
			}
			_, err := taskExt.PrepareCancelTask(ctx, &taskParams)
			return true, err
		}

	case a2a.MethodTaskResubscription:
		if streamExt, ok := ext.(TaskResubscriptionProfileExtension); ok {
			var taskParams a2a.TaskIDParams
			if err := json.Unmarshal(params, &taskParams); err != nil {
				return false, err
			}
			_, err := streamExt.PrepareTaskResubscription(ctx, &taskParams)
			return true, err
		}

	case a2a.MethodSetTaskPushNotificationConfig:
		if pushExt, ok := ext.(SetTaskPushNotificationConfigProfileExtension); ok {
			var pushParams a2a.TaskPushNotificationConfig
			if err := json.Unmarshal(params, &pushParams); err != nil {
				return false, err
			}
			_, err := pushExt.PrepareSetTaskPushNotificationConfig(ctx, &pushParams)
			return true, err
		}

	case a2a.MethodGetTaskPushNotificationConfig:
		if pushExt, ok := ext.(GetTaskPushNotificationConfigProfileExtension); ok {
			var pushParams a2a.GetTaskPushNotificationConfigParams
			if err := json.Unmarshal(params, &pushParams); err != nil {
				return false, err
			}
			_, err := pushExt.PrepareGetTaskPushNotificationConfig(ctx, &pushParams)
			return true, err
		}

	case a2a.MethodListTaskPushNotificationConfig:
		if pushExt, ok := ext.(ListTaskPushNotificationConfigProfileExtension); ok {
			var taskParams a2a.TaskIDParams
			if err := json.Unmarshal(params, &taskParams); err != nil {
				return false, err
			}
			_, err := pushExt.PrepareListTaskPushNotificationConfig(ctx, &taskParams)
			return true, err
		}

	case a2a.MethodDeleteTaskPushNotificationConfig:
		if pushExt, ok := ext.(DeleteTaskPushNotificationConfigProfileExtension); ok {
			var pushParams a2a.DeleteTaskPushNotificationConfigParams
			if err := json.Unmarshal(params, &pushParams); err != nil {
				return false, err
			}
			_, err := pushExt.PrepareDeleteTaskPushNotificationConfig(ctx, &pushParams)
			return true, err
		}
	}

	return false, nil // No method-specific interface found
}

// writeSuccessResponse writes a successful JSON-RPC response
func (h *Handler) writeSuccessResponse(w http.ResponseWriter, id interface{}, result interface{}) {
	resp := a2a.NewJSONRPCResponse(result, id)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.config.logger.Error("failed to write response", "error", err)
	}
}

// writeErrorResponse writes an error JSON-RPC response
func (h *Handler) writeErrorResponse(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	resp := a2a.NewJSONRPCErrorResponse(code, message, data, id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.config.logger.Error("failed to write response", "error", err)
	}
}

// setupSSEHeaders sets up Server-Sent Events headers
func (h *Handler) setupSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// writeSSEError writes an error as SSE event
func (h *Handler) writeSSEError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	resp := a2a.NewJSONRPCErrorResponse(code, message, data, id)

	w.Header().Set("Content-Type", "text/event-stream")
	respBytes, err := json.Marshal(resp)
	if err != nil {
		// This should not happen with a well-formed response structure
		h.config.logger.Error("failed to marshal SSE error response", "error", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", respBytes)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// canHandle checks if the handler can process the request
func (h *Handler) canHandle(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == h.config.agentCardPath) ||
		(r.Method == http.MethodPost && r.URL.Path == h.config.rpcPath)
}

// A2AMiddleware creates middleware that handles A2A requests and passes others to next handler
func A2AMiddleware(service AgentService, options ...HandlerOption) func(http.Handler) http.Handler {
	a2aHandler := NewHandler(service, options...)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a2aHandler.canHandle(r) {
				a2aHandler.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeSSEStream writes a stream of responses from a channel as Server-Sent Events
func writeSSEStream(w http.ResponseWriter, id interface{}, streamChan <-chan a2a.StreamResponse) error {
	// Ensure we have flusher support
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}

	// Process each response from the channel
	for response := range streamChan {
		// Convert response to JSON-RPC format
		jsonrpcResp := a2a.NewJSONRPCResponse(response, id)

		// Marshal to JSON
		respBytes, err := json.Marshal(jsonrpcResp)
		if err != nil {
			// Write error event and continue
			errorResp := a2a.NewInternalError(id, err.Error())
			errorBytes, err := json.Marshal(errorResp)
			if err != nil {
				// This should not happen with a well-formed response structure
				return err
			}
			fmt.Fprintf(w, "data: %s\n\n", errorBytes)
			flusher.Flush()
			continue
		}

		// Write as SSE data event
		fmt.Fprintf(w, "data: %s\n\n", respBytes)
		flusher.Flush()
	}

	return nil
}

// WriteSSEError writes an error event as Server-Sent Events
func WriteSSEError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) error {
	// Ensure we have flusher support
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}

	// Create error response
	errorResp := a2a.NewJSONRPCErrorResponse(code, message, data, id)

	// Marshal to JSON
	respBytes, err := json.Marshal(errorResp)
	if err != nil {
		return err
	}

	// Write as SSE data event
	fmt.Fprintf(w, "data: %s\n\n", respBytes)
	flusher.Flush()

	return nil
}

// writeAuthError writes an authentication error response
func (h *Handler) writeAuthError(w http.ResponseWriter, err error) {
	var authErr *AuthError
	if errors.As(err, &authErr) {
		// Authentication error - return 401 with proper error format
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)

		errorResp := a2a.NewJSONRPCErrorResponse(
			a2a.ErrorCodeUnauthorized,
			authErr.Message,
			map[string]interface{}{
				"code":   authErr.Code,
				"scheme": authErr.Scheme,
			},
			nil,
		)

		respBytes, _ := json.Marshal(errorResp)
		w.Write(respBytes)
	} else {
		// Generic authentication error
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)

		errorResp := a2a.NewJSONRPCErrorResponse(
			a2a.ErrorCodeUnauthorized,
			"Authentication required",
			nil,
			nil,
		)

		respBytes, _ := json.Marshal(errorResp)
		w.Write(respBytes)
	}
}
