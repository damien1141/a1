package browsertool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/bw"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
	imgutil "github.com/damien1141/a1/internal/util/image"
)

// The browser tool drives BetterWright, a persistent browser daemon, through
// its CLI. The tool surface (open/navigate/search/click/type/screenshot/
// content/close/profile/proxy) and the APPA gating are unchanged from the
// playwright era; the engine underneath swapped. The daemon keeps tabs and
// page state alive between calls, the profiles keep their cookies on disk
// after close, and bot challenges surface as handoff events routed through
// the permission ask flow.
//
// The model still cannot spawn the browser on its own: runBrowser refuses
// until the user opens it (/browser open …), which keeps the "ask nicely"
// gate while the daemon makes the opened browser actually stick.

var (
	// browserMu serializes every action against the one shared session —
	// two interleaved calls against the same page produce nonsense, and the
	// daemon serializes calls within a session anyway, so the mutex makes
	// the tool safe, the daemon makes it correct.
	browserMu sync.Mutex
	// bwOpened is the user-opened latch. Only /browser (RunBrowserCommand)
	// sets it; runBrowser refuses everything but proxy until then.
	bwOpened bool
	// currentProfile is the active BetterWright profile ("default" = the
	// shared profile).
	currentProfile = bw.DefaultProfile
	// browserProxyURL / lastProxyURL carry the proxy state. Chromium on
	// Linux honors HTTP(S)_PROXY env vars, so the URL rides the CLI spawn;
	// other platforms pick proxies up from system settings.
	browserProxyURL string
	lastProxyURL    string
)

const contentMaxChars = 12_000

var browserDescription = `Control the user's BetterWright browser daemon; the user must open it
first (/browser open <url>) — every action except proxy errors until then.
The daemon keeps tabs, page state, and profile logins alive between calls.

open/navigate: url, scheme auto-added. search: text, Mojeek results.
click: selector. type: selector+text (fill, not append). screenshot: PNG
to a temp path, visible to vision models; optional name grades the
screenshot:<name> evidence check. content: visible text, boilerplate
stripped, 12K cap. profile: subcommands (see param). close. proxy:
on|off|URL for the next launch; works with no browser open.

Prefer browser_navigate/browser_interact/browser_read for page work.`

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
						"description": "open|navigate|search|click|type|screenshot|content|close|profile|proxy",
					},
					"url": llm.Object{
						"type":        "string",
						"description": "URL for open/navigate.",
					},
					"selector": llm.Object{
						"type":        "string",
						"description": "CSS selector, e.g. #submit",
					},
					"text": llm.Object{
						"type":        "string",
						"description": "Text to type, or the search query.",
					},
					"profile": llm.Object{
						"type":        "string",
						"description": "Name for open; or list|create|switch|delete <name>.",
					},
					"name": llm.Object{
						"type":        "string",
						"description": "Optional screenshot name: grades the screenshot:<name> evidence check when declared in a contract.",
					},
				},
				Required: []string{"action"},
			},
			// Not Readable: every action drives one shared page (open, navigate,
			// click, type). Marking it read-only let the executor run a batch
			// concurrently, and two calls interleaved against the same page
			// produce nonsense — a click landing on a not-yet-navigated page. The
			// mutex makes that safe, not correct.
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in browserInput
			_ = json.Unmarshal(input, &in)
			a := strings.ToLower(strings.TrimSpace(in.Action))
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
	Name     string `json:"name,omitempty"`
}

// runBrowser is the LLM-facing tool handler. It does NOT open the browser;
// it only controls the user's already-open daemon session. This prevents the
// model from spawning browsers on its own. If the browser is not open, it
// returns an error directing the user to /browser open <url>.
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
	// URL used by the next spawn. The daemon picks the proxy up from the
	// environment at launch time, so a change takes effect on the next open
	// after a close.
	if action == "proxy" {
		return browserProxy(in)
	}

	// Hold browserMu for the whole action so every sub-call can read and
	// mutate the shared state without a race. No sub-action re-acquires it.
	browserMu.Lock()
	defer browserMu.Unlock()

	if !bwOpened {
		return tooldef.Result{}, fmt.Errorf(
			"browser is not open; please ask the user nicely to open the browser with /browser open <url> or use the fetch tool",
		)
	}
	return dispatchBrowserAction(ctx, action, in)
}

