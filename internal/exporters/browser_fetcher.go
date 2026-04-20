package exporters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const chromeAppPath = "/Applications/Google Chrome.app"
const chromeDebugPortEnv = "CGME_CHROME_DEBUG_PORT"
const browserWaitEnv = "CGME_BROWSER_WAIT_SECONDS"

type CDPBrowserProjectFetcher struct {
	chromePath   string
	waitAfter    time.Duration
	cookieHeader string
}

type CompositeProjectFetcher struct {
	fetchers []ProjectFetcher
}

type browserConversationPayload struct {
	Title    string                   `json:"title"`
	URL      string                   `json:"url"`
	Snippet  string                   `json:"snippet"`
	Messages []browserConversationMsg `json:"messages"`
	Error    string                   `json:"error"`
}

type browserConversationMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c CompositeProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if len(c.fetchers) == 1 {
		return c.fetchers[0].FetchConversation(ctx, info)
	}

	var errs []string
	for _, fetcher := range c.fetchers {
		fetched, err := fetcher.FetchConversation(ctx, info)
		if err == nil {
			return fetched, nil
		}
		errs = append(errs, err.Error())
	}

	if len(errs) == 0 {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.no_fetcher",
			Message: "No project URL fetcher is available in the current environment.",
		}
	}

	return FetchedConversation{}, &ProjectFetchError{
		Code:    "source.project_url.fetch_chain_failed",
		Message: strings.Join(errs, " | "),
	}
}

func newBrowserProjectFetcher() (ProjectFetcher, bool) {
	if runtime.GOOS != "darwin" {
		return nil, false
	}
	if _, err := os.Stat(chromeAppPath); err != nil {
		return nil, false
	}
	if _, err := exec.LookPath("node"); err != nil {
		return nil, false
	}
	cookieHeader := strings.TrimSpace(os.Getenv(sessionCookieEnv))
	if cookieHeader == "" {
		return nil, false
	}

	return &CDPBrowserProjectFetcher{
		chromePath:   chromeAppPath,
		waitAfter:    browserWaitDuration(),
		cookieHeader: cookieHeader,
	}, true
}

func (f *CDPBrowserProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if info.ConversationID == "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.conversation_missing",
			Message: "The project URL does not contain a conversation identifier.",
		}
	}
	if strings.TrimSpace(f.cookieHeader) == "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.session_cookie_missing",
			Message: fmt.Sprintf("Set %s to a valid ChatGPT session cookie before project URL export can fetch live data.", sessionCookieEnv),
		}
	}

	profileRoot, err := createTempBrowserProfileRoot()
	if err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.browser_profile_prepare_failed",
			Message: fmt.Sprintf("Failed to prepare temporary browser profile: %v", err),
		}
	}
	defer os.RemoveAll(profileRoot)

	chromeCmd, port, err := launchChromeWithDebugging(ctx, f.chromePath, profileRoot)
	if err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.browser_launch_failed",
			Message: fmt.Sprintf("Failed to launch Chrome with DevTools enabled: %v", err),
		}
	}
	defer func() {
		if chromeCmd.Process != nil {
			_ = chromeCmd.Process.Kill()
		}
	}()

	payload, err := runCDPDOMExtraction(ctx, port, buildConversationPageURL(info), f.cookieHeader, f.waitAfter)
	if err != nil {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.browser_cdp_failed",
			Message: err.Error(),
		}
	}
	if payload.Error != "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.browser_dom_failed",
			Message: payload.Error,
		}
	}

	messages := make([]Message, 0, len(payload.Messages))
	for _, msg := range payload.Messages {
		role := strings.TrimSpace(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		messages = append(messages, Message{Role: role, Content: content})
	}
	messages, warnings := normalizeBrowserMessages(messages)
	if len(messages) == 0 {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.browser_dom_empty",
			Message: browserDOMEmptyMessage(payload),
		}
	}

	return FetchedConversation{
		ProjectName: firstNonEmpty(payload.Title, info.GPTSlug, "chatgpt-project"),
		Messages:    messages,
		Warnings:    warnings,
	}, nil
}

