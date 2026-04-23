package exporters

import (
	"github.com/lihd/chatgpt-math-exporter/internal/config"
	mdrender "github.com/lihd/chatgpt-math-exporter/internal/render/markdown"
)

func renderConversationMarkdown(conv Conversation, opts config.OptionConfig) (string, []warningRecord) {
	rendered, warnings := mdrender.RenderConversation(conv)
	return rendered, toWarningRecords(warnings)
}
