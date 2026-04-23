package exporters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	rendermarkdown "github.com/lihd/chatgpt-math-exporter/internal/render/markdown"
)

const chromeAppPath = "/Applications/Google Chrome.app"
const chromeDebugPortEnv = "CGME_CHROME_DEBUG_PORT"
const browserWaitEnv = "CGME_BROWSER_WAIT_SECONDS"
const browserProfileRootEnv = "CGME_CHROME_PROFILE_ROOT"

var osStat = os.Stat
var execLookPath = exec.LookPath
var cdpReady = isCDPReady
var ensureBrowserProfile = ensureBrowserProfileRoot
var ensureChromeSession = ensureChromeDebuggingSession
var runCDPExtraction = runCDPDOMExtraction

type CDPBrowserProjectFetcher struct {
	chromePath    string
	waitAfter     time.Duration
	cookieHeader  string
	profileRoot   string
	debugPort     int
	bootAttempted bool
	sessionReady  bool
	launchErr     error
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

type browserDiscoveryPayload struct {
	Title string                       `json:"title"`
	URL   string                       `json:"url"`
	Links []discoveredConversationLink `json:"links"`
	Error string                       `json:"error"`
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

func newBrowserProjectFetcher(cookieHeader string) (ProjectFetcher, bool) {
	if runtime.GOOS != "darwin" {
		return nil, false
	}
	if _, err := osStat(chromeAppPath); err != nil {
		return nil, false
	}
	if _, err := execLookPath("node"); err != nil {
		return nil, false
	}
	cookieHeader = strings.TrimSpace(cookieHeader)
	port := chromeDebugPort()
	if cookieHeader == "" && !cdpReady(context.Background(), port) {
		return nil, false
	}

	return &CDPBrowserProjectFetcher{
		chromePath:   chromeAppPath,
		waitAfter:    browserWaitDuration(),
		cookieHeader: cookieHeader,
		profileRoot:  browserProfileRoot(),
		debugPort:    port,
	}, true
}

func (f *CDPBrowserProjectFetcher) FetchConversation(ctx context.Context, info ProjectURLInfo) (FetchedConversation, error) {
	if info.ConversationID == "" {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.conversation_missing",
			Message: "The project URL does not contain a conversation identifier.",
		}
	}
	if strings.TrimSpace(f.cookieHeader) == "" && !cdpReady(ctx, f.debugPort) {
		return FetchedConversation{}, &ProjectFetchError{
			Code:    "source.project_url.session_cookie_missing",
			Message: fmt.Sprintf("Set %s to a valid ChatGPT session cookie before the first project URL export, or keep the CGME Chrome session running for reuse.", sessionCookieEnv),
		}
	}

	port, launched, err := f.ensureSession(ctx)
	if err != nil {
		return FetchedConversation{}, err
	}

	payload, err := runCDPExtraction(ctx, port, buildConversationPageURL(info), f.cookieHeader, f.waitAfter)
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

	projectName, title := splitChatGPTProjectAndTitle(payload.Title)
	return FetchedConversation{
		ProjectName: firstNonEmpty(projectName, info.GPTSlug, "chatgpt-project"),
		Title:       firstNonEmpty(title, payload.Title, "Untitled Conversation"),
		Messages:    messages,
		Warnings:    appendSessionWarnings(warnings, launched),
	}, nil
}

func (f *CDPBrowserProjectFetcher) DiscoverProjectPageURLs(ctx context.Context, pageURL string) ([]discoveredConversationLink, error) {
	if strings.TrimSpace(f.cookieHeader) == "" && !cdpReady(ctx, f.debugPort) {
		return nil, fmt.Errorf("set %s to a valid ChatGPT session cookie before the first discovery run, or reuse an existing CGME Chrome session", sessionCookieEnv)
	}

	port, _, err := f.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	payload, err := runCDPDiscovery(ctx, port, pageURL, buildProjectConversationPrefix(pageURL), f.cookieHeader, f.waitAfter)
	if err != nil {
		return nil, err
	}
	if payload.Error != "" {
		return nil, fmt.Errorf(payload.Error)
	}
	return payload.Links, nil
}

func (f *CDPBrowserProjectFetcher) ensureSession(ctx context.Context) (int, bool, error) {
	if f.sessionReady && cdpReady(ctx, f.debugPort) {
		return f.debugPort, false, nil
	}
	if f.sessionReady {
		if err := waitForCDPReady(ctx, f.debugPort, 2*time.Second); err == nil {
			return f.debugPort, false, nil
		}
	}
	if f.bootAttempted {
		if f.launchErr != nil {
			return 0, false, f.launchErr
		}
		return 0, false, &ProjectFetchError{
			Code:    "source.project_url.browser_session_lost",
			Message: "The reusable Chrome DevTools session was lost during this run. CGME will not relaunch Chrome automatically; keep one session alive and rerun.",
		}
	}

	f.bootAttempted = true
	if cdpReady(ctx, f.debugPort) {
		f.sessionReady = true
		return f.debugPort, false, nil
	}

	profileRoot, err := ensureBrowserProfile(f.profileRoot)
	if err != nil {
		f.launchErr = &ProjectFetchError{
			Code:    "source.project_url.browser_profile_prepare_failed",
			Message: fmt.Sprintf("Failed to prepare browser profile root: %v", err),
		}
		return 0, false, f.launchErr
	}
	port, launched, err := ensureChromeSession(ctx, f.chromePath, profileRoot, f.debugPort)
	if err != nil {
		f.launchErr = &ProjectFetchError{
			Code:    "source.project_url.browser_launch_failed",
			Message: fmt.Sprintf("Failed to launch Chrome with DevTools enabled: %v", err),
		}
		return 0, false, f.launchErr
	}
	f.sessionReady = true
	f.launchErr = nil
	return port, launched, nil
}

func ensureBrowserProfileRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = browserProfileRoot()
	}
	if err := os.MkdirAll(filepath.Join(root, "Default"), 0o700); err != nil {
		return "", err
	}
	localState := filepath.Join(root, "Local State")
	if _, err := os.Stat(localState); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.WriteFile(localState, []byte(`{"profile":{"last_used":"Default"}}`), 0o600); err != nil {
			return "", err
		}
	}
	return root, nil
}

