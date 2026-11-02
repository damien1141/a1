// Package bw is the Go client for BetterWright, the persistent browser
// daemon a1 drives instead of embedding a browser engine. One thin CLI
// subprocess per action (`betterwright run --session a1 -c "<js>"`), one
// JSON envelope back; the daemon owns the browser lifecycle, the profile
// directories, and the cookie jars. playwright-go left the module on
// purpose — BetterWright is an optional external runtime, not a Go
// dependency, which keeps the lean-deps constraint intact.
//
// Session vs profile: a1 uses one session lane ("a1") inside a profile, so
// every call in a profile is strictly ordered — the same serialization the
// browserMu already enforces. Profiles are identities: separate cookie
// jars, separate daemons, on-disk under $BETTERWRIGHT_HOME/browser/profiles
// (the shared default profile lives at browser/profile instead). Closing a
// session keeps the profile and its logins on disk.
package bw

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/project"
)

// Session is the a1 lane inside a BetterWright profile: the interactive
// browser surface (the /browser command and the model's browser tool) runs
// here.
const Session = "a1"

// BackgroundSession is the lane background traffic uses (the fetch tool's
// search fallback, PDF rendering): a DIFFERENT lane from the user's
// interactive browser, so a background Mojeek query never navigates the
// page the user is looking at, and a cold headless spawn for background
// work is attributable — the interactive path can see it happened and
// recover (stop + respawn headed) before showing the user a browser.
const BackgroundSession = "a1-bg"

// BackgroundProfile is the dedicated BetterWright profile for background
// search traffic. A profile owns its own daemon, so routing the fetch
// fallback here isolates its headless spawns from the default profile the
// interactive browser uses — a background search can no longer cold-spawn
// the shared daemon headless and make "/browser open" land in an invisible
// window. It also keeps background scraping off the user's logged-in
// identity, which the search fallback never needed anyway.
const BackgroundProfile = "a1-bg"

// runTimeout bounds one CLI call: covers a cold daemon spawn plus a slow
// page without letting a hung browser stall the agent loop forever.
const runTimeout = 120 * time.Second

// DefaultProfile names a1's default profile, which maps to BetterWright's
// shared profile (no --profile flag) rather than a directory under
// browser/profiles.
const DefaultProfile = "default"

// Envelope is the single JSON object `betterwright run` prints.
type Envelope struct {
        Ok         bool            `json:"ok"`
        Result     json.RawMessage `json:"result"`
        Error      string          `json:"error,omitempty"`
        Challenges []Challenge     `json:"challenges,omitempty"`
}

// Challenge is one detected bot check (page, provider, where, what to do).
type Challenge struct {
        Page     string `json:"page,omitempty"`
        Provider string `json:"provider,omitempty"`
        URL      string `json:"url,omitempty"`
        Advice   string `json:"advice,omitempty"`
}

// HandoffReason renders the first challenge as the human-facing reason for
// a handoff ask. Empty when no challenge is present.
func (e Envelope) HandoffReason() string {
        if len(e.Challenges) == 0 {
                return ""
        }
        c := e.Challenges[0]
        where := c.URL
        if where == "" {
                where = c.Page
        }
        reason := "bot challenge"
        if c.Provider != "" {
                reason += " (" + c.Provider + ")"
        }
        if where != "" {
                reason += " at " + where
        }
        if c.Advice != "" {
                reason += ": " + c.Advice
        }
        return reason
}

// RunCommand executes the betterwright CLI and returns stdout. Var so tests
// swap in a fake daemon; production uses exec.
var RunCommand = func(ctx context.Context, bin string, args []string, env []string) ([]byte, error) {
        cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: the binary is resolved, args are ours
        cmd.Env = env
        var stdout, stderr bytes.Buffer
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
                if stdout.Len() == 0 && stderr.Len() > 0 {
                        return nil, fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
                }
                return stdout.Bytes(), err
        }
        return stdout.Bytes(), nil
}

