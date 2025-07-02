package transport

import (
	"reflect"
	"testing"

	"github.com/mashiike/atlasic/a2a"
)

func TestMatchesOutputMode(t *testing.T) {
	tests := []struct {
		name      string
		accepted  string
		supported string
		expected  bool
	}{
		{
			name:      "Exact match",
			accepted:  "text/plain",
			supported: "text/plain",
			expected:  true,
		},
		{
			name:      "No match",
			accepted:  "text/plain",
			supported: "application/json",
			expected:  false,
		},
		{
			name:      "Wildcard * matches any",
			accepted:  "*",
			supported: "text/plain",
			expected:  true,
		},
		{
			name:      "Wildcard */* matches any",
			accepted:  "*/*",
			supported: "application/json",
			expected:  true,
		},
		{
			name:      "Type wildcard text/* matches text/plain",
			accepted:  "text/*",
			supported: "text/plain",
			expected:  true,
		},
		{
			name:      "Type wildcard text/* matches text/markdown",
			accepted:  "text/*",
			supported: "text/markdown",
			expected:  true,
		},
		{
			name:      "Type wildcard text/* does not match application/json",
			accepted:  "text/*",
			supported: "application/json",
			expected:  false,
		},
		{
			name:      "Type wildcard application/* matches application/json",
			accepted:  "application/*",
			supported: "application/json",
			expected:  true,
		},
		{
			name:      "Type wildcard image/* matches image/png",
			accepted:  "image/*",
			supported: "image/png",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesOutputMode(tt.accepted, tt.supported)
			if result != tt.expected {
				t.Errorf("MatchesOutputMode(%q, %q) = %v, want %v", tt.accepted, tt.supported, result, tt.expected)
			}
		})
	}
}

func TestFindCompatibleOutputModes(t *testing.T) {
	tests := []struct {
		name          string
		acceptedModes []string
		supportedModes []string
		expected      []string
		expectError   bool
	}{
		{
			name:          "Exact match single mode",
			acceptedModes: []string{"text/plain"},
			supportedModes: []string{"text/plain", "application/json"},
			expected:      []string{"text/plain"},
			expectError:   false,
		},
		{
			name:          "Multiple exact matches",
			acceptedModes: []string{"text/plain", "application/json"},
			supportedModes: []string{"text/plain", "application/json", "text/markdown"},
			expected:      []string{"text/plain", "application/json"},
			expectError:   false,
		},
		{
			name:          "Wildcard * matches all",
			acceptedModes: []string{"*"},
			supportedModes: []string{"text/plain", "application/json"},
			expected:      []string{"text/plain", "application/json"},
			expectError:   false,
		},
		{
			name:          "Wildcard */* matches all",
			acceptedModes: []string{"*/*"},
			supportedModes: []string{"text/plain", "application/json"},
			expected:      []string{"text/plain", "application/json"},
			expectError:   false,
		},
		{
			name:          "Type wildcard text/* matches text types",
			acceptedModes: []string{"text/*"},
			supportedModes: []string{"text/plain", "text/markdown", "application/json"},
			expected:      []string{"text/plain", "text/markdown"},
			expectError:   false,
		},
		{
			name:          "Mixed patterns",
			acceptedModes: []string{"text/*", "application/json"},
			supportedModes: []string{"text/plain", "text/markdown", "application/json", "image/png"},
			expected:      []string{"text/plain", "text/markdown", "application/json"},
			expectError:   false,
		},
		{
			name:          "No matches",
			acceptedModes: []string{"text/plain"},
			supportedModes: []string{"application/json", "image/png"},
			expected:      nil,
			expectError:   true,
		},
		{
			name:          "Empty accepted defaults to */*",
			acceptedModes: []string{},
			supportedModes: []string{"text/plain", "application/json"},
			expected:      []string{"text/plain", "application/json"},
			expectError:   false,
		},
		{
			name:          "Priority order preference",
			acceptedModes: []string{"application/json", "text/*"},
			supportedModes: []string{"text/plain", "application/json"},
			expected:      []string{"text/plain", "application/json"},
			expectError:   false,
		},
		{
			name:          "No duplicates",
			acceptedModes: []string{"text/*", "text/plain"},
			supportedModes: []string{"text/plain"},
			expected:      []string{"text/plain"},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FindCompatibleOutputModes(tt.acceptedModes, tt.supportedModes)

			if tt.expectError {
				if err == nil {
					t.Errorf("FindCompatibleOutputModes() expected error, got nil")
				}
				if _, ok := err.(*a2a.JSONRPCError); !ok {
					t.Errorf("FindCompatibleOutputModes() expected JSONRPCError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("FindCompatibleOutputModes() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("FindCompatibleOutputModes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFindCompatibleOutputModes_ErrorCode(t *testing.T) {
	acceptedModes := []string{"text/plain"}
	supportedModes := []string{"application/json"}

	_, err := FindCompatibleOutputModes(acceptedModes, supportedModes)
	if err == nil {
		t.Fatal("FindCompatibleOutputModes() expected error, got nil")
	}

	jsonRPCErr, ok := err.(*a2a.JSONRPCError)
	if !ok {
		t.Fatalf("FindCompatibleOutputModes() expected *a2a.JSONRPCError, got %T", err)
	}

	if jsonRPCErr.Code != a2a.ErrorCodeContentTypeNotSupported {
		t.Errorf("Expected error code %d, got %d", a2a.ErrorCodeContentTypeNotSupported, jsonRPCErr.Code)
	}

	// Check error data contains both acceptedModes and supportedModes
	data, ok := jsonRPCErr.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected error data to be map[string]interface{}, got %T", jsonRPCErr.Data)
	}

	if _, exists := data["acceptedModes"]; !exists {
		t.Error("Error data should contain 'acceptedModes'")
	}

	if _, exists := data["supportedModes"]; !exists {
		t.Error("Error data should contain 'supportedModes'")
	}
}