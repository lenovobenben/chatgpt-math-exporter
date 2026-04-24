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

func TestRenderConversationRendersComplexMathTableWithDetails(t *testing.T) {
	conv := model.Conversation{
		Title: "Math Table",
		Messages: []model.Message{
			{
				Role: "assistant",
				Blocks: []model.Block{
					{
						Kind: model.BlockTable,
						Table: &model.Table{
							Headers: []string{"方程组", "最小整数解"},
							Rows: [][]string{
								{`$\begin{cases} x + 2y - z = 0 \\ 3x - y + 4z = 0 \end{cases}$`, `$[-1, 1, 1]$`},
							},
						},
					},
				},
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if !strings.Contains(got, "| 方程组 | 最小整数解 |") {
		t.Fatalf("expected table structure to remain: %s", got)
	}
	if !strings.Contains(got, "| 见公式1 | $[-1, 1, 1]$ |") {
		t.Fatalf("expected math-heavy cell to be replaced by a table reference: %s", got)
	}
	if !strings.Contains(got, "##### 公式1（Row 1 方程组）") {
		t.Fatalf("expected formula appendix heading: %s", got)
	}
	if !strings.Contains(got, "```math\n\\begin{cases} x + 2y - z = 0 \\\\ 3x - y + 4z = 0 \\end{cases}\n```") {
		t.Fatalf("expected math-heavy cell to render in appendix as fenced math block: %s", got)
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

func TestRenderConversationRewritesTrailingMathTagForGitHubCompatibility(t *testing.T) {
	conv := model.Conversation{
		Title: "Math Tag Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Blocks: []model.Block{
					{Kind: model.BlockMath, Text: `a_1 b_1 = a_2 b_2. \tag{4}`},
				},
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if !strings.Contains(got, "\n(4)\n\n```math\na_1 b_1 = a_2 b_2.\n```") {
		t.Fatalf("expected trailing math tag to be rewritten into a standalone label: %s", got)
	}
	if strings.Contains(got, `\tag{4}`) {
		t.Fatalf("expected raw \\tag to be removed for markdown output: %s", got)
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

func TestRenderConversationSplitsCheckmarkSummaryLinesIntoSeparateParagraphs(t *testing.T) {
	conv := model.Conversation{
		Title: "Summary Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					"#### ✓ 总结",
					"",
					"✔ 无理 / 有理性质由数值决定",
					"✔ 与所使用的进制无关",
					"✔ 有理数在任意进制中展开最终循环",
					"✔ 无理数在任意进制中展开都不循环",
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if !strings.Contains(got, "✔ 无理 / 有理性质由数值决定\n\n✔ 与所使用的进制无关\n\n✔ 有理数在任意进制中展开最终循环\n\n✔ 无理数在任意进制中展开都不循环") {
		t.Fatalf("expected checkmark summary lines to become separate markdown paragraphs: %s", got)
	}
}

func TestRenderConversationRewritesAllowedSpecialFunctionNamesAwayFromOperatorname(t *testing.T) {
	conv := model.Conversation{
		Title: "Operatorname Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: strings.Join([]string{
					`误差函数 $\operatorname{erf}(x)$。`,
					`正弦积分 $\operatorname{Si}(x)$。`,
					`虚误差函数 $\operatorname{erfi}(x)=-i\operatorname{erf}(ix)$。`,
				}, "\n"),
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if strings.Contains(got, `\operatorname{erf}`) || strings.Contains(got, `\operatorname{Si}`) || strings.Contains(got, `\operatorname{erfi}`) {
		t.Fatalf("expected known special functions to be rewritten away from operatorname: %s", got)
	}
	if !strings.Contains(got, `\mathrm{erf}(x)`) || !strings.Contains(got, `\mathrm{Si}(x)`) || !strings.Contains(got, `\mathrm{erfi}(x)=-i\mathrm{erf}(ix)`) {
		t.Fatalf("expected known special functions to use \\mathrm instead: %s", got)
	}
}

func TestRenderConversationRewritesSpecialFunctionNamesInsideTableCells(t *testing.T) {
	conv := model.Conversation{
		Title: "Table Operatorname Demo",
		Messages: []model.Message{
			{
				Role: "assistant",
				Blocks: []model.Block{
					{
						Kind: model.BlockTable,
						Table: &model.Table{
							Headers: []string{"函数", "说明"},
							Rows: [][]string{
								{`$e^{-x^2}$`, `误差函数 $\operatorname{erf}(x)$ 表示`},
								{`$\frac{\sin x}{x}$`, `正弦积分 $\operatorname{Si}(x)$`},
							},
						},
					},
				},
			},
		},
	}

	got, warnings := RenderConversation(conv)

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if strings.Contains(got, `\operatorname{erf}`) || strings.Contains(got, `\operatorname{Si}`) {
		t.Fatalf("expected table cells to rewrite known special functions away from operatorname: %s", got)
	}
	if !strings.Contains(got, `\mathrm{erf}(x)`) || !strings.Contains(got, `\mathrm{Si}(x)`) {
		t.Fatalf("expected table cells to use \\mathrm for known special functions: %s", got)
	}
}
