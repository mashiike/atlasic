package transport

import (
	"strings"

	"github.com/mashiike/atlasic/a2a"
)

// MatchesOutputMode checks if a supported output mode matches an accepted output mode pattern
// Supports RFC 9110 wildcards: *, */* and type/*
func MatchesOutputMode(accepted, supported string) bool {
	if accepted == "*" || accepted == "*/*" {
		return true
	}

	if strings.HasSuffix(accepted, "/*") {
		// type/* pattern
		acceptedType := strings.TrimSuffix(accepted, "/*")
		supportedType, _, _ := strings.Cut(supported, "/")
		return acceptedType == supportedType
	}

	// Exact match
	return accepted == supported
}

// FindCompatibleOutputModes returns the intersection of accepted and supported output modes
// Returns all compatible modes from supportedModes that match any acceptedModes
func FindCompatibleOutputModes(acceptedModes []string, supportedModes []string) ([]string, error) {
	// Handle default case: empty accepted means */*
	if len(acceptedModes) == 0 {
		acceptedModes = []string{"*/*"}
	}

	var compatible []string
	for _, supported := range supportedModes {
		for _, accepted := range acceptedModes {
			if MatchesOutputMode(accepted, supported) {
				compatible = append(compatible, supported)
				break // Avoid duplicates
			}
		}
	}

	if len(compatible) == 0 {
		return nil, a2a.NewJSONRPCError(a2a.ErrorCodeContentTypeNotSupported, map[string]interface{}{
			"acceptedModes":  acceptedModes,
			"supportedModes": supportedModes,
		})
	}

	return compatible, nil
}