// RunBrowserCommand is the slash-command handler. It opens the browser
// daemon if needed, then runs the requested action. Only the user-facing
// /browser command should call this; the LLM tool uses runBrowser instead.
func RunBrowserCommand(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
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

	if action == "proxy" {
		return browserProxy(in)
	}

	if err := ensureBrowser(ctx); err != nil {
		return tooldef.Result{}, err
	}

	browserMu.Lock()
	defer browserMu.Unlock()

	return dispatchBrowserAction(ctx, action, in)
}

func dispatchBrowserAction(ctx context.Context, action string, in browserInput) (tooldef.Result, error) {
	switch action {
	case "open":
		return browserOpen(ctx, in)
	case "navigate":
		return browserNavigate(ctx, in)
	case "search":
		return browserSearch(ctx, in)
	case "click":
		return browserClick(ctx, in)
	case "type":
		return browserType(ctx, in)
	case "screenshot":
		return browserScreenshot(ctx, in)
	case "content":
		return browserContent(ctx)
	case "close":
		return browserClose(ctx)
	case "profile":
		return browserProfile(ctx, in)
	default:
		return tooldef.Result{}, fmt.Errorf(
			"unknown action %q: use open, navigate, click, type, screenshot, content, close, profile",
			action,
		)
	}
}

// hasDisplay reports whether a graphical session is reachable. Headed mode
// needs one; a daemon spawn without a display would fail slowly otherwise.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// browserHeadless decides the daemon's launch mode, honouring
// A1_BROWSER_HEADLESS as an explicit override.
func browserHeadless() bool {
	if v := strings.TrimSpace(os.Getenv("A1_BROWSER_HEADLESS")); v != "" {
		return v != "0" && !strings.EqualFold(v, "false")
	}
	return !hasDisplay()
}

// ensureBrowser warms the daemon: a trivial run per profile spawns its
// browser, mirroring the old launch-all-at-open behavior. Called from
// RunBrowserCommand while NOT holding browserMu, so the lock is free to
// acquire below; it holds browserMu for the whole warm-up so two concurrent
// commands cannot double-spawn.
func ensureBrowser(ctx context.Context) error {
	// Slash commands run on the UI goroutine and may pass a nil context.
	if ctx == nil {
		ctx = context.Background()
	}
	browserMu.Lock()
	defer browserMu.Unlock()
	if bwOpened {
		return nil
	}

	// Seed the proxy from config at startup so a configured proxy takes
	// effect on the very first spawn. Runtime overrides via SetProxy /
	// browserProxy win because they are checked first.
	if browserProxyURL == "" {
		if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
			browserProxyURL = proj.Config().Browser.Proxy
		}
	}

	headed := !browserHeadless()
	if _, err := bw.Run(ctx, "", headed, "return true"); err != nil {
		return err
	}
	// Warm any additional profiles from config. Errors are logged by failing
	// silently — a bad profile name must not block the default one.
	if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
		seen := make(map[string]struct{}, len(proj.Config().Browser.Profiles))
		for _, name := range proj.Config().Browser.Profiles {
			name = strings.TrimSpace(name)
			if name == "" || name == bw.DefaultProfile {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if !validProfileName(name) {
				continue
			}
			_, _ = bw.Run(ctx, name, headed, "return true")
		}
	}

	bwOpened = true
	return nil
}

// validProfileName gates profile names: BetterWright allows letters, digits,
// dot, dash, underscore; "/" and friends would escape the profiles dir.
func validProfileName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return name != "." && name != ".."
}

