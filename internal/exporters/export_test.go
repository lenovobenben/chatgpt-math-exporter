package exporters

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
)

func TestRunBundleExport(t *testing.T) {
	bundleDir := t.TempDir()
	outputDir := t.TempDir()

	conversations := strings.Join([]string{
		"[",
		"  {",
		`    "id": "conv-1",`,
		`    "title": "Algebra Notes",`,
		`    "create_time": 1710000000,`,
		`    "mapping": {`,
		`      "root": {`,
		`        "id": "root",`,
		`        "children": ["user-1"]`,
		`      },`,
		`      "user-1": {`,
		`        "id": "user-1",`,
		`        "parent": "root",`,
		`        "children": ["assistant-1"],`,
		`        "message": {`,
		`          "author": { "role": "user" },`,
		`          "content": {`,
		`            "content_type": "text",`,
		`            "parts": ["Solve x^2 - 1 = 0. Also note that x ∈ R and x ≥ 0 in the restricted case."]`,
		`          }`,
		`        }`,
		`      },`,
		`      "assistant-1": {`,
		`        "id": "assistant-1",`,
		`        "parent": "user-1",`,
		`        "children": [],`,
		`        "message": {`,
		`          "author": { "role": "assistant" },`,
		`          "content": {`,
		`            "content_type": "text",`,
		"            \"parts\": [\"The solutions are x = 1 and x = -1.\\n\\nx^2 - 1 = 0\\nx = ±1\\n\\nDo not rewrite code: `if x ≥ 0 { return x }`\"]",
		`          }`,
		`        }`,
		`      }`,
		`    }`,
		`  }`,
		"]",
	}, "\n")

	if err := os.WriteFile(filepath.Join(bundleDir, "conversations.json"), []byte(conversations), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg := config.Config{
		Source: config.SourceConfig{
			Type: "bundle",
			Path: bundleDir,
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
		Options: config.OptionConfig{
			WriteReadme:   true,
			WriteWarnings: true,
			PreserveLinks: true,
		},
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	markdownPath := filepath.Join(outputDir, "algebra-notes", "001_algebra-notes.md")
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}

	markdown := string(content)
	if !strings.Contains(markdown, "# Algebra Notes") {
		t.Fatalf("markdown missing title: %s", markdown)
	}
	if !strings.Contains(markdown, "## Question") || !strings.Contains(markdown, "## Answer") {
		t.Fatalf("markdown missing sections: %s", markdown)
	}
	if !strings.Contains(markdown, `Solve x^2 - 1 = 0. Also note that x \in R and x \ge 0 in the restricted case.`) {
		t.Fatalf("markdown missing user content: %s", markdown)
	}
	if !strings.Contains(markdown, "The solutions are x = 1 and x = -1.") {
		t.Fatalf("markdown missing assistant content: %s", markdown)
	}
	if !strings.Contains(markdown, "```math\nx^2 - 1 = 0\nx = \\pm1\n```") {
		t.Fatalf("markdown should wrap standalone math lines: %s", markdown)
	}
	if !strings.Contains(markdown, "`if x ≥ 0 { return x }`") {
		t.Fatalf("markdown should preserve inline code: %s", markdown)
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	warnings := string(warningsContent)
	if !strings.Contains(warnings, "math.symbol_normalized") || !strings.Contains(warnings, "math.block_wrapped") {
		t.Fatalf("warnings should include math normalization entries: %s", warnings)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "README.md")); err != nil {
		t.Fatalf("README.md not generated: %v", err)
	}
}

func TestNormalizeMathTextPreservesCodeFencesAndInlineCode(t *testing.T) {
	input := strings.Join([]string{
		"Outside code: a ≤ b and x → y.",
		"",
		"```go",
		"if a ≤ b {",
		"    return x → y",
		"}",
		"```",
		"",
		"Inline code stays: `x ≤ y` but normal text keeps z ∈ A.",
	}, "\n")

	got, _ := normalizeMathText(input)

	if !strings.Contains(got, `Outside code: a \le b and x \to y.`) {
		t.Fatalf("expected normalized plain text, got: %s", got)
	}
	if !strings.Contains(got, "if a ≤ b {") || !strings.Contains(got, "return x → y") {
		t.Fatalf("expected fenced code to remain untouched, got: %s", got)
	}
	if !strings.Contains(got, "`x ≤ y`") {
		t.Fatalf("expected inline code to remain untouched, got: %s", got)
	}
	if !strings.Contains(got, `normal text keeps z \in A.`) {
		t.Fatalf("expected plain text after inline code to normalize, got: %s", got)
	}
}

func TestNormalizeMathTextWrapsStandaloneMathLines(t *testing.T) {
	input := strings.Join([]string{
		"Here is the derivation:",
		"",
		"x^2 - 1 = 0",
		"x = ±1",
		"",
		"This sentence should stay plain text.",
	}, "\n")

	got, warnings := normalizeMathText(input)

	if !strings.Contains(got, "```math\nx^2 - 1 = 0\nx = \\pm1\n```") {
		t.Fatalf("expected math block wrapping, got: %s", got)
	}
	if !strings.Contains(got, "Here is the derivation:") || !strings.Contains(got, "This sentence should stay plain text.") {
		t.Fatalf("expected surrounding prose to remain, got: %s", got)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected math warnings to be emitted")
	}
}

func TestRenderConversationMarkdownMergesConsecutiveSameRoleSections(t *testing.T) {
	conv := Conversation{
		Title: "Merged Sections",
		Messages: []Message{
			{Role: "user", Content: "First question"},
			{Role: "assistant", Content: "First answer"},
			{Role: "assistant", Content: "Second answer chunk"},
			{Role: "user", Content: "Follow-up"},
		},
	}

	got, _ := renderConversationMarkdown(conv)

	if strings.Count(got, "## Answer") != 1 {
		t.Fatalf("expected one merged answer section, got: %s", got)
	}
	if !strings.Contains(got, "First answer\n\nSecond answer chunk") {
		t.Fatalf("expected consecutive assistant content to be merged, got: %s", got)
	}
	if strings.Count(got, "## Question") != 2 {
		t.Fatalf("expected separate question sections around assistant block, got: %s", got)
	}
}

func TestParseProjectURLGPTConversation(t *testing.T) {
	info, err := parseProjectURL("https://chatgpt.com/g/g-p-69b35dca021081918246c3df20a7bf27-jing-dian-shu-xue-ti-100li-6/c/69b8017a-69a0-8328-b934-c6fced4a3c0d")
	if err != nil {
		t.Fatalf("parseProjectURL() error = %v", err)
	}
	if info.PathType != "gpt_conversation" {
		t.Fatalf("unexpected path type: %#v", info)
	}
	if info.ConversationID != "69b8017a-69a0-8328-b934-c6fced4a3c0d" {
		t.Fatalf("unexpected conversation id: %#v", info)
	}
	if info.GPTSlug != "jing-dian-shu-xue-ti-100li-6" {
		t.Fatalf("unexpected slug: %#v", info)
	}
}

func TestProjectFetcherRequiresSessionCookie(t *testing.T) {
	t.Setenv(sessionCookieEnv, "")

	fetcher := NewProjectFetcher(config.Config{})
	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		PathType:       "gpt_conversation",
		ConversationID: "69b8017a-69a0-8328-b934-c6fced4a3c0d",
	})
	if err == nil {
		t.Fatalf("expected missing-cookie error")
	}
	if !strings.Contains(err.Error(), sessionCookieEnv) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunProjectURLExportWritesFetchedMarkdown(t *testing.T) {
	outputDir := t.TempDir()

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			fetched: FetchedConversation{
				ProjectName: "Fetched Algebra",
				Messages: []Message{
					{Role: "user", Content: "x ∈ R"},
					{Role: "assistant", Content: "x = ±1"},
				},
				Warnings: []warningRecord{
					{Code: "source.project_url.browser_message_deduped", Message: "Dropped 1 consecutive duplicate DOM message(s) during browser extraction."},
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/g/g-p-69b35dca021081918246c3df20a7bf27-jing-dian-shu-xue-ti-100li-6/c/69b8017a-69a0-8328-b934-c6fced4a3c0d",
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
		Options: config.OptionConfig{
			WriteReadme:   true,
			WriteWarnings: true,
			PreserveLinks: true,
		},
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	projectDir := filepath.Join(outputDir, "fetched-algebra")
	content, err := os.ReadFile(filepath.Join(projectDir, "001_fetched-algebra.md"))
	if err != nil {
		t.Fatalf("read fetched markdown: %v", err)
	}
	markdown := string(content)
	if !strings.Contains(markdown, "# Fetched Algebra") {
		t.Fatalf("markdown missing fetched title: %s", markdown)
	}
	if !strings.Contains(markdown, `x \in R`) {
		t.Fatalf("markdown missing normalized user content: %s", markdown)
	}
	if !strings.Contains(markdown, "```math\nx = \\pm1\n```") {
		t.Fatalf("markdown missing math block: %s", markdown)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "001_placeholder.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("placeholder should not exist on successful fetch, err=%v", err)
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	if !strings.Contains(string(warningsContent), "source.project_url.browser_message_deduped") {
		t.Fatalf("warnings should include fetched browser warnings: %s", string(warningsContent))
	}

	readmeContent, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readmeContent), "Live project URL conversation content was fetched and rendered into Markdown files.") ||
		!strings.Contains(string(readmeContent), "Project: Fetched Algebra") {
		t.Fatalf("unexpected README content: %s", string(readmeContent))
	}
}

func TestRunProjectURLExportWritesPlaceholderForFetchFailure(t *testing.T) {
	outputDir := t.TempDir()

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			err: &ProjectFetchError{
				Code:    "source.project_url.auth_failed",
				Message: "ChatGPT rejected the session cookie for the conversation request.",
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/g/g-p-69b35dca021081918246c3df20a7bf27-jing-dian-shu-xue-ti-100li-6/c/69b8017a-69a0-8328-b934-c6fced4a3c0d",
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
		Options: config.OptionConfig{
			WriteReadme:   true,
			WriteWarnings: true,
			PreserveLinks: true,
		},
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	projectDir := filepath.Join(outputDir, "jing-dian-shu-xue-ti-100li-6")
	placeholderContent, err := os.ReadFile(filepath.Join(projectDir, "001_placeholder.md"))
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if !strings.Contains(string(placeholderContent), "Live project URL fetch did not return exportable messages") {
		t.Fatalf("placeholder missing updated status: %s", string(placeholderContent))
	}
	if !strings.Contains(string(placeholderContent), "Fetch Status: source.project_url.auth_failed") {
		t.Fatalf("placeholder missing fetch status: %s", string(placeholderContent))
	}

	readmeContent, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readmeContent), "live project URL fetch did not yield exportable conversation content") {
		t.Fatalf("README missing placeholder note: %s", string(readmeContent))
	}
}

func TestProjectFetcherBuildsConversationRequest(t *testing.T) {
	fetcher := &ChatGPTProjectFetcher{
		client:        &http.Client{},
		sessionCookie: "__Secure-next-auth.session-token=test",
		baseURL:       "https://chatgpt.com",
	}

	req, err := fetcher.newConversationRequest(context.Background(), ProjectURLInfo{
		ConversationID: "69b8017a-69a0-8328-b934-c6fced4a3c0d",
	})
	if err != nil {
		t.Fatalf("newConversationRequest() error = %v", err)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("unexpected method: %s", req.Method)
	}
	if got := req.URL.String(); got != "https://chatgpt.com/backend-api/conversation/69b8017a-69a0-8328-b934-c6fced4a3c0d" {
		t.Fatalf("unexpected request url: %s", got)
	}
	if req.Header.Get("Cookie") == "" {
		t.Fatalf("expected cookie header to be set")
	}
	if req.Header.Get("Accept") != "application/json" {
		t.Fatalf("unexpected accept header: %s", req.Header.Get("Accept"))
	}
}

func TestProjectFetcherDecodesConversationResponse(t *testing.T) {
	fetcher := &ChatGPTProjectFetcher{}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: ioNopCloser(strings.NewReader(`{
			"id":"conv-1",
			"title":"Fetched Algebra",
			"create_time":1710000000,
			"mapping":{
				"root":{"id":"root","children":["user-1"]},
				"user-1":{
					"id":"user-1",
					"parent":"root",
					"children":["assistant-1"],
					"message":{"author":{"role":"user"},"content":{"content_type":"text","parts":["x ∈ R"]}}
				},
				"assistant-1":{
					"id":"assistant-1",
					"parent":"user-1",
					"children":[],
					"message":{"author":{"role":"assistant"},"content":{"content_type":"text","parts":["x = ±1"]}}
				}
			}
		}`)),
	}

	got, err := fetcher.decodeConversationResponse(resp)
	if err != nil {
		t.Fatalf("decodeConversationResponse() error = %v", err)
	}
	if got.ProjectName != "Fetched Algebra" {
		t.Fatalf("unexpected project name: %#v", got)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("unexpected message count: %#v", got)
	}
}

