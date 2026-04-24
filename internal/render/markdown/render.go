package markdown

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/model"
)

type Warning struct {
	Code    string
	Message string
}

type imageMarker struct {
	Src string `json:"src"`
	Alt string `json:"alt"`
}

func RenderConversation(conv model.Conversation) (string, []Warning) {
	var b strings.Builder
	warnings := make([]Warning, 0)

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
		rendered, msgWarnings := renderMessageContent(msg)
		warnings = append(warnings, msgWarnings...)
		b.WriteString(strings.TrimSpace(rendered))
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

func groupMessagesForRender(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}

	grouped := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if role == "" || (content == "" && len(msg.Blocks) == 0) {
			continue
		}
		if len(grouped) > 0 && grouped[len(grouped)-1].Role == role {
			switch {
			case grouped[len(grouped)-1].Content == "":
				grouped[len(grouped)-1].Content = content
			case content != "":
				grouped[len(grouped)-1].Content = strings.TrimSpace(grouped[len(grouped)-1].Content + "\n\n" + content)
			}
			grouped[len(grouped)-1].Blocks = append(grouped[len(grouped)-1].Blocks, msg.Blocks...)
			continue
		}
		grouped = append(grouped, model.Message{
			ID:      msg.ID,
			Role:    role,
			Content: content,
			Blocks:  append([]model.Block(nil), msg.Blocks...),
		})
	}
	return grouped
}

func renderMessageContent(msg model.Message) (string, []Warning) {
	blocks := msg.Blocks
	if len(blocks) == 0 {
		blocks = ParseTextBlocks(msg.Content)
	}
	if len(blocks) == 0 {
		return NormalizeMathText(msg.Content, NormalizeOptions{})
	}

	var b strings.Builder
	warnings := make([]Warning, 0)

	for i, block := range blocks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		rendered, blockWarnings := renderBlock(block)
		warnings = append(warnings, blockWarnings...)
		b.WriteString(rendered)
	}

	return b.String(), warnings
}

func ParseTextBlocks(content string) []model.Block {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	blocks := make([]model.Block, 0)
	paragraph := make([]string, 0)

	flushParagraph := func() {
		text := strings.TrimSpace(strings.Join(paragraph, "\n"))
		if text != "" {
			blocks = append(blocks, splitParagraphBlocks(text)...)
		}
		paragraph = paragraph[:0]
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```math") {
			flushParagraph()
			i++
			mathLines := make([]string, 0)
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				mathLines = append(mathLines, strings.TrimRight(lines[i], " \t"))
				i++
			}
			if i < len(lines) && strings.TrimSpace(lines[i]) == "```" {
				i++
			}
			text := strings.TrimSpace(strings.Join(mathLines, "\n"))
			if text != "" {
				blocks = append(blocks, model.Block{Kind: model.BlockMath, Text: text})
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			flushParagraph()
			language := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			codeLines := make([]string, 0)
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				codeLines = append(codeLines, strings.TrimRight(lines[i], " \t"))
				i++
			}
			if i < len(lines) && strings.TrimSpace(lines[i]) == "```" {
				i++
			}
			text := strings.TrimRight(strings.Join(codeLines, "\n"), "\n")
			blocks = append(blocks, model.Block{
				Kind: model.BlockCode,
				Code: &model.Code{
					Language: language,
					Text:     text,
				},
			})
			continue
		}

		if isMarkdownTableHeaderLine(trimmed) && i+1 < len(lines) && isMarkdownTableSeparatorLine(strings.TrimSpace(lines[i+1])) {
			flushParagraph()
			headers := parseMarkdownTableLine(trimmed)
			rows := make([][]string, 0)
			i += 2
			for i < len(lines) {
				rowLine := strings.TrimSpace(lines[i])
				if !isMarkdownTableDataLine(rowLine) {
					break
				}
				rows = append(rows, parseMarkdownTableLine(rowLine))
				i++
			}
			if len(headers) > 0 {
				blocks = append(blocks, model.Block{
					Kind: model.BlockTable,
					Table: &model.Table{
						Headers: headers,
						Rows:    rows,
					},
				})
			}
			continue
		}

		if trimmed == "" {
			flushParagraph()
			i++
			continue
		}

		paragraph = append(paragraph, strings.TrimRight(line, " \t"))
		i++
	}

	flushParagraph()
	return blocks
}

var (
	standaloneInlineMathLine = regexp.MustCompile(`^\$(.+)\$[ \t]*[。．.]?$`)
	labelInlineMathLine      = regexp.MustCompile(`^(.+?[：:])[ \t]*\$(.+)\$[ \t]*$`)
	imageMarkerLine          = regexp.MustCompile(`^\[\[CGME_IMAGE:(\{.*\})\]\]$`)
	trailingMathTagLine      = regexp.MustCompile(`(?s)^(.+?)\s*\\tag\{([^{}]+)\}\s*$`)
)

func splitParagraphBlocks(text string) []model.Block {
	lines := strings.Split(text, "\n")
	blocks := make([]model.Block, 0)
	paragraph := make([]string, 0)

	flushParagraph := func() {
		joined := strings.TrimSpace(strings.Join(paragraph, "\n"))
		if joined != "" {
			blocks = append(blocks, model.Block{Kind: model.BlockParagraph, Text: joined})
		}
		paragraph = paragraph[:0]
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			flushParagraph()
			continue
		}
		if image, ok := extractImageMarker(line); ok {
			flushParagraph()
			blocks = append(blocks, model.Block{Kind: model.BlockImage, Image: image})
			continue
		}
		if math, ok := extractStandaloneInlineMath(line); ok {
			flushParagraph()
			blocks = append(blocks, model.Block{Kind: model.BlockMath, Text: math})
			continue
		}
		if label, math, ok := extractLabelInlineMath(line); ok {
			flushParagraph()
			blocks = append(blocks, model.Block{Kind: model.BlockParagraph, Text: label})
			blocks = append(blocks, model.Block{Kind: model.BlockMath, Text: math})
			continue
		}
		paragraph = append(paragraph, strings.TrimRight(rawLine, " \t"))
	}

	flushParagraph()
	return blocks
}

