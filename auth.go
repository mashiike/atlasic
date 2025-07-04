package atlasic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

// Context keys for JWT authentication
type jwtContextKey struct{}
type jwtTokenContextKey struct{}

// GetJWTClaims retrieves JWT claims from the request context
func GetJWTClaims(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(jwtContextKey{}).(jwt.MapClaims)
	return claims, ok
}

// GetJWTToken retrieves the raw JWT token string from the request context
func GetJWTToken(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(jwtTokenContextKey{}).(string)
	return token, ok
}

// GetJWTSubject retrieves the subject (sub) claim from JWT
func GetJWTSubject(ctx context.Context) (string, bool) {
	claims, ok := GetJWTClaims(ctx)
	if !ok {
		return "", false
	}
	sub, ok := claims["sub"].(string)
	return sub, ok
}

// StaticAPIKeyAuthenticator implements a simple static API key authenticator
// This is a sample implementation for demonstration purposes that only checks API key matching
type StaticAPIKeyAuthenticator struct {
	APIKey     string // The expected API key value
	HeaderName string // The header name to check (default: X-API-Key)
}

// Authenticate implements the transport.Authenticator interface
func (s StaticAPIKeyAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*http.Request, error) {
	headerName := s.HeaderName
	if headerName == "" {
		headerName = "X-API-Key"
	}

	apiKey := r.Header.Get(headerName)
	if apiKey == "" {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeMissingCredentials,
			fmt.Sprintf("missing %s header", headerName),
			"apiKey",
		)
	}

	if apiKey != s.APIKey {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeInvalidCredentials,
			"invalid API key",
			"apiKey",
		)
	}

	// API key matched - return the request as-is (no additional context needed for this simple example)
	return r, nil
}

// GetSecuritySchemes implements the transport.Authenticator interface
func (s StaticAPIKeyAuthenticator) GetSecuritySchemes() map[string]a2a.SecurityScheme {
	headerName := s.HeaderName
	if headerName == "" {
		headerName = "X-API-Key"
	}

	return map[string]a2a.SecurityScheme{
		"apiKey": {
			Type:        a2a.SecurityTypeAPIKey,
			Description: "API key authentication",
			Name:        headerName,
			In:          "header",
		},
	}
}

// GetSecurityRequirements implements the transport.Authenticator interface
func (s StaticAPIKeyAuthenticator) GetSecurityRequirements() []map[string][]string {
	return []map[string][]string{
		{"apiKey": {}}, // No specific scopes required for this simple example
	}
}

// JWTAuthenticator implements JWT (JSON Web Token) based authentication
type JWTAuthenticator struct {
	// SecretKey is used for HMAC signing methods (HS256, HS384, HS512)
	SecretKey []byte

	// SigningMethod specifies the JWT signing method (default: HS256)
	SigningMethod jwt.SigningMethod

	// Audience specifies the expected audience (aud) claim
	// If empty, audience validation is skipped
	Audience string

	// ValidateFunc allows custom validation of JWT claims
	// If nil, only signature, expiration, and audience are validated
	ValidateFunc func(claims jwt.MapClaims) error
}

// NewJWTAuthenticator creates a new JWT authenticator with HMAC-SHA256
func NewJWTAuthenticator(secretKey []byte) *JWTAuthenticator {
	return &JWTAuthenticator{
		SecretKey:     secretKey,
		SigningMethod: jwt.SigningMethodHS256,
	}
}

// WithValidateFunc sets a custom validation function for JWT claims
func (j *JWTAuthenticator) WithValidateFunc(fn func(claims jwt.MapClaims) error) *JWTAuthenticator {
	j.ValidateFunc = fn
	return j
}

// WithSigningMethod sets the JWT signing method
func (j *JWTAuthenticator) WithSigningMethod(method jwt.SigningMethod) *JWTAuthenticator {
	j.SigningMethod = method
	return j
}

// WithAudience sets the expected audience for JWT validation
func (j *JWTAuthenticator) WithAudience(audience string) *JWTAuthenticator {
	j.Audience = audience
	return j
}

// Authenticate implements the transport.Authenticator interface
func (j *JWTAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*http.Request, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeMissingCredentials,
			"missing Authorization header",
			"bearer",
		)
	}

	// Parse Bearer token
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeInvalidCredentials,
			"invalid Authorization header format",
			"bearer",
		)
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	// Parse and validate JWT with standard claims validation
	token, err := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if token.Method != j.SigningMethod {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.SecretKey, nil
	})

	if err != nil {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeInvalidCredentials,
			fmt.Sprintf("invalid JWT: %v", err),
			"bearer",
		)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, transport.NewAuthErrorWithScheme(
			transport.AuthErrorCodeInvalidCredentials,
			"invalid JWT claims",
			"bearer",
		)
	}

	// Validate expiration (exp claim) - JWT library already validates this by default
	// Additional manual check for custom error message
	if expTime, err := claims.GetExpirationTime(); err == nil && expTime != nil {
		if expTime.Before(time.Now()) {
			return nil, transport.NewAuthErrorWithScheme(
				transport.AuthErrorCodeExpiredCredentials,
				"JWT token has expired",
				"bearer",
			)
		}
	}

	// Validate audience (aud claim) if specified
	if j.Audience != "" {
		if aud, ok := claims["aud"]; ok {
			var audienceValid bool
			switch audValue := aud.(type) {
			case string:
				audienceValid = audValue == j.Audience
			case []interface{}:
				for _, a := range audValue {
					if audStr, ok := a.(string); ok && audStr == j.Audience {
						audienceValid = true
						break
					}
				}
			}
			if !audienceValid {
				return nil, transport.NewAuthErrorWithScheme(
					transport.AuthErrorCodeInvalidCredentials,
					"invalid audience",
					"bearer",
				)
			}
		} else {
			return nil, transport.NewAuthErrorWithScheme(
				transport.AuthErrorCodeInvalidCredentials,
				"missing audience claim",
				"bearer",
			)
		}
	}

	// Custom validation if provided
	if j.ValidateFunc != nil {
		if err := j.ValidateFunc(claims); err != nil {
			return nil, transport.NewAuthErrorWithScheme(
				transport.AuthErrorCodeInvalidCredentials,
				fmt.Sprintf("JWT validation failed: %v", err),
				"bearer",
			)
		}
	}

	// Add JWT claims to request context using typed keys
	newCtx := context.WithValue(r.Context(), jwtContextKey{}, claims)
	newCtx = context.WithValue(newCtx, jwtTokenContextKey{}, tokenString)

	return r.WithContext(newCtx), nil
}

// GetSecuritySchemes implements the transport.Authenticator interface
func (j *JWTAuthenticator) GetSecuritySchemes() map[string]a2a.SecurityScheme {
	return map[string]a2a.SecurityScheme{
		"bearer": {
			Type:         a2a.SecurityTypeHTTP,
			Description:  "JWT Bearer token authentication",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}
}

// GetSecurityRequirements implements the transport.Authenticator interface
func (j *JWTAuthenticator) GetSecurityRequirements() []map[string][]string {
	return []map[string][]string{
		{"bearer": {}}, // JWT validation handles scopes internally
	}
}
