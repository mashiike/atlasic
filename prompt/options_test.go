package prompt

import (
	"testing"
)

func TestFunctionalOptions(t *testing.T) {
	tests := []struct {
		name     string
		options  []Option
		expected config
	}{
		{
			name:    "default config",
			options: []Option{},
			expected: config{
				Format:           "markdown",
				MaxHistory:       -1,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
		{
			name:    "WithFormat",
			options: []Option{WithFormat("json")},
			expected: config{
				Format:           "json",
				MaxHistory:       -1,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
		{
			name:    "WithMaxHistory",
			options: []Option{WithMaxHistory(10)},
			expected: config{
				Format:           "markdown",
				MaxHistory:       10,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
		{
			name:    "WithoutFileParts",
			options: []Option{WithoutFileParts()},
			expected: config{
				Format:           "markdown",
				MaxHistory:       -1,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
		{
			name:    "WithoutArtifacts",
			options: []Option{WithoutArtifacts()},
			expected: config{
				Format:           "markdown",
				MaxHistory:       -1,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
		{
			name:    "multiple options",
			options: []Option{WithFormat("json"), WithMaxHistory(5), WithoutFileParts(), WithoutArtifacts()},
			expected: config{
				Format:           "json",
				MaxHistory:       5,
				IncludeHistory:   true,
				IncludeStatus:    true,
				IncludeTaskInfo:  true,
				IncludeFileParts: false,
				IncludeArtifacts: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			for _, opt := range tt.options {
				opt(cfg)
			}

			if cfg.Format != tt.expected.Format {
				t.Errorf("Format: expected %s, got %s", tt.expected.Format, cfg.Format)
			}
			if cfg.MaxHistory != tt.expected.MaxHistory {
				t.Errorf("MaxHistory: expected %d, got %d", tt.expected.MaxHistory, cfg.MaxHistory)
			}
			if cfg.IncludeHistory != tt.expected.IncludeHistory {
				t.Errorf("IncludeHistory: expected %t, got %t", tt.expected.IncludeHistory, cfg.IncludeHistory)
			}
			if cfg.IncludeStatus != tt.expected.IncludeStatus {
				t.Errorf("IncludeStatus: expected %t, got %t", tt.expected.IncludeStatus, cfg.IncludeStatus)
			}
			if cfg.IncludeTaskInfo != tt.expected.IncludeTaskInfo {
				t.Errorf("IncludeTaskInfo: expected %t, got %t", tt.expected.IncludeTaskInfo, cfg.IncludeTaskInfo)
			}
			if cfg.IncludeFileParts != tt.expected.IncludeFileParts {
				t.Errorf("IncludeFileParts: expected %t, got %t", tt.expected.IncludeFileParts, cfg.IncludeFileParts)
			}
			if cfg.IncludeArtifacts != tt.expected.IncludeArtifacts {
				t.Errorf("IncludeArtifacts: expected %t, got %t", tt.expected.IncludeArtifacts, cfg.IncludeArtifacts)
			}
		})
	}
}

func TestConvenienceFunctions(t *testing.T) {
	t.Run("Summary", func(t *testing.T) {
		cfg := defaultConfig()
		Summary()(cfg)

		expected := config{
			Format:           "summary",
			MaxHistory:       5,
			IncludeHistory:   true,
			IncludeStatus:    true,
			IncludeTaskInfo:  true,
			IncludeFileParts: false,
			IncludeArtifacts: false,
		}

		if cfg.Format != expected.Format {
			t.Errorf("Format: expected %s, got %s", expected.Format, cfg.Format)
		}
		if cfg.IncludeHistory != expected.IncludeHistory {
			t.Errorf("IncludeHistory: expected %t, got %t", expected.IncludeHistory, cfg.IncludeHistory)
		}
	})

	t.Run("Detailed", func(t *testing.T) {
		cfg := defaultConfig()
		Detailed()(cfg)

		expected := config{
			Format:           "detailed",
			MaxHistory:       -1,
			IncludeHistory:   true,
			IncludeStatus:    true,
			IncludeTaskInfo:  true,
			IncludeFileParts: true,
			IncludeArtifacts: true,
		}

		if cfg.Format != expected.Format {
			t.Errorf("Format: expected %s, got %s", expected.Format, cfg.Format)
		}
		if cfg.IncludeFileParts != expected.IncludeFileParts {
			t.Errorf("IncludeFileParts: expected %t, got %t", expected.IncludeFileParts, cfg.IncludeFileParts)
		}
		if cfg.IncludeArtifacts != expected.IncludeArtifacts {
			t.Errorf("IncludeArtifacts: expected %t, got %t", expected.IncludeArtifacts, cfg.IncludeArtifacts)
		}
	})

	t.Run("Minimal", func(t *testing.T) {
		cfg := defaultConfig()
		Minimal()(cfg)

		expected := config{
			Format:           "minimal",
			MaxHistory:       3,
			IncludeHistory:   true,
			IncludeStatus:    false,
			IncludeTaskInfo:  false,
			IncludeFileParts: false,
			IncludeArtifacts: false,
		}

		if cfg.Format != expected.Format {
			t.Errorf("Format: expected %s, got %s", expected.Format, cfg.Format)
		}
		if cfg.MaxHistory != expected.MaxHistory {
			t.Errorf("MaxHistory: expected %d, got %d", expected.MaxHistory, cfg.MaxHistory)
		}
		if cfg.IncludeStatus != expected.IncludeStatus {
			t.Errorf("IncludeStatus: expected %t, got %t", expected.IncludeStatus, cfg.IncludeStatus)
		}
		if cfg.IncludeTaskInfo != expected.IncludeTaskInfo {
			t.Errorf("IncludeTaskInfo: expected %t, got %t", expected.IncludeTaskInfo, cfg.IncludeTaskInfo)
		}
	})
}

func TestOptionComposition(t *testing.T) {
	// Test that options can be composed and the last one wins for conflicting settings
	cfg := defaultConfig()

	// Apply conflicting options
	WithFormat("json")(cfg)
	WithFormat("summary")(cfg) // This should override the previous one
	WithMaxHistory(10)(cfg)
	WithMaxHistory(20)(cfg) // This should override the previous one

	if cfg.Format != "summary" {
		t.Errorf("Expected format 'summary', got %s", cfg.Format)
	}
	if cfg.MaxHistory != 20 {
		t.Errorf("Expected MaxHistory 20, got %d", cfg.MaxHistory)
	}
}