var (
        resolveOnce sync.Once
        binPath     string
        resolveErr  error
        proxyMu     sync.Mutex
        proxyURL    string

        // spawnMu guards spawnModes: which mode (headed/headless) each
        // profile's daemon was last spawned in BY THIS PROCESS. The
        // interactive browser path consults it to detect "a background
        // lane cold-spawned the daemon headless" — a warm daemon ignores
        // the --headed flag, so the recovery is close + respawn. Process-
        // local by design: a daemon warm from an earlier process is
        // indistinguishable from a cold one, and the fallback for that
        // case is the user's /browser close + open.
        spawnMu   sync.Mutex
        spawnModes map[string]bool // profile → last spawn was headed
)

// noteSpawn records the mode a profile's daemon was spawned in.
func noteSpawn(profile string, headed bool) {
        spawnMu.Lock()
        defer spawnMu.Unlock()
        if spawnModes == nil {
                spawnModes = make(map[string]bool)
        }
        spawnModes[normalizeProfile(profile)] = headed
}

// LastSpawnHeaded reports whether the profile's daemon was last spawned
// HEADED by this process, and whether this process spawned it at all.
func LastSpawnHeaded(profile string) (headed, spawned bool) {
        spawnMu.Lock()
        defer spawnMu.Unlock()
        headed, spawned = spawnModes[normalizeProfile(profile)]
        return headed, spawned
}

// ResetSpawnTracking drops the process's spawn-mode knowledge (used after
// a close-all: every daemon is gone, so the next spawn decides the mode).
func ResetSpawnTracking() {
        spawnMu.Lock()
        defer spawnMu.Unlock()
        spawnModes = nil
}

// normalizeProfile maps ""/"default" to the shared key the spawn map uses.
func normalizeProfile(profile string) string {
        if p := strings.TrimSpace(profile); p != "" && p != DefaultProfile {
                return p
        }
        return DefaultProfile
}

// Resolve returns the betterwright binary path: a1's managed bin dir
// (~/.a1/bin) first, then PATH. Cached after the first call; tests swap the
// var or reset the cache with ResetForTest.
var Resolve = func() (string, error) {
        resolveOnce.Do(func() {
                if proj := project.GetDefaultProject(); proj != nil {
                        if bin, err := proj.Global().LookBin("betterwright"); err == nil {
                                binPath, resolveErr = bin, nil
                                return
                        }
                }
                if bin, err := exec.LookPath("betterwright"); err == nil {
                        binPath, resolveErr = bin, nil
                        return
                }
                binPath, resolveErr = "", notInstalledError()
        })
        return binPath, resolveErr
}

// ResetForTest clears the cached binary resolution.
func ResetForTest() {
        resolveOnce = sync.Once{}
}

// Available reports whether the betterwright CLI is resolvable.
func Available() bool {
        _, err := Resolve()
        return err == nil
}

func notInstalledError() error {
        return errors.New("betterwright is not installed — run `make betterwright-install` " +
                "(installs the pinned CLI and downloads the browser), or `bun install -g betterwright && betterwright setup` " +
                "manually; a1 pins the version in internal/toolmanager (BunTools); " +
                "see https://github.com/BetterWright/betterwright")
}

// Home returns the BetterWright home directory ($BETTERWRIGHT_HOME or
// ~/.betterwright).
func Home() string {
        if h := strings.TrimSpace(os.Getenv("BETTERWRIGHT_HOME")); h != "" {
                return h
        }
        home, err := os.UserHomeDir()
        if err != nil {
                return ".betterwright"
        }
        return filepath.Join(home, ".betterwright")
}

// ProfilesDir is where named profiles live. The shared default profile does
// NOT live here (it is browser/profile), so "default" is special-cased by
// callers.
func ProfilesDir() string {
        return filepath.Join(Home(), "browser", "profiles")
}

// ProfileDir is the on-disk directory of a named profile ("" for default).
func ProfileDir(name string) string {
        return filepath.Join(ProfilesDir(), name)
}

// SetProxy stores the proxy URL applied (best effort) to CLI spawns via
// HTTP(S)_PROXY/ALL_PROXY environment variables — Chromium on Linux honors
// them; other platforms pick proxies up from system settings. "" disables.
func SetProxy(url string) {
        proxyMu.Lock()
        defer proxyMu.Unlock()
        proxyURL = strings.TrimSpace(url)
}

