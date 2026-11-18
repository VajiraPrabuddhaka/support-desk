// Package agentkit holds what both agents share: settings from the
// environment, the OpenRouter model, MCP connections, tool-call logging and
// the A2A server.
package agentkit

import (
	"fmt"
	"os"
	"strings"
)

// Env returns the value of key, or def when the variable is unset or empty.
func Env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// RequireEnv returns the value of key, or an error that names the variable.
func RequireEnv(key string) (string, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return v, nil
}