func extractStandaloneInlineMath(line string) (string, bool) {
	matches := standaloneInlineMathLine.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 2 {
		return "", false
	}
	math := strings.TrimSpace(matches[1])
	if math == "" || strings.Contains(math, "$") {
		return "", false
	}
	return math, true
}

func extractLabelInlineMath(line string) (string, string, bool) {
	matches := labelInlineMathLine.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return "", "", false
	}
	label := strings.TrimSpace(matches[1])
	math := strings.TrimSpace(matches[2])
	if label == "" || math == "" || strings.Contains(math, "$") {
		return "", "", false
	}
	return label, math, true
}

func extractImageMarker(line string) (*model.Image, bool) {
	matches := imageMarkerLine.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 2 {
		return nil, false
	}
	var marker imageMarker
	if err := json.Unmarshal([]byte(matches[1]), &marker); err != nil {
		return nil, false
	}
	src := strings.TrimSpace(marker.Src)
	if src == "" {
		return nil, false
	}
	return &model.Image{
		Alt: strings.TrimSpace(marker.Alt),
		Src: src,
	}, true
}

func isMarkdownTableHeaderLine(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|")
}

func isMarkdownTableSeparatorLine(line string) bool {
	if !isMarkdownTableHeaderLine(line) {
		return false
	}
	cells := parseMarkdownTableLine(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		for _, r := range cell {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func isMarkdownTableDataLine(line string) bool {
	return isMarkdownTableHeaderLine(line) && !isMarkdownTableSeparatorLine(line)
}

func parseMarkdownTableLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		cell = strings.ReplaceAll(cell, "<br>", "\n")
		cell = strings.ReplaceAll(cell, `\|`, "|")
		out = append(out, cell)
	}
	return out
}

func renderBlock(block model.Block) (string, []Warning) {
	switch block.Kind {
	case model.BlockMath:
		text := strings.TrimSpace(block.Text)
		if text == "" {
			return "", nil
		}
		if body, tag, ok := splitTrailingMathTag(text); ok {
			return formatMathTagLabel(tag) + "\n\n```math\n" + body + "\n```", nil
		}
		return "```math\n" + text + "\n```", nil
	case model.BlockImage:
		if block.Image == nil {
			return "", nil
		}
		alt := strings.TrimSpace(block.Image.Alt)
		if alt == "" {
			alt = "image"
		}
		src := strings.TrimSpace(block.Image.Src)
		if src == "" {
			return "", nil
		}
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			payload, err := json.Marshal(imageMarker{Src: src, Alt: alt})
			if err != nil {
				return fmt.Sprintf("![%s](%s)", escapeImageAlt(alt), src), nil
			}
			return fmt.Sprintf("[[CGME_IMAGE:%s]]", payload), nil
		}
		return fmt.Sprintf("![%s](%s)", escapeImageAlt(alt), src), nil
	case model.BlockTable:
		if block.Table == nil {
			return "", nil
		}
		return renderTable(block.Table), nil
	case model.BlockCode:
		if block.Code == nil {
			return "", nil
		}
		language := strings.TrimSpace(block.Code.Language)
		text := strings.TrimRight(block.Code.Text, "\n")
		if language == "" {
			return "```\n" + text + "\n```", nil
		}
		return "```" + language + "\n" + text + "\n```", nil
	case model.BlockParagraph:
		fallthrough
	default:
		return NormalizeMathText(block.Text, NormalizeOptions{})
	}
}

func splitTrailingMathTag(input string) (string, string, bool) {
	matches := trailingMathTagLine.FindStringSubmatch(strings.TrimSpace(input))
	if len(matches) != 3 {
		return "", "", false
	}
	body := strings.TrimSpace(matches[1])
	tag := strings.TrimSpace(matches[2])
	if body == "" || tag == "" {
		return "", "", false
	}
	return body, tag, true
}

func formatMathTagLabel(tag string) string {
	if strings.HasPrefix(tag, "(") && strings.HasSuffix(tag, ")") {
		return tag
	}
	return "(" + tag + ")"
}

func renderTable(table *model.Table) string {
	headers := normalizeTableRow(table.Headers)
	if len(headers) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("| ")
	b.WriteString(strings.Join(headers, " | "))
	b.WriteString(" |\n| ")
	separators := make([]string, len(headers))
	for i := range separators {
		separators[i] = "---"
	}
	b.WriteString(strings.Join(separators, " | "))
	b.WriteString(" |")

	for _, row := range table.Rows {
		cells := normalizeTableRow(row)
		for len(cells) < len(headers) {
			cells = append(cells, "")
		}
		if len(cells) > len(headers) {
			cells = cells[:len(headers)]
		}
		b.WriteString("\n| ")
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString(" |")
	}

	return b.String()
}

func normalizeTableRow(cells []string) []string {
	if len(cells) == 0 {
		return nil
	}
	out := make([]string, 0, len(cells))
	for _, cell := range cells {
		clean := strings.TrimSpace(cell)
		clean = strings.ReplaceAll(clean, "\n", "<br>")
		clean = strings.ReplaceAll(clean, "|", `\|`)
		out = append(out, clean)
	}
	return out
}

func escapeImageAlt(alt string) string {
	return strings.ReplaceAll(alt, "]", `\]`)
}
