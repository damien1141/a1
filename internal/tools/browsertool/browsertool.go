package browsertool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/mxschmitt/playwright-go"
)

var (
	browser        playwright.Browser
	browserCtx     context.Context
	browserCancel  context.CancelFunc
	// pw is the Playwright driver instance. It is kept global so browserClose
	// can call pw.Stop() to tear down the node driver; otherwise the driver
	// lingers and the next playwright.Run() fails with "browser is already
	// in use".
	pw             *playwright.Playwright
	profiles       map[string]playwright.BrowserContext
	pages          map[string]playwright.Page
	currentProfile string
	// browserProxyURL is the proxy used by the next browser launch. It is set
	// from config at startup and can be changed at runtime via the
	// "proxy" action. Because Chromium picks up the proxy at launch time, a
	// change takes effect on the next open after a close.
	browserProxyURL string
	// lastProxyURL remembers the last explicit proxy URL so "proxy on" can
	// re-enable it without retyping. Empty until a URL has been set.
	lastProxyURL   string
	browserMu      sync.Mutex
)

func init() {
	profiles = make(map[string]playwright.BrowserContext)
	pages = make(map[string]playwright.Page)
	currentProfile = "default"
}

const (
	browserDefaultTimeout = 30 * time.Second
	// contentMaxChars caps the text returned by browser content. A full DOM
	// for a Google results page is several MB and blows past the context
	// window; ~12k chars of visible text is more than enough for the model
	// to summarize or extract what it needs.
	contentMaxChars = 12_000
)

var browserDescription = `Control a live web browser via Playwright.

Spawns a visible Chromium window and exposes navigation, clicking, typing,
screenshots, and page inspection. Supports multiple isolated browser profiles.

Actions:
- open <url>: open a URL in the current profile (scheme auto-prepended, so
  "google.com" works).
- navigate <url>: go to a URL in the already-open page.
- search <query>: open google.com and run a search in one step.
- click <selector>: click the first element matching a CSS selector.
- type <selector> <text>: fill an input matching a CSS selector.
- screenshot: save the visible viewport to /tmp/browser-screenshot.png.
- content: return the visible text of the page (trimmed, not raw HTML).
- close: close the browser and release the Playwright driver so a subsequent
  open can spawn a new one.
- profile list|create <name>|switch <name>|delete <name>: manage profiles.
- proxy on|off|<url>: toggle the HTTP/HTTPS/SOCKS proxy for the next launch
  (e.g. "http://127.0.0.1:8885"). "on" re-enables the last-used URL (or the
  browser.proxy config value); "off" disables it. Takes effect on the next
  open after a close; also configurable via browser.proxy in config.yaml.

Use /browser to open a URL, then use the browser_* tools to interact with it.`

// BrowserTool returns the browser control tool definition + handler.
func BrowserTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "browser",
			Description: browserDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "Action: open, navigate, search, click, type, screenshot, content, close, profile. Example: open",
					},
					"url": llm.Object{
						"type":        "string",
						"description": "URL to open or navigate to. Example: https://example.com",
					},
					"selector": llm.Object{
						"type":        "string",
						"description": "CSS selector for click/type. Example: #submit",
					},
					"text": llm.Object{
						"type":        "string",
						"description": "Text to type into an input. Example: hello world",
					},
					"profile": llm.Object{
						"type":        "string",
						"description": "Browser profile name for isolation, or profile subcommand. Example: work",
					},
				},
				Required: []string{"action"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in browserInput
			_ = json.Unmarshal(input, &in)
			a := strings.TrimSpace(in.Action)
			if a == "" {
				return "browser"
			}
			return fmt.Sprintf("browser %s", a)
		},
		Run: runBrowser,
	}
}

