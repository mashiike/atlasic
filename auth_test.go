package atlasic

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mashiike/atlasic/a2a"
	"github.com/mashiike/atlasic/transport"
)

func TestStaticAPIKeyAuthenticator(t *testing.T) {
	tests := []struct {
		name           string
		auth           StaticAPIKeyAuthenticator
		headers        map[string]string
		expectError    bool
		expectedScheme string
	}{
		{
			name:           "valid API key with default header",
			auth:           StaticAPIKeyAuthenticator{APIKey: "secret-key"},
			headers:        map[string]string{"X-API-Key": "secret-key"},
			expectError:    false,
			expectedScheme: "apiKey",
		},
		{
			name:           "valid API key with custom header",
			auth:           StaticAPIKeyAuthenticator{APIKey: "secret-key", HeaderName: "X-Custom-Key"},
			headers:        map[string]string{"X-Custom-Key": "secret-key"},
			expectError:    false,
			expectedScheme: "apiKey",
		},
		{
			name:           "missing API key header",
			auth:           StaticAPIKeyAuthenticator{APIKey: "secret-key"},
			headers:        map[string]string{},
			expectError:    true,
			expectedScheme: "apiKey",
		},
		{
			name:           "invalid API key",
			auth:           StaticAPIKeyAuthenticator{APIKey: "secret-key"},
			headers:        map[string]string{"X-API-Key": "wrong-key"},
			expectError:    true,
			expectedScheme: "apiKey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			result, err := tt.auth.Authenticate(context.Background(), req)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				// Check if it's an AuthError
				if authErr, ok := err.(*transport.AuthError); ok {
					if authErr.Scheme != tt.expectedScheme {
						t.Errorf("expected scheme %s but got %s", tt.expectedScheme, authErr.Scheme)
					}
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
					return
				}
				if result == nil {
					t.Errorf("expected request but got nil")
				}
			}
		})
	}
}

func TestStaticAPIKeyAuthenticator_SecuritySchemes(t *testing.T) {
	tests := []struct {
		name           string
		auth           StaticAPIKeyAuthenticator
		expectedHeader string
	}{
		{
			name:           "default header name",
			auth:           StaticAPIKeyAuthenticator{APIKey: "key"},
			expectedHeader: "X-API-Key",
		},
		{
			name:           "custom header name",
			auth:           StaticAPIKeyAuthenticator{APIKey: "key", HeaderName: "Authorization"},
			expectedHeader: "Authorization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schemes := tt.auth.GetSecuritySchemes()

			apiKeyScheme, exists := schemes["apiKey"]
			if !exists {
				t.Errorf("expected apiKey scheme to exist")
				return
			}

			if apiKeyScheme.Type != a2a.SecurityTypeAPIKey {
				t.Errorf("expected type %s but got %s", a2a.SecurityTypeAPIKey, apiKeyScheme.Type)
			}

			if apiKeyScheme.Name != tt.expectedHeader {
				t.Errorf("expected header name %s but got %s", tt.expectedHeader, apiKeyScheme.Name)
			}

			if apiKeyScheme.In != "header" {
				t.Errorf("expected 'header' but got %s", apiKeyScheme.In)
			}
		})
	}
}

