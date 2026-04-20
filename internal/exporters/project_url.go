package exporters

import (
	"fmt"
	"net/url"
	"strings"
)

type ProjectURLInfo struct {
	Host           string
	PathType       string
	GPTID          string
	GPTSlug        string
	ConversationID string
}

func parseProjectURL(raw string) (ProjectURLInfo, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ProjectURLInfo{}, fmt.Errorf("parse project URL: %w", err)
	}
	if u.Scheme != "https" {
		return ProjectURLInfo{}, fmt.Errorf("project URL must use https")
	}
	if u.Host == "" {
		return ProjectURLInfo{}, fmt.Errorf("project URL host is required")
	}

	parts := splitURLPath(u.Path)
	info := ProjectURLInfo{Host: u.Host}
	if len(parts) == 0 {
		return info, fmt.Errorf("project URL path is empty")
	}

	switch {
	case len(parts) >= 4 && parts[0] == "g" && parts[2] == "c":
		info.PathType = "gpt_conversation"
		info.GPTID = parts[1]
		info.ConversationID = parts[3]
		info.GPTSlug = deriveGPTSlug(parts[1])
	case len(parts) >= 2 && parts[0] == "c":
		info.PathType = "conversation"
		info.ConversationID = parts[1]
	default:
		return info, fmt.Errorf("unsupported project URL path %q", u.Path)
	}

	if info.GPTID == "" && info.ConversationID == "" {
		return info, fmt.Errorf("project URL does not contain a GPT or conversation identifier")
	}

	return info, nil
}

func splitURLPath(path string) []string {
	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func deriveGPTSlug(segment string) string {
	if !strings.HasPrefix(segment, "g-") {
		return segment
	}

	if strings.HasPrefix(segment, "g-p-") {
		remainder := strings.TrimPrefix(segment, "g-p-")
		firstDash := strings.Index(remainder, "-")
		if firstDash <= 0 || firstDash == len(remainder)-1 {
			return segment
		}
		return remainder[firstDash+1:]
	}

	parts := strings.Split(segment, "-")
	if len(parts) <= 2 {
		return segment
	}
	return strings.Join(parts[2:], "-")
}