type browserInput struct {
	Action   string `json:"action"`
	Profile  string `json:"profile,omitempty"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
}

func runBrowser(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in browserInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse browser arguments: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		return tooldef.Result{}, fmt.Errorf(
			"action is required: open, navigate, search, click, type, screenshot, content, close, profile, proxy",
		)
	}

	// proxy is the only action that does not need the browser up: it sets the
	// URL used by the next launch. Chromium picks up the proxy at launch time,
	// so a change takes effect on the next open after a close.
	if action == "proxy" {
		return browserProxy(in)
	}

	// ensureBrowser launches Chromium on first use. It must run BEFORE this
	// function takes browserMu: ensureBrowser acquires browserMu itself for
	// the whole launch, so calling it while already holding the lock would
	// deadlock (sync.Mutex is not re-entrant). Because browser is Readable and
	// can run in a concurrent batch, ensureBrowser also serializes launch so
	// two concurrent opens cannot both spawn a browser.
	if err := ensureBrowser(ctx); err != nil {
		return tooldef.Result{}, err
	}

	// Hold browserMu for the whole action so every sub-call can read and
	// mutate the shared maps without a race. No sub-action re-acquires it.
	browserMu.Lock()
	defer browserMu.Unlock()

	switch action {
	case "open":
		return browserOpen(in)
	case "navigate":
		return browserNavigate(in)
	case "search":
		return browserSearch(in)
	case "click":
		return browserClick(in)
	case "type":
		return browserType(in)
	case "screenshot":
		return browserScreenshot()
	case "content":
		return browserContent()
	case "close":
		return browserClose()
	case "profile":
		return browserProfile(in)
	default:
		return tooldef.Result{}, fmt.Errorf(
			"unknown action %q: use open, navigate, click, type, screenshot, content, close, profile",
			action,
		)
	}
}

// ensureBrowser launches the persistent Chromium process and the default
// profile on first use. It is called from runBrowser while NOT holding
// browserMu, so the lock is free to acquire below. It holds browserMu for
// the whole launch so two concurrent runBrowser calls (browser is Readable,
// so it can run in a concurrent batch) cannot both spawn a browser.
func ensureBrowser(ctx context.Context) error {
	// Slash commands run on the UI goroutine and may pass a nil context.
	// context.WithCancel panics on a nil parent, so fall back to a
	// background context here rather than propagating the panic.
	if ctx == nil {
		ctx = context.Background()
	}
	browserMu.Lock()
	defer browserMu.Unlock()
	if browser != nil {
		return nil
	}

	// Seed the proxy from config at startup so a configured proxy takes
	// effect on the very first launch. Runtime overrides via SetProxy /
	// browserProxy win because they are checked first.
	if browserProxyURL == "" {
		if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
			browserProxyURL = proj.Config().Browser.Proxy
		}
	}

	pwLocal, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright run: %w", err)
	}
	pw = pwLocal

	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	}
	if browserProxyURL != "" {
		launchOpts.Proxy = &playwright.Proxy{Server: browserProxyURL}
	}
	b, err := pwLocal.Chromium.Launch(launchOpts)
	if err != nil {
		pw.Stop()
		pw = nil
		return fmt.Errorf("launch browser: %w", err)
	}

	browser = b
	browserCtx, browserCancel = context.WithCancel(ctx)

	// Create default profile.
	if err := createProfile("default"); err != nil {
		b.Close()
		browser = nil
		browserCancel = nil
		pw.Stop()
		return fmt.Errorf("create default profile: %w", err)
	}

	// Create any additional profiles from config.
	if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
		seen := make(map[string]struct{}, len(proj.Config().Browser.Profiles))
		for _, name := range proj.Config().Browser.Profiles {
			name = strings.TrimSpace(name)
			if name == "" || name == "default" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if err := createProfile(name); err != nil {
				// Log but don't fail startup for a bad profile name.
				continue
			}
		}
	}

	return nil
}

func createProfile(name string) error {
	if browser == nil {
		return fmt.Errorf("browser not initialized")
	}
	ctx, err := browser.NewContext()
	if err != nil {
		return fmt.Errorf("create profile %q: %w", name, err)
	}
	profiles[name] = ctx
	page, err := ctx.NewPage()
	if err != nil {
		ctx.Close()
		delete(profiles, name)
		return fmt.Errorf("new page for profile %q: %w", name, err)
	}
	pages[name] = page
	return nil
}

// SetProxy sets the proxy URL used by the next browser launch. It takes effect
// on the next open after a close, since Chromium picks up the proxy at launch
// time. Pass "" to disable the proxy. Thread-safe. The URL is remembered so
// "proxy on" can re-enable it later without retyping.
func SetProxy(url string) {
	browserMu.Lock()
	defer browserMu.Unlock()
	url = strings.TrimSpace(url)
	if url != "" {
		lastProxyURL = url
	}
	browserProxyURL = url
}

// ProxyURL returns the proxy currently configured for the next launch.
func ProxyURL() string {
	browserMu.Lock()
	defer browserMu.Unlock()
	return browserProxyURL
}

func currentPage() playwright.Page {
	if p, ok := pages[currentProfile]; ok {
		return p
	}
	return nil
}

// browserSearch is called from runBrowser while browserMu is held. It opens
// google.com, types the query into the search box and submits it in one step
// so the model does not need a separate open → type → click → submit chain.
func browserSearch(in browserInput) (tooldef.Result, error) {
	query := strings.TrimSpace(in.Text)
	if query == "" {
		return tooldef.Result{}, fmt.Errorf("text (the search query) is required for search")
	}
	profile := currentProfile
	page := pages[profile]
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	url := normalizeURL("https://www.google.com")
	if _, err := page.Goto(url); err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate: %w", err)
	}
	// Wait for the search input to be present, then type and submit.
	if _, err := page.WaitForSelector("input[name=q]", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		return tooldef.Result{}, fmt.Errorf("search input not found: %w", err)
	}
	if err := page.Fill("input[name=q]", query); err != nil {
		return tooldef.Result{}, fmt.Errorf("type query: %w", err)
	}
	if err := page.Press("input[name=q]", "Enter"); err != nil {
		return tooldef.Result{}, fmt.Errorf("submit search: %w", err)
	}
	title, _ := page.Title()
	return tooldef.Result{
		Content: fmt.Sprintf("Searched Google for %q\nURL: %s\nTitle: %s", query, url, title),
		Detail:  fmt.Sprintf("search %q", query),
		Output:  fmt.Sprintf("Searched Google for %q", query),
	}, nil
}

// browserOpen is called from runBrowser while browserMu is held.
func browserOpen(in browserInput) (tooldef.Result, error) {
	profile := strings.TrimSpace(in.Profile)
	if profile == "" {
		profile = currentProfile
	}
	url := normalizeURL(strings.TrimSpace(in.URL))
	if url == "" {
		return tooldef.Result{}, fmt.Errorf("url is required for open")
	}
	// ensureBrowser was already called by runBrowser before the lock, so
	// browserMu is held here by the caller for the whole action.
	if _, exists := profiles[profile]; !exists {
		if err := createProfile(profile); err != nil {
			return tooldef.Result{}, err
		}
	}
	currentProfile = profile
	page := pages[profile]
	if _, err := page.Goto(url); err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate: %w", err)
	}
	title, _ := page.Title()
	return tooldef.Result{
		Content: fmt.Sprintf("Opened %s\nProfile: %s\nTitle: %s", url, profile, title),
		Detail:  url,
		Output:  fmt.Sprintf("Opened %s", url),
	}, nil
}

// browserNavigate is called from runBrowser while browserMu is held.
func browserNavigate(in browserInput) (tooldef.Result, error) {
	url := normalizeURL(strings.TrimSpace(in.URL))
	if url == "" {
		return tooldef.Result{}, fmt.Errorf("url is required for navigate")
	}
	page := currentPage()
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	if _, err := page.Goto(url); err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate: %w", err)
	}
	title, _ := page.Title()
	return tooldef.Result{
		Content: fmt.Sprintf("Navigated to %s\nTitle: %s", url, title),
		Detail:  url,
		Output:  fmt.Sprintf("Navigated to %s", url),
	}, nil
}

// browserClick is called from runBrowser while browserMu is held.
func browserClick(in browserInput) (tooldef.Result, error) {
	sel := strings.TrimSpace(in.Selector)
	if sel == "" {
		return tooldef.Result{}, fmt.Errorf("selector is required for click")
	}
	page := currentPage()
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	if err := page.Click(sel); err != nil {
		return tooldef.Result{}, fmt.Errorf("click %q: %w", sel, err)
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Clicked %s", sel),
		Detail:  sel,
		Output:  fmt.Sprintf("Clicked %s", sel),
	}, nil
}

// browserType is called from runBrowser while browserMu is held.
func browserType(in browserInput) (tooldef.Result, error) {
	sel := strings.TrimSpace(in.Selector)
	text := in.Text
	if sel == "" {
		return tooldef.Result{}, fmt.Errorf("selector is required for type")
	}
	page := currentPage()
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	if err := page.Fill(sel, text); err != nil {
		return tooldef.Result{}, fmt.Errorf("type into %q: %w", sel, err)
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Typed into %s", sel),
		Detail:  sel,
		Output:  fmt.Sprintf("Typed into %s", sel),
	}, nil
}

func browserScreenshot() (tooldef.Result, error) {
	page := currentPage()
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	buf, err := page.Screenshot()
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("screenshot: %w", err)
	}
	path := "/tmp/browser-screenshot.png"
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return tooldef.Result{}, fmt.Errorf("write screenshot: %w", err)
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Screenshot saved to %s (%d bytes)", path, len(buf)),
		Detail:  path,
		Output:  fmt.Sprintf("Screenshot: %s", path),
	}, nil
}

func browserContent() (tooldef.Result, error) {
	page := currentPage()
	if page == nil {
		return tooldef.Result{}, fmt.Errorf("browser not open; use action=open first")
	}
	// page.Content() returns the full raw DOM — a Google results page alone is
	// several MB and blows past the context window. Extract text and cap it.
	text, err := page.TextContent("body")
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("content: %w", err)
	}
	title, _ := page.Title()
	url := page.URL()

	text = strings.TrimSpace(text)
	// Collapse runs of whitespace so the model sees readable prose, not a
	// preformatted dump.
	text = collapseWhitespace(text)
	if len(text) > contentMaxChars {
		text = text[:contentMaxChars] + "\n…[truncated]"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("URL: %s\nTitle: %s\nProfile: %s\n\n", url, title, currentProfile))
	sb.WriteString(text)
	return tooldef.Result{Content: sb.String(), Detail: title, Output: sb.String()}, nil
}

// collapseWhitespace replaces runs of whitespace (including newlines) with a
// single space so extracted page text reads as prose.
func collapseWhitespace(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				sb.WriteRune(' ')
				inSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		inSpace = false
	}
	return sb.String()
}

func browserClose() (tooldef.Result, error) {
	for name, ctx := range profiles {
		ctx.Close()
		delete(profiles, name)
	}
	for name := range pages {
		delete(pages, name)
	}
	currentProfile = "default"
	if browser != nil {
		browser.Close()
		browser = nil
	}
	if browserCancel != nil {
		browserCancel()
		browserCtx = nil
		browserCancel = nil
	}
	// Stop the Playwright node driver. Without this the driver process
	// lingers and the next playwright.Run() fails with "browser is already
	// in use", so a subsequent /browser open could never spawn a new one.
	if pw != nil {
		pw.Stop()
		pw = nil
	}
	return tooldef.Result{Content: "Browser closed", Detail: "closed", Output: "Browser closed"}, nil
}

func browserProfile(in browserInput) (tooldef.Result, error) {
	profile := strings.TrimSpace(in.Profile)
	if profile == "" {
		return browserProfileList()
	}
	parts := strings.SplitN(profile, " ", 2)
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}
	switch cmd {
	case "list":
		return browserProfileList()
	case "create":
		return browserProfileCreate(arg)
	case "switch":
		return browserProfileSwitch(arg)
	case "delete":
		return browserProfileDelete(arg)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown profile command %q: use list, create, switch, delete", cmd)
	}
}

func browserProfileList() (tooldef.Result, error) {
	if len(profiles) == 0 {
		return tooldef.Result{Content: "No profiles", Detail: "0 profiles", Output: "No profiles"}, nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Profiles (%d):\n", len(profiles)))
	for name := range profiles {
		marker := ""
		if name == currentProfile {
			marker = " (active)"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", name, marker))
	}
	return tooldef.Result{
		Content: sb.String(),
		Detail:  fmt.Sprintf("%d profiles", len(profiles)),
		Output:  sb.String(),
	}, nil
}

// browserProfileCreate is called from runBrowser while browserMu is held.
// The browser is already up (runBrowser ensures it), so this only needs to
// create the named context.
func browserProfileCreate(name string) (tooldef.Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tooldef.Result{}, fmt.Errorf("profile name is required")
	}
	if name == "default" {
		return tooldef.Result{}, fmt.Errorf("cannot create profile named 'default'")
	}
	if _, exists := profiles[name]; exists {
		return tooldef.Result{}, fmt.Errorf("profile %q already exists", name)
	}
	if err := createProfile(name); err != nil {
		return tooldef.Result{}, err
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Created profile %q", name),
		Detail:  name,
		Output:  fmt.Sprintf("Created profile %q", name),
	}, nil
}

// browserProfileSwitch is called from runBrowser while browserMu is held.
func browserProfileSwitch(name string) (tooldef.Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tooldef.Result{}, fmt.Errorf("profile name is required")
	}
	if _, exists := profiles[name]; !exists {
		return tooldef.Result{}, fmt.Errorf("profile %q not found", name)
	}
	currentProfile = name
	return tooldef.Result{
		Content: fmt.Sprintf("Switched to profile %q", name),
		Detail:  name,
		Output:  fmt.Sprintf("Switched to profile %q", name),
	}, nil
}

// browserProfileDelete is called from runBrowser while browserMu is held.
func browserProfileDelete(name string) (tooldef.Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tooldef.Result{}, fmt.Errorf("profile name is required")
	}
	if name == "default" {
		return tooldef.Result{}, fmt.Errorf("cannot delete the default profile")
	}
	ctx, ok := profiles[name]
	if !ok {
		return tooldef.Result{}, fmt.Errorf("profile %q not found", name)
	}
	ctx.Close()
	delete(profiles, name)
	delete(pages, name)
	if currentProfile == name {
		currentProfile = "default"
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Deleted profile %q", name),
		Detail:  name,
		Output:  fmt.Sprintf("Deleted profile %q", name),
	}, nil
}

// normalizeURL prepends a scheme when one is missing so bare hostnames like
// "google.com" resolve to https://google.com. Playwright's page.Goto treats
// a scheme-less string as a relative path and silently no-ops.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Already has a scheme (http://, https://, ftp://, about:, javascript:).
	// Match a scheme token followed by a colon, which covers both "//" URLs
	// and special schemes like about:blank.
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		scheme := raw[:idx]
		if isSchemeToken(scheme) {
			return raw
		}
	}
	// A leading slash means a path, not a host — leave it alone.
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "https://" + raw
}

// isSchemeToken reports whether s is a valid URI scheme per RFC 3986
// (alpha followed by alphanumerics, plus, dot, hyphen).
func isSchemeToken(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		if i > 0 && ((r >= '0' && r <= '9') || r == '+' || r == '.' || r == '-') {
			continue
		}
		return false
	}
	return true
}

// browserProxy sets the proxy URL used by the next browser launch. Chromium
// picks up the proxy at launch time, so a change takes effect on the next
// open after a close.
//
//   /browser proxy <url>     set an explicit proxy URL
//   /browser proxy on        re-enable the last-used proxy (or the config
//                            browser.proxy value if none was set yet)
//   /browser proxy off       disable the proxy
func browserProxy(in browserInput) (tooldef.Result, error) {
	url := strings.TrimSpace(in.URL)
	switch strings.ToLower(url) {
	case "on":
		browserMu.Lock()
		url = lastProxyURL
		browserMu.Unlock()
		if url == "" {
			if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
				url = proj.Config().Browser.Proxy
			}
		}
		if url == "" {
			return tooldef.Result{
				Content: "No proxy URL to enable: set one first with /browser proxy <url>, or browser.proxy in config.yaml",
				Detail:  "proxy none",
				Output:  "No proxy configured",
			}, nil
		}
		SetProxy(url)
		return tooldef.Result{
			Content: fmt.Sprintf("Proxy enabled: %s (takes effect on the next open after a close)", url),
			Detail:  fmt.Sprintf("proxy on %s", url),
			Output:  fmt.Sprintf("Proxy: %s", url),
		}, nil
	case "off", "":
		SetProxy("")
		return tooldef.Result{
			Content: "Proxy disabled (takes effect on the next open after a close)",
			Detail:  "proxy off",
			Output:  "Proxy disabled",
		}, nil
	}
	SetProxy(url)
	return tooldef.Result{
		Content: fmt.Sprintf("Proxy set to %s (takes effect on the next open after a close)", url),
		Detail:  fmt.Sprintf("proxy %s", url),
		Output:  fmt.Sprintf("Proxy: %s", url),
	}, nil
}