func TestJWTAuthenticator(t *testing.T) {
	secretKey := []byte("test-secret-key")

	// Create a valid JWT token
	createToken := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, _ := token.SignedString(secretKey)
		return tokenString
	}

	tests := []struct {
		name        string
		auth        *JWTAuthenticator
		token       string
		expectError bool
		checkClaims func(ctx context.Context) error
	}{
		{
			name: "valid JWT token",
			auth: NewJWTAuthenticator(secretKey),
			token: createToken(jwt.MapClaims{
				"sub": "user123",
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}),
			expectError: false,
			checkClaims: func(ctx context.Context) error {
				if subject, ok := GetJWTSubject(ctx); !ok || subject != "user123" {
					t.Errorf("expected subject 'user123' but got '%s'", subject)
				}
				return nil
			},
		},
		{
			name: "expired JWT token",
			auth: NewJWTAuthenticator(secretKey),
			token: createToken(jwt.MapClaims{
				"sub": "user123",
				"exp": time.Now().Add(-time.Hour).Unix(), // Expired
				"iat": time.Now().Add(-2 * time.Hour).Unix(),
			}),
			expectError: true,
		},
		{
			name: "valid JWT with audience validation",
			auth: NewJWTAuthenticator(secretKey).WithAudience("test-service"),
			token: createToken(jwt.MapClaims{
				"sub": "user123",
				"aud": "test-service",
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}),
			expectError: false,
		},
		{
			name: "invalid audience",
			auth: NewJWTAuthenticator(secretKey).WithAudience("test-service"),
			token: createToken(jwt.MapClaims{
				"sub": "user123",
				"aud": "wrong-service",
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}),
			expectError: true,
		},
		{
			name: "missing audience when required",
			auth: NewJWTAuthenticator(secretKey).WithAudience("test-service"),
			token: createToken(jwt.MapClaims{
				"sub": "user123",
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}),
			expectError: true,
		},
		{
			name: "custom validation failure",
			auth: NewJWTAuthenticator(secretKey).WithValidateFunc(func(claims jwt.MapClaims) error {
				if role, ok := claims["role"].(string); !ok || role != "admin" {
					return transport.NewAuthError(transport.AuthErrorCodeInsufficientScope, "admin role required")
				}
				return nil
			}),
			token: createToken(jwt.MapClaims{
				"sub":  "user123",
				"role": "user", // Not admin
				"exp":  time.Now().Add(time.Hour).Unix(),
				"iat":  time.Now().Unix(),
			}),
			expectError: true,
		},
		{
			name: "custom validation success",
			auth: NewJWTAuthenticator(secretKey).WithValidateFunc(func(claims jwt.MapClaims) error {
				if role, ok := claims["role"].(string); !ok || role != "admin" {
					return transport.NewAuthError(transport.AuthErrorCodeInsufficientScope, "admin role required")
				}
				return nil
			}),
			token: createToken(jwt.MapClaims{
				"sub":  "user123",
				"role": "admin",
				"exp":  time.Now().Add(time.Hour).Unix(),
				"iat":  time.Now().Unix(),
			}),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)

			result, err := tt.auth.Authenticate(context.Background(), req)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
					return
				}
				if result == nil {
					t.Errorf("expected request but got nil")
					return
				}

				// Check if claims are properly set in context
				if tt.checkClaims != nil {
					if err := tt.checkClaims(result.Context()); err != nil {
						t.Errorf("claims check failed: %v", err)
					}
				}
			}
		})
	}
}

func TestJWTAuthenticator_MissingBearer(t *testing.T) {
	auth := NewJWTAuthenticator([]byte("secret"))

	tests := []struct {
		name   string
		header string
	}{
		{"missing authorization header", ""},
		{"invalid header format", "InvalidFormat token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			_, err := auth.Authenticate(context.Background(), req)
			if err == nil {
				t.Errorf("expected error but got none")
			}
		})
	}
}

func TestJWTAuthenticator_SecuritySchemes(t *testing.T) {
	auth := NewJWTAuthenticator([]byte("secret"))
	schemes := auth.GetSecuritySchemes()

	bearerScheme, exists := schemes["bearer"]
	if !exists {
		t.Errorf("expected bearer scheme to exist")
		return
	}

	if bearerScheme.Type != a2a.SecurityTypeHTTP {
		t.Errorf("expected type %s but got %s", a2a.SecurityTypeHTTP, bearerScheme.Type)
	}

	if bearerScheme.Scheme != "bearer" {
		t.Errorf("expected scheme 'bearer' but got %s", bearerScheme.Scheme)
	}

	if bearerScheme.BearerFormat != "JWT" {
		t.Errorf("expected bearer format 'JWT' but got %s", bearerScheme.BearerFormat)
	}
}

func TestJWTHelperFunctions(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": "user123",
		"aud": "test-service",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := "test-token"

	ctx := context.Background()
	ctx = context.WithValue(ctx, jwtContextKey{}, claims)
	ctx = context.WithValue(ctx, jwtTokenContextKey{}, token)

	// Test GetJWTClaims
	if retrievedClaims, ok := GetJWTClaims(ctx); !ok {
		t.Errorf("expected to retrieve JWT claims")
	} else if retrievedClaims["sub"] != "user123" {
		t.Errorf("expected sub 'user123' but got %v", retrievedClaims["sub"])
	}

	// Test GetJWTToken
	if retrievedToken, ok := GetJWTToken(ctx); !ok {
		t.Errorf("expected to retrieve JWT token")
	} else if retrievedToken != token {
		t.Errorf("expected token '%s' but got '%s'", token, retrievedToken)
	}

	// Test GetJWTSubject
	if subject, ok := GetJWTSubject(ctx); !ok {
		t.Errorf("expected to retrieve JWT subject")
	} else if subject != "user123" {
		t.Errorf("expected subject 'user123' but got '%s'", subject)
	}

	// Test with empty context
	emptyCtx := context.Background()
	if _, ok := GetJWTClaims(emptyCtx); ok {
		t.Errorf("expected no claims from empty context")
	}
	if _, ok := GetJWTToken(emptyCtx); ok {
		t.Errorf("expected no token from empty context")
	}
	if _, ok := GetJWTSubject(emptyCtx); ok {
		t.Errorf("expected no subject from empty context")
	}
}
