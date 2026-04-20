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

type FetchedConversation struct {
	ProjectName string
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
	fetchers := make([]ProjectFetcher, 0, 2)
	if browserFetcher, ok := newBrowserProjectFetcher(); ok {
		fetchers = append(fetchers, browserFetcher)
	}
	sessionCookie := strings.TrimSpace(os.Getenv(sessionCookieEnv))
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

	return FetchedConversation{
		ProjectName: firstNonEmpty(conv.Title, "chatgpt-project"),
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
