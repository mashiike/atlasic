package atlasic

import (
	"context"
	"testing"

	"github.com/mashiike/atlasic/a2a"
	"go.uber.org/mock/gomock"
)

func TestDisablePushNotifications_SetTaskPushNotificationConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Create agent service and disable push notifications for this test
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true

	config := a2a.TaskPushNotificationConfig{
		TaskID: "test-task",
		PushNotificationConfig: a2a.PushNotificationConfig{
			URL: "https://example.com/webhook",
		},
	}

	ctx := context.Background()

	// Call SetTaskPushNotificationConfig - should return UnsupportedOperation error
	_, err := agentService.SetTaskPushNotificationConfig(ctx, config)
	if err == nil {
		t.Fatal("Expected error when push notifications are disabled")
	}

	// Check error type
	if jsonErr, ok := err.(*a2a.JSONRPCError); ok {
		if jsonErr.Code != a2a.ErrorCodePushNotificationNotSupported {
			t.Errorf("Expected ErrorCodePushNotificationNotSupported (%d), got %d", a2a.ErrorCodePushNotificationNotSupported, jsonErr.Code)
		}
	} else {
		t.Errorf("Expected JSONRPCError, got %T", err)
	}
}

func TestDisablePushNotifications_GetTaskPushNotificationConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Create agent service and disable push notifications for this test
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true

	params := a2a.GetTaskPushNotificationConfigParams{
		ID: "test-task",
	}

	ctx := context.Background()

	// Call GetTaskPushNotificationConfig - should return UnsupportedOperation error
	_, err := agentService.GetTaskPushNotificationConfig(ctx, params)
	if err == nil {
		t.Fatal("Expected error when push notifications are disabled")
	}

	// Check error type
	if jsonErr, ok := err.(*a2a.JSONRPCError); ok {
		if jsonErr.Code != a2a.ErrorCodePushNotificationNotSupported {
			t.Errorf("Expected ErrorCodePushNotificationNotSupported (%d), got %d", a2a.ErrorCodePushNotificationNotSupported, jsonErr.Code)
		}
	} else {
		t.Errorf("Expected JSONRPCError, got %T", err)
	}
}

func TestDisablePushNotifications_AgentCard(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Setup agent metadata
	mockAgent.EXPECT().GetMetadata(gomock.Any()).Return(&AgentMetadata{
		Name:        "Test Agent",
		Description: "Test Description",
		Skills:      []a2a.AgentSkill{},
	}, nil)

	// Create agent service and disable push notifications for this test
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = true

	ctx := context.Background()

	// Get agent card
	card, err := agentService.GetAgentCard(ctx)
	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}

	// Check that push notification capability is disabled
	if card.Capabilities.PushNotifications {
		t.Error("Expected push notification capability to be disabled")
	}
}

func TestPushNotificationsEnabled_WithNilNotifier(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Create agent service and set PushNotifier to nil
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.PushNotifier = nil

	// Should return false even if DisablePushNotifications is false
	if agentService.pushNotificationsEnabled() {
		t.Error("Expected push notifications to be disabled when PushNotifier is nil")
	}
}

func TestPushNotificationsEnabled_Normal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks
	mockAgent := NewMockAgent(ctrl)
	mockStorage := NewMockStorage(ctrl)

	// Create agent service and enable push notifications
	agentService := NewAgentService(mockStorage, mockAgent)
	agentService.DisablePushNotifications = false // Enable push notifications

	// Should return true when enabled
	if !agentService.pushNotificationsEnabled() {
		t.Error("Expected push notifications to be enabled by default")
	}
}
