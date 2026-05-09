package exporters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
)

const sessionCookieEnv = "CGME_CHATGPT_SESSION_COOKIE"

type ProjectFetcher interface {
	FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error)
}

var sessionCookieOnlineValidator = validateSessionCookieOnline

type FetchedConversation struct {
	ProjectName string
	Title       string
	Messages    []Message
	Warnings    []warningRecord
}

type ChatGPTProjectFetcher struct {
	client        *http.Client
	sessionCookie string
	baseURL       string
}

type ProjectFetchError struct {
	Code    string
	Message string
}

func (e *ProjectFetchError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ProjectFetchError) Warning() warningRecord {
	return warningRecord{
		Code:    e.Code,
		Message: e.Message,
	}
}

func NewProjectFetcher(cfg config.Config) ProjectFetcher {
	sessionCookie, err := resolveSessionCookie(cfg)
	if err != nil {
		return CompositeProjectFetcher{fetchers: []ProjectFetcher{
			errorProjectFetcher{err: err},
		}}
	}

	fetchers := make([]ProjectFetcher, 0, 2)
	if browserFetcher, ok := newBrowserProjectFetcher(sessionCookie); ok {
		fetchers = append(fetchers, browserFetcher)
	}
	if sessionCookie != "" || len(fetchers) == 0 {
		fetchers = append(fetchers, &ChatGPTProjectFetcher{
			client: &http.Client{
				Timeout: 20 * time.Second,
			},
			sessionCookie: sessionCookie,
			baseURL:       "https://chatgpt.com",
		})
	}
	return CompositeProjectFetcher{fetchers: fetchers}
}

type errorProjectFetcher struct {
	err error
}

func (f errorProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	return FetchedConversation{}, f.err
}

func (f *ChatGPTProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if f.sessionCookie == "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.session_cookie_missing",
			Message: fmt.Sprintf("Set %s to a valid ChatGPT session cookie before project URL export can fetch live data.", sessionCookieEnv),
		}
	}

	if info.ConversationID == "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.conversation_missing",
			Message: "The project URL does not contain a conversation identifier.",
		}
	}

	req, err := f.newConversationRequest(ctx, info)
	if err != nil {
		return FetchedConversation{}, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.request_failed",
			Message: fmt.Sprintf("Conversation request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	return f.decodeConversationResponse(resp)
}

func (f *ChatGPTProjectFetcher) newConversationRequest(ctx context.Context, info ProjectURLInfo) (*http.Request, error) {
	url := strings.TrimRight(f.baseURL, "/") + "/backend-api/conversation/" + info.ConversationID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &ProjectFetchError{
			Code:    "source.project_url.request_invalid",
			Message: fmt.Sprintf("Failed to build conversation request: %v", err),
		}
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", f.sessionCookie)
	req.Header.Set("User-Agent", "CGME/0.1")

	return req, nil
}

func (f *ChatGPTProjectFetcher) decodeConversationResponse(resp *http.Response) (FetchedConversation, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.response_read_failed",
			Message: fmt.Sprintf("Failed to read conversation response: %v", err),
		}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if isCloudflareChallenge(resp, body) {
			return FetchedConversation{}, &ProjectFetchError{
				Code:    "source.project_url.cloudflare_challenge",
				Message: "ChatGPT returned a Cloudflare challenge page instead of conversation JSON. A browser-backed fetch path is likely required.",
			}
		}
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.auth_failed",
			Message: "ChatGPT rejected the session cookie for the conversation request.",
		}
	}
	if resp.StatusCode != http.StatusOK {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.bad_status",
			Message: fmt.Sprintf("Conversation request returned HTTP %d.", resp.StatusCode),
		}
	}

	var payload rawConversation
	if err := json.Unmarshal(body, &payload); err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.response_invalid_json",
			Message: fmt.Sprintf("Conversation response was not valid JSON: %v", err),
		}
	}

	conv, warnings := convertConversation(payload)
	if len(warnings) > 0 {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.fetch_partial",
			Message: fmt.Sprintf("Conversation JSON decoded, but %d message(s) could not be fully rendered yet.", len(warnings)),
		}
	}
	if len(conv.Messages) == 0 {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.empty_result",
			Message: "Conversation JSON decoded, but no exportable text messages were found.",
		}
	}

	projectName, title := splitChatGPTProjectAndTitle(conv.Title)
	return FetchedConversation{
		ProjectName: firstNonEmpty(projectName, conv.Title, "chatgpt-project"),
		Title:       firstNonEmpty(title, conv.Title, "Untitled Conversation"),
		Messages:    conv.Messages,
	}, nil
}

func warningFromError(err error) (warningRecord, bool) {
	var fetchErr *ProjectFetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.Warning(), true
	}
	return warningRecord{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isCloudflareChallenge(resp *http.Response, body []byte) bool {
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("cf-mitigated")), "challenge") {
		return true
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") {
		lowerBody := strings.ToLower(string(body))
		if strings.Contains(lowerBody, "cloudflare") || strings.Contains(lowerBody, "<html") {
			return true
		}
	}
	return false
}

