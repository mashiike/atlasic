// Package a2a provides Agent-to-Agent (A2A) Protocol implementation for ATLASIC.
// This package implements Google A2A Protocol specification with JSON-RPC 2.0 over HTTP.
package a2a

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// BASIC TYPES AND ENUMS
// =============================================================================

// Kind represents the type of various A2A objects
type Kind string

const (
	// Kind field values
	KindMessage        Kind = "message"
	KindTask           Kind = "task"
	KindTextPart       Kind = "text"
	KindFilePart       Kind = "file"
	KindDataPart       Kind = "data"
	KindStatusUpdate   Kind = "status-update"
	KindArtifactUpdate Kind = "artifact-update"
)

// String returns the string representation of the kind.
func (k Kind) String() string {
	return string(k)
}

// Role represents the role of a message sender
type Role string

const (
	// Role field values
	RoleUser  Role = "user"
	RoleAgent Role = "agent"
)

// IsValid returns true if the role is valid.
func (r Role) IsValid() bool {
	switch r {
	case RoleUser, RoleAgent:
		return true
	default:
		return false
	}
}

// String returns the string representation of the role.
func (r Role) String() string {
	return string(r)
}

// TaskState represents the possible states of a Task
type TaskState string

const (
	// Task state values
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateFailed        TaskState = "failed"
	TaskStateRejected      TaskState = "rejected"
	TaskStateAuthRequired  TaskState = "auth-required"
	TaskStateUnknown       TaskState = "unknown"
)

