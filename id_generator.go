package atlasic

import "github.com/google/uuid"

//go:generate go tool mockgen -source=id_generator.go -destination=mock_id_generator_test.go -package=atlasic

// IDGenerator provides unique ID generation for A2A entities
type IDGenerator interface {
	// GenerateTaskID generates a unique task identifier
	GenerateTaskID() string
	// GenerateContextID generates a unique context identifier
	GenerateContextID() string
	// GenerateMessageID generates a unique message identifier
	GenerateMessageID() string
	// GeneratePushNotificationConfigID generates a unique push notification config identifier
	GeneratePushNotificationConfigID() string
}

// DefaultIDGenerator implements IDGenerator using UUID v7
type DefaultIDGenerator struct{}

// GenerateTaskID generates a task ID using UUID v7
func (g *DefaultIDGenerator) GenerateTaskID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// GenerateContextID generates a context ID using UUID v7
func (g *DefaultIDGenerator) GenerateContextID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// GenerateMessageID generates a message ID using UUID v7
func (g *DefaultIDGenerator) GenerateMessageID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// GeneratePushNotificationConfigID generates a push notification config ID using UUID v7
func (g *DefaultIDGenerator) GeneratePushNotificationConfigID() string {
	return uuid.Must(uuid.NewV7()).String()
}
