package transport

import (
	"context"
	"net/http"
)

// Context keys for the transport package
type contextKey string

const (
	// Extension management
	activeExtensionsKey contextKey = "active-extensions"

	// HTTP headers from original request
	httpHeadersKey contextKey = "http-headers"
)

// Extension-related context functions

// WithActiveExtensions adds active extensions to the context
func WithActiveExtensions(ctx context.Context, extensions []Extension) context.Context {
	return context.WithValue(ctx, activeExtensionsKey, extensions)
}

// GetActiveExtensions retrieves active extensions from the context
func GetActiveExtensions(ctx context.Context) []Extension {
	if extensions, ok := ctx.Value(activeExtensionsKey).([]Extension); ok {
		return extensions
	}
	return nil
}

// HTTP header-related context functions

// WithHTTPHeaders adds HTTP headers to the context
func WithHTTPHeaders(ctx context.Context, headers http.Header) context.Context {
	if headers == nil {
		return ctx
	}
	return context.WithValue(ctx, httpHeadersKey, headers)
}

// GetHTTPHeaders retrieves HTTP headers from the context
func GetHTTPHeaders(ctx context.Context) http.Header {
	if headers, ok := ctx.Value(httpHeadersKey).(http.Header); ok {
		return headers
	}
	return nil
}
