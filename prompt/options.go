package prompt

// Option represents a functional option for configuring builder operations
type Option func(*config)

// config holds configuration options for formatting operations
type config struct {
	Format           string // "markdown", "json", "xml", "summary", "detailed", "minimal"
	IncludeFileParts bool   // Include file parts in output
	IncludeArtifacts bool   // Include artifacts in output
	IncludeTaskInfo  bool   // Include task basic information
	IncludeStatus    bool   // Include task status
	IncludeHistory   bool   // Include conversation history
	MaxHistory       int    // Maximum number of history messages (-1 for all)
}

// defaultConfig returns the default configuration
func defaultConfig() *config {
	return &config{
		Format:           "markdown",
		IncludeFileParts: false, // Default: convert to tool references
		IncludeArtifacts: false, // Default: convert to tool references
		IncludeTaskInfo:  true,
		IncludeStatus:    true,
		IncludeHistory:   true,
		MaxHistory:       -1, // All history by default
	}
}

// WithFormat sets the output format
func WithFormat(format string) Option {
	return func(c *config) {
		c.Format = format
	}
}

// WithMaxHistory sets the maximum number of history messages to include
func WithMaxHistory(n int) Option {
	return func(c *config) {
		c.MaxHistory = n
	}
}

// WithFileParts includes file parts in the output (instead of tool references)
func WithFileParts() Option {
	return func(c *config) {
		c.IncludeFileParts = true
	}
}

// WithoutFileParts excludes file parts from output (converts to tool references)
func WithoutFileParts() Option {
	return func(c *config) {
		c.IncludeFileParts = false
	}
}

// WithArtifacts includes artifacts in the output (instead of tool references)
func WithArtifacts() Option {
	return func(c *config) {
		c.IncludeArtifacts = true
	}
}

// WithoutArtifacts excludes artifacts from output (converts to tool references)
func WithoutArtifacts() Option {
	return func(c *config) {
		c.IncludeArtifacts = false
	}
}

// WithTaskInfo includes task basic information
func WithTaskInfo() Option {
	return func(c *config) {
		c.IncludeTaskInfo = true
	}
}

// WithoutTaskInfo excludes task basic information
func WithoutTaskInfo() Option {
	return func(c *config) {
		c.IncludeTaskInfo = false
	}
}

// WithStatus includes task status information
func WithStatus() Option {
	return func(c *config) {
		c.IncludeStatus = true
	}
}

// WithoutStatus excludes task status information
func WithoutStatus() Option {
	return func(c *config) {
		c.IncludeStatus = false
	}
}

// WithHistory includes conversation history
func WithHistory() Option {
	return func(c *config) {
		c.IncludeHistory = true
	}
}

// WithoutHistory excludes conversation history
func WithoutHistory() Option {
	return func(c *config) {
		c.IncludeHistory = false
	}
}

// Summary is a convenience option for summary format with limited content
func Summary() Option {
	return func(c *config) {
		c.Format = "summary"
		c.IncludeFileParts = false
		c.IncludeArtifacts = false
		c.MaxHistory = 5
	}
}

// Detailed is a convenience option for detailed format with full content
func Detailed() Option {
	return func(c *config) {
		c.Format = "detailed"
		c.IncludeFileParts = true
		c.IncludeArtifacts = true
		c.MaxHistory = -1
	}
}

// Minimal is a convenience option for minimal format with essential content only
func Minimal() Option {
	return func(c *config) {
		c.Format = "minimal"
		c.IncludeFileParts = false
		c.IncludeArtifacts = false
		c.IncludeTaskInfo = false
		c.IncludeStatus = false
		c.MaxHistory = 3
	}
}
