package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

type Extension interface {
	GetMetadata() a2a.AgentExtension
}

type RPCMethodExtension interface {
	Extension
	MethodName() string
	MethodHandler() JSONRPCMethodHandler
}

type DataOnlyExtension interface {
	Extension
	EnrichAgentCard(builder *AgentCardBuilder) error
}

type ProfileExtension interface {
	Extension
	PrepareContext(ctx context.Context, method string, params json.RawMessage) (context.Context, error)
}

type BaseProfileExtension struct{}

func (b *BaseProfileExtension) PrepareContext(ctx context.Context, method string, params json.RawMessage) (context.Context, error) {
	return ctx, nil
}

// Method-specific profile extensions - one interface per method

// SendMessageProfileExtension provides preprocessing for message/send method
type SendMessageProfileExtension interface {
	Extension
	PrepareSendMessage(ctx context.Context, params *a2a.MessageSendParams) (context.Context, error)
}

// SendStreamingMessageProfileExtension provides preprocessing for message/streaming method
type SendStreamingMessageProfileExtension interface {
	Extension
	PrepareSendStreamingMessage(ctx context.Context, params *a2a.MessageSendParams) (context.Context, error)
}

// GetTaskProfileExtension provides preprocessing for task/get method
type GetTaskProfileExtension interface {
	Extension
	PrepareGetTask(ctx context.Context, params *a2a.TaskQueryParams) (context.Context, error)
}

// CancelTaskProfileExtension provides preprocessing for task/cancel method
type CancelTaskProfileExtension interface {
	Extension
	PrepareCancelTask(ctx context.Context, params *a2a.TaskIDParams) (context.Context, error)
}

// TaskResubscriptionProfileExtension provides preprocessing for task/resubscription method
type TaskResubscriptionProfileExtension interface {
	Extension
	PrepareTaskResubscription(ctx context.Context, params *a2a.TaskIDParams) (context.Context, error)
}

// SetTaskPushNotificationConfigProfileExtension provides preprocessing for push notification config set
type SetTaskPushNotificationConfigProfileExtension interface {
	Extension
	PrepareSetTaskPushNotificationConfig(ctx context.Context, params *a2a.TaskPushNotificationConfig) (context.Context, error)
}

// GetTaskPushNotificationConfigProfileExtension provides preprocessing for push notification config get
type GetTaskPushNotificationConfigProfileExtension interface {
	Extension
	PrepareGetTaskPushNotificationConfig(ctx context.Context, params *a2a.GetTaskPushNotificationConfigParams) (context.Context, error)
}

// ListTaskPushNotificationConfigProfileExtension provides preprocessing for push notification config list
type ListTaskPushNotificationConfigProfileExtension interface {
	Extension
	PrepareListTaskPushNotificationConfig(ctx context.Context, params *a2a.TaskIDParams) (context.Context, error)
}

// DeleteTaskPushNotificationConfigProfileExtension provides preprocessing for push notification config delete
type DeleteTaskPushNotificationConfigProfileExtension interface {
	Extension
	PrepareDeleteTaskPushNotificationConfig(ctx context.Context, params *a2a.DeleteTaskPushNotificationConfigParams) (context.Context, error)
}

type AgentCardBuilder struct {
	base       a2a.AgentCard
	extensions map[string]any
}

func NewAgentCardBuilder(base a2a.AgentCard) *AgentCardBuilder {
	return &AgentCardBuilder{
		base:       base,
		extensions: make(map[string]any),
	}
}

func (b *AgentCardBuilder) AddExtensionField(key string, value any) error {
	if isReservedField(key) {
		return fmt.Errorf("cannot override reserved field: %s", key)
	}
	
	b.extensions[key] = value
	return nil
}

func (b *AgentCardBuilder) Build() (map[string]any, error) {
	result := structToMap(b.base)
	
	maps.Copy(result, b.extensions)
	
	if err := b.validate(result); err != nil {
		return nil, err
	}
	
	return result, nil
}