// IsValid returns true if the task state is valid.
func (state TaskState) IsValid() bool {
	switch state {
	case TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired,
		TaskStateCompleted, TaskStateCanceled, TaskStateFailed,
		TaskStateRejected, TaskStateAuthRequired, TaskStateUnknown:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the task state is terminal (final).
func (state TaskState) IsTerminal() bool {
	switch state {
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected:
		return true
	default:
		return false
	}
}

// IsInterrupted returns true if the task state is interrupted (waiting for input).
func (state TaskState) IsInterrupted() bool {
	switch state {
	case TaskStateInputRequired, TaskStateAuthRequired:
		return true
	default:
		return false
	}
}

// CanCancel returns true if the task can be canceled based on its current state.
func (state TaskState) CanCancel() bool {
	return !state.IsTerminal()
}

// String returns the string representation of the task state.
func (state TaskState) String() string {
	return string(state)
}

// SecurityType represents security scheme types
type SecurityType string

const (
	// Security scheme types
	SecurityTypeAPIKey        SecurityType = "apiKey"
	SecurityTypeHTTP          SecurityType = "http"
	SecurityTypeOAuth2        SecurityType = "oauth2"
	SecurityTypeOpenIDConnect SecurityType = "openIdConnect"
)

// A2A Protocol Version
const (
	ProtocolVersion = "0.2.5"
)

// A2A method names
const (
	MethodSendMessage                      = "message/send"
	MethodSendStreamingMessage             = "message/stream"
	MethodGetTask                          = "tasks/get"
	MethodCancelTask                       = "tasks/cancel"
	MethodTaskResubscription               = "tasks/resubscribe"
	MethodSetTaskPushNotificationConfig    = "tasks/pushNotificationConfig/set"
	MethodGetTaskPushNotificationConfig    = "tasks/pushNotificationConfig/get"
	MethodListTaskPushNotificationConfig   = "tasks/pushNotificationConfig/list"
	MethodDeleteTaskPushNotificationConfig = "tasks/pushNotificationConfig/delete"
)

// JSON-RPC error codes
const (
	// Standard JSON-RPC error codes
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603

	// A2A specific error codes
	ErrorCodeTaskNotFound                 = -32001
	ErrorCodeTaskNotCancelable            = -32002
	ErrorCodePushNotificationNotSupported = -32003
	ErrorCodeUnsupportedOperation         = -32004
	ErrorCodeContentTypeNotSupported      = -32005
	ErrorCodeInvalidAgentResponse         = -32006
	ErrorCodeUnauthorized                 = -32007
	ErrorCodeBlockingNotAllowed           = -32021
)

// ErrorCodeText returns the text description for an error code.
func ErrorCodeText(code int) string {
	switch code {
	case ErrorCodeParseError:
		return "Invalid JSON payload"
	case ErrorCodeInvalidRequest:
		return "Request payload validation error"
	case ErrorCodeMethodNotFound:
		return "Method not found"
	case ErrorCodeInvalidParams:
		return "Invalid parameters"
	case ErrorCodeInternalError:
		return "Internal error"
	case ErrorCodeTaskNotFound:
		return "Task not found"
	case ErrorCodeTaskNotCancelable:
		return "Task cannot be canceled"
	case ErrorCodePushNotificationNotSupported:
		return "Push Notification is not supported"
	case ErrorCodeUnsupportedOperation:
		return "This operation is not supported"
	case ErrorCodeContentTypeNotSupported:
		return "Incompatible content types"
	case ErrorCodeInvalidAgentResponse:
		return "Invalid agent response"
	case ErrorCodeUnauthorized:
		return "Authentication required"
	case ErrorCodeBlockingNotAllowed:
		return "Blocking mode is not supported for streaming messages"
	default:
		return "Unknown error"
	}
}

// =============================================================================
// JSON-RPC TYPES
// =============================================================================

// JSONRPCRequest represents a JSON-RPC 2.0 Request object.
type JSONRPCRequest struct {
	JSONRpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 Response object.
type JSONRPCResponse struct {
	JSONRpc string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	ID      interface{}   `json:"id"`
}

// JSONRPCError represents a JSON-RPC 2.0 Error object.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface for JSONRPCError
func (e *JSONRPCError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("JSON-RPC error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// =============================================================================
// JSON-RPC PARAMETER TYPES
// =============================================================================

// MessageSendParams represents parameters for message/send method.
type MessageSendParams struct {
	Message       Message                   `json:"message"`
	Configuration *MessageSendConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]interface{}    `json:"metadata,omitempty"`
}

// MessageSendConfiguration represents configuration for the send message request.
type MessageSendConfiguration struct {
	AcceptedOutputModes    []string                `json:"acceptedOutputModes"`
	Blocking               bool                    `json:"blocking,omitempty"`
	HistoryLength          *int                    `json:"historyLength,omitempty"` // Pointer to distinguish between unset and 0
	PushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
}

// TaskQueryParams represents parameters for querying a task.
type TaskQueryParams struct {
	ID            string                 `json:"id"`
	HistoryLength *int                   `json:"historyLength,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TaskIDParams represents parameters containing only a task ID.
type TaskIDParams struct {
	ID       string                 `json:"id"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SetTaskPushNotificationConfigParams represents parameters for setting push notification config.
type SetTaskPushNotificationConfigParams struct {
	Parent string                     `json:"parent"`
	Config TaskPushNotificationConfig `json:"config"`
}

// GetTaskPushNotificationConfigParams represents parameters for getting push notification config.
type GetTaskPushNotificationConfigParams struct {
	ID                       string `json:"id"`
	PushNotificationConfigID string `json:"pushNotificationConfigId,omitempty"`
}

// DeleteTaskPushNotificationConfigParams represents parameters for deleting push notification config.
type DeleteTaskPushNotificationConfigParams struct {
	ID                       string `json:"id"`
	PushNotificationConfigID string `json:"pushNotificationConfigId"`
}

// TaskPushNotificationConfig represents parameters for setting or getting push notification configuration.
type TaskPushNotificationConfig struct {
	TaskID                 string                 `json:"taskId"`
	PushNotificationConfig PushNotificationConfig `json:"pushNotificationConfig"`
}

// =============================================================================
// JSON-RPC RESPONSE TYPES
// =============================================================================

// SendMessageResult represents possible results for message/send.
type SendMessageResult struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// StreamResponse represents responses for streaming methods.
type StreamResponse struct {
	Task     *Task                    `json:"task,omitempty"`
	Message  *Message                 `json:"message,omitempty"`
	Status   *TaskStatusUpdateEvent   `json:"status,omitempty"`
	Artifact *TaskArtifactUpdateEvent `json:"artifact,omitempty"`
}

// =============================================================================
// CORE A2A TYPES
// =============================================================================

// Message represents a single message exchanged between user and agent.
type Message struct {
	Kind             Kind                   `json:"kind"`
	MessageID        string                 `json:"messageId"`
	ContextID        string                 `json:"contextId,omitempty"`
	TaskID           string                 `json:"taskId,omitempty"`
	Role             Role                   `json:"role"`
	Parts            []Part                 `json:"parts"`
	ReferenceTaskIDs []string               `json:"referenceTaskIds,omitempty"`
	Extensions       []string               `json:"extensions,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// Part represents a part of a message, which can be text, a file, or structured data.
type Part struct {
	Kind     Kind                   `json:"kind"`
	Text     string                 `json:"text,omitempty"`
	File     *FilePart              `json:"file,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// FilePart represents a File segment within parts.
type FilePart struct {
	URI      string `json:"uri,omitempty"`
	Bytes    string `json:"bytes,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Task represents the state and execution context of a task.
type Task struct {
	Kind      Kind           `json:"kind"`
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	History   []Message      `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus represents the current state and accompanying message of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp *string   `json:"timestamp,omitempty"`
}

// Artifact represents an artifact generated for a task.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Extensions  []string       `json:"extensions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// AgentInterface provides a declaration of a combination of the
// target url and the supported transport to interact with the agent.
type AgentInterface struct {
	Transport string `json:"transport"` // Required: supported transport protocol
	URL       string `json:"url"`       // Required: URL for this interface
}

// AgentCard conveys key information about an agent.
type AgentCard struct {
	Name                              string                    `json:"name"`            // Required
	Version                           string                    `json:"version"`         // Required
	Description                       string                    `json:"description"`     // Required
	URL                               string                    `json:"url"`             // Required
	ProtocolVersion                   string                    `json:"protocolVersion"` // Required
	IconURL                           string                    `json:"iconUrl,omitempty"`
	DocumentationURL                  string                    `json:"documentationUrl,omitempty"`
	PreferredTransport                string                    `json:"preferredTransport,omitempty"`   // New field
	DefaultInputModes                 []string                  `json:"defaultInputModes"`              // Required
	DefaultOutputModes                []string                  `json:"defaultOutputModes"`             // Required
	Skills                            []AgentSkill              `json:"skills"`                         // Required
	Capabilities                      AgentCapabilities         `json:"capabilities"`                   // Required
	AdditionalInterfaces              []AgentInterface          `json:"additionalInterfaces,omitempty"` // New field
	Provider                          *AgentProvider            `json:"provider,omitempty"`
	Security                          []map[string][]string     `json:"security,omitempty"`
	SecuritySchemes                   map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	SupportsAuthenticatedExtendedCard bool                      `json:"supportsAuthenticatedExtendedCard,omitempty"`
}

// AgentSkill represents a unit of capability that an agent can perform.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
	Tags        []string `json:"tags"`
}

// AgentCapabilities defines optional capabilities supported by an agent.
type AgentCapabilities struct {
	Streaming              bool             `json:"streaming,omitempty"`
	PushNotifications      bool             `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool             `json:"stateTransitionHistory,omitempty"`
	Extensions             []AgentExtension `json:"extensions,omitempty"`
}

// AgentExtension represents a declaration of an extension supported by an Agent.
type AgentExtension struct {
	URI         string                 `json:"uri"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// AgentProvider represents the service provider of an agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

// SecurityScheme represents security scheme configuration.
type SecurityScheme struct {
	Type             SecurityType `json:"type"`
	Description      string       `json:"description,omitempty"`
	Name             string       `json:"name,omitempty"` // Required
	In               string       `json:"in,omitempty"`   // Required: "query", "header", or "cookie"
	Scheme           string       `json:"scheme,omitempty"`
	BearerFormat     string       `json:"bearerFormat,omitempty"`
	OpenIDConnectURL string       `json:"openIdConnectUrl,omitempty"`
	Flows            OAuthFlows   `json:"flows,omitempty"`
}

// OAuthFlows allows configuration of the supported OAuth Flows.
type OAuthFlows struct {
	Implicit          *ImplicitOAuthFlow          `json:"implicit,omitempty"`
	Password          *PasswordOAuthFlow          `json:"password,omitempty"`
	ClientCredentials *ClientCredentialsOAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode *AuthorizationCodeOAuthFlow `json:"authorizationCode,omitempty"`
}

// ImplicitOAuthFlow represents configuration details for a supported OAuth Flow.
type ImplicitOAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// PasswordOAuthFlow represents configuration details for a supported OAuth Flow.
type PasswordOAuthFlow struct {
	TokenURL   string            `json:"tokenUrl"`
	RefreshURL string            `json:"refreshUrl,omitempty"`
	Scopes     map[string]string `json:"scopes"`
}

// ClientCredentialsOAuthFlow represents configuration details for a supported OAuth Flow.
type ClientCredentialsOAuthFlow struct {
	TokenURL   string            `json:"tokenUrl"`
	RefreshURL string            `json:"refreshUrl,omitempty"`
	Scopes     map[string]string `json:"scopes"`
}

// AuthorizationCodeOAuthFlow represents configuration details for a supported OAuth Flow.
type AuthorizationCodeOAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl"`
	TokenURL         string            `json:"tokenUrl"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// PushNotificationConfig represents configuration for setting up push notifications for task updates.
type PushNotificationConfig struct {
	URL            string                              `json:"url"`
	ID             string                              `json:"id,omitempty"`
	Token          string                              `json:"token,omitempty"`
	Authentication *PushNotificationAuthenticationInfo `json:"authentication,omitempty"`
}

// PushNotificationAuthenticationInfo defines authentication details for push notifications.
type PushNotificationAuthenticationInfo struct {
	Schemes     []string `json:"schemes"`
	Credentials string   `json:"credentials,omitempty"`
}

// TaskStatusUpdateEvent is sent by server during sendStream or subscribe requests.
type TaskStatusUpdateEvent struct {
	Kind      Kind                   `json:"kind"`
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId"`
	Status    TaskStatus             `json:"status"`
	Final     bool                   `json:"final"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent is sent by server during sendStream or subscribe requests.
type TaskArtifactUpdateEvent struct {
	Kind      Kind                   `json:"kind"`
	TaskID    string                 `json:"taskId"`
	ContextID string                 `json:"contextId"`
	Artifact  Artifact               `json:"artifact"`
	Append    bool                   `json:"append,omitempty"`
	LastChunk bool                   `json:"lastChunk,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// =============================================================================
// VALIDATION METHODS
// =============================================================================

// Validate validates a Message structure according to A2A specification.
func (m *Message) Validate() error {
	if m.Kind != KindMessage {
		return fmt.Errorf("message kind must be %q, got %q", KindMessage, m.Kind)
	}

	if m.MessageID == "" {
		return fmt.Errorf("messageId is required")
	}

	if m.Role == "" {
		return fmt.Errorf("role is required")
	}

	if !m.Role.IsValid() {
		return fmt.Errorf("invalid role %q, must be one of: %s", m.Role, commasJoin(validRoles()))
	}

	if len(m.Parts) == 0 {
		return fmt.Errorf("parts is required and must not be empty")
	}

	for i, part := range m.Parts {
		if err := part.Validate(); err != nil {
			return fmt.Errorf("parts[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a Part structure according to A2A specification.
func (p *Part) Validate() error {
	if p.Kind == "" {
		return fmt.Errorf("kind is required")
	}

	if !isValidPartKind(p.Kind) {
		return fmt.Errorf("invalid part kind %q, must be one of: %s", p.Kind, commasJoin(validPartKinds()))
	}

	switch p.Kind {
	case KindTextPart:
		if p.Text == "" {
			return fmt.Errorf("text is required for text part")
		}
		if p.File != nil || p.Data != nil {
			return fmt.Errorf("text part cannot have file or data fields")
		}

	case KindFilePart:
		if p.File == nil {
			return fmt.Errorf("file is required for file part")
		}
		if err := p.File.Validate(); err != nil {
			return fmt.Errorf("file: %w", err)
		}
		if p.Text != "" || p.Data != nil {
			return fmt.Errorf("file part cannot have text or data fields")
		}

	case KindDataPart:
		if p.Data == nil {
			return fmt.Errorf("data is required for data part")
		}
		if p.Text != "" || p.File != nil {
			return fmt.Errorf("data part cannot have text or file fields")
		}
	}

	return nil
}

// Validate validates a FilePart structure according to A2A specification.
func (f *FilePart) Validate() error {
	hasURI := f.URI != ""
	hasBytes := f.Bytes != ""

	if hasURI && hasBytes {
		return fmt.Errorf("file part cannot have both uri and bytes")
	}

	if !hasURI && !hasBytes {
		return fmt.Errorf("file part must have either uri or bytes")
	}

	return nil
}

// Validate validates a Task structure according to A2A specification.
func (t *Task) Validate() error {
	if t.Kind != KindTask {
		return fmt.Errorf("task kind must be %q, got %q", KindTask, t.Kind)
	}

	if t.ID == "" {
		return fmt.Errorf("id is required")
	}

	if t.ContextID == "" {
		return fmt.Errorf("contextId is required")
	}

	if err := t.Status.Validate(); err != nil {
		return fmt.Errorf("status: %w", err)
	}

	for i, message := range t.History {
		if err := message.Validate(); err != nil {
			return fmt.Errorf("history[%d]: %w", i, err)
		}
	}

	for i, artifact := range t.Artifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("artifacts[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a TaskStatus structure according to A2A specification.
func (ts *TaskStatus) Validate() error {
	if ts.State == "" {
		return fmt.Errorf("state is required")
	}

	if !ts.State.IsValid() {
		return fmt.Errorf("invalid task state %q, must be one of: %s", ts.State, commasJoin(validTaskStates()))
	}

	if ts.Message != nil {
		if err := ts.Message.Validate(); err != nil {
			return fmt.Errorf("message: %w", err)
		}
	}

	return nil
}

// Validate validates an Artifact structure according to A2A specification.
func (a *Artifact) Validate() error {
	if a.ArtifactID == "" {
		return fmt.Errorf("artifactId is required")
	}

	if len(a.Parts) == 0 {
		return fmt.Errorf("parts is required and must not be empty")
	}

	for i, part := range a.Parts {
		if err := part.Validate(); err != nil {
			return fmt.Errorf("parts[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a JSON-RPC request according to A2A specification.
func (r *JSONRPCRequest) Validate() error {
	if r.JSONRpc != "2.0" {
		return fmt.Errorf("jsonrpc must be \"2.0\", got %q", r.JSONRpc)
	}

	if r.Method == "" {
		return fmt.Errorf("method is required")
	}

	if !isValidMethod(r.Method) {
		return fmt.Errorf("invalid A2A method %q, must be one of: %s", r.Method, validMethodsString())
	}

	if r.ID == nil {
		return fmt.Errorf("id is required")
	}

	return nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// isValidMethod returns true if the method is a valid A2A method.
func isValidMethod(method string) bool {
	switch method {
	case MethodSendMessage, MethodSendStreamingMessage, MethodGetTask,
		MethodCancelTask, MethodTaskResubscription,
		MethodSetTaskPushNotificationConfig, MethodGetTaskPushNotificationConfig,
		MethodListTaskPushNotificationConfig, MethodDeleteTaskPushNotificationConfig:
		return true
	default:
		return false
	}
}

// validMethodsString returns a comma-separated string of valid methods.
func validMethodsString() string {
	methods := []string{
		MethodSendMessage, MethodSendStreamingMessage, MethodGetTask,
		MethodCancelTask, MethodTaskResubscription,
		MethodSetTaskPushNotificationConfig, MethodGetTaskPushNotificationConfig,
		MethodListTaskPushNotificationConfig, MethodDeleteTaskPushNotificationConfig,
	}
	return strings.Join(methods, ", ")
}

// Helper functions for validation
func commasJoin[T fmt.Stringer](items []T) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, item.String())
	}
	return strings.Join(parts, ", ")
}

func validRoles() []Role {
	return []Role{RoleUser, RoleAgent}
}

func isValidPartKind(kind Kind) bool {
	switch kind {
	case KindTextPart, KindFilePart, KindDataPart:
		return true
	default:
		return false
	}
}

func validPartKinds() []Kind {
	return []Kind{KindTextPart, KindFilePart, KindDataPart}
}

func validTaskStates() []TaskState {
	return []TaskState{
		TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired,
		TaskStateCompleted, TaskStateCanceled, TaskStateFailed,
		TaskStateRejected, TaskStateAuthRequired, TaskStateUnknown,
	}
}

// =============================================================================
// HELPER CONSTRUCTORS
// =============================================================================

// NewTextPart creates a new text part.
func NewTextPart(text string) Part {
	return Part{
		Kind: KindTextPart,
		Text: text,
	}
}

// NewFilePart creates a new file part with URI.
func NewFilePart(uri, name, mimeType string) Part {
	return Part{
		Kind: KindFilePart,
		File: &FilePart{
			URI:      uri,
			Name:     name,
			MimeType: mimeType,
		},
	}
}

// NewFilePartWithBytes creates a new file part with base64 bytes.
func NewFilePartWithBytes(bytes, name, mimeType string) Part {
	return Part{
		Kind: KindFilePart,
		File: &FilePart{
			Bytes:    bytes,
			Name:     name,
			MimeType: mimeType,
		},
	}
}

// NewDataPart creates a new data part.
func NewDataPart(data map[string]interface{}) Part {
	return Part{
		Kind: KindDataPart,
		Data: data,
	}
}

// MessageOptions represents optional fields for message creation.
type MessageOptions struct {
	ReferenceTaskIDs []string               `json:"reference_task_ids,omitempty"`
	Extensions       []string               `json:"extensions,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// NewMessage creates a new message with optional configuration.
func NewMessage(messageID string, role Role, parts []Part, optFns ...func(*MessageOptions)) Message {
	msg := Message{
		Kind:      KindMessage,
		MessageID: messageID,
		Role:      role,
		Parts:     parts,
	}

	// Apply options if provided
	if len(optFns) > 0 {
		opts := &MessageOptions{}
		for _, fn := range optFns {
			fn(opts)
		}

		msg.ReferenceTaskIDs = opts.ReferenceTaskIDs
		msg.Extensions = opts.Extensions
		msg.Metadata = opts.Metadata
	}

	return msg
}

// NewTask creates a new task.
func NewTask(id string, contextID string, state TaskState) Task {
	return Task{
		Kind:      KindTask,
		ID:        id,
		ContextID: contextID,
		Status: TaskStatus{
			State: state,
		},
	}
}

// SetTimestamp sets the timestamp for task status.
func (ts *TaskStatus) SetTimestamp(t time.Time) {
	timestamp := t.Format(time.RFC3339)
	ts.Timestamp = &timestamp
}

// GetTimestamp parses and returns the timestamp.
func (ts *TaskStatus) GetTimestamp() (*time.Time, error) {
	if ts.Timestamp == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *ts.Timestamp)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// =============================================================================
// JSON-RPC CONSTRUCTOR FUNCTIONS
// =============================================================================

// NewJSONRPCError creates a new JSON-RPC error with the specified code and optional data
func NewJSONRPCError(code int, data interface{}) *JSONRPCError {
	return &JSONRPCError{
		Code:    code,
		Message: ErrorCodeText(code),
		Data:    data,
	}
}

// NewJSONRPCErrorWithMessage creates a new JSON-RPC error with a custom message
func NewJSONRPCErrorWithMessage(code int, message string, data interface{}) *JSONRPCError {
	return &JSONRPCError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

func NewJSONRPCInternalError(message string, data interface{}) *JSONRPCError {
	return NewJSONRPCErrorWithMessage(ErrorCodeInternalError, message, data)
}

func NewJSONRPCInvalidParamsError(message string) *JSONRPCError {
	return NewJSONRPCErrorWithMessage(ErrorCodeInvalidParams, message, nil)
}

func NewJSONRPCTaskNotFoundError(taskID string) *JSONRPCError {
	return NewJSONRPCError(ErrorCodeTaskNotFound, map[string]string{"taskId": taskID})
}

func NewJSONRPCMethodNotFoundError(method string) *JSONRPCError {
	return NewJSONRPCError(ErrorCodeMethodNotFound, map[string]string{"method": method})
}

// NewJSONRPCRequest creates a new JSON-RPC request.
func NewJSONRPCRequest(method string, params interface{}, id interface{}) JSONRPCRequest {
	return JSONRPCRequest{
		JSONRpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}
}

// NewJSONRPCResponse creates a new JSON-RPC success response.
func NewJSONRPCResponse(result interface{}, id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRpc: "2.0",
		Result:  result,
		ID:      id,
	}
}

// NewJSONRPCErrorResponse creates a new JSON-RPC error response.
func NewJSONRPCErrorResponse(code int, message string, data interface{}, id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRpc: "2.0",
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
}

// Predefined error responses

// NewParseError creates a parse error response.
func NewParseError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeParseError, ErrorCodeText(ErrorCodeParseError), nil, id)
}

// NewInvalidRequestError creates an invalid request error response.
func NewInvalidRequestError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeInvalidRequest, ErrorCodeText(ErrorCodeInvalidRequest), nil, id)
}

// NewMethodNotFoundError creates a method not found error response.
func NewMethodNotFoundError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeMethodNotFound, ErrorCodeText(ErrorCodeMethodNotFound), nil, id)
}

// NewInvalidParamsError creates an invalid parameters error response.
func NewInvalidParamsError(id interface{}, data interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeInvalidParams, ErrorCodeText(ErrorCodeInvalidParams), data, id)
}

// NewInternalError creates an internal error response.
func NewInternalError(id interface{}, data interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeInternalError, ErrorCodeText(ErrorCodeInternalError), data, id)
}

// NewTaskNotFoundError creates a task not found error response.
func NewTaskNotFoundError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeTaskNotFound, ErrorCodeText(ErrorCodeTaskNotFound), nil, id)
}

// NewTaskNotCancelableError creates a task not cancelable error response.
func NewTaskNotCancelableError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeTaskNotCancelable, ErrorCodeText(ErrorCodeTaskNotCancelable), nil, id)
}

// NewUnsupportedOperationError creates an unsupported operation error response.
func NewUnsupportedOperationError(id interface{}) JSONRPCResponse {
	return NewJSONRPCErrorResponse(ErrorCodeUnsupportedOperation, ErrorCodeText(ErrorCodeUnsupportedOperation), nil, id)
}