func resolveSessionCookie(cfg config.Config) (string, error) {
	if strings.TrimSpace(cfg.Source.CookieFile) != "" {
		data, err := os.ReadFile(cfg.Source.CookieFile)
		if err != nil {
			return "", &ProjectFetchError{
				Code:    "source.project_url.cookie_file_unreadable",
				Message: fmt.Sprintf("Failed to read cookie file %q: %v", cfg.Source.CookieFile, err),
			}
		}
		cookie := strings.TrimSpace(string(data))
		if cookie == "" {
			return "", &ProjectFetchError{
				Code:    "source.project_url.cookie_file_empty",
				Message: fmt.Sprintf("Cookie file %q is empty.", cfg.Source.CookieFile),
			}
		}
		if err := validateSessionCookieHeader(cookie); err != nil {
			return "", err
		}
		return cookie, nil
	}
	cookie := strings.TrimSpace(os.Getenv(sessionCookieEnv))
	if cookie == "" {
		return "", nil
	}
	if err := validateSessionCookieHeader(cookie); err != nil {
		return "", err
	}
	return cookie, nil
}

func validateProjectSessionCookie(cfg config.Config) error {
	if cfg.Source.Type != "project_url" && cfg.Source.Type != "project_url_list" {
		return nil
	}
	cookie, err := resolveSessionCookie(cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cookie) == "" {
		return &ProjectFetchError{
			Code:    "source.project_url.session_cookie_missing",
			Message: fmt.Sprintf("Set %s or --cookie-file to a valid ChatGPT session cookie before live project export.", sessionCookieEnv),
		}
	}

	info, ok, err := cookieValidationConversation(cfg)
	if err != nil {
		return err
	}
	if ok {
		return sessionCookieOnlineValidator(context.Background(), cookie, info)
	}
	return nil
}

func cookieValidationConversation(cfg config.Config) (ProjectURLInfo, bool, error) {
	switch cfg.Source.Type {
	case "project_url":
		info, err := parseProjectURL(cfg.Source.ProjectURL)
		if err != nil {
			return ProjectURLInfo{}, false, err
		}
		return info, info.ConversationID != "", nil
	case "project_url_list":
		urls, err := readProjectURLList(cfg.Source.URLList)
		if err != nil {
			return ProjectURLInfo{}, false, err
		}
		if len(urls) == 0 {
			return ProjectURLInfo{}, false, nil
		}
		info, err := parseProjectURL(urls[0])
		if err != nil {
			return ProjectURLInfo{}, false, err
		}
		return info, info.ConversationID != "", nil
	default:
		return ProjectURLInfo{}, false, nil
	}
}

func validateSessionCookieOnline(ctx context.Context, cookie string, info ProjectURLInfo) error {
	if strings.TrimSpace(info.ConversationID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fetcher := &ChatGPTProjectFetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		sessionCookie: cookie,
		baseURL:       "https://chatgpt.com",
	}
	req, err := fetcher.newConversationRequest(ctx, info)
	if err != nil {
		return err
	}
	resp, err := fetcher.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if isCloudflareChallenge(resp, body) {
			return nil
		}
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_auth_failed",
			Message: "ChatGPT rejected the session cookie during preflight validation. Refresh the cookie and rerun.",
		}
	}
	return nil
}

func validateSessionCookieHeader(cookie string) error {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_empty",
			Message: "ChatGPT session cookie is empty.",
		}
	}
	if strings.ContainsAny(cookie, "\r\n") {
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_invalid",
			Message: "ChatGPT session cookie must be a single-line Cookie header.",
		}
	}

	hasPair := false
	hasChatGPTSession := false
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			return &ProjectFetchError{
				Code:    "source.project_url.cookie_invalid",
				Message: fmt.Sprintf("Cookie entry %q is not in name=value form.", part),
			}
		}
		name := strings.TrimSpace(part[:eq])
		value := strings.TrimSpace(part[eq+1:])
		if name == "" || value == "" || strings.ContainsAny(name, " \t") {
			return &ProjectFetchError{
				Code:    "source.project_url.cookie_invalid",
				Message: fmt.Sprintf("Cookie entry %q is not a valid name=value pair.", part),
			}
		}
		hasPair = true
		if isChatGPTSessionCookieName(name) {
			hasChatGPTSession = true
		}
	}
	if !hasPair {
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_invalid",
			Message: "ChatGPT session cookie does not contain any name=value entries.",
		}
	}
	if !hasChatGPTSession {
		return &ProjectFetchError{
			Code:    "source.project_url.cookie_invalid",
			Message: "Cookie header does not contain a known ChatGPT session token cookie.",
		}
	}
	return nil
}

func isChatGPTSessionCookieName(name string) bool {
	return name == "__Secure-next-auth.session-token" ||
		strings.HasPrefix(name, "__Secure-next-auth.session-token.") ||
		name == "oai-auth-token"
}
