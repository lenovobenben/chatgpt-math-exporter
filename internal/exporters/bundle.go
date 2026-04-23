package exporters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type rawConversation struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	CreateTime float64            `json:"create_time"`
	Mapping    map[string]rawNode `json:"mapping"`
}

type rawNode struct {
	ID       string      `json:"id"`
	Parent   *string     `json:"parent"`
	Children []string    `json:"children"`
	Message  *rawMessage `json:"message"`
}

type rawMessage struct {
	Author  rawAuthor     `json:"author"`
	Content rawMsgContent `json:"content"`
}

type rawAuthor struct {
	Role string `json:"role"`
}

type rawMsgContent struct {
	ContentType string        `json:"content_type"`
	Parts       []interface{} `json:"parts"`
}

func loadBundle(bundlePath string) ([]Conversation, []warningRecord, error) {
	data, err := os.ReadFile(filepath.Join(bundlePath, "conversations.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("read conversations.json: %w", err)
	}

	var raw []rawConversation
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse conversations.json: %w", err)
	}

	conversations := make([]Conversation, 0, len(raw))
	warnings := make([]warningRecord, 0)
	for _, item := range raw {
		conv, convWarnings := convertConversation(item)
		if len(conv.Messages) == 0 {
			warnings = append(warnings, warningRecord{
				Code:    "conversation.empty",
				Message: fmt.Sprintf("Conversation %q did not contain any exportable text messages.", item.Title),
			})
			continue
		}
		conversations = append(conversations, conv)
		warnings = append(warnings, convWarnings...)
	}

	sort.Slice(conversations, func(i, j int) bool {
		if conversations[i].CreateAt == conversations[j].CreateAt {
			return conversations[i].Title < conversations[j].Title
		}
		return conversations[i].CreateAt < conversations[j].CreateAt
	})

	if len(warnings) == 0 {
		warnings = append(warnings, warningRecord{
			Code:    "export.bundle.parsed",
			Message: "Bundle export completed without parser warnings.",
		})
	}

	return conversations, warnings, nil
}

func convertConversation(raw rawConversation) (Conversation, []warningRecord) {
	orderedIDs := orderedNodeIDs(raw.Mapping)
	messages := make([]Message, 0, len(orderedIDs))
	warnings := make([]warningRecord, 0)

	for _, nodeID := range orderedIDs {
		node := raw.Mapping[nodeID]
		if node.Message == nil {
			continue
		}

		role := strings.TrimSpace(node.Message.Author.Role)
		if role == "" || role == "system" {
			continue
		}

		content, ok := extractText(node.Message.Content)
		if !ok {
			warnings = append(warnings, warningRecord{
				Code:    "message.unsupported_content",
				Message: fmt.Sprintf("Conversation %q contains a %q message that could not be rendered as text.", raw.Title, node.Message.Content.ContentType),
			})
			continue
		}

		if strings.TrimSpace(content) == "" {
			continue
		}

		messages = append(messages, Message{
			ID:      nodeID,
			Role:    role,
			Content: content,
		})
	}

	return Conversation{
		ID:       raw.ID,
		Title:    strings.TrimSpace(raw.Title),
		CreateAt: raw.CreateTime,
		Messages: messages,
	}, warnings
}

func orderedNodeIDs(mapping map[string]rawNode) []string {
	if len(mapping) == 0 {
		return nil
	}

	rootID := ""
	for id, node := range mapping {
		if node.Parent == nil {
			rootID = id
			break
		}
	}
	if rootID == "" {
		ids := make([]string, 0, len(mapping))
		for id := range mapping {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return ids
	}

	out := make([]string, 0, len(mapping))
	var walk func(string)
	walk = func(id string) {
		out = append(out, id)
		children := append([]string(nil), mapping[id].Children...)
		sort.Strings(children)
		for _, childID := range children {
			if _, ok := mapping[childID]; ok {
				walk(childID)
			}
		}
	}
	walk(rootID)
	return out
}

func extractText(content rawMsgContent) (string, bool) {
	switch content.ContentType {
	case "", "text":
		parts := make([]string, 0, len(content.Parts))
		for _, part := range content.Parts {
			text, ok := part.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n\n"), true
	default:
		return "", false
	}
}

func filterConversations(conversations []Conversation, project string) []Conversation {
	if strings.TrimSpace(project) == "" {
		return conversations
	}

	filtered := make([]Conversation, 0)
	for _, conv := range conversations {
		if conv.Title == project {
			filtered = append(filtered, conv)
		}
	}
	return filtered
}

func slugify(value string) string {
	s := strings.ToLower(strings.TrimSpace(value))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = regexp.MustCompile(`[^a-z0-9\-\p{Han}]`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "export"
	}
	return s
}

func emptyFallback(value string) string {
	if value == "" {
		return "(not provided)"
	}
	return value
}
