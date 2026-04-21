package exporters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
)

type warningRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var projectFetcherFactory = NewProjectFetcher

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
		rendered, renderWarnings := renderConversationMarkdown(conv)
		warnings = append(warnings, qualifyWarnings(conv, renderWarnings)...)
		filename := fmt.Sprintf("%03d_%s.md", i+1, slugify(conversationFileBase(conv.Title, i+1)))
		if err := os.WriteFile(filepath.Join(projectDir, filename), []byte(rendered), 0o644); err != nil {
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
	result, err := exportProjectURL(cfg, fetcher, cfg.Source.ProjectURL)
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
	allWarnings := make([]warningRecord, 0, len(urls))
	exportedCount := 0
	placeholderCount := 0

	for i, rawURL := range urls {
		result, err := exportProjectURL(cfg, fetcher, rawURL)
		if err != nil {
			allWarnings = append(allWarnings, warningRecord{
				Code:    "source.project_url_list.invalid_url",
				Message: fmt.Sprintf("Line %d URL %q could not be exported: %v", i+1, rawURL, err),
			})
			continue
		}
		allWarnings = append(allWarnings, result.warnings...)
		if result.placeholder {
			placeholderCount++
		} else {
			exportedCount += result.conversationCount
		}
	}

	if cfg.Options.WriteReadme {
		if err := writeOutputReadme(filepath.Join(cfg.Output.Dir, "README.md"), cfg, fmt.Sprintf("%d project URL(s)", len(urls)), exportedCount, placeholderCount > 0); err != nil {
			return err
		}
	}

	if cfg.Options.WriteWarnings {
		allWarnings = append(allWarnings, warningRecord{
			Code:    "source.project_url_list.completed",
			Message: fmt.Sprintf("Processed %d project URL(s): %d exported, %d placeholder result(s).", len(urls), exportedCount, placeholderCount),
		})
		if err := writeWarnings(filepath.Join(cfg.Output.Dir, "warnings.json"), allWarnings); err != nil {
			return err
		}
	}

	return nil
}

type projectURLExportResult struct {
	projectName       string
	conversationCount int
	placeholder       bool
	warnings          []warningRecord
}

func exportProjectURL(cfg config.Config, fetcher ProjectFetcher, rawURL string) (projectURLExportResult, error) {
	urlInfo, err := parseProjectURL(rawURL)
	if err != nil {
		return projectURLExportResult{}, err
	}

	fetched, fetchErr := fetcher.FetchConversation(context.Background(), urlInfo)

	projectName := chooseProjectName(cfg, nil)
	if cfg.Source.Project == "" && urlInfo.GPTSlug != "" {
		projectName = urlInfo.GPTSlug
	}
	if cfg.Source.Project == "" && strings.TrimSpace(fetched.ProjectName) != "" {
		projectName = fetched.ProjectName
	}
	projectDir := filepath.Join(cfg.Output.Dir, slugify(projectName))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return projectURLExportResult{}, fmt.Errorf("create project directory %q: %w", projectDir, err)
	}

	warnings := []warningRecord{
		{
			Code:    "source.project_url.parsed",
			Message: fmt.Sprintf("Recognized URL host=%q path_type=%q conversation_id=%q.", urlInfo.Host, urlInfo.PathType, emptyFallback(urlInfo.ConversationID)),
		},
	}
	placeholder := true
	conversationCount := 1

	if fetchErr == nil && len(fetched.Messages) > 0 {
		placeholder = false
		conv := conversationFromFetched(urlInfo, fetched)
		warnings = append(warnings, fetched.Warnings...)
		rendered, renderWarnings := renderConversationMarkdown(conv)
		warnings = append(warnings, qualifyWarnings(conv, renderWarnings)...)
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.fetch_success",
			Message: fmt.Sprintf("Project URL fetch returned %d message(s) and was rendered into Markdown.", len(fetched.Messages)),
		})

		filename := fmt.Sprintf("%03d_%s.md", 1, slugify(conversationFileBase(conv.Title, 1)))
		if err := os.WriteFile(filepath.Join(projectDir, filename), []byte(rendered), 0o644); err != nil {
			return projectURLExportResult{}, fmt.Errorf("write conversation markdown %q: %w", filename, err)
		}
		placeholderPath := filepath.Join(projectDir, "001_placeholder.md")
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
	} else {
		if err := writePlaceholderConversation(filepath.Join(projectDir, "001_placeholder.md"), cfg, projectName, urlInfo, fetchErr, rawURL); err != nil {
			return projectURLExportResult{}, err
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
	}
	if fetchErr == nil && len(fetched.Messages) == 0 {
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.empty_result",
			Message: "Project URL fetch returned no messages. A placeholder file was generated instead.",
		})
	}

	return projectURLExportResult{
		projectName:       projectName,
		conversationCount: conversationCount,
		placeholder:       placeholder,
		warnings:          warnings,
	}, nil
}

func conversationFromFetched(urlInfo ProjectURLInfo, fetched FetchedConversation) Conversation {
	title := firstNonEmpty(fetched.ProjectName, urlInfo.GPTSlug, "chatgpt-project")
	return Conversation{
		ID:       urlInfo.ConversationID,
		Title:    title,
		Messages: fetched.Messages,
	}
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
