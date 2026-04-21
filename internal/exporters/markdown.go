package exporters

import (
	"fmt"
	"strings"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
)

func renderConversationMarkdown(conv Conversation, opts config.OptionConfig) (string, []warningRecord) {
	var b strings.Builder
	warnings := make([]warningRecord, 0)

	title := conv.Title
	if title == "" {
		title = "Untitled Conversation"
	}

	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	if conv.ID != "" {
		fmt.Fprintf(&b, "- Conversation ID: `%s`\n", conv.ID)
	}
	if conv.CreateAt > 0 {
		fmt.Fprintf(&b, "- Created At: %s\n", time.Unix(int64(conv.CreateAt), 0).UTC().Format(time.RFC3339))
	}
	b.WriteString("\n")

	grouped := groupMessagesForRender(conv.Messages)
	for _, msg := range grouped {
		b.WriteString(sectionTitle(msg.Role))
		b.WriteString("\n\n")
		normalized, msgWarnings := normalizeMathText(msg.Content, normalizeMathOptions{
			Role:         msg.Role,
			FixUserLatex: opts.FixUserLatex,
		})
		warnings = append(warnings, msgWarnings...)
		b.WriteString(strings.TrimSpace(normalized))
		b.WriteString("\n\n")
	}

	return b.String(), warnings
}

func sectionTitle(role string) string {
	switch role {
	case "user":
		return "## Question"
	case "assistant":
		return "## Answer"
	case "tool":
		return "## Tool"
	default:
		return "## Message"
	}
}

func groupMessagesForRender(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}

	grouped := make([]Message, 0, len(messages))
	for _, msg := range messages {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		if len(grouped) > 0 && grouped[len(grouped)-1].Role == role {
			grouped[len(grouped)-1].Content = strings.TrimSpace(grouped[len(grouped)-1].Content + "\n\n" + content)
			continue
		}
		grouped = append(grouped, Message{
			ID:      msg.ID,
			Role:    role,
			Content: content,
		})
	}
	return grouped
}