// profileName maps an a1 profile name to the BetterWright profile flag:
// "default" means the shared profile (empty flag).
func profileName(name string) string {
	if name == bw.DefaultProfile {
		return ""
	}
	return name
}

// runPage executes js in the current profile and returns the envelope.
func runPage(ctx context.Context, js string) (bw.Envelope, error) {
	return bw.Run(ctx, profileName(currentProfile), !browserHeadless(), js)
}

// browserSearch jumps straight to a results page. BetterWright blocks
// automation of the big public search UIs (Google, Bing, DuckDuckGo) on its
// managed browser, so the in-browser search action uses Mojeek — an
// independent index with no aggressive bot walls. The fetch tool's search
// router remains the primary search path; this action is for when the model
// is already in the browser and wants to follow a query.
func browserSearch(ctx context.Context, in browserInput) (tooldef.Result, error) {
	query := strings.TrimSpace(in.Text)
	if query == "" {
		return tooldef.Result{}, fmt.Errorf("text (the search query) is required for search")
	}
	searchURL := "https://www.mojeek.com/search?q=" + url.QueryEscape(query)
	env, err := runPage(ctx, fmt.Sprintf(
		"await page.goto(%s); return {url: page.url(), title: await page.title()}", bw.JSString(searchURL),
	))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate to search: %w", err)
	}
	var out struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(env.Result, &out)
	res := tooldef.Result{
		Content: fmt.Sprintf("Searched Mojeek for %q\nURL: %s\nTitle: %s", query, searchURL, out.Title),
		Detail:  fmt.Sprintf("search %q", query),
		Summary: Summary("Searched", hostOf(searchURL), 0, 0),
		Output:  fmt.Sprintf("Searched Mojeek for %q", query),
	}
	applyChallenges(&res, env)
	return res, nil
}

// browserOpen is called from runBrowser while browserMu is held.
func browserOpen(ctx context.Context, in browserInput) (tooldef.Result, error) {
	profile := strings.TrimSpace(in.Profile)
	if profile == "" {
		profile = currentProfile
	}
	u := normalizeURL(strings.TrimSpace(in.URL))
	if u == "" {
		return tooldef.Result{}, fmt.Errorf("url is required for open")
	}
	if profile != bw.DefaultProfile {
		if !validProfileName(profile) {
			return tooldef.Result{}, fmt.Errorf("invalid profile name %q", profile)
		}
		if err := os.MkdirAll(bw.ProfileDir(profile), 0o755); err != nil {
			return tooldef.Result{}, fmt.Errorf("create profile dir: %w", err)
		}
	}
	currentProfile = profile
	env, err := bw.Run(ctx, profileName(profile), !browserHeadless(), fmt.Sprintf(
		"await page.goto(%s); return {url: page.url(), title: await page.title()}", bw.JSString(u),
	))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate: %w", err)
	}
	var out struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(env.Result, &out)
	res := tooldef.Result{
		Content: fmt.Sprintf("Opened %s\nProfile: %s\nTitle: %s", u, profile, out.Title),
		Detail:  u,
		Summary: Summary("Opened", hostOf(u), 0, 0),
		Output:  fmt.Sprintf("Opened %s", u),
	}
	applyChallenges(&res, env)
	return res, nil
}

// browserNavigate is called from runBrowser while browserMu is held.
func browserNavigate(ctx context.Context, in browserInput) (tooldef.Result, error) {
	u := normalizeURL(strings.TrimSpace(in.URL))
	if u == "" {
		return tooldef.Result{}, fmt.Errorf("url is required for navigate")
	}
	env, err := runPage(ctx, fmt.Sprintf(
		"await page.goto(%s); return {url: page.url(), title: await page.title()}", bw.JSString(u),
	))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("navigate: %w", err)
	}
	var out struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(env.Result, &out)
	res := tooldef.Result{
		Content: fmt.Sprintf("Navigated to %s\nTitle: %s", u, out.Title),
		Detail:  u,
		Summary: Summary("Navigated", hostOf(u), 0, 0),
		Output:  fmt.Sprintf("Navigated to %s", u),
	}
	applyChallenges(&res, env)
	return res, nil
}

