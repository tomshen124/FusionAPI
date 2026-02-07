package core

import (
	"net/http"
	"strings"
)

// DetectTool 从 HTTP 头识别调用工具
func DetectTool(headers http.Header) string {
	ua := strings.ToLower(headers.Get("User-Agent"))

	// Check specific headers first
	if v := headers.Get("X-Client-Name"); v != "" {
		return normalizeToolName(v)
	}

	// User-Agent patterns
	patterns := []struct {
		pattern string
		name    string
	}{
		{"cursor", "cursor"},
		{"claude-code", "claude-code"},
		{"codex-cli", "codex-cli"},
		{"continue", "continue"},
		{"copilot", "copilot"},
		{"openai-python", "openai-sdk"},
		{"openai-node", "openai-sdk"},
		{"anthropic-python", "anthropic-sdk"},
		{"anthropic-typescript", "anthropic-sdk"},
	}

	for _, p := range patterns {
		if strings.Contains(ua, p.pattern) {
			return p.name
		}
	}

	return "unknown"
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
