package redact

import (
	"net/url"
	"strings"
)

func String(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func URL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return String(raw)
	}
	segments := strings.Split(parsed.Path, "/")
	for idx := len(segments) - 1; idx >= 0; idx-- {
		if segments[idx] == "" {
			continue
		}
		segments[idx] = String(segments[idx])
		break
	}
	parsed.Path = strings.Join(segments, "/")
	return parsed.String()
}
