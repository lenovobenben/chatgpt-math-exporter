package markdown

import (
	"strings"
	"testing"

	"github.com/lihd/chatgpt-math-exporter/internal/model"
)

func TestRenderConversationWithBlocks(t *testing.T) {
	conv := model.Conversation{
		ID:    "conv-1",
		Title: "Block Demo",
		Messages: []model.Message{
			{
				Role: "user",
				Blocks: []model.Block{
					{Kind: model.BlockParagraph, Text: "Read the table."},
					{
						Kind: model.BlockTable,
						Table: &model.Table{
							Headers: []string{"n", "phi(n)", "group"},
							Rows: [][]string{
								{"1", "1", "{1}"},
								{"5", "4", "C_4"},
							},
						},
					},
				},
			},
			{
				Role: "assistant",
				Blocks: []model.Block{
					{Kind: model.BlockMath, Text: `\varphi(5)=4`},
					{Kind: model.BlockCode, Code: &model.Code{Language: "python", Text: "print('hi')"}},
					{Kind: model.BlockImage, Image: &model.Image{Alt: "figure", Src: "assets/image-001.png"}},
				},
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for explicit blocks, got %#v", warnings)
	}
	if !strings.Contains(got, "# Block Demo") || !strings.Contains(got, "## Question") || !strings.Contains(got, "## Answer") {
		t.Fatalf("missing basic markdown structure: %s", got)
	}
	if !strings.Contains(got, "| n | phi(n) | group |") || !strings.Contains(got, "| 5 | 4 | C_4 |") {
		t.Fatalf("table block did not render as markdown table: %s", got)
	}
	if !strings.Contains(got, "```math\n\\varphi(5)=4\n```") {
		t.Fatalf("math block did not render as fenced math block: %s", got)
	}
	if !strings.Contains(got, "```python\nprint('hi')\n```") {
		t.Fatalf("code block did not render as fenced code block: %s", got)
	}
	if !strings.Contains(got, "![figure](assets/image-001.png)") {
		t.Fatalf("image block did not render as markdown image: %s", got)
	}
}

func TestGroupMessagesForRenderKeepsBlockOnlyMessages(t *testing.T) {
	grouped := groupMessagesForRender([]model.Message{
		{
			Role: "assistant",
			Blocks: []model.Block{
				{Kind: model.BlockMath, Text: "x=1"},
			},
		},
	})

	if len(grouped) != 1 {
		t.Fatalf("expected block-only message to be kept, got %#v", grouped)
	}
	if len(grouped[0].Blocks) != 1 || grouped[0].Blocks[0].Kind != model.BlockMath {
		t.Fatalf("unexpected grouped blocks: %#v", grouped[0])
	}
}

func TestRenderConversationParsesTableAndMathBlocksFromText(t *testing.T) {
	conv := model.Conversation{
		Title: "Legacy Rich Text",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"我先给一个表格样式：",
					"",
					"| n | phi(n) | group |",
					"| --- | --- | --- |",
					"| 1 | 1 | {1} |",
					"| 5 | 4 | C_4 |",
					"",
					"```math",
					`(\mathbb{Z}/5\mathbb{Z})^\times \cong C_4`,
					"```",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if !strings.Contains(got, "| n | phi(n) | group |") || !strings.Contains(got, "| 5 | 4 | C_4 |") {
		t.Fatalf("expected markdown table to survive rendering: %s", got)
	}
	if !strings.Contains(got, "```math\n(\\mathbb{Z}/5\\mathbb{Z})^\\times \\cong C_4\n```") {
		t.Fatalf("expected fenced math block to survive rendering: %s", got)
	}
}

func TestRenderConversationParsesCodeBlocksFromText(t *testing.T) {
	conv := model.Conversation{
		Title: "Code Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"示例代码：",
					"",
					"```python",
					"print('hello')",
					"```",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if !strings.Contains(got, "```python\nprint('hello')\n```") {
		t.Fatalf("expected fenced code block to survive rendering: %s", got)
	}
}

func TestRenderConversationPreservesMarkdownHeadingsFromBrowserText(t *testing.T) {
	conv := model.Conversation{
		Title: "Heading Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"先说明。",
					"",
					"### 第一步：建立坐标系",
					"",
					"设 $x = 1$。",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if !strings.Contains(got, "\n### 第一步：建立坐标系\n") {
		t.Fatalf("expected markdown heading to survive rendering: %s", got)
	}
}

func TestRenderConversationParsesImageMarkerIntoBlockAndRendersMarkerForRemoteAsset(t *testing.T) {
	conv := model.Conversation{
		Title: "Image Marker",
		Messages: []model.Message{
			{
				Role: "user",
				Content: strings.Join([]string{
					"这是题图：",
					`[[CGME_IMAGE:{"src":"https://example.com/image.png","alt":"figure one"}]]`,
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if !strings.Contains(got, "这是题图：") {
		t.Fatalf("expected surrounding paragraph to remain: %s", got)
	}
	if !strings.Contains(got, `[[CGME_IMAGE:{"src":"https://example.com/image.png","alt":"figure one"}]]`) {
		t.Fatalf("expected remote image block to render back into image marker for asset materialization: %s", got)
	}
}

func TestRenderConversationSplitsStandaloneInlineMathIntoBlock(t *testing.T) {
	conv := model.Conversation{
		Title: "Inline Math Split",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"因此",
					"",
					"$AB=\\sqrt{(3-1)^2+(1-2)^2}=\\sqrt5$",
					"",
					"答案：$\\boxed{\\sqrt5}$。",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if !strings.Contains(got, "因此\n\n```math\nAB=\\sqrt{(3-1)^2+(1-2)^2}=\\sqrt5\n```") {
		t.Fatalf("expected standalone inline math line to become math block: %s", got)
	}
	if !strings.Contains(got, "答案：$\\boxed{\\sqrt5}$。") {
		t.Fatalf("expected inline answer sentence to remain paragraph text: %s", got)
	}
}

func TestRenderConversationSplitsLabelPlusInlineMathIntoParagraphAndBlock(t *testing.T) {
	conv := model.Conversation{
		Title: "Label Math Split",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"左圆：$\\dfrac{|3r_1-4(3-r_1)|}{5}=r_1$",
					"$\\dfrac{|12-7r_2|}{5}=r_2$",
					"$\\Rightarrow \\dfrac{|7r_1-12|}{5}=r_1$ ，解得 $r_1=1$.",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if !strings.Contains(got, "左圆：\n\n```math\n\\dfrac{|3r_1-4(3-r_1)|}{5}=r_1\n```") {
		t.Fatalf("expected label + inline math line to split into paragraph and math block: %s", got)
	}
	if !strings.Contains(got, "```math\n\\dfrac{|12-7r_2|}{5}=r_2\n```") {
		t.Fatalf("expected pure inline math line to become math block: %s", got)
	}
	if !strings.Contains(got, "$\\Rightarrow \\dfrac{|7r_1-12|}{5}=r_1$ ，解得 $r_1=1$.") {
		t.Fatalf("expected mixed prose+math line to remain unchanged: %s", got)
	}
}
