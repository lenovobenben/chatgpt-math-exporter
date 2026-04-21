package exporters

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type discoveredConversationLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type projectPageDiscoverer interface {
	DiscoverProjectPageURLs(ctx context.Context, pageURL string) ([]discoveredConversationLink, error)
}

var browserProjectFetcherFactory = newBrowserProjectFetcher

func DiscoverProjectPageURLs(projectPageURL, cookieFile, outputList string) error {
	if err := validateDiscoveryURL(projectPageURL); err != nil {
		return err
	}

	cookie := ""
	if strings.TrimSpace(cookieFile) != "" {
		data, err := os.ReadFile(cookieFile)
		if err != nil {
			return fmt.Errorf("read cookie file %q: %w", cookieFile, err)
		}
		cookie = strings.TrimSpace(string(data))
		if cookie == "" {
			return fmt.Errorf("cookie file %q is empty", cookieFile)
		}
	}

	fetcher, ok := browserProjectFetcherFactory(cookie)
	if !ok {
		return fmt.Errorf("browser-backed discovery is not available in the current environment")
	}

	browserFetcher, ok := fetcher.(projectPageDiscoverer)
	if !ok {
		return fmt.Errorf("browser-backed discovery requires the Chrome CDP fetcher")
	}

	links, err := browserFetcher.DiscoverProjectPageURLs(context.Background(), projectPageURL)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return fmt.Errorf("no conversation URLs were discovered from %q", projectPageURL)
	}

	links = filterDiscoveredLinks(projectPageURL, links)
	if len(links) == 0 {
		return fmt.Errorf("no project-scoped conversation URLs were discovered from %q", projectPageURL)
	}

	lines := make([]string, 0, len(links))
	for _, link := range links {
		lines = append(lines, link.URL)
	}

	if err := os.MkdirAll(filepath.Dir(outputList), 0o755); err != nil {
		return fmt.Errorf("create output list directory: %w", err)
	}
	if err := os.WriteFile(outputList, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write output list %q: %w", outputList, err)
	}
	return nil
}

func validateDiscoveryURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse project page URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("project page URL must use https")
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("project page URL host is required")
	}
	return nil
}

func filterDiscoveredLinks(projectPageURL string, links []discoveredConversationLink) []discoveredConversationLink {
	u, err := url.Parse(strings.TrimSpace(projectPageURL))
	if err != nil {
		return links
	}

	parts := splitURLPath(u.Path)
	if len(parts) < 2 || parts[0] != "g" {
		return links
	}
	prefix := fmt.Sprintf("https://%s/g/%s/c/", u.Host, parts[1])
	filtered := make([]discoveredConversationLink, 0, len(links))
	for _, link := range links {
		if strings.HasPrefix(link.URL, prefix) {
			filtered = append(filtered, link)
		}
	}
	return filtered
}