func (b *AgentCardBuilder) validate(result map[string]any) error {
	if _, ok := result["name"]; !ok {
		return fmt.Errorf("agent card must have a name")
	}
	if _, ok := result["description"]; !ok {
		return fmt.Errorf("agent card must have a description")
	}
	return nil
}

func isReservedField(key string) bool {
	reservedFields := []string{
		"name", "description", "version", "url",
		"capabilities", "defaultInputModes", "defaultOutputModes",
		"skills", "metadata",
	}
	return slices.Contains(reservedFields, key)
}

func structToMap(obj any) map[string]any {
	result := make(map[string]any)
	val := reflect.ValueOf(obj)
	typ := reflect.TypeOf(obj)
	
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)
		
		jsonTag := fieldType.Tag.Get("json")
		if jsonTag == "" {
			jsonTag = fieldType.Name
		} else {
			if idx := len(jsonTag); idx > 0 {
				for j, r := range jsonTag {
					if r == ',' {
						jsonTag = jsonTag[:j]
						break
					}
				}
			}
		}
		
		if jsonTag != "-" {
			result[jsonTag] = field.Interface()
		}
	}
	
	return result
}

type rpcMethodExtensionHandler struct {
	handler    JSONRPCMethodHandler
	methodName string
	metadata   a2a.AgentExtension
}

func (r *rpcMethodExtensionHandler) ServeRPC(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// Check if this extension is active
	activeExtensions := GetActiveExtensions(ctx)
	if !isExtensionActive(activeExtensions, r.metadata.URI) {
		return nil, &a2a.JSONRPCError{
			Code:    a2a.ErrorCodeMethodNotFound,
			Message: a2a.ErrorCodeText(a2a.ErrorCodeMethodNotFound),
			Data:    fmt.Sprintf("Extension method %s not activated", r.methodName),
		}
	}

	return r.handler(ctx, params)
}

// Context keys for extension management
type contextKey string

const (
	activeExtensionsKey contextKey = "activeExtensions"
)

// WithActiveExtensions adds active extensions to context
func WithActiveExtensions(ctx context.Context, extensions []Extension) context.Context {
	return context.WithValue(ctx, activeExtensionsKey, extensions)
}

// GetActiveExtensions retrieves active extensions from context
func GetActiveExtensions(ctx context.Context) []Extension {
	if extensions, ok := ctx.Value(activeExtensionsKey).([]Extension); ok {
		return extensions
	}
	return nil
}

// ParseExtensionHeader parses X-A2A-Extensions header
func ParseExtensionHeader(header string) []string {
	if header == "" {
		return nil
	}

	var extensions []string
	for _, ext := range strings.Split(header, ",") {
		ext = strings.TrimSpace(ext)
		if ext != "" {
			extensions = append(extensions, ext)
		}
	}
	return extensions
}

// ResolveActiveExtensions resolves which extensions should be active
func ResolveActiveExtensions(availableExtensions map[string]Extension, requestedURIs []string) []Extension {
	var activeExtensions []Extension
	for _, uri := range requestedURIs {
		if ext, exists := availableExtensions[uri]; exists {
			activeExtensions = append(activeExtensions, ext)
		}
	}
	return activeExtensions
}

// ValidateRequiredExtensions checks if all required extensions are activated
func ValidateRequiredExtensions(availableExtensions map[string]Extension, activeURIs []string) error {
	for uri, ext := range availableExtensions {
		metadata := ext.GetMetadata()
		if metadata.Required && !slices.Contains(activeURIs, uri) {
			return fmt.Errorf("required extension not activated: %s", uri)
		}
	}
	return nil
}

// FormatExtensionHeader formats extension URIs for response header
func FormatExtensionHeader(extensions []Extension) string {
	var uris []string
	for _, ext := range extensions {
		uris = append(uris, ext.GetMetadata().URI)
	}
	return strings.Join(uris, ", ")
}

// isExtensionActive checks if an extension URI is in the active list
func isExtensionActive(activeExtensions []Extension, uri string) bool {
	for _, ext := range activeExtensions {
		if ext.GetMetadata().URI == uri {
			return true
		}
	}
	return false
}