func ensureChromeDebuggingSession(ctx context.Context, chromePath, profileRoot string, port int) (int, bool, error) {
	if cdpReady(ctx, port) {
		return port, false, nil
	}
	// Keep the debugging Chrome process independent from per-conversation timeout contexts.
	// Otherwise it gets terminated as soon as one URL fetch context is canceled.
	cmd := exec.Command(filepath.Join(chromePath, "Contents", "MacOS", "Google Chrome"),
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+profileRoot,
		"--profile-directory=Default",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-features=DialMediaRouteProvider,OptimizationHints,MediaRouter",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		return 0, false, err
	}
	if err := waitForCDPReady(ctx, port, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return 0, false, err
	}
	return port, true, nil
}

func waitForCDPReady(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Chrome DevTools on port %d", port)
		}
		if cdpReady(ctx, port) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func isCDPReady(ctx context.Context, port int) bool {
	client := &http.Client{Timeout: time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
  const fence = String.fromCharCode(96, 96, 96);
  const escapeCell = (text) => (text || '').replace(/\|/g, '\\|').replace(/\n/g, '<br>').trim();
  const serializeFragment = (root) => {
    const clone = root.cloneNode(true);
    clone.querySelectorAll('pre').forEach((el) => {
      const code = el.querySelector('code');
      const text = ((code || el).innerText || '').replace(/\u00a0/g, ' ').trimEnd();
      const className = (code && code.getAttribute('class')) || el.getAttribute('data-language') || '';
      const match = className.match(/language-([a-z0-9_+-]+)/i);
      const language = match ? match[1] : '';
      const fenceText = "\n" + fence + language + "\n" + text + "\n" + fence + "\n";
      el.replaceWith(document.createTextNode(fenceText));
    });
    clone.querySelectorAll('table').forEach((el) => {
      const markdown = tableToMarkdown(el);
      const replacement = document.createTextNode(markdown ? "\n" + markdown + "\n" : "\n");
      el.replaceWith(replacement);
    });
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
    clone.querySelectorAll('img').forEach((el) => {
      const src = (el.currentSrc || el.getAttribute('src') || '').trim();
      if (!src) {
        el.remove();
        return;
      }
      const alt = (el.getAttribute('alt') || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim();
      const marker = "[[CGME_IMAGE:" + JSON.stringify({ src, alt }) + "]]";
      el.replaceWith(document.createTextNode("\n" + marker + "\n"));
    });
    clone.querySelectorAll('picture, source').forEach((el) => {
      if (el.querySelector && el.querySelector('img')) return;
      el.remove();
    });
    clone.querySelectorAll('button').forEach((el) => {
      const text = (el.textContent || '').trim();
      if (text.includes('[[CGME_IMAGE:')) {
        el.replaceWith(document.createTextNode("\n" + text + "\n"));
        return;
      }
      el.remove();
    });
    clone.querySelectorAll('h1,h2,h3,h4,h5,h6').forEach((el) => {
      const text = (el.innerText || el.textContent || '').trim();
      if (!text) {
        el.remove();
        return;
      }
      const rawLevel = Number((el.tagName || 'H3').slice(1)) || 3;
      const level = Math.min(rawLevel + 2, 6);
      el.replaceWith(document.createTextNode("\n\n" + "#".repeat(level) + " " + text + "\n\n"));
    });
    clone.querySelectorAll('p').forEach((el) => {
      const text = (el.innerText || el.textContent || '').trim();
      el.replaceWith(document.createTextNode(text ? "\n\n" + text + "\n\n" : "\n\n"));
    });
    clone.querySelectorAll('svg, script, style').forEach((el) => el.remove());
    return (clone.innerText || '').trim();
  };
  const tableToMarkdown = (table) => {
    const rows = Array.from(table.querySelectorAll('tr'))
      .map((tr) => Array.from(tr.querySelectorAll('th,td')).map((cell) => escapeCell(serializeFragment(cell))))
      .filter((row) => row.length > 0);
    if (!rows.length) return '';
    const headers = rows[0];
    const body = rows.slice(1);
    const lines = [];
    lines.push('| ' + headers.join(' | ') + ' |');
    lines.push('| ' + headers.map(() => '---').join(' | ') + ' |');
    body.forEach((row) => {
      const cells = row.slice();
      while (cells.length < headers.length) cells.push('');
      if (cells.length > headers.length) cells.length = headers.length;
      lines.push('| ' + cells.join(' | ') + ' |');
    });
    return lines.join('\n');
  };
  const serializeNode = (node) => {
    return serializeFragment(node);
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
  const targets = await fetch('http://127.0.0.1:%d/json/list').then(r => r.json());
  let target = targets.find(t => t.type === 'page' && (t.url === 'about:blank' || (t.url || '').startsWith('https://chatgpt.com/')));
  if (!target) {
    target = await fetch('http://127.0.0.1:%d/json/new?' + encodeURIComponent('about:blank'), { method: 'PUT' }).then(r => r.json());
  }
  const ws = await connectWS(target.webSocketDebuggerUrl);
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
`, pageURL, cookieHeader, waitMS, port, port, expression)

	cmd := exec.CommandContext(ctx, "node", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return browserConversationPayload{}, fmt.Errorf("node cdp extraction failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return parseBrowserConversationPayload(strings.TrimSpace(string(out)))
}

func runCDPDiscovery(ctx context.Context, port int, pageURL, projectPrefix, cookieHeader string, waitAfter time.Duration) (browserDiscoveryPayload, error) {
	waitMS := int(waitAfter / time.Millisecond)
	if waitMS < 1500 {
		waitMS = 1500
	}

	expression := `(() => {
  const projectPrefix = %q;
  const normalizeHref = (href) => {
    try {
      return new URL(href, location.origin).toString();
    } catch {
      return '';
    }
  };
  const isProjectConversationHref = (href) => !!href && (!projectPrefix || href.startsWith(projectPrefix));
  const collectLinks = (root) => {
    const seen = new Set();
    const links = [];
    const scope = root || document;
    for (const anchor of Array.from(scope.querySelectorAll('a[href]'))) {
      const href = normalizeHref(anchor.getAttribute('href') || '');
      if (!href || !isProjectConversationHref(href) || seen.has(href)) continue;
      const rect = anchor.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0) continue;
      seen.add(href);
      const title = (anchor.innerText || anchor.textContent || '').trim();
      links.push({ title, url: href });
    }
    return links;
  };
  const isVisibleBox = (el) => {
    const rect = el.getBoundingClientRect();
    return rect.width > 160 && rect.height > 120;
  };
  const findMainRoot = () => {
    const direct = document.querySelector('main,[role="main"]');
    if (direct) return direct;
    const candidates = Array.from(document.querySelectorAll('main,[role="main"],section,div'));
    let best = document.body;
    let bestScore = -1;
    for (const el of candidates) {
      const rect = el.getBoundingClientRect();
      if (rect.width < 240 || rect.height < 240) continue;
      if (rect.right < window.innerWidth * 0.45) continue;
      const count = collectLinks(el).length;
      const score = count * 10000 + rect.width * rect.height;
      if (score > bestScore) {
        best = el;
        bestScore = score;
      }
    }
    return best || document.body;
  };
  const isScrollable = (el) => {
    if (!el) return false;
    const style = getComputedStyle(el);
    if (!style) return false;
    const overflowY = style.overflowY || '';
    return (overflowY === 'auto' || overflowY === 'scroll') && el.scrollHeight > el.clientHeight + 50;
  };
  const loadMorePattern = /(加载更多对话|加载更多聊天|加载更多|Load more chats|Load more conversations|Load more)/i;
  const findLoadMoreButton = (root) => {
    const scope = root || document;
    for (const el of Array.from(scope.querySelectorAll('button,[role="button"]'))) {
      const text = (el.innerText || el.textContent || '').trim();
      if (!text || !loadMorePattern.test(text)) continue;
      return el;
    }
    return null;
  };
  const findScrollContainer = (root) => {
    const candidates = [];
    for (const el of [root, ...Array.from(root.querySelectorAll('*'))]) {
      if (!isScrollable(el) || !isVisibleBox(el)) continue;
      const rect = el.getBoundingClientRect();
      if (rect.right < window.innerWidth * 0.45) continue;
      const anchorCount = collectLinks(el).length;
      const score = anchorCount * 10000 + rect.height * rect.width + (rect.left > window.innerWidth * 0.2 ? 5000 : 0);
      candidates.push({ el, score });
    }
    candidates.sort((a, b) => b.score - a.score);
    if (candidates.length > 0) return candidates[0].el;
    return isScrollable(document.scrollingElement || document.documentElement)
      ? (document.scrollingElement || document.documentElement)
      : root;
  };
  const scrollOneStep = (el) => {
    const step = Math.max(Math.floor((el.clientHeight || window.innerHeight || 800) * 0.85), 480);
    const before = el === document.scrollingElement || el === document.documentElement || el === document.body
      ? (window.scrollY || document.documentElement.scrollTop || 0)
      : el.scrollTop;
    if (el === document.scrollingElement || el === document.documentElement || el === document.body) {
      window.scrollTo(0, before + step);
      return (window.scrollY || document.documentElement.scrollTop || 0) > before;
    }
    try {
      el.scrollTop = before + step;
    } catch {}
    return el.scrollTop > before;
  };
  return (async () => {
    const root = findMainRoot();
    let stableRounds = 0;
    let stagnantScrollRounds = 0;
    let lastCount = -1;
    for (let round = 0; round < 80; round++) {
      const beforeCount = collectLinks(root).length;
      const loadMore = findLoadMoreButton(root);
      let moved = false;
      if (loadMore) {
        try {
          loadMore.scrollIntoView({ block: 'center' });
        } catch {}
        try {
          loadMore.click();
        } catch {}
        moved = true;
      } else {
        const container = findScrollContainer(root);
        moved = scrollOneStep(container);
      }
      if (!moved) stagnantScrollRounds++;
      else stagnantScrollRounds = 0;
      await new Promise(r => setTimeout(r, loadMore ? 900 : 450));
      const count = collectLinks(root).length;
      if (count === lastCount && count === beforeCount) stableRounds++;
      else stableRounds = 0;
      lastCount = count;
      if (stableRounds >= 4 && stagnantScrollRounds >= 2) break;
    }
    return JSON.stringify({
      title: document.title.replace(/\s*-\s*ChatGPT$/, ''),
      url: location.href,
      links: collectLinks(root),
      error: ''
    });
  })();
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
  const targets = await fetch('http://127.0.0.1:%d/json/list').then(r => r.json());
  let target = targets.find(t => t.type === 'page' && (t.url === 'about:blank' || (t.url || '').startsWith('https://chatgpt.com/')));
  if (!target) {
    target = await fetch('http://127.0.0.1:%d/json/new?' + encodeURIComponent('about:blank'), { method: 'PUT' }).then(r => r.json());
  }
  const ws = await connectWS(target.webSocketDebuggerUrl);
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
    awaitPromise: true,
    returnByValue: true,
  });
  console.log(result.result.value);
  ws.close();
})().catch(err => {
  console.log(JSON.stringify({ title: '', url: '', links: [], error: String(err) }));
  process.exit(0);
});
`, pageURL, cookieHeader, waitMS, port, port, fmt.Sprintf(expression, projectPrefix))

	cmd := exec.CommandContext(ctx, "node", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return browserDiscoveryPayload{}, fmt.Errorf("node cdp discovery failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	var payload browserDiscoveryPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &payload); err != nil {
		return browserDiscoveryPayload{}, err
	}
	return payload, nil
}

func buildProjectConversationPrefix(pageURL string) string {
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil {
		return ""
	}
	parts := splitURLPath(u.Path)
	if len(parts) < 2 || parts[0] != "g" {
		return ""
	}
	return fmt.Sprintf("https://%s/g/%s/c/", u.Host, parts[1])
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

func appendSessionWarnings(warnings []warningRecord, launched bool) []warningRecord {
	if launched {
		return append(warnings, warningRecord{
			Code:    "source.project_url.browser_session_started",
			Message: "Started a reusable Chrome debugging session for CGME project URL export.",
		})
	}
	return warnings
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
			out[len(out)-1].Blocks = rendermarkdown.ParseTextBlocks(out[len(out)-1].Content)
			mergedCount++
			continue
		}
		out = append(out, Message{
			Role:    role,
			Content: content,
			Blocks:  rendermarkdown.ParseTextBlocks(content),
		})
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

func browserProfileRoot() string {
	if override := strings.TrimSpace(os.Getenv(browserProfileRootEnv)); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cgme-browser-profile")
	}
	return filepath.Join(home, "Library", "Application Support", "cgme", "browser-profile")
}
