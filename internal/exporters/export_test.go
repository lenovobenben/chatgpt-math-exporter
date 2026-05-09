package exporters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
	"github.com/lihd/chatgpt-math-exporter/internal/model"
)

func withValidSessionCookiePreflight(t *testing.T) {
	t.Helper()
	t.Setenv(sessionCookieEnv, "__Secure-next-auth.session-token=test")
	originalValidator := sessionCookieOnlineValidator
	sessionCookieOnlineValidator = func(context.Context, string, ProjectURLInfo) error {
		return nil
	}
	t.Cleanup(func() {
		sessionCookieOnlineValidator = originalValidator
	})
}

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

func TestMaterializeMarkdownAssetsDownloadsAndRewritesImageMarkers(t *testing.T) {
	oldClientFactory := newAssetHTTPClient
	newAssetHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://example.com/image.png" {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     make(http.Header),
						Body:       http.NoBody,
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"image/png"},
					},
					Body:    ioNopCloser(strings.NewReader(string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}))),
					Request: req,
				}, nil
			}),
		}
	}
	defer func() { newAssetHTTPClient = oldClientFactory }()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "note.md")
	assetsDir := filepath.Join(outputDir, "assets")
	marker := `[[CGME_IMAGE:{"src":"https://example.com/image.png","alt":"figure one"}]]`

	got, warnings, err := materializeMarkdownAssets("before\n\n"+marker+"\n\nafter", outputPath, assetsDir, "")
	if err != nil {
		t.Fatalf("materializeMarkdownAssets() error = %v", err)
	}

	if !strings.Contains(got, "![figure one](assets/image-001.png)") {
		t.Fatalf("expected markdown image link, got: %s", got)
	}
	if !strings.Contains(fmt.Sprintf("%v", warnings), "asset.image_saved") {
		t.Fatalf("expected asset saved warning, got: %#v", warnings)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "assets", "image-001.png")); err != nil {
		t.Fatalf("expected downloaded asset on disk: %v", err)
	}
}

func TestMaterializeMarkdownAssetsFallsBackToRemoteLinkWhenDownloadFails(t *testing.T) {
	oldClientFactory := newAssetHTTPClient
	newAssetHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("boom")
			}),
		}
	}
	defer func() { newAssetHTTPClient = oldClientFactory }()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "note.md")
	assetsDir := filepath.Join(outputDir, "assets")
	marker := `[[CGME_IMAGE:{"src":"https://example.invalid/missing.png","alt":"broken"}]]`

	got, warnings, err := materializeMarkdownAssets(marker, outputPath, assetsDir, "")
	if err != nil {
		t.Fatalf("materializeMarkdownAssets() error = %v", err)
	}

	if got != "![broken](https://example.invalid/missing.png)" {
		t.Fatalf("expected remote fallback link, got: %s", got)
	}
	if !strings.Contains(fmt.Sprintf("%v", warnings), "asset.image_download_failed") {
		t.Fatalf("expected download failed warning, got: %#v", warnings)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
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

	got, _ := normalizeMathText(input, normalizeMathOptions{})

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

	got, warnings := normalizeMathText(input, normalizeMathOptions{})

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

func TestNormalizeMathTextDoesNotWrapProseLineContainingInlineMath(t *testing.T) {
	input := strings.Join([]string{
		"4. 为什么这个 $P$ 一定最小（结论闭环）",
		"由作法第 8–9 步，点 $P$ 满足 $OP=1$ 且在第一象限，并且它在 $OA$ 方向的投影为",
		"cos\\theta=OC=\\frac{1+\\sqrt7}{4}",
	}, "\n")

	got, _ := normalizeMathText(input, normalizeMathOptions{})

	if strings.Contains(got, "```math\n由作法第 8–9 步") {
		t.Fatalf("prose line with inline math should not be wrapped as a math block: %s", got)
	}
	if !strings.Contains(got, "cos\\theta=OC=\\frac{1+\\sqrt7}{4}") {
		t.Fatalf("formula line should remain present: %s", got)
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

	got, _ := renderConversationMarkdown(conv, config.OptionConfig{})

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

func TestProjectFetcherReadsCookieFromFile(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte("__Secure-next-auth.session-token=from-file"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}
	t.Setenv(sessionCookieEnv, "__Secure-next-auth.session-token=from-env")

	fetcher := NewProjectFetcher(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
			CookieFile: cookieFile,
		},
	})

	composite, ok := fetcher.(CompositeProjectFetcher)
	if !ok {
		t.Fatalf("expected composite fetcher, got %T", fetcher)
	}

	var httpFetcher *ChatGPTProjectFetcher
	for _, candidate := range composite.fetchers {
		if typed, ok := candidate.(*ChatGPTProjectFetcher); ok {
			httpFetcher = typed
			break
		}
	}
	if httpFetcher == nil {
		t.Fatalf("expected HTTP fallback fetcher to be configured")
	}
	if httpFetcher.sessionCookie != "__Secure-next-auth.session-token=from-file" {
		t.Fatalf("expected cookie from file, got %q", httpFetcher.sessionCookie)
	}
}

func TestProjectFetcherRejectsEmptyCookieFile(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	fetcher := NewProjectFetcher(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
			CookieFile: cookieFile,
		},
	})

	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-1",
	})
	if err == nil || !strings.Contains(err.Error(), "cookie_file_empty") {
		t.Fatalf("expected empty cookie file error, got %v", err)
	}
}