func createTempBrowserProfileRoot() (string, error) {
	root, err := os.MkdirTemp("", "cgme-chrome-profile-*")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "Default"), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(`{"profile":{"last_used":"Default"}}`), 0o600); err != nil {
		return "", err
	}
	return root, nil
}

func launchChromeWithDebugging(ctx context.Context, chromePath, profileRoot string) (*exec.Cmd, int, error) {
	port := chromeDebugPort()
	cmd := exec.CommandContext(ctx, filepath.Join(chromePath, "Contents", "MacOS", "Google Chrome"),
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+profileRoot,
		"--profile-directory=Default",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}
	if err := waitForCDPReady(ctx, port, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, 0, err
	}
	return cmd, port, nil
}

func waitForCDPReady(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Chrome DevTools on port %d", port)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func runCDPDOMExtraction(ctx context.Context, port int, pageURL, cookieHeader string, waitAfter time.Duration) (browserConversationPayload, error) {
	waitMS := int(waitAfter / time.Millisecond)
	if waitMS < 1000 {
		waitMS = 1000
	}

	expression := `(() => {
  const getKatexTex = (node) => {
    const annotation = node.querySelector('annotation[encoding="application/x-tex"]');
    return annotation ? (annotation.textContent || '').trim() : '';
  };
  const serializeNode = (node) => {
    const clone = node.cloneNode(true);
    const fence = String.fromCharCode(96, 96, 96);
    clone.querySelectorAll('.katex-display').forEach((el) => {
      const tex = getKatexTex(el);
      const replacement = document.createTextNode(tex ? "\n" + fence + "math\n" + tex + "\n" + fence + "\n" : "\n");
      el.replaceWith(replacement);
    });
    clone.querySelectorAll('.katex').forEach((el) => {
      if (el.closest('.katex-display')) return;
      const tex = getKatexTex(el);
      const replacement = document.createTextNode(tex ? "$" + tex + "$" : (el.textContent || ''));
      el.replaceWith(replacement);
    });
    clone.querySelectorAll('button, svg, script, style').forEach((el) => el.remove());
    return (clone.innerText || '').trim();
  };
  const roleNodes = Array.from(document.querySelectorAll('[data-message-author-role]'));
  const messages = roleNodes
    .map(el => ({
      role: el.getAttribute('data-message-author-role') || '',
      content: serializeNode(el)
    }))
    .filter(item => item.role && item.content);
  return JSON.stringify({
    title: document.title.replace(/\s*-\s*ChatGPT$/, ''),
    url: location.href,
    snippet: (document.body?.innerText || '').trim().slice(0, 240),
    messages,
    error: ''
  });
})()`

	script := fmt.Sprintf(`
const targetUrl = %q;
const cookieHeader = %q;
const waitMS = %d;
async function connectWS(url) {
  const ws = new WebSocket(url);
  await new Promise((resolve, reject) => {
    ws.addEventListener('open', resolve, { once: true });
    ws.addEventListener('error', reject, { once: true });
  });
  return ws;
}
(async () => {
  const newTarget = await fetch('http://127.0.0.1:%d/json/new?' + encodeURIComponent('about:blank'), { method: 'PUT' }).then(r => r.json());
  const ws = await connectWS(newTarget.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const msgId = ++id;
    pending.set(msgId, { resolve, reject });
    ws.send(JSON.stringify({ id: msgId, method, params }));
  });
  ws.addEventListener('message', event => {
    const msg = JSON.parse(event.data);
    if (msg.id && pending.has(msg.id)) {
      const pair = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) pair.reject(new Error(JSON.stringify(msg.error)));
      else pair.resolve(msg.result);
    }
  });
  await send('Page.enable');
  await send('Runtime.enable');
  await send('Network.enable');
  for (const pair of cookieHeader.split(/;\s*/)) {
    const eq = pair.indexOf('=');
    if (eq <= 0) continue;
    await send('Network.setCookie', { url: 'https://chatgpt.com/', name: pair.slice(0, eq), value: pair.slice(eq + 1), secure: true }).catch(() => {});
  }
  await send('Page.navigate', { url: targetUrl });
  await new Promise(r => setTimeout(r, waitMS));
  const result = await send('Runtime.evaluate', {
    expression: %q,
    returnByValue: true,
  });
  console.log(result.result.value);
  ws.close();
})().catch(err => {
  console.log(JSON.stringify({ title: '', url: '', snippet: '', messages: [], error: String(err) }));
  process.exit(0);
});
`, pageURL, cookieHeader, waitMS, port, expression)

	cmd := exec.CommandContext(ctx, "node", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return browserConversationPayload{}, fmt.Errorf("node cdp extraction failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return parseBrowserConversationPayload(strings.TrimSpace(string(out)))
}

func parseBrowserConversationPayload(output string) (browserConversationPayload, error) {
	var payload browserConversationPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return browserConversationPayload{}, err
	}
	return payload, nil
}

func buildConversationPageURL(info ProjectURLInfo) string {
	if info.Host == "" {
		info.Host = "chatgpt.com"
	}
	if info.PathType == "gpt_conversation" && info.GPTID != "" {
		return fmt.Sprintf("https://%s/g/%s/c/%s", info.Host, info.GPTID, info.ConversationID)
	}
	return fmt.Sprintf("https://%s/c/%s", info.Host, info.ConversationID)
}

func browserDOMEmptyMessage(payload browserConversationPayload) string {
	parts := []string{
		"Browser automation loaded the page, but no structured messages were found in the DOM.",
	}
	if payload.Title != "" {
		parts = append(parts, fmt.Sprintf("title=%q", payload.Title))
	}
	if payload.URL != "" {
		parts = append(parts, fmt.Sprintf("url=%q", payload.URL))
	}
	if payload.Snippet != "" {
		parts = append(parts, fmt.Sprintf("snippet=%q", payload.Snippet))
	}
	return strings.Join(parts, " ")
}

func normalizeBrowserMessages(messages []Message) ([]Message, []warningRecord) {
	out := make([]Message, 0, len(messages))
	var dedupedCount int
	var filteredNoiseCount int
	var mergedCount int
	for _, msg := range messages {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		content := normalizeBrowserMessageContent(msg.Content)
		if role == "" || content == "" {
			continue
		}
		if isBrowserNoiseContent(content) {
			filteredNoiseCount++
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == role && out[len(out)-1].Content == content {
			dedupedCount++
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = strings.TrimSpace(out[len(out)-1].Content + "\n\n" + content)
			mergedCount++
			continue
		}
		out = append(out, Message{Role: role, Content: content})
	}
	warnings := make([]warningRecord, 0, 2)
	if filteredNoiseCount > 0 {
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.browser_noise_filtered",
			Message: fmt.Sprintf("Filtered %d browser UI noise message(s) from DOM extraction.", filteredNoiseCount),
		})
	}
	if dedupedCount > 0 {
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.browser_message_deduped",
			Message: fmt.Sprintf("Dropped %d consecutive duplicate DOM message(s) during browser extraction.", dedupedCount),
		})
	}
	if mergedCount > 0 {
		warnings = append(warnings, warningRecord{
			Code:    "source.project_url.browser_message_merged",
			Message: fmt.Sprintf("Merged %d consecutive same-role DOM message(s) during browser extraction.", mergedCount),
		})
	}
	return out, warnings
}

func normalizeBrowserMessageContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, len(lines))
	lastBlank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		blank := strings.TrimSpace(line) == ""
		if blank {
			if lastBlank {
				continue
			}
			lastBlank = true
			normalized = append(normalized, "")
			continue
		}
		lastBlank = false
		normalized = append(normalized, line)
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func isBrowserNoiseContent(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return true
	}
	noise := []string{
		"chatgpt can make mistakes. check important info.",
		"chatgpt 也可能会犯错。请核查重要信息。",
		"you said:",
		"chatgpt said:",
	}
	for _, item := range noise {
		if lower == item {
			return true
		}
	}
	return false
}

func browserWaitDuration() time.Duration {
	raw := strings.TrimSpace(os.Getenv(browserWaitEnv))
	if raw == "" {
		return 8 * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 {
		return 8 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func chromeDebugPort() int {
	raw := strings.TrimSpace(os.Getenv(chromeDebugPortEnv))
	if raw == "" {
		return 9223
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1024 || port > 65535 {
		return 9223
	}
	return port
}