// browserClick is called from runBrowser while browserMu is held.
func browserClick(ctx context.Context, in browserInput) (tooldef.Result, error) {
	sel := strings.TrimSpace(in.Selector)
	if sel == "" {
		return tooldef.Result{}, fmt.Errorf("selector is required for click")
	}
	env, err := runPage(ctx, fmt.Sprintf(
		"await page.click(%s); await page.waitForLoadState('domcontentloaded').catch(() => {}); return true",
		bw.JSString(sel),
	))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("click %q: %w", sel, err)
	}
	res := tooldef.Result{
		Content: fmt.Sprintf("Clicked %s", sel),
		Detail:  sel,
		Summary: Summary("Clicked", sel, 0, 0),
		Output:  fmt.Sprintf("Clicked %s", sel),
	}
	applyChallenges(&res, env)
	return res, nil
}

// browserType is called from runBrowser while browserMu is held.
func browserType(ctx context.Context, in browserInput) (tooldef.Result, error) {
	sel := strings.TrimSpace(in.Selector)
	text := in.Text
	if sel == "" {
		return tooldef.Result{}, fmt.Errorf("selector is required for type")
	}
	env, err := runPage(ctx, fmt.Sprintf(
		"await page.fill(%s, %s); await page.waitForLoadState('domcontentloaded').catch(() => {}); return true",
		bw.JSString(sel), bw.JSString(text),
	))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("type into %q: %w", sel, err)
	}
	res := tooldef.Result{
		Content: fmt.Sprintf("Typed into %s", sel),
		Detail:  sel,
		Summary: Summary("Typed into", sel, 0, 0),
		Output:  fmt.Sprintf("Typed into %s", sel),
	}
	applyChallenges(&res, env)
	return res, nil
}

// applyChallenges turns envelope challenges into a handoff hint so the
// executor can route the state through the permission ask flow.
func applyChallenges(res *tooldef.Result, env bw.Envelope) {
	if reason := env.HandoffReason(); reason != "" {
		res.Handoff = &tooldef.Handoff{Reason: reason}
		for _, c := range env.Challenges {
			if c.URL != "" {
				res.Handoff.URL = c.URL
				break
			}
		}
	}
}

func browserScreenshot(ctx context.Context, in browserInput) (tooldef.Result, error) {
	env, err := runPage(ctx,
		"const s = await screenshot({kind: 'proof', name: 'a1.png', type: 'png'}); return s;")
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("screenshot: %w", err)
	}
	var shot struct {
		Kind  string `json:"kind"`
		Path  string `json:"path"`
		Media string `json:"media"`
	}
	if err := json.Unmarshal(env.Result, &shot); err != nil || shot.Path == "" {
		return tooldef.Result{}, fmt.Errorf("screenshot: unexpected daemon result %.200s", string(env.Result))
	}
	buf, err := os.ReadFile(shot.Path)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("read capture artifact: %w", err)
	}
	// Copy into a per-call temp file: concurrent sessions and repeated
	// screenshots used to clobber /tmp/browser-screenshot.png, and daemon
	// artifacts live under a size quota that evicts the oldest — the a1
	// path is the stable copy the transcript points at.
	path, closeFn, err := newScreenshotPath()
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("create screenshot temp file: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		closeFn(true) // remove on failure
		return tooldef.Result{}, fmt.Errorf("write screenshot: %w", err)
	}
	closeFn(false) // close only, keep the file
	// The action is the identity and the filename is the summary. The full path
	// stays in Content and in the expanded body, where it can be copied — the row
	// header is the glanceable part and a long path made it unreadable.
	res := tooldef.Result{
		Content: fmt.Sprintf("Screenshot saved to %s (%d bytes)", path, len(buf)),
		Detail:  "screenshot",
		Summary: ScreenshotSummary(path),
		Output:  fmt.Sprintf("Screenshot: %s", path),
	}
	// Vision-capable models see the capture itself, not just its path. The
	// fit bounds the bytes (~1 MiB, ≤1600px) so a 4K screenshot cannot
	// dominate the context; blind models keep the path-only text above.
	if tooldef.ImageEnabledFrom(ctx) {
		if fitted, mime, err := imgutil.FitForModel(buf, "image/png", 0, 0); err == nil {
			res.Images = []llm.Image{imgutil.ToLLM(imgutil.Result{Data: fitted, MimeType: mime})}
		}
	}
	// Named captures feed the evidence ledger's screenshot:<name> watches;
	// the executor's hook grades them against the stored baseline.
	if name := strings.TrimSpace(in.Name); name != "" {
		res.Screenshot = &tooldef.ScreenshotFact{Name: name, Path: path}
	}
	applyChallenges(&res, env)
	return res, nil
}