func TestProjectFetcherDetectsCloudflareChallenge(t *testing.T) {
	fetcher := &ChatGPTProjectFetcher{}
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{
			"Content-Type": []string{"text/html; charset=UTF-8"},
			"cf-mitigated": []string{"challenge"},
		},
		Body: ioNopCloser(strings.NewReader("<html><body>Cloudflare challenge</body></html>")),
	}

	_, err := fetcher.decodeConversationResponse(resp)
	if err == nil {
		t.Fatalf("expected challenge error")
	}
	if !strings.Contains(err.Error(), "cloudflare_challenge") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildConversationPageURL(t *testing.T) {
	got := buildConversationPageURL(ProjectURLInfo{
		Host:           "chatgpt.com",
		PathType:       "gpt_conversation",
		GPTID:          "g-p-123-demo",
		ConversationID: "conv-1",
	})
	if got != "https://chatgpt.com/g/g-p-123-demo/c/conv-1" {
		t.Fatalf("unexpected page url: %s", got)
	}
}

func TestParseBrowserConversationPayload(t *testing.T) {
	payload, err := parseBrowserConversationPayload(`{"title":"Fetched Algebra","url":"https://chatgpt.com/c/conv-1","snippet":"preview","messages":[{"role":"user","content":"x ∈ R"},{"role":"assistant","content":"x = ±1"}],"error":""}`)
	if err != nil {
		t.Fatalf("parseBrowserConversationPayload() error = %v", err)
	}
	if payload.Title != "Fetched Algebra" || payload.URL != "https://chatgpt.com/c/conv-1" || payload.Snippet != "preview" || len(payload.Messages) != 2 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestCreateTempBrowserProfileRoot(t *testing.T) {
	root, err := createTempBrowserProfileRoot()
	if err != nil {
		t.Fatalf("createTempBrowserProfileRoot() error = %v", err)
	}
	defer os.RemoveAll(root)
	if _, err := os.Stat(filepath.Join(root, "Default")); err != nil {
		t.Fatalf("expected Default profile dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Local State")); err != nil {
		t.Fatalf("expected Local State file: %v", err)
	}
}

func TestNormalizeBrowserMessages(t *testing.T) {
	got, warnings := normalizeBrowserMessages([]Message{
		{Role: "user", Content: "First line\r\n\r\n\r\nSecond line   "},
		{Role: "user", Content: "First line\n\nSecond line"},
		{Role: "assistant", Content: "ChatGPT can make mistakes. Check important info."},
		{Role: "assistant", Content: "Result"},
		{Role: "assistant", Content: "x = 1"},
	})

	if len(got) != 2 {
		t.Fatalf("unexpected normalized message count: %#v", got)
	}
	if got[0].Role != "user" || got[0].Content != "First line\n\nSecond line" {
		t.Fatalf("unexpected first normalized message: %#v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "Result\n\nx = 1" {
		t.Fatalf("unexpected second normalized message: %#v", got[1])
	}
	if len(warnings) != 3 {
		t.Fatalf("expected normalization warnings, got %#v", warnings)
	}
}

func TestBrowserWaitDurationFromEnv(t *testing.T) {
	t.Setenv(browserWaitEnv, "12")
	if got := browserWaitDuration(); got != 12*time.Second {
		t.Fatalf("unexpected wait duration: %v", got)
	}
}

func TestChromeDebugPortFromEnv(t *testing.T) {
	t.Setenv(chromeDebugPortEnv, "9333")
	if got := chromeDebugPort(); got != 9333 {
		t.Fatalf("unexpected debug port: %d", got)
	}
}

func TestBrowserDOMEmptyMessage(t *testing.T) {
	got := browserDOMEmptyMessage(browserConversationPayload{
		Title:   "Login",
		URL:     "https://chatgpt.com/auth/login",
		Snippet: "Log in to ChatGPT",
	})
	if !strings.Contains(got, `title="Login"`) || !strings.Contains(got, `url="https://chatgpt.com/auth/login"`) || !strings.Contains(got, `snippet="Log in to ChatGPT"`) {
		t.Fatalf("unexpected empty DOM message: %s", got)
	}
}

type testReadCloser struct {
	*strings.Reader
}

func (t testReadCloser) Close() error { return nil }

func ioNopCloser(r *strings.Reader) testReadCloser {
	return testReadCloser{Reader: r}
}

type stubProjectFetcher struct {
	fetched FetchedConversation
	err     error
}

func (s stubProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	return s.fetched, s.err
}
