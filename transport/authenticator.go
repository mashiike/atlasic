package transport

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mashiike/atlasic/a2a"
)

// Authenticator defines the interface for authentication providers
type Authenticator interface {
	// Authenticate verifies the authentication credentials in the request
	// and returns a new request with authentication context added
	Authenticate(ctx context.Context, r *http.Request) (*http.Request, error)
	
	// GetSecuritySchemes returns the security schemes supported by this authenticator
	// This information is used to populate the AgentCard
	GetSecuritySchemes() map[string]a2a.SecurityScheme
	
	// GetSecurityRequirements returns the security requirements for this authenticator
	// This information is used to populate the AgentCard
	GetSecurityRequirements() []map[string][]string
}

// AuthError represents an authentication error
type AuthError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Scheme  string `json:"scheme,omitempty"`
}

func (e *AuthError) Error() string {
	if e.Scheme != "" {
		return fmt.Sprintf("authentication failed [%s:%s]: %s", e.Scheme, e.Code, e.Message)
	}
	return fmt.Sprintf("authentication failed [%s]: %s", e.Code, e.Message)
}

// Common auth error codes
const (
	AuthErrorCodeMissingCredentials = "missing_credentials"
	AuthErrorCodeInvalidCredentials = "invalid_credentials"
	AuthErrorCodeExpiredCredentials = "expired_credentials"
	AuthErrorCodeInsufficientScope  = "insufficient_scope"
)

// NewAuthError creates a new authentication error
func NewAuthError(code, message string) *AuthError {
	return &AuthError{
		Code:    code,
		Message: message,
	}
}

// NewAuthErrorWithScheme creates a new authentication error with scheme information
func NewAuthErrorWithScheme(code, message, scheme string) *AuthError {
	return &AuthError{
		Code:    code,
		Message: message,
		Scheme:  scheme,
	}
}