// newScreenshotPath creates a per-call temp file for a browser screenshot and
// returns its path plus a cleanup closure. The closure should be called with
// remove=true on write failure (deletes the file) or remove=false on success
// (just closes the handle). Each call returns a unique path so concurrent
// sessions and repeated screenshots in one session no longer clobber each
// other at /tmp/browser-screenshot.png.
//
// The file is created with the `a1-screenshot-*.png` pattern under the OS temp
// dir; the * is replaced by a random string by os.CreateTemp.
func newScreenshotPath() (path string, cleanup func(remove bool), err error) {
	tmp, err := os.CreateTemp("", "a1-screenshot-*.png")
	if err != nil {
		return "", nil, err
	}
	path = tmp.Name()
	cleanup = func(remove bool) {
		_ = tmp.Close()
		if remove {
			_ = os.Remove(path)
		}
	}
	return path, cleanup, nil
}

// contentJS extracts readable text from the page body: clone, strip the
// noise tags, append hrefs to links so the model knows where they point
// without guessing. Executed inside the daemon's page.evaluate.
const contentJS = `const text = await page.evaluate(() => {
        const clone = document.body.cloneNode(true);
        const noise = clone.querySelectorAll('script, style, noscript, svg, iframe, nav, footer, aside, header, [role="banner"], [role="navigation"], [role="complementary"]');
        noise.forEach(el => el.remove());
        clone.querySelectorAll('a').forEach(a => {
                const href = a.getAttribute('href');
                if (href && !href.startsWith('#') && !href.startsWith('javascript:')) {
                        a.innerText = a.innerText + ' (' + href + ')';
                }
        });
        return clone.innerText;
});
return {text: text, url: page.url(), title: await page.title()};`

func browserContent(ctx context.Context) (tooldef.Result, error) {
	env, err := runPage(ctx, contentJS)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("content: %w", err)
	}
	var out struct {
		Text  string `json:"text"`
		URL   string `json:"url"`
		Title string `json:"title"`
		// Spilled-output shape: BetterWright bounds result sizes; an
		// over-limit value is spilled to a file and replaced with
		// {truncated, preview, fullOutputPath}.
		Truncated      bool   `json:"truncated"`
		FullOutputPath string `json:"fullOutputPath"`
	}
	if err := json.Unmarshal(env.Result, &out); err != nil {
		return tooldef.Result{}, fmt.Errorf("content: unexpected daemon result %.200s", string(env.Result))
	}
	if out.Truncated && out.FullOutputPath != "" {
		if b, rerr := os.ReadFile(out.FullOutputPath); rerr == nil {
			var spilled struct {
				Text  string `json:"text"`
				URL   string `json:"url"`
				Title string `json:"title"`
			}
			if jerr := json.Unmarshal(b, &spilled); jerr == nil && spilled.Text != "" {
				out.Text, out.URL, out.Title = spilled.Text, spilled.URL, spilled.Title
			}
		}
	}

	text := strings.TrimSpace(out.Text)
	text = collapseWhitespace(text)
	if len(text) > contentMaxChars {
		text = text[:contentMaxChars] + "\n…[truncated]"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("URL: %s\nTitle: %s\nProfile: %s\n\n", out.URL, out.Title, currentProfile))
	sb.WriteString(text)
	// Detail was the page title here, which silently replaced the call's identity
	// ("browser content" became whatever the page happened to be called). The
	// url is the identity; the title and size belong in Summary.
	res := tooldef.Result{
		Content: sb.String(),
		Detail:  "content",
		Summary: Summary("Read", hostOf(out.URL), len(text), 0),
		Output:  sb.String(),
	}
	applyChallenges(&res, env)
	return res, nil
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

