package exporters

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
)

type warningRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type projectURLExportOptions struct {
	AllowPlaceholder bool
	Overwrite        bool
}

type batchExportReport struct {
	SourceType string             `json:"source_type"`
	URLList    string             `json:"url_list"`
	UpdatedAt  string             `json:"updated_at"`
	Summary    batchExportSummary `json:"summary"`
	Entries    []batchExportEntry `json:"entries"`
}

type batchExportSummary struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type batchExportEntry struct {
	Line        int    `json:"line"`
	URL         string `json:"url"`
	Status      string `json:"status"`
	ProjectName string `json:"project_name,omitempty"`
	OutputPath  string `json:"output_path,omitempty"`
	Error       string `json:"error,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

var projectFetcherFactory = NewProjectFetcher
var imageMarkerPattern = regexp.MustCompile(`\[\[CGME_IMAGE:(\{.*?\})\]\]`)
var newAssetHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
var projectURLFetchTimeout = 75 * time.Second

func Run(cfg config.Config) error {
	if err := os.MkdirAll(cfg.Output.Dir, 0o755); err != nil {
		return fmt.Errorf("create output directory %q: %w", cfg.Output.Dir, err)
	}
	if err := os.MkdirAll(cfg.Output.AssetsDir, 0o755); err != nil {
		return fmt.Errorf("create assets directory %q: %w", cfg.Output.AssetsDir, err)
	}

	switch cfg.Source.Type {
	case "bundle":
		return runBundleExport(cfg)
	case "project_url":
		return runProjectURLExport(cfg)
	case "project_url_list":
		return runProjectURLListExport(cfg)
	default:
		return fmt.Errorf("unsupported source type %q", cfg.Source.Type)
	}
}

func runBundleExport(cfg config.Config) error {
	conversations, warnings, err := loadBundle(cfg.Source.Path)
	if err != nil {
		return err
	}

	filtered := filterConversations(conversations, cfg.Source.Project)
	if len(filtered) == 0 {
		if cfg.Source.Project != "" {
			return fmt.Errorf("no conversations matched project %q in %q", cfg.Source.Project, cfg.Source.Path)
		}
		return fmt.Errorf("no conversations found in %q", cfg.Source.Path)
	}

	projectName := chooseProjectName(cfg, filtered)
	projectDir := filepath.Join(cfg.Output.Dir, slugify(projectName))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project directory %q: %w", projectDir, err)
	}

	for i, conv := range filtered {
		rendered, renderWarnings := renderConversationMarkdown(conv, cfg.Options)
		warnings = append(warnings, qualifyWarnings(conv, renderWarnings)...)
		filename := fmt.Sprintf("%03d_%s.md", i+1, slugify(conversationFileBase(conv.Title, i+1)))
		outputPath := filepath.Join(projectDir, filename)
		rendered, assetWarnings, err := materializeMarkdownAssets(rendered, outputPath, cfg.Output.AssetsDir, "")
		if err != nil {
			return fmt.Errorf("materialize conversation assets %q: %w", filename, err)
		}
		warnings = append(warnings, qualifyWarnings(conv, assetWarnings)...)
		if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write conversation markdown %q: %w", filename, err)
		}
	}

	if cfg.Options.WriteReadme {
		if err := writeOutputReadme(filepath.Join(cfg.Output.Dir, "README.md"), cfg, projectName, len(filtered), false); err != nil {
			return err
		}
	}

	if cfg.Options.WriteWarnings {
		if err := writeWarnings(filepath.Join(cfg.Output.Dir, "warnings.json"), warnings); err != nil {
			return err
		}
	}

	return nil
}

func runProjectURLExport(cfg config.Config) error {
	fetcher := projectFetcherFactory(cfg)
	result, err := exportProjectURL(cfg, fetcher, cfg.Source.ProjectURL, projectURLExportOptions{
		AllowPlaceholder: true,
		Overwrite:        cfg.Options.OverwriteExisting,
	})
	if err != nil {
		return err
	}

	if cfg.Options.WriteReadme {
		if err := writeOutputReadme(filepath.Join(cfg.Output.Dir, "README.md"), cfg, result.projectName, result.conversationCount, result.placeholder); err != nil {
			return err
		}
	}

	if cfg.Options.WriteWarnings {
		if err := writeWarnings(filepath.Join(cfg.Output.Dir, "warnings.json"), result.warnings); err != nil {
			return err
		}
	}

	return nil
}

func runProjectURLListExport(cfg config.Config) error {
	urls, err := readProjectURLList(cfg.Source.URLList)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("no project URLs found in %q", cfg.Source.URLList)
	}

	fetcher := projectFetcherFactory(cfg)
	reportPath := filepath.Join(cfg.Output.Dir, "export-report.json")
	report, err := loadBatchExportReport(reportPath, cfg.Source.URLList)
	if err != nil {
		return err
	}
	allWarnings := make([]warningRecord, 0, len(urls))

	for i, rawURL := range urls {
		if !cfg.Options.OverwriteExisting {
			if entry, ok := report.completedEntry(rawURL); ok && strings.TrimSpace(entry.OutputPath) != "" && fileExists(entry.OutputPath) {
				report.record(batchExportEntry{
					Line:        i + 1,
					URL:         rawURL,
					Status:      "skipped_existing",
					ProjectName: entry.ProjectName,
					OutputPath:  entry.OutputPath,
					UpdatedAt:   timeNowString(),
				})
				allWarnings = append(allWarnings, warningRecord{
					Code:    "source.project_url_list.skipped_existing",
					Message: fmt.Sprintf("Line %d URL %q was skipped because a successful export already exists at %q.", i+1, rawURL, entry.OutputPath),
				})
				if err := writeBatchExportReport(reportPath, report); err != nil {
					return err
				}
				continue
			}
		}

		result, err := exportProjectURL(cfg, fetcher, rawURL, projectURLExportOptions{
			AllowPlaceholder: false,
			Overwrite:        cfg.Options.OverwriteExisting,
		})
		allWarnings = append(allWarnings, result.warnings...)
		if err != nil {
			report.record(batchExportEntry{
				Line:        i + 1,
				URL:         rawURL,
				Status:      "failed",
				ProjectName: result.projectName,
				OutputPath:  result.outputPath,
				Error:       err.Error(),
				UpdatedAt:   timeNowString(),
			})
			allWarnings = append(allWarnings, warningRecord{
				Code:    "source.project_url_list.failed",
				Message: fmt.Sprintf("Line %d URL %q could not be exported: %v", i+1, rawURL, err),
			})
			if err := writeBatchExportReport(reportPath, report); err != nil {
				return err
			}
			if isBrowserSessionLostError(err) {
				allWarnings = append(allWarnings, warningRecord{
					Code:    "source.project_url_list.aborted_session_lost",
					Message: fmt.Sprintf("Aborted batch at line %d because the reusable browser session was lost; rerun after stabilizing the Chrome DevTools session.", i+1),
				})
				break
			}
			continue
		}

		status := "success"
		if result.skipped {
			status = "skipped_existing"
		} else {
			status = "success"
		}
		report.record(batchExportEntry{
			Line:        i + 1,
			URL:         rawURL,
			Status:      status,
			ProjectName: result.projectName,
			OutputPath:  result.outputPath,
			UpdatedAt:   timeNowString(),
		})
		if err := writeBatchExportReport(reportPath, report); err != nil {
			return err
		}
	}

	if cfg.Options.WriteReadme {
		if err := writeOutputReadme(filepath.Join(cfg.Output.Dir, "README.md"), cfg, fmt.Sprintf("%d project URL(s)", len(urls)), report.Summary.Success, false); err != nil {
			return err
		}
	}

	if cfg.Options.WriteWarnings {
		allWarnings = append(allWarnings, warningRecord{
			Code:    "source.project_url_list.completed",
			Message: fmt.Sprintf("Processed %d project URL(s): %d succeeded, %d failed, %d skipped.", len(urls), report.Summary.Success, report.Summary.Failed, report.Summary.Skipped),
		})
		if err := writeWarnings(filepath.Join(cfg.Output.Dir, "warnings.json"), allWarnings); err != nil {
			return err
		}
	}

	return nil
}

func isBrowserSessionLostError(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "source.project_url.browser_session_lost") {
		return true
	}
	var fetchErr *ProjectFetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.Code == "source.project_url.browser_session_lost"
	}
	return false
}

type projectURLExportResult struct {
	projectName       string
	conversationCount int
	placeholder       bool
	warnings          []warningRecord
	outputPath        string
	skipped           bool
}

func exportProjectURL(cfg config.Config, fetcher ProjectFetcher, rawURL string, opts projectURLExportOptions) (projectURLExportResult, error) {
	urlInfo, err := parseProjectURL(rawURL)
	if err != nil {
		return projectURLExportResult{}, err
	}

	fetchCtx, cancel := context.WithTimeout(context.Background(), projectURLFetchTimeout)
	defer cancel()

	fetched, fetchErr := fetcher.FetchConversation(fetchCtx, urlInfo)
	if fetchErr != nil && (errors.Is(fetchErr, context.DeadlineExceeded) || errors.Is(fetchCtx.Err(), context.DeadlineExceeded)) {
		fetchErr = &ProjectFetchError{
			Code:    "source.project_url.fetch_timeout",
			Message: fmt.Sprintf("Timed out while exporting conversation %q after %s.", emptyFallback(urlInfo.ConversationID), projectURLFetchTimeout),
		}
	}

	projectName := chooseProjectName(cfg, nil)
	if cfg.Source.Project == "" && urlInfo.GPTSlug != "" {
		projectName = urlInfo.GPTSlug
	}
	if cfg.Source.Project == "" && strings.TrimSpace(fetched.ProjectName) != "" {
		projectName = fetched.ProjectName
	}

	warnings := []warningRecord{
		{
			Code:    "source.project_url.parsed",
			Message: fmt.Sprintf("Recognized URL host=%q path_type=%q conversation_id=%q.", urlInfo.Host, urlInfo.PathType, emptyFallback(urlInfo.ConversationID)),
		},
	}
	conversationCount := 1

	if fetchErr == nil && len(fetched.Messages) > 0 {
		conv := conversationFromFetched(urlInfo, fetched)
		projectDir := filepath.Join(cfg.Output.Dir, slugify(projectName))
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			return projectURLExportResult{}, fmt.Errorf("create project directory %q: %w", projectDir, err)
		}
		warnings = append(warnings, fetched.Warnings...)
		rendered, renderWarnings := renderConversationMarkdown(conv, cfg.Options)
		warnings = append(warnings, qualifyWarnings(conv, renderWarnings)...)
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.fetch_success",
			Message: fmt.Sprintf("Project URL fetch returned %d message(s) and was rendered into Markdown.", len(fetched.Messages)),
		})

		dirName := liveConversationDirName(conv, 1)
		conversationDir := filepath.Join(projectDir, dirName)
		if err := os.MkdirAll(conversationDir, 0o755); err != nil {
			return projectURLExportResult{}, fmt.Errorf("create conversation directory %q: %w", conversationDir, err)
		}
		filename := liveConversationFilename(conv, 1)
		outputPath := filepath.Join(conversationDir, filename)
		if !opts.Overwrite && fileExists(outputPath) {
			warnings = append(warnings, warningRecord{
				Code:    "source.project_url.skipped_existing",
				Message: fmt.Sprintf("Skipped writing %q because it already exists.", outputPath),
			})
			return projectURLExportResult{
				projectName:       projectName,
				conversationCount: 0,
				placeholder:       false,
				warnings:          warnings,
				outputPath:        outputPath,
				skipped:           true,
			}, nil
		}
		sessionCookie, _ := resolveSessionCookie(cfg)
		rendered, assetWarnings, err := materializeMarkdownAssets(rendered, outputPath, cfg.Output.AssetsDir, sessionCookie)
		if err != nil {
			return projectURLExportResult{}, fmt.Errorf("materialize conversation assets %q: %w", filename, err)
		}
		warnings = append(warnings, qualifyWarnings(conv, assetWarnings)...)
		if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
			return projectURLExportResult{}, fmt.Errorf("write conversation markdown %q: %w", filename, err)
		}
		placeholderPath := filepath.Join(conversationDir, "001_placeholder.md")
		if err := os.Remove(placeholderPath); err == nil {
			warnings = append(warnings, warningRecord{
				Code:    "source.project_url.stale_placeholder_removed",
				Message: fmt.Sprintf("Removed stale placeholder file %q after successful export.", placeholderPath),
			})
		} else if err != nil && !os.IsNotExist(err) {
			return projectURLExportResult{}, fmt.Errorf("remove stale placeholder %q: %w", placeholderPath, err)
		}
		legacyProjectDir := legacyProjectURLDir(cfg, urlInfo, projectDir)
		if legacyProjectDir != "" {
			legacyPlaceholderPath := filepath.Join(legacyProjectDir, "001_placeholder.md")
			if err := os.Remove(legacyPlaceholderPath); err == nil {
				warnings = append(warnings, warningRecord{
					Code:    "source.project_url.legacy_placeholder_removed",
					Message: fmt.Sprintf("Removed legacy placeholder file %q after successful export.", legacyPlaceholderPath),
				})
				_ = os.Remove(legacyProjectDir)
			} else if err != nil && !os.IsNotExist(err) {
				return projectURLExportResult{}, fmt.Errorf("remove legacy placeholder %q: %w", legacyPlaceholderPath, err)
			}
		}
		return projectURLExportResult{
			projectName:       projectName,
			conversationCount: conversationCount,
			placeholder:       false,
			warnings:          warnings,
			outputPath:        outputPath,
		}, nil
	} else {
		if opts.AllowPlaceholder {
			if fetchErr != nil {
				if warning, ok := warningFromError(fetchErr); ok {
					warnings = append(warnings, warning)
				} else {
					warnings = append(warnings, warningRecord{
						Code:    "source.project_url.fetch_failed",
						Message: fetchErr.Error(),
					})
				}
			}
			if fetchErr == nil && len(fetched.Messages) == 0 {
				warnings = append(warnings, warningRecord{
					Code:    "source.project_url.empty_result",
					Message: "Project URL fetch returned no messages. A placeholder file was generated instead.",
				})
			}
			projectDir := filepath.Join(cfg.Output.Dir, slugify(projectName))
			if err := os.MkdirAll(projectDir, 0o755); err != nil {
				return projectURLExportResult{}, fmt.Errorf("create project directory %q: %w", projectDir, err)
			}
			conversationDir := filepath.Join(projectDir, livePlaceholderDirName(projectName, urlInfo, 1))
			if err := os.MkdirAll(conversationDir, 0o755); err != nil {
				return projectURLExportResult{}, fmt.Errorf("create conversation directory %q: %w", conversationDir, err)
			}
			placeholderPath := filepath.Join(conversationDir, "001_placeholder.md")
			if err := writePlaceholderConversation(placeholderPath, cfg, projectName, urlInfo, fetchErr, rawURL); err != nil {
				return projectURLExportResult{}, err
			}
			return projectURLExportResult{
				projectName:       projectName,
				conversationCount: conversationCount,
				placeholder:       true,
				warnings:          warnings,
				outputPath:        placeholderPath,
			}, nil
		}
	}

	if fetchErr != nil {
		if warning, ok := warningFromError(fetchErr); ok {
			warnings = append(warnings, warning)
		} else {
			warnings = append(warnings, warningRecord{
				Code:    "source.project_url.fetch_failed",
				Message: fetchErr.Error(),
			})
		}
		return projectURLExportResult{
			projectName: projectName,
			warnings:    warnings,
		}, fetchErr
	}
	if fetchErr == nil && len(fetched.Messages) == 0 {
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.empty_result",
			Message: "Project URL fetch returned no messages.",
		})
		return projectURLExportResult{
			projectName: projectName,
			warnings:    warnings,
		}, fmt.Errorf("project URL fetch returned no messages")
	}
	return projectURLExportResult{}, nil
}

func conversationFromFetched(urlInfo ProjectURLInfo, fetched FetchedConversation) Conversation {
	title := firstNonEmpty(fetched.Title, fetched.ProjectName, urlInfo.GPTSlug, "Untitled Conversation")
	return Conversation{
		ID:       urlInfo.ConversationID,
		Title:    title,
		Messages: fetched.Messages,
	}
}

func splitChatGPTProjectAndTitle(rawTitle string) (string, string) {
	title := strings.TrimSpace(rawTitle)
	if title == "" {
		return "", ""
	}
	for _, sep := range []string{" - ", " ｜ ", " | "} {
		if left, right, ok := strings.Cut(title, sep); ok {
			left = strings.TrimSpace(left)
			right = strings.TrimSpace(right)
			if left != "" && right != "" {
				return left, right
			}
		}
	}
	return "", title
}

func chooseProjectName(cfg config.Config, conversations []Conversation) string {
	if cfg.Source.Project != "" {
		return cfg.Source.Project
	}
	if len(conversations) == 1 && conversations[0].Title != "" {
		return conversations[0].Title
	}
	switch cfg.Source.Type {
	case "bundle":
		return "chatgpt-export"
	case "project_url":
		return "chatgpt-project"
	default:
		return "export"
	}
}

func conversationFileBase(title string, index int) string {
	if title == "" {
		return fmt.Sprintf("conversation-%03d", index)
	}
	return title
}

func liveConversationFilename(conv Conversation, index int) string {
	return liveConversationStem(conv, index) + ".md"
}

func liveConversationDirName(conv Conversation, index int) string {
	return liveConversationStem(conv, index)
}

func liveConversationStem(conv Conversation, index int) string {
	base := slugify(conversationFileBase(conv.Title, index))
	if id := strings.TrimSpace(conv.ID); id != "" {
		return fmt.Sprintf("%03d_%s__%s", index, base, slugify(id))
	}
	return fmt.Sprintf("%03d_%s", index, base)
}

func livePlaceholderDirName(projectName string, urlInfo ProjectURLInfo, index int) string {
	base := slugify(projectName)
	if base == "" {
		base = slugify(urlInfo.GPTSlug)
	}
	if base == "" {
		base = "chatgpt-project"
	}
	if id := strings.TrimSpace(urlInfo.ConversationID); id != "" {
		return fmt.Sprintf("%03d_%s__%s", index, base, slugify(id))
	}
	return fmt.Sprintf("%03d_%s", index, base)
}

func writePlaceholderConversation(path string, cfg config.Config, projectName string, urlInfo ProjectURLInfo, fetchErr error, rawURL string) error {
	var details strings.Builder
	statusHeading := "Live project URL fetch did not return exportable messages for this run, so a placeholder file was written instead."
	if urlInfo.Host != "" {
		fmt.Fprintf(&details, "- URL Host: %s\n", urlInfo.Host)
	}
	if urlInfo.PathType != "" {
		fmt.Fprintf(&details, "- URL Type: %s\n", urlInfo.PathType)
	}
	if urlInfo.GPTID != "" {
		fmt.Fprintf(&details, "- GPT ID: %s\n", urlInfo.GPTID)
	}
	if urlInfo.GPTSlug != "" {
		fmt.Fprintf(&details, "- GPT Slug: %s\n", urlInfo.GPTSlug)
	}
	if urlInfo.ConversationID != "" {
		fmt.Fprintf(&details, "- Conversation ID: %s\n", urlInfo.ConversationID)
	}
	if fetchErr != nil {
		fmt.Fprintf(&details, "- Fetch Status: %s\n", fetchErr.Error())
	}

	content := fmt.Sprintf(`# %s

> Placeholder export generated because this run did not produce exportable conversation content.

## Source

- Type: %s
- Bundle Path: %s
- Project URL: %s
%s

## Status

%s
`,
		projectName,
		cfg.Source.Type,
		emptyFallback(cfg.Source.Path),
		emptyFallback(rawURL),
		details.String(),
		statusHeading,
	)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write placeholder markdown %q: %w", path, err)
	}
	return nil
}

func writeOutputReadme(path string, cfg config.Config, projectName string, conversationCount int, placeholder bool) error {
	status := "Bundle conversations were exported into Markdown files."
	if cfg.Source.Type == "project_url" {
		status = "Live project URL conversation content was fetched and rendered into Markdown files."
	}
	if cfg.Source.Type == "project_url_list" {
		status = "Live project URL list entries were fetched and rendered into Markdown files."
	}
	if placeholder {
		status = "This output contains a placeholder because the live project URL fetch did not yield exportable conversation content."
	}

	content := fmt.Sprintf(`# CGME Export Output

This directory was generated by CGME.

## Summary

- Project: %s
- Source Type: %s
- Conversation Files: %d
- Assets Directory: %s
- Preserve Links: %t

## Notes

- %s
- This output layout is intended to be reviewed locally and pushed to GitHub directly.
`, projectName, cfg.Source.Type, conversationCount, cfg.Output.AssetsDir, cfg.Options.PreserveLinks, status)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write output README %q: %w", path, err)
	}
	return nil
}

func writeWarnings(path string, warnings []warningRecord) error {
	data, err := json.MarshalIndent(warnings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal warnings: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write warnings %q: %w", path, err)
	}
	return nil
}

type imageMarker struct {
	Src string `json:"src"`
	Alt string `json:"alt"`
}

func materializeMarkdownAssets(markdown, outputPath, assetsDir, cookieHeader string) (string, []warningRecord, error) {
	matches := imageMarkerPattern.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	_ = assetsDir
	assetDir := filepath.Join(filepath.Dir(outputPath), "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create asset directory %q: %w", assetDir, err)
	}

	client := newAssetHTTPClient()
	cache := make(map[string]string)
	var b strings.Builder
	last := 0
	warnings := make([]warningRecord, 0)
	savedCount := 0

	for i, match := range matches {
		b.WriteString(markdown[last:match[0]])
		last = match[1]

		var marker imageMarker
		if err := json.Unmarshal([]byte(markdown[match[2]:match[3]]), &marker); err != nil {
			warnings = append(warnings, warningRecord{
				Code:    "asset.image_marker_invalid",
				Message: fmt.Sprintf("Skipped an invalid embedded image marker near %q.", outputPath),
			})
			continue
		}
		src := strings.TrimSpace(marker.Src)
		alt := strings.TrimSpace(marker.Alt)
		if src == "" {
			warnings = append(warnings, warningRecord{
				Code:    "asset.image_src_missing",
				Message: fmt.Sprintf("Skipped an embedded image without a source near %q.", outputPath),
			})
			continue
		}

		relativePath, cached := cache[src]
		if !cached {
			savedPath, err := downloadImageAsset(client, src, assetDir, i+1, cookieHeader)
			if err != nil {
				warnings = append(warnings, warningRecord{
					Code:    "asset.image_download_failed",
					Message: fmt.Sprintf("Could not download image %q: %v. Kept the remote link instead.", src, err),
				})
				b.WriteString(renderImageMarkdown(alt, src))
				continue
			}
			relativePath, err = filepath.Rel(filepath.Dir(outputPath), savedPath)
			if err != nil {
				return "", nil, fmt.Errorf("build relative asset path for %q: %w", savedPath, err)
			}
			relativePath = filepath.ToSlash(relativePath)
			cache[src] = relativePath
			savedCount++
		}

		b.WriteString(renderImageMarkdown(alt, relativePath))
	}
	b.WriteString(markdown[last:])

	if savedCount > 0 {
		warnings = append(warnings, warningRecord{
			Code:    "asset.image_saved",
			Message: fmt.Sprintf("Saved %d image asset(s) into %q.", savedCount, assetDir),
		})
	}
	return b.String(), warnings, nil
}

func downloadImageAsset(client *http.Client, rawURL, assetDir string, index int, cookieHeader string) (string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse image URL: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("User-Agent", "CGME/0.1")
	if strings.EqualFold(parsed.Hostname(), "chatgpt.com") && strings.TrimSpace(cookieHeader) != "" {
		req.Header.Set("Cookie", cookieHeader)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read image body: %w", err)
	}
	ext := inferImageExtension(parsed, resp.Header.Get("Content-Type"), data)
	filename := fmt.Sprintf("image-%03d%s", index, ext)
	outputPath := filepath.Join(assetDir, filename)
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write image asset: %w", err)
	}
	return outputPath, nil
}

func inferImageExtension(u *neturl.URL, contentType string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(u.Path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp":
		return ext
	}

	contentType = strings.TrimSpace(strings.ToLower(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/bmp":
		return ".bmp"
	}

	if strings.HasPrefix(contentType, "image/") {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			return exts[0]
		}
	}
	if len(data) > 0 {
		switch http.DetectContentType(data) {
		case "image/png":
			return ".png"
		case "image/jpeg":
			return ".jpg"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		}
	}
	return ".bin"
}

func renderImageMarkdown(alt, target string) string {
	alt = strings.ReplaceAll(alt, "\n", " ")
	alt = strings.TrimSpace(alt)
	if alt == "" {
		alt = "image"
	}
	alt = strings.ReplaceAll(alt, "]", `\]`)
	target = strings.TrimSpace(target)
	return fmt.Sprintf("![%s](%s)", alt, target)
}

func loadBatchExportReport(path, urlList string) (batchExportReport, error) {
	report := batchExportReport{
		SourceType: "project_url_list",
		URLList:    urlList,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return batchExportReport{}, fmt.Errorf("read export report %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return batchExportReport{}, fmt.Errorf("parse export report %q: %w", path, err)
	}
	if report.SourceType == "" {
		report.SourceType = "project_url_list"
	}
	if report.URLList == "" {
		report.URLList = urlList
	}
	report.recomputeSummary()
	return report, nil
}

func writeBatchExportReport(path string, report batchExportReport) error {
	report.UpdatedAt = timeNowString()
	report.recomputeSummary()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write export report %q: %w", path, err)
	}
	return nil
}

func (r *batchExportReport) record(entry batchExportEntry) {
	for i := range r.Entries {
		if r.Entries[i].URL == entry.URL {
			r.Entries[i] = entry
			r.recomputeSummary()
			return
		}
	}
	r.Entries = append(r.Entries, entry)
	r.recomputeSummary()
}

func (r batchExportReport) completedEntry(rawURL string) (batchExportEntry, bool) {
	for _, entry := range r.Entries {
		if entry.URL != rawURL {
			continue
		}
		if entry.Status == "success" || entry.Status == "skipped_existing" {
			return entry, true
		}
	}
	return batchExportEntry{}, false
}

func (r *batchExportReport) recomputeSummary() {
	r.Summary = batchExportSummary{Total: len(r.Entries)}
	for _, entry := range r.Entries {
		switch entry.Status {
		case "success":
			r.Summary.Success++
		case "failed":
			r.Summary.Failed++
		case "skipped_existing":
			r.Summary.Skipped++
		}
	}
}

func timeNowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func qualifyWarnings(conv Conversation, warnings []warningRecord) []warningRecord {
	if len(warnings) == 0 {
		return nil
	}

	out := make([]warningRecord, 0, len(warnings))
	title := conv.Title
	if strings.TrimSpace(title) == "" {
		title = conv.ID
	}
	for _, warning := range warnings {
		out = append(out, warningRecord{
			Code:    warning.Code,
			Message: fmt.Sprintf("Conversation %q: %s", title, warning.Message),
		})
	}
	return out
}

func readProjectURLList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open URL list %q: %w", path, err)
	}
	defer file.Close()

	out := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read URL list %q: %w", path, err)
	}
	return out, nil
}

func legacyProjectURLDir(cfg config.Config, urlInfo ProjectURLInfo, currentProjectDir string) string {
	if strings.TrimSpace(cfg.Source.Project) != "" || strings.TrimSpace(urlInfo.GPTSlug) == "" {
		return ""
	}
	legacyDir := filepath.Join(cfg.Output.Dir, slugify(urlInfo.GPTSlug))
	if legacyDir == currentProjectDir {
		return ""
	}
	return legacyDir
}