// ProxyEnv returns os.Environ plus the proxy variables when one is set.
func ProxyEnv() []string {
        proxyMu.Lock()
        url := proxyURL
        proxyMu.Unlock()
        env := os.Environ()
        if url == "" {
                return env
        }
        return append(env,
                "HTTP_PROXY="+url,
                "HTTPS_PROXY="+url,
                "ALL_PROXY="+url,
        )
}

// Run executes one JS snippet in the persistent interactive session and
// decodes the result envelope. profile "" or "default" uses the shared
// profile. An ok:false envelope is returned as an error (challenges ride
// the message); transport and parse failures are errors too.
func Run(ctx context.Context, profile string, headed bool, js string) (Envelope, error) {
        return RunLane(ctx, Session, profile, headed, js)
}

// RunLane executes one JS snippet in a named session lane. lane "a1" is
// the interactive browser; background callers (fetch search, PDF render)
// pass BackgroundSession so their page state never interleaves with the
// user's. Headless spawns are recorded per profile so the interactive path
// can detect and recover a daemon that a background lane froze headless.
func RunLane(ctx context.Context, lane, profile string, headed bool, js string) (Envelope, error) {
        if ctx == nil {
                ctx = context.Background()
        }
        bin, err := Resolve()
        if err != nil {
                return Envelope{}, err
        }
        noteSpawn(profile, headed)
        ctx, cancel := context.WithTimeout(ctx, runTimeout)
        defer cancel()

        if strings.TrimSpace(lane) == "" {
                lane = Session
        }
        args := []string{"run", "--session", lane}
        if p := strings.TrimSpace(profile); p != "" && p != DefaultProfile {
                args = append(args, "--profile", p)
        }
        if headed {
                args = append(args, "--headed")
        }
        args = append(args, "-c", js)

        out, err := RunCommand(ctx, bin, args, ProxyEnv())
        env := Envelope{}
        if len(out) > 0 {
                if jerr := json.Unmarshal(bytes.TrimSpace(out), &env); jerr == nil {
                        if env.Ok {
                                return env, nil
                        }
                        if env.Error == "" {
                                env.Error = strings.TrimSpace(string(out))
                        }
                        return env, fmt.Errorf("betterwright: %s", env.Error)
                }
        }
        if err != nil {
                return env, fmt.Errorf("betterwright run: %w", err)
        }
        return env, fmt.Errorf("betterwright run: unparseable output: %.200s", strings.TrimSpace(string(out)))
}

// Close ends the session. all=true stops every profile's daemon (open tabs
// go away; the on-disk profiles and their logins stay). Otherwise one
// session of one profile closes. A close-all also drops the process's
// spawn-mode knowledge: every daemon is gone, so the next spawn decides
// the mode fresh.
func Close(ctx context.Context, all bool, profile string) error {
        if ctx == nil {
                ctx = context.Background()
        }
        bin, err := Resolve()
        if err != nil {
                return err
        }
        ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()

        args := []string{"close"}
        if all {
                args = append(args, "--all")
                ResetSpawnTracking()
        } else {
                args = append(args, "--session", Session)
                if p := strings.TrimSpace(profile); p != "" && p != DefaultProfile {
                        args = append(args, "--profile", p)
                }
        }
        _, err = RunCommand(ctx, bin, args, ProxyEnv())
        return err
}

// JSString renders s as a single-quoted JavaScript string literal.
func JSString(s string) string {
        var b strings.Builder
        b.WriteByte('\'')
        for _, r := range s {
                switch r {
                case '\'':
                        b.WriteString("\\'")
                case '\\':
                        b.WriteString("\\\\")
                case '\n':
                        b.WriteString("\\n")
                case '\r':
                        b.WriteString("\\r")
                case '\t':
                        b.WriteString("\\t")
                default:
                        b.WriteRune(r)
                }
        }
        b.WriteByte('\'')
        return b.String()
}