func browserClose(ctx context.Context) (tooldef.Result, error) {
	// --all closes every profile's daemon and stops them now; the on-disk
	// profiles and their logins survive, which is the whole point of the
	// daemon — a later open lands in a still-signed-in session.
	if err := bw.Close(ctx, true, ""); err != nil {
		return tooldef.Result{}, fmt.Errorf("close: %w", err)
	}
	bwOpened = false
	currentProfile = bw.DefaultProfile
	return tooldef.Result{
		Content: "Browser closed (profiles and logins kept on disk)",
		Detail:  "closed",
		Summary: Summary("Closed", "", 0, 0),
		Output:  "Browser closed (profiles and logins kept on disk)",
	}, nil
}

func browserProfile(ctx context.Context, in browserInput) (tooldef.Result, error) {
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
		return browserProfileDelete(ctx, arg)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown profile command %q: use list, create, switch, delete", cmd)
	}
}

// listProfiles returns the known profile names: "default" (the shared
// profile) plus every directory under BetterWright's profiles dir.
func listProfiles() []string {
	names := []string{bw.DefaultProfile}
	entries, err := os.ReadDir(bw.ProfilesDir())
	if err != nil {
		return names
	}
	for _, e := range entries {
		if e.IsDir() && validProfileName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

func browserProfileList() (tooldef.Result, error) {
	names := listProfiles()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Profiles (%d):\n", len(names)))
	for _, name := range names {
		marker := ""
		if name == currentProfile {
			marker = " (active)"
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", name, marker))
	}
	return tooldef.Result{
		Content: sb.String(),
		Detail:  fmt.Sprintf("%d profiles", len(names)),
		Summary: ProfilesSummary(len(names)),
		Output:  sb.String(),
	}, nil
}

// browserProfileCreate is called from runBrowser while browserMu is held.
// BetterWright materializes a profile directory on first use; creating it
// here just registers the name so list/switch see it before the first open.
func browserProfileCreate(name string) (tooldef.Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tooldef.Result{}, fmt.Errorf("profile name is required")
	}
	if name == bw.DefaultProfile {
		return tooldef.Result{}, fmt.Errorf("cannot create profile named 'default'")
	}
	if !validProfileName(name) {
		return tooldef.Result{}, fmt.Errorf(
			"invalid profile name %q: letters, digits, dot, dash, underscore only",
			name,
		)
	}
	if _, err := os.Stat(bw.ProfileDir(name)); err == nil {
		return tooldef.Result{}, fmt.Errorf("profile %q already exists", name)
	}
	if err := os.MkdirAll(bw.ProfileDir(name), 0o755); err != nil {
		return tooldef.Result{}, fmt.Errorf("create profile dir: %w", err)
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
	found := false
	for _, p := range listProfiles() {
		if p == name {
			found = true
			break
		}
	}
	if !found {
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
func browserProfileDelete(ctx context.Context, name string) (tooldef.Result, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tooldef.Result{}, fmt.Errorf("profile name is required")
	}
	if name == bw.DefaultProfile {
		return tooldef.Result{}, fmt.Errorf("cannot delete the default profile")
	}
	dir := bw.ProfileDir(name)
	if _, err := os.Stat(dir); err != nil {
		return tooldef.Result{}, fmt.Errorf("profile %q not found", name)
	}
	// Stop the profile's daemon first so the browser is not running on a
	// directory about to disappear.
	_ = bw.Close(ctx, false, name)
	if err := os.RemoveAll(dir); err != nil {
		return tooldef.Result{}, fmt.Errorf("delete profile %q: %w", name, err)
	}
	if currentProfile == name {
		currentProfile = bw.DefaultProfile
	}
	return tooldef.Result{
		Content: fmt.Sprintf("Deleted profile %q", name),
		Detail:  name,
		Output:  fmt.Sprintf("Deleted profile %q", name),
	}, nil
}

// normalizeURL prepends a scheme when one is missing so bare hostnames like
// "google.com" resolve to https://google.com. The daemon's page.goto treats
// a scheme-less string as a relative path and silently no-ops.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Check against a whitelist so "localhost:8080" is not misidentified as
	// having the scheme "localhost" — that would bypass the https:// prepend
	// and goto would fail with a navigation error.
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		scheme := strings.ToLower(raw[:idx])
		switch scheme {
		case "http", "https", "ftp", "file", "about", "javascript", "data", "mailto":
			return raw
		}
	}
	// A leading slash means a path, not a host — leave it alone.
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "https://" + raw
}

// browserProxy sets the proxy URL used by the next browser spawn. The
// daemon's Chromium picks the proxy up from the environment at launch time
// (HTTP(S)_PROXY on Linux; system settings elsewhere), so a change takes
// effect on the next open after a close.
//
//	/browser proxy <url>     set an explicit proxy URL
//	/browser proxy on        re-enable the last-used proxy (or the config
//	                         browser.proxy value if none was set yet)
//	/browser proxy off       disable the proxy
func browserProxy(in browserInput) (tooldef.Result, error) {
	u := strings.TrimSpace(in.URL)
	switch strings.ToLower(u) {
	case "on":
		browserMu.Lock()
		u = lastProxyURL
		browserMu.Unlock()
		if u == "" {
			if proj := project.GetDefaultProject(); proj != nil && proj.Config() != nil {
				u = proj.Config().Browser.Proxy
			}
		}
		if u == "" {
			return tooldef.Result{
				Content: "No proxy URL to enable: set one first with /browser proxy <url>, or browser.proxy in config.yaml",
				Detail:  "proxy none",
				Output:  "No proxy configured",
			}, nil
		}
		SetProxy(u)
		return tooldef.Result{
			Content: fmt.Sprintf("Proxy enabled: %s (takes effect on the next open after a close)", u),
			Detail:  fmt.Sprintf("proxy on %s", u),
			Output:  fmt.Sprintf("Proxy: %s", u),
		}, nil
	case "off", "":
		SetProxy("")
		return tooldef.Result{
			Content: "Proxy disabled (takes effect on the next open after a close)",
			Detail:  "proxy off",
			Output:  "Proxy disabled",
		}, nil
	}
	SetProxy(u)
	return tooldef.Result{
		Content: fmt.Sprintf("Proxy set to %s (takes effect on the next open after a close)", u),
		Detail:  fmt.Sprintf("proxy %s", u),
		Output:  fmt.Sprintf("Proxy: %s", u),
	}, nil
}

// SetProxy sets the proxy URL used by the next browser spawn. It takes
// effect on the next open after a close, since the daemon's Chromium reads
// proxy settings at launch time. Pass "" to disable. Thread-safe. The URL
// is remembered so "proxy on" can re-enable it later without retyping.
func SetProxy(u string) {
	browserMu.Lock()
	defer browserMu.Unlock()
	u = strings.TrimSpace(u)
	if u != "" {
		lastProxyURL = u
	}
	browserProxyURL = u
	bw.SetProxy(u)
}

// ProxyURL returns the proxy currently configured for the next spawn.
func ProxyURL() string {
	browserMu.Lock()
	defer browserMu.Unlock()
	return browserProxyURL
}