func TestProjectFetcherRejectsMalformedCookieFile(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte("not-a-cookie"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	fetcher := NewProjectFetcher(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
			CookieFile: cookieFile,
		},
	})

	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-1",
	})
	if err == nil || !strings.Contains(err.Error(), "cookie_invalid") {
		t.Fatalf("expected invalid cookie error, got %v", err)
	}
}

func TestProjectFetcherRejectsCookieWithoutChatGPTSessionToken(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte("foo=bar; baz=qux"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	fetcher := NewProjectFetcher(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
			CookieFile: cookieFile,
		},
	})

	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-1",
	})
	if err == nil || !strings.Contains(err.Error(), "known ChatGPT session token") {
		t.Fatalf("expected missing ChatGPT session token error, got %v", err)
	}
}

func TestRunProjectURLExportRejectsMissingCookieBeforeOutputSetup(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "out")
	t.Setenv(sessionCookieEnv, "")

	err := Run(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "session_cookie_missing") {
		t.Fatalf("expected missing cookie error, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output directory should not be created before cookie validation, stat err=%v", statErr)
	}
}

func TestRunProjectURLExportRejectsExpiredCookiePreflight(t *testing.T) {
	t.Setenv(sessionCookieEnv, "__Secure-next-auth.session-token=expired")
	outputDir := filepath.Join(t.TempDir(), "out")

	originalValidator := sessionCookieOnlineValidator
	sessionCookieOnlineValidator = func(context.Context, string, ProjectURLInfo) error {
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_auth_failed",
			Message: "ChatGPT rejected the session cookie during preflight validation. Refresh the cookie and rerun.",
		}
	}
	t.Cleanup(func() {
		sessionCookieOnlineValidator = originalValidator
	})

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			err: errors.New("fetcher should not be created after failed cookie preflight"),
		}
	}
	t.Cleanup(func() {
		projectFetcherFactory = originalFactory
	})

	err := Run(config.Config{
		Source: config.SourceConfig{
			Type:       "project_url",
			ProjectURL: "https://chatgpt.com/c/conv-1",
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cookie_auth_failed") {
		t.Fatalf("expected expired cookie preflight error, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output directory should not be created after failed cookie preflight, stat err=%v", statErr)
	}
}

func TestRunProjectURLExportWritesFetchedMarkdown(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	conversationDir := filepath.Join(outputDir, "fetched-algebra", "001_fetched-algebra__69b8017a-69a0-8328-b934-c6fced4a3c0d")
	if err := os.MkdirAll(conversationDir, 0o755); err != nil {
		t.Fatalf("mkdir conversation dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(conversationDir, "001_placeholder.md"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale placeholder: %v", err)
	}

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

	content, err := os.ReadFile(filepath.Join(conversationDir, "001_fetched-algebra__69b8017a-69a0-8328-b934-c6fced4a3c0d.md"))
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
	if _, err := os.Stat(filepath.Join(conversationDir, "001_placeholder.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("placeholder should not exist on successful fetch, err=%v", err)
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	if !strings.Contains(string(warningsContent), "source.project_url.browser_message_deduped") {
		t.Fatalf("warnings should include fetched browser warnings: %s", string(warningsContent))
	}
	if !strings.Contains(string(warningsContent), "source.project_url.stale_placeholder_removed") {
		t.Fatalf("warnings should include stale placeholder cleanup: %s", string(warningsContent))
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

func TestRunProjectURLExportRemovesLegacySlugPlaceholder(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	legacyDir := filepath.Join(outputDir, "jing-dian-shu-xue-ti-100li-6")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "001_placeholder.md"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write legacy placeholder: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			fetched: FetchedConversation{
				ProjectName: "经典数学题100例 6 - 三角形三边推导",
				Messages: []Message{
					{Role: "user", Content: "Question"},
					{Role: "assistant", Content: "Answer"},
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

	if _, err := os.Stat(filepath.Join(legacyDir, "001_placeholder.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy placeholder should be removed, err=%v", err)
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	if !strings.Contains(string(warningsContent), "source.project_url.legacy_placeholder_removed") {
		t.Fatalf("warnings should include legacy placeholder cleanup: %s", string(warningsContent))
	}
}

func TestRunProjectURLExportWritesPlaceholderForFetchFailure(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			err: &ProjectFetchError{
				Code:    "source.project_url.bad_status",
				Message: "Conversation request returned HTTP 500.",
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

	placeholderPath := filepath.Join(outputDir, "jing-dian-shu-xue-ti-100li-6", "001_jing-dian-shu-xue-ti-100li-6__69b8017a-69a0-8328-b934-c6fced4a3c0d", "001_placeholder.md")
	placeholderContent, err := os.ReadFile(placeholderPath)
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if !strings.Contains(string(placeholderContent), "Live project URL fetch did not return exportable messages") {
		t.Fatalf("placeholder missing updated status: %s", string(placeholderContent))
	}
	if !strings.Contains(string(placeholderContent), "Fetch Status: source.project_url.bad_status") {
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

func TestRunProjectURLExportReturnsAuthFailureWithoutPlaceholder(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return stubProjectFetcher{
			err: &ProjectFetchError{
				Code:    "source.project_url.cookie_auth_failed",
				Message: "ChatGPT opened the login page during browser export.",
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

	err := Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "cookie_auth_failed") {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if matches, globErr := filepath.Glob(filepath.Join(outputDir, "*", "*", "001_placeholder.md")); globErr != nil || len(matches) != 0 {
		t.Fatalf("expected no placeholder for auth failure, matches=%v err=%v", matches, globErr)
	}
}

func TestRunProjectURLListExportWritesMultipleConversations(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "math-sessions.txt")
	urls := strings.Join([]string{
		"https://chatgpt.com/c/conv-1",
		"",
		"# comment",
		"https://chatgpt.com/c/conv-2",
	}, "\n")
	if err := os.WriteFile(urlListPath, []byte(urls), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			routes: map[string]FetchedConversation{
				"conv-1": {
					ProjectName: "Problem One",
					Messages: []Message{
						{Role: "user", Content: "Question 1"},
						{Role: "assistant", Content: "Answer 1"},
					},
				},
				"conv-2": {
					ProjectName: "Problem Two",
					Messages: []Message{
						{Role: "user", Content: "Question 2"},
						{Role: "assistant", Content: "Answer 2"},
					},
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
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

	for _, path := range []string{
		filepath.Join(outputDir, "problem-one", "001_problem-one__conv-1", "001_problem-one__conv-1.md"),
		filepath.Join(outputDir, "problem-two", "001_problem-two__conv-2", "001_problem-two__conv-2.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected exported markdown %q: %v", path, err)
		}
	}

	readmeContent, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readmeContent), "Live project URL list entries were fetched and rendered into Markdown files.") {
		t.Fatalf("unexpected batch README: %s", string(readmeContent))
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	if !strings.Contains(string(warningsContent), "source.project_url_list.completed") {
		t.Fatalf("expected batch completion warning: %s", string(warningsContent))
	}

	reportContent, err := os.ReadFile(filepath.Join(outputDir, "export-report.json"))
	if err != nil {
		t.Fatalf("read export report: %v", err)
	}
	var report batchExportReport
	if err := json.Unmarshal(reportContent, &report); err != nil {
		t.Fatalf("unmarshal export report: %v", err)
	}
	if report.Summary.Success != 2 || report.Summary.Failed != 0 || report.Summary.Skipped != 0 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
}

func TestRunProjectURLListExportSeparatesProjectNameAndConversationTitle(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	urls := strings.Join([]string{
		"https://chatgpt.com/g/g-p-demo/c/conv-1",
		"",
	}, "\n")
	if err := os.WriteFile(urlListPath, []byte(urls), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			routes: map[string]FetchedConversation{
				"conv-1": {
					ProjectName: "经典数学题100例 1",
					Title:       "求圆的面积",
					Messages: []Message{
						{Role: "user", Content: "Question 1"},
						{Role: "assistant", Content: "Answer 1"},
					},
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
		Options: config.OptionConfig{
			WriteWarnings: true,
		},
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	outputPath := filepath.Join(outputDir, "经典数学题100例-1", "001_求圆的面积__conv-1", "001_求圆的面积__conv-1.md")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read exported markdown %q: %v", outputPath, err)
	}
	if !strings.Contains(string(content), "# 求圆的面积") {
		t.Fatalf("markdown should use conversation title, got: %s", string(content))
	}
}

func TestSplitChatGPTProjectAndTitle(t *testing.T) {
	projectName, title := splitChatGPTProjectAndTitle("经典数学题100例 1 - 求圆的面积")
	if projectName != "经典数学题100例 1" || title != "求圆的面积" {
		t.Fatalf("unexpected split: project=%q title=%q", projectName, title)
	}

	projectName, title = splitChatGPTProjectAndTitle("求圆的面积")
	if projectName != "" || title != "求圆的面积" {
		t.Fatalf("unexpected plain title split: project=%q title=%q", projectName, title)
	}
}

func TestRunProjectURLListExportContinuesAndDoesNotWritePlaceholderOnFailure(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(urlListPath, []byte(strings.Join([]string{
		"https://chatgpt.com/g/g-p-1/c/conv-1",
		"https://chatgpt.com/g/g-p-1/c/conv-2",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			routes: map[string]FetchedConversation{
				"conv-1": {
					ProjectName: "Problem One",
					Messages: []Message{
						{Role: "user", Content: "Question 1"},
						{Role: "assistant", Content: "Answer 1"},
					},
				},
			},
			errByConversation: map[string]error{
				"conv-2": &ProjectFetchError{
					Code:    "source.project_url.auth_failed",
					Message: "Auth failed",
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
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

	if _, err := os.Stat(filepath.Join(outputDir, "problem-one", "001_problem-one__conv-1", "001_problem-one__conv-1.md")); err != nil {
		t.Fatalf("expected successful export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "problem-two", "001_problem-two__conv-2", "001_placeholder.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("did not expect placeholder for failed batch item, err=%v", err)
	}

	reportContent, err := os.ReadFile(filepath.Join(outputDir, "export-report.json"))
	if err != nil {
		t.Fatalf("read export report: %v", err)
	}
	var report batchExportReport
	if err := json.Unmarshal(reportContent, &report); err != nil {
		t.Fatalf("unmarshal export report: %v", err)
	}
	if report.Summary.Success != 1 || report.Summary.Failed != 1 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
}

func TestRunProjectURLListExportContinuesAfterPerURLTimeout(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(urlListPath, []byte(strings.Join([]string{
		"https://chatgpt.com/g/g-p-1/c/conv-1",
		"https://chatgpt.com/g/g-p-1/c/conv-2",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	originalFactory := projectFetcherFactory
	originalTimeout := projectURLFetchTimeout
	projectURLFetchTimeout = 20 * time.Millisecond
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return timeoutStubProjectFetcher{
			blockedConversation: "conv-1",
			routes: map[string]FetchedConversation{
				"conv-2": {
					ProjectName: "Problem Two",
					Messages: []Message{
						{Role: "user", Content: "Question 2"},
						{Role: "assistant", Content: "Answer 2"},
					},
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
		projectURLFetchTimeout = originalTimeout
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
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

	if _, err := os.Stat(filepath.Join(outputDir, "problem-two", "001_problem-two__conv-2", "001_problem-two__conv-2.md")); err != nil {
		t.Fatalf("expected second export to continue after timeout: %v", err)
	}

	reportContent, err := os.ReadFile(filepath.Join(outputDir, "export-report.json"))
	if err != nil {
		t.Fatalf("read export report: %v", err)
	}
	var report batchExportReport
	if err := json.Unmarshal(reportContent, &report); err != nil {
		t.Fatalf("unmarshal export report: %v", err)
	}
	if report.Summary.Success != 1 || report.Summary.Failed != 1 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
	if !strings.Contains(string(reportContent), "source.project_url.fetch_timeout") {
		t.Fatalf("expected timeout error in report: %s", string(reportContent))
	}
}

func TestRunProjectURLListExportStopsAfterBrowserSessionLost(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(urlListPath, []byte(strings.Join([]string{
		"https://chatgpt.com/g/g-p-1/c/conv-1",
		"https://chatgpt.com/g/g-p-1/c/conv-2",
		"https://chatgpt.com/g/g-p-1/c/conv-3",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			routes: map[string]FetchedConversation{
				"conv-1": {
					ProjectName: "Problem One",
					Messages: []Message{
						{Role: "user", Content: "Question 1"},
						{Role: "assistant", Content: "Answer 1"},
					},
				},
			},
			errByConversation: map[string]error{
				"conv-2": &ProjectFetchError{
					Code:    "source.project_url.browser_session_lost",
					Message: "session lost in test",
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
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

	reportContent, err := os.ReadFile(filepath.Join(outputDir, "export-report.json"))
	if err != nil {
		t.Fatalf("read export report: %v", err)
	}
	var report batchExportReport
	if err := json.Unmarshal(reportContent, &report); err != nil {
		t.Fatalf("unmarshal export report: %v", err)
	}
	if report.Summary.Success != 1 || report.Summary.Failed != 1 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
	if report.Summary.Total != 2 {
		t.Fatalf("expected batch to stop after session_lost, total processed=%d", report.Summary.Total)
	}

	warningsContent, err := os.ReadFile(filepath.Join(outputDir, "warnings.json"))
	if err != nil {
		t.Fatalf("read warnings: %v", err)
	}
	if !strings.Contains(string(warningsContent), "source.project_url_list.aborted_session_lost") {
		t.Fatalf("expected abort warning for session loss, got: %s", string(warningsContent))
	}
}

func TestRunProjectURLListExportSkipsCompletedEntriesByDefault(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	rawURL := "https://chatgpt.com/g/g-p-1/c/conv-1"
	if err := os.WriteFile(urlListPath, []byte(rawURL+"\n"), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	projectDir := filepath.Join(outputDir, "problem-one", "001_problem-one__conv-1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	outputPath := filepath.Join(projectDir, "001_problem-one__conv-1.md")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing export: %v", err)
	}
	existingReport := batchExportReport{
		SourceType: "project_url_list",
		URLList:    urlListPath,
		Entries: []batchExportEntry{
			{
				Line:        1,
				URL:         rawURL,
				Status:      "success",
				ProjectName: "Problem One",
				OutputPath:  outputPath,
				UpdatedAt:   timeNowString(),
			},
		},
	}
	if err := writeBatchExportReport(filepath.Join(outputDir, "export-report.json"), existingReport); err != nil {
		t.Fatalf("write existing report: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			err: errors.New("fetcher should not be called for skipped entry"),
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
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

	reportContent, err := os.ReadFile(filepath.Join(outputDir, "export-report.json"))
	if err != nil {
		t.Fatalf("read export report: %v", err)
	}
	var report batchExportReport
	if err := json.Unmarshal(reportContent, &report); err != nil {
		t.Fatalf("unmarshal export report: %v", err)
	}
	if report.Summary.Skipped != 1 || report.Entries[0].Status != "skipped_existing" {
		t.Fatalf("unexpected skipped report state: %#v", report)
	}
}

func TestRunProjectURLListExportOverwriteRefetches(t *testing.T) {
	withValidSessionCookiePreflight(t)
	outputDir := t.TempDir()
	urlListPath := filepath.Join(t.TempDir(), "urls.txt")
	rawURL := "https://chatgpt.com/g/g-p-1/c/conv-1"
	if err := os.WriteFile(urlListPath, []byte(rawURL+"\n"), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	projectDir := filepath.Join(outputDir, "problem-one", "001_problem-one__conv-1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	outputPath := filepath.Join(projectDir, "001_problem-one__conv-1.md")
	if err := os.WriteFile(outputPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing export: %v", err)
	}
	existingReport := batchExportReport{
		SourceType: "project_url_list",
		URLList:    urlListPath,
		Entries: []batchExportEntry{
			{
				Line:        1,
				URL:         rawURL,
				Status:      "success",
				ProjectName: "Problem One",
				OutputPath:  outputPath,
				UpdatedAt:   timeNowString(),
			},
		},
	}
	if err := writeBatchExportReport(filepath.Join(outputDir, "export-report.json"), existingReport); err != nil {
		t.Fatalf("write existing report: %v", err)
	}

	originalFactory := projectFetcherFactory
	projectFetcherFactory = func(cfg config.Config) ProjectFetcher {
		return routedStubProjectFetcher{
			routes: map[string]FetchedConversation{
				"conv-1": {
					ProjectName: "Problem One",
					Messages: []Message{
						{Role: "user", Content: "Question 1"},
						{Role: "assistant", Content: "New Answer"},
					},
				},
			},
		}
	}
	defer func() {
		projectFetcherFactory = originalFactory
	}()

	cfg := config.Config{
		Source: config.SourceConfig{
			Type:    "project_url_list",
			URLList: urlListPath,
		},
		Output: config.OutputConfig{
			Dir:       outputDir,
			AssetsDir: filepath.Join(outputDir, "assets"),
		},
		Options: config.OptionConfig{
			WriteReadme:       true,
			WriteWarnings:     true,
			PreserveLinks:     true,
			OverwriteExisting: true,
		},
	}

	if err := Run(cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read overwritten export: %v", err)
	}
	if !strings.Contains(string(content), "New Answer") {
		t.Fatalf("expected overwritten export content, got: %s", string(content))
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

func TestCDPExtractionScriptContainsTableSerialization(t *testing.T) {
	src, err := os.ReadFile("browser_fetcher.go")
	if err != nil {
		t.Fatalf("read browser_fetcher.go: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, "const tableToMarkdown = (table) =>") {
		t.Fatalf("expected table serialization helper in CDP extraction script")
	}
	if !strings.Contains(text, "clone.querySelectorAll('pre').forEach((el) => {") {
		t.Fatalf("expected pre/code block preservation in CDP extraction script")
	}
	if !strings.Contains(text, "clone.querySelectorAll('table').forEach((el) =>") {
		t.Fatalf("expected table replacement in CDP extraction script")
	}
	if !strings.Contains(text, "map((tr) => Array.from(tr.querySelectorAll('th,td')).map((cell) => escapeCell(serializeFragment(cell))))") {
		t.Fatalf("expected table cells to be serialized through the shared fragment serializer")
	}
	if !strings.Contains(text, "clone.querySelectorAll('h1,h2,h3,h4,h5,h6').forEach((el) =>") {
		t.Fatalf("expected heading preservation in CDP extraction script")
	}
	if !strings.Contains(text, `" + "#".repeat(level) + " " + text + "`) {
		t.Fatalf("expected headings to be converted to markdown headings")
	}
	if !strings.Contains(text, "clone.querySelectorAll('p').forEach((el) =>") {
		t.Fatalf("expected paragraph preservation in CDP extraction script")
	}
}

func TestEnsureBrowserProfileRoot(t *testing.T) {
	root, err := ensureBrowserProfileRoot(filepath.Join(t.TempDir(), "browser-profile"))
	if err != nil {
		t.Fatalf("ensureBrowserProfileRoot() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Default")); err != nil {
		t.Fatalf("expected Default profile dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Local State")); err != nil {
		t.Fatalf("expected Local State file: %v", err)
	}
}

func TestBrowserProfileRootFromEnv(t *testing.T) {
	t.Setenv(browserProfileRootEnv, "/tmp/cgme-browser-profile-test")
	if got := browserProfileRoot(); got != "/tmp/cgme-browser-profile-test" {
		t.Fatalf("unexpected browser profile root: %s", got)
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
	if len(got[0].Blocks) != 2 || got[0].Blocks[0].Kind != model.BlockParagraph || got[0].Blocks[1].Kind != model.BlockParagraph {
		t.Fatalf("expected user content to be parsed into paragraph blocks, got %#v", got[0].Blocks)
	}
	if len(got[1].Blocks) != 2 || got[1].Blocks[0].Kind != model.BlockParagraph || got[1].Blocks[1].Kind != model.BlockParagraph {
		t.Fatalf("expected merged assistant content to be parsed into paragraph blocks, got %#v", got[1].Blocks)
	}
	if len(warnings) != 3 {
		t.Fatalf("expected normalization warnings, got %#v", warnings)
	}
}

func TestNormalizeBrowserMessagesParsesStructuredBlocks(t *testing.T) {
	got, _ := normalizeBrowserMessages([]Message{
		{
			Role: "assistant",
			Content: strings.Join([]string{
				"我先给一个表格：",
				"",
				"| n | phi(n) |",
				"| --- | --- |",
				"| 5 | 4 |",
				"",
				"```math",
				`(\mathbb{Z}/5\mathbb{Z})^\times \cong C_4`,
				"```",
			}, "\n"),
		},
	})

	if len(got) != 1 {
		t.Fatalf("expected one message, got %#v", got)
	}
	if len(got[0].Blocks) != 3 {
		t.Fatalf("expected paragraph + table + math blocks, got %#v", got[0].Blocks)
	}
	if got[0].Blocks[0].Kind != model.BlockParagraph || got[0].Blocks[1].Kind != model.BlockTable || got[0].Blocks[2].Kind != model.BlockMath {
		t.Fatalf("unexpected block kinds: %#v", got[0].Blocks)
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

func TestBrowserLoginPageDetection(t *testing.T) {
	cases := []browserConversationPayload{
		{URL: "https://chatgpt.com/auth/login/?next=%2Fc%2Fconv-1"},
		{Title: "开始使用 | ChatGPT", Snippet: "开始使用\n登录\n免费注册"},
		{Title: "Login | ChatGPT", Snippet: "Log in\nSign up"},
	}
	for _, tc := range cases {
		if !isBrowserLoginPage(tc) {
			t.Fatalf("expected login page detection for %#v", tc)
		}
	}
	if isBrowserLoginPage(browserConversationPayload{
		Title:   "ChatGPT - 经典数学题100例",
		URL:     "https://chatgpt.com/g/g-p-demo/project",
		Snippet: "历史聊天记录\n经典数学题100例",
	}) {
		t.Fatalf("did not expect project page to be detected as login")
	}
}

func TestNewBrowserProjectFetcherAllowsExistingSessionWithoutCookie(t *testing.T) {
	origStat := osStat
	origLookPath := execLookPath
	origCDPReady := cdpReady
	defer func() {
		osStat = origStat
		execLookPath = origLookPath
		cdpReady = origCDPReady
	}()

	osStat = func(name string) (os.FileInfo, error) { return fakeFileInfo{name: filepath.Base(name)}, nil }
	execLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	cdpReady = func(ctx context.Context, port int) bool { return true }

	fetcher, ok := newBrowserProjectFetcher("")
	if !ok || fetcher == nil {
		t.Fatalf("expected browser fetcher to be available when an existing CDP session is ready")
	}
}

func TestBrowserFetcherDoesNotRelaunchAfterStartupFailure(t *testing.T) {
	origEnsureProfile := ensureBrowserProfile
	origEnsureSession := ensureChromeSession
	origRunCDP := runCDPExtraction
	origCDPReady := cdpReady
	defer func() {
		ensureBrowserProfile = origEnsureProfile
		ensureChromeSession = origEnsureSession
		runCDPExtraction = origRunCDP
		cdpReady = origCDPReady
	}()

	ensureBrowserProfile = func(root string) (string, error) { return root, nil }
	launchCalls := 0
	ensureChromeSession = func(ctx context.Context, chromePath, profileRoot string, port int) (int, bool, error) {
		launchCalls++
		return 0, false, fmt.Errorf("boom")
	}
	runCDPExtraction = func(ctx context.Context, port int, pageURL, cookieHeader string, waitAfter time.Duration) (browserConversationPayload, error) {
		t.Fatalf("runCDPExtraction should not be called when startup fails")
		return browserConversationPayload{}, nil
	}
	cdpReady = func(ctx context.Context, port int) bool { return false }

	fetcher := &CDPBrowserProjectFetcher{
		chromePath:   chromeAppPath,
		waitAfter:    time.Second,
		cookieHeader: "session=test",
		profileRoot:  "/tmp/cgme-browser-profile",
		debugPort:    9223,
	}

	for i := 0; i < 2; i++ {
		_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
			Host:           "chatgpt.com",
			ConversationID: "conv-1",
		})
		if err == nil || !strings.Contains(err.Error(), "browser_launch_failed") {
			t.Fatalf("expected startup failure on attempt %d, got %v", i+1, err)
		}
	}

	if launchCalls != 1 {
		t.Fatalf("expected one browser launch attempt, got %d", launchCalls)
	}
}

func TestBrowserFetcherReturnsAuthFailedForLoginPage(t *testing.T) {
	origEnsureProfile := ensureBrowserProfile
	origEnsureSession := ensureChromeSession
	origRunCDP := runCDPExtraction
	origCDPReady := cdpReady
	defer func() {
		ensureBrowserProfile = origEnsureProfile
		ensureChromeSession = origEnsureSession
		runCDPExtraction = origRunCDP
		cdpReady = origCDPReady
	}()

	ensureBrowserProfile = func(root string) (string, error) { return root, nil }
	ensureChromeSession = func(ctx context.Context, chromePath, profileRoot string, port int) (int, bool, error) {
		return port, true, nil
	}
	runCDPExtraction = func(ctx context.Context, port int, pageURL, cookieHeader string, waitAfter time.Duration) (browserConversationPayload, error) {
		return browserConversationPayload{
			Title:   "开始使用 | ChatGPT",
			URL:     "https://chatgpt.com/auth/login/?next=%2Fc%2Fconv-1",
			Snippet: "开始使用\n登录\n免费注册",
		}, nil
	}
	cdpReady = func(ctx context.Context, port int) bool { return false }

	fetcher := &CDPBrowserProjectFetcher{
		chromePath:   chromeAppPath,
		waitAfter:    time.Second,
		cookieHeader: "__Secure-next-auth.session-token=expired",
		profileRoot:  "/tmp/cgme-browser-profile",
		debugPort:    9223,
	}

	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-1",
	})
	if err == nil || !strings.Contains(err.Error(), "cookie_auth_failed") {
		t.Fatalf("expected cookie_auth_failed for login page, got %v", err)
	}
}

func TestBrowserFetcherDoesNotRelaunchAfterSessionLoss(t *testing.T) {
	origEnsureProfile := ensureBrowserProfile
	origEnsureSession := ensureChromeSession
	origRunCDP := runCDPExtraction
	origCDPReady := cdpReady
	defer func() {
		ensureBrowserProfile = origEnsureProfile
		ensureChromeSession = origEnsureSession
		runCDPExtraction = origRunCDP
		cdpReady = origCDPReady
	}()

	ensureBrowserProfile = func(root string) (string, error) { return root, nil }
	launchCalls := 0
	ensureChromeSession = func(ctx context.Context, chromePath, profileRoot string, port int) (int, bool, error) {
		launchCalls++
		return port, true, nil
	}
	runCDPExtraction = func(ctx context.Context, port int, pageURL, cookieHeader string, waitAfter time.Duration) (browserConversationPayload, error) {
		return browserConversationPayload{
			Title: "Recovered Session",
			Messages: []browserConversationMsg{
				{Role: "user", Content: "Q"},
				{Role: "assistant", Content: "A"},
			},
		}, nil
	}
	cdpReady = func(ctx context.Context, port int) bool { return false }

	fetcher := &CDPBrowserProjectFetcher{
		chromePath:   chromeAppPath,
		waitAfter:    time.Second,
		cookieHeader: "session=test",
		profileRoot:  "/tmp/cgme-browser-profile",
		debugPort:    9223,
	}

	_, err := fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-1",
	})
	if err != nil {
		t.Fatalf("initial fetch error = %v", err)
	}

	_, err = fetcher.FetchConversation(t.Context(), ProjectURLInfo{
		Host:           "chatgpt.com",
		ConversationID: "conv-2",
	})
	if err == nil || !strings.Contains(err.Error(), "browser_session_lost") {
		t.Fatalf("expected session_lost error, got %v", err)
	}

	if launchCalls != 1 {
		t.Fatalf("expected no relaunch after session loss, got %d launch attempt(s)", launchCalls)
	}
}

func TestReadProjectURLList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	content := strings.Join([]string{
		"  ",
		"# comment",
		"https://chatgpt.com/c/conv-1",
		" https://chatgpt.com/c/conv-2 ",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write url list: %v", err)
	}

	got, err := readProjectURLList(path)
	if err != nil {
		t.Fatalf("readProjectURLList() error = %v", err)
	}
	if len(got) != 2 || got[0] != "https://chatgpt.com/c/conv-1" || got[1] != "https://chatgpt.com/c/conv-2" {
		t.Fatalf("unexpected URL list: %#v", got)
	}
}

func TestValidateDiscoveryURL(t *testing.T) {
	if err := validateDiscoveryURL("https://chatgpt.com/g/g-p-demo"); err != nil {
		t.Fatalf("expected valid discovery URL, got %v", err)
	}
	if err := validateDiscoveryURL("http://chatgpt.com/g/g-p-demo"); err == nil {
		t.Fatalf("expected https validation failure")
	}
}

func TestDiscoverProjectPageURLsWritesOutputList(t *testing.T) {
	t.Setenv(sessionCookieEnv, "__Secure-next-auth.session-token=test")
	outputList := filepath.Join(t.TempDir(), "math-sessions.txt")

	origFactory := browserProjectFetcherFactory
	defer func() {
		browserProjectFetcherFactory = origFactory
	}()

	browserProjectFetcherFactory = func(cookieHeader string) (ProjectFetcher, bool) {
		return discoveryStubFetcher{
			links: []discoveredConversationLink{
				{Title: "题目 1", URL: "https://chatgpt.com/c/conv-1"},
				{Title: "题目 2", URL: "https://chatgpt.com/g/g-p-demo/c/conv-2"},
				{Title: "题目 3", URL: "https://chatgpt.com/g/g-p-demo/c/conv-3"},
			},
		}, true
	}

	if err := DiscoverProjectPageURLs("https://chatgpt.com/g/g-p-demo", "", outputList); err != nil {
		t.Fatalf("DiscoverProjectPageURLs() error = %v", err)
	}

	content, err := os.ReadFile(outputList)
	if err != nil {
		t.Fatalf("read output list: %v", err)
	}
	got := string(content)
	if strings.Contains(got, "https://chatgpt.com/c/conv-1\n") {
		t.Fatalf("expected non-project conversation URL to be filtered out: %s", got)
	}
	if !strings.Contains(got, "https://chatgpt.com/g/g-p-demo/c/conv-2\n") || !strings.Contains(got, "https://chatgpt.com/g/g-p-demo/c/conv-3\n") {
		t.Fatalf("unexpected output list content: %s", got)
	}
}

func TestFilterDiscoveredLinks(t *testing.T) {
	links := []discoveredConversationLink{
		{URL: "https://chatgpt.com/c/conv-1"},
		{URL: "https://chatgpt.com/g/g-p-demo/c/conv-2"},
		{URL: "https://chatgpt.com/g/g-p-other/c/conv-3"},
	}
	got := filterDiscoveredLinks("https://chatgpt.com/g/g-p-demo", links)
	if len(got) != 1 || got[0].URL != "https://chatgpt.com/g/g-p-demo/c/conv-2" {
		t.Fatalf("unexpected filtered links: %#v", got)
	}
}

func TestBuildProjectConversationPrefix(t *testing.T) {
	got := buildProjectConversationPrefix("https://chatgpt.com/g/g-p-demo-project/project")
	if got != "https://chatgpt.com/g/g-p-demo-project/c/" {
		t.Fatalf("unexpected conversation prefix: %q", got)
	}
	if got := buildProjectConversationPrefix("https://chatgpt.com/c/conv-1"); got != "" {
		t.Fatalf("expected empty prefix for non-project URL, got %q", got)
	}
}

type fakeFileInfo struct {
	name string
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

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

type routedStubProjectFetcher struct {
	routes            map[string]FetchedConversation
	err               error
	errByConversation map[string]error
}

func (s routedStubProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if s.err != nil {
		return FetchedConversation{}, s.err
	}
	if err, ok := s.errByConversation[info.ConversationID]; ok {
		return FetchedConversation{}, err
	}
	if fetched, ok := s.routes[info.ConversationID]; ok {
		return fetched, nil
	}
	return FetchedConversation{}, &ProjectFetchError{
		Code:    "source.project_url.missing_test_route",
		Message: fmt.Sprintf("no stub fetch route for conversation %q", info.ConversationID),
	}
}

type discoveryStubFetcher struct {
	links []discoveredConversationLink
	err   error
}

func (s discoveryStubFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	return FetchedConversation{}, s.err
}

func (s discoveryStubFetcher) DiscoverProjectPageURLs(ctx context.Context, pageURL string) ([]discoveredConversationLink, error) {
	return s.links, s.err
}

type timeoutStubProjectFetcher struct {
	blockedConversation string
	routes              map[string]FetchedConversation
}

func (s timeoutStubProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if info.ConversationID == s.blockedConversation {
		<-ctx.Done()
		return FetchedConversation{}, ctx.Err()
	}
	if fetched, ok := s.routes[info.ConversationID]; ok {
		return fetched, nil
	}
	return FetchedConversation{}, &ProjectFetchError{
		Code:    "source.project_url.missing_test_route",
		Message: fmt.Sprintf("no timeout stub fetch route for conversation %q", info.ConversationID),
	}
}
