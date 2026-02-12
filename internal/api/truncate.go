package api

import "strings"

func truncateBody(b []byte, max int) string {
	if max <= 0 {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
