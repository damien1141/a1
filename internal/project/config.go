package project

import (
        "errors"
        "fmt"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "time"

        "gopkg.in/yaml.v3"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/llm/jev"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/project/model"
        "github.com/damien1141/a1/internal/schedule"
)

// Config is the project-level configuration loaded from ~/.a1/config.yaml.
// All models live in one flat list under the models key; DefaultModel names
// the entry used to start sessions (empty → the first entry).
type Config struct {
        Models         []llm.ModelConfig
        DefaultModel   string // name of the default model; "" → first entry
        SkillPath      string
        Permissions    permission.Policy
        Agents         AgentsConfig
        SemanticSearch SemanticSearchConfig
        Jev            jev.Settings
        Browser        BrowserConfig
        Personal       PersonalConfig
        UI             UIConfig
        Web            WebConfig
        Personas       *PersonasConfig
        Tools          ToolsConfig
        Checks         []CheckConfig
        Schedules      []ScheduleConfig
}

// CheckConfig defines one config-defined evidence check: a named command
// whose completed runs record `check:<name>` evidence in the ledger. The
// contract tool can then declare `check:<name>` as a done-criterion and
// the harness judges it like any built-in check — five lines of config
// teaches the ledger a custom stack.
//
//      checks:
//        - name: lint
//          command: "make lint"
//        - name: health
//          command: "curl -s localhost:3000/health"
//          pattern: '"status":"ok"'
type CheckConfig struct {
        Name    string `yaml:"name"`
        Command string `yaml:"command"`
        // Success is the expected exit status in "exit:<n>" form; empty
        // defaults to exit:0.
        Success string `yaml:"success,omitempty"`
        // Pattern is an optional success regex over the combined output; a
        // passing run must match it in addition to the exit status.
        Pattern string `yaml:"pattern,omitempty"`
}

// EvidenceChecks converts the config-defined checks into ledger check
// definitions. Entries with a missing name or command are dropped (a
// half-written check teaches nothing); a malformed success form falls
// back to exit:0.
func (c *Config) EvidenceChecks() []evidence.CheckDef {
        if c == nil || len(c.Checks) == 0 {
                return nil
        }
        defs := make([]evidence.CheckDef, 0, len(c.Checks))
        for _, cc := range c.Checks {
                name := strings.TrimSpace(cc.Name)
                command := strings.TrimSpace(cc.Command)
                if name == "" || command == "" {
                        continue
                }
                def := evidence.CheckDef{Name: name, Command: command, Pattern: cc.Pattern}
                if rest, ok := strings.CutPrefix(strings.TrimSpace(cc.Success), "exit:"); ok {
                        if n, err := strconv.Atoi(rest); err == nil && n >= 0 {
                                def.Exit = n
                        }
                }
                defs = append(defs, def)
        }
        if len(defs) == 0 {
                return nil
        }
        return defs
}

// ScheduleConfig declares one wake-up schedule (Bet 01 Clock) in
// config.yaml:
//
//      schedules:
//        - id: nightly-lint
//          prompt: "Run make lint and fix new warnings."
//          cron: "30 3 * * *"
//          # or fixed_rate: 15m
//          # or one_time: 2026-12-01T09:00:00Z
//          role: worker        # default: worker (headless-strict)
//          contract: [go-build, go-test:...]
//          timeout_sec: 1800
//          enabled: true       # default true; false removes it from the active set
//
// Exactly one trigger (cron / fixed_rate / one_time) per entry. Entries are
// synced into the vault's schedules table at boot; config wins id conflicts.
type ScheduleConfig struct {
        ID          string   `yaml:"id"`
        Prompt      string   `yaml:"prompt"`
        Description string   `yaml:"description,omitempty"`
        Cron        string   `yaml:"cron,omitempty"`
        FixedRate   string   `yaml:"fixed_rate,omitempty"`
        OneTime     string   `yaml:"one_time,omitempty"`
        WorkDir     string   `yaml:"workdir,omitempty"`
        Role        string   `yaml:"role,omitempty"`
        Contract    []string `yaml:"contract,omitempty"`
        TimeoutSec  int      `yaml:"timeout_sec,omitempty"`
        Enabled     *bool    `yaml:"enabled,omitempty"`
}

// ScheduleDefs converts config-declared schedules into scheduler entries.
// Entries missing a prompt or id are dropped (a half-written schedule
// teaches nothing); a malformed trigger fails loud — a typo'd cron that
// silently never fires is exactly the failure the Clock exists to kill.
// enabled:false entries are dropped from the active set (Sync's orphan
// pass disables whatever row the id previously owned).
func (c *Config) ScheduleDefs() ([]schedule.Schedule, error) {
        if c == nil || len(c.Schedules) == 0 {
                return nil, nil
        }
        var defs []schedule.Schedule
        for i, sc := range c.Schedules {
                id := strings.TrimSpace(sc.ID)
                prompt := strings.TrimSpace(sc.Prompt)
                if id == "" || prompt == "" {
                        continue
                }
                if sc.Enabled != nil && !*sc.Enabled {
                        continue
                }
                trigger, err := schedule.ParseTrigger(sc.Cron, sc.FixedRate, sc.OneTime)
                if err != nil {
                        return nil, fmt.Errorf("schedules[%d] (%s): %w", i, id, err)
                }
                def := schedule.Schedule{
                        ID:          id,
                        Prompt:      prompt,
                        Description: strings.TrimSpace(sc.Description),
                        Trigger:     trigger,
                        WorkDir:     strings.TrimSpace(sc.WorkDir),
                        Role:        strings.TrimSpace(sc.Role),
                        Contract:    sc.Contract,
                        Source:      "config",
                }
                if sc.TimeoutSec > 0 {
                        def.Timeout = time.Duration(sc.TimeoutSec) * time.Second
                }
                defs = append(defs, def)
        }
        return defs, nil
}

// ToolsConfig holds tool-execution limits.
type ToolsConfig struct {
        // MaxToolCalls caps executed tool calls per user turn. 0 (the default,
        // and what a missing key means) = unlimited. Yolo permission mode
        // ignores the cap entirely.
        MaxToolCalls int
}

// PersonasConfig stores the active persona ID at the config-file level. The
// persona registry itself (with avatars and per-persona memory banks) lives
// under ~/.a1/personas/.
type PersonasConfig struct {
        Active string `yaml:"active,omitempty"`
}

// AgentsConfig controls whether the main agent may spawn sub-agents
// (agent_spawn / agent_wait / …). Default is enabled; set enabled: false
// to keep ordinary sessions lean and avoid loading the extra tool schemas.
type AgentsConfig struct {
        Enabled bool // true when absent from config
        // Models names optional per-role defaults (explore|review|worker).
        // Empty → inherit the parent session model.
        Models AgentsRoleModels
}

// AgentsRoleModels maps sub-agent roles to configured model names.
type AgentsRoleModels struct {
        Explore string `yaml:"explore"`
        Review  string `yaml:"review"`
        Worker  string `yaml:"worker"`
}

// SemanticSearchConfig controls the local semantic code search index.
type SemanticSearchConfig struct {
        Enabled        bool   `yaml:"enabled"`
        OllamaBaseURL  string `yaml:"ollama_base_url"`
        EmbeddingModel string `yaml:"embedding_model"`
}

// BrowserConfig controls browser tool behavior and profiles.
type BrowserConfig struct {
        Profiles []string `yaml:"profiles"`
        // Proxy is an optional HTTP/HTTPS/SOCKS proxy URL used by the browser, e.g.
        // "http://127.0.0.1:8885". Empty → no proxy. Set at launch time, so the
        // browser must be closed and reopened for a change to take effect.
        Proxy string `yaml:"proxy,omitempty"`
        // Headless overrides the display probe for the interactive browser:
        // true forces an invisible daemon even on a desktop (CI, shared
        // machines); false forces a visible window even where DISPLAY/
        // WAYLAND_DISPLAY were not inherited (tmux over SSH). nil → detect
        // (headed when a graphical session is reachable). The A1_BROWSER_HEADLESS
        // env var still wins over this for per-invocation control.
        Headless *bool `yaml:"headless,omitempty"`
}

// PersonalConfig is the personal-data section (personal:): the dashboard's
// calendar / kanban / mail widgets and the agent-side personal tools all
// hang off one master switch, plus the sources the calendar and mail
// surfaces read. The calendar is deliberately local-file based; the mail
// surface reads local Maildirs AND remote IMAP accounts (chunk 7 — the
// v1 "local only" mail ruling was relaxed by the user's ask for multiple
// accounts and mail servers; everything stays READ-ONLY either way).
type PersonalConfig struct {
        // Enabled is the master switch for personal-data surfaces. Nil means
        // off: personal data stays locked until the user opts in — the section
        // existing in config.yaml is not consent.
        Enabled *bool `yaml:"enabled,omitempty"`
        // CalendarDir is a local directory of .ics files the calendar widget
        // (and, when the model is opted in, the calendar tool) reads. Empty =
        // the feature reports "not configured".
        CalendarDir string `yaml:"calendar_dir,omitempty"`
        // MailDir is a local Maildir root the mail surface reads (read-only,
        // always — flags are never written back). Empty = not configured.
        // Legacy single-root spelling: it feeds ResolvedMailAccounts as one
        // account named "local", so the two spellings compose.
        MailDir string `yaml:"mail_dir,omitempty"`
        // MailAccounts is the multi-account mail list (chunk 7): any mix of
        // maildir roots and IMAP servers. Every account is read-only on every
        // surface — the web reader shows bodies, the agent facade keeps to
        // envelopes, and nothing anywhere writes back.
        MailAccounts []MailAccount `yaml:"mail_accounts,omitempty"`
}

// MailAccount is one mail source: a local Maildir root or a remote IMAP
// server. The two kinds share one shape so the web widget can treat them
// identically — Type picks the reader, the rest fills in per kind.
type MailAccount struct {
        // Name is the display label and the ?account= key on the mail
        // endpoints. Empty names default to the root's basename (maildir)
        // or the host (imap); collisions get numeric suffixes at resolve
        // time, so the key is always unique.
        Name string `yaml:"name,omitempty"`
        // Type is "maildir" (the default) or "imap". Anything else degrades
        // that account to an honest per-account error, never a 500.
        Type string `yaml:"type,omitempty"`
        // Root is the Maildir root directory (maildir accounts).
        Root string `yaml:"root,omitempty"`
        // Host, Port, Username, Password are the IMAP server's address and
        // credentials (imap accounts). Port defaults to 993 with TLS on,
        // 143 with it off. The password sits in config.yaml beside the
        // model api_keys — the config file is already this project's
        // secret store.
        Host     string `yaml:"host,omitempty"`
        Port     int    `yaml:"port,omitempty"`
        Username string `yaml:"username,omitempty"`
        Password string `yaml:"password,omitempty"`
        // TLS selects implicit TLS (the default) for imap accounts. There is
        // no starttls spelling: implicit TLS or plaintext, stated plainly.
        TLS *bool `yaml:"tls,omitempty"`
        // Mailbox is the box the widget opens first for this account.
        // Empty = "INBOX" (the IMAP convention; the maildir reader maps it
        // to the box literally named inbox when INBOX is absent).
        Mailbox string `yaml:"mailbox,omitempty"`
}

// ResolvedMailAccounts normalizes the mail configuration into the account
// list the surfaces read: the legacy mail_dir (when set and not duplicated
// by an explicit account) is PREPENDED as "local" so an existing setup's
// widget default does not move, names are defaulted and de-duplicated, and
// types default to maildir. Resolution is lenient — a malformed account
// (an imap entry with no host, say) stays in the list and degrades to an
// honest per-account error at read time; configuration mistakes must be
// visible, never fatal to the whole surface.
func (c PersonalConfig) ResolvedMailAccounts() []MailAccount {
        var out []MailAccount
        if root := strings.TrimSpace(c.MailDir); root != "" {
                out = append(out, MailAccount{Name: "local", Type: "maildir", Root: root})
        }
        for i, a := range c.MailAccounts {
                r := a
                if strings.TrimSpace(r.Type) == "" {
                        r.Type = "maildir"
                }
                r.Type = strings.ToLower(strings.TrimSpace(r.Type))
                r.Name = strings.TrimSpace(r.Name)
                if r.Name == "" {
                        switch {
                        case r.Type == "imap" && strings.TrimSpace(r.Host) != "":
                                r.Name = strings.TrimSpace(r.Host)
                        case strings.TrimSpace(r.Root) != "":
                                r.Name = filepath.Base(strings.TrimSpace(r.Root))
                        default:
                                r.Name = fmt.Sprintf("account-%d", i+1)
                        }
                }
                out = append(out, r)
        }
        // De-duplicate names: the ?account= key must be unambiguous. First
        // occurrence keeps the bare name; later twins get "-2", "-3", …
        seen := map[string]int{}
        for i := range out {
                n := out[i].Name
                if _, dup := seen[n]; dup {
                        seen[n]++
                        out[i].Name = fmt.Sprintf("%s-%d", n, seen[n])
                } else {
                        seen[n] = 1
                }
        }
        return out
}

// EnabledResolved resolves the master switch, defaulting to LOCKED when
// the key is absent: personal data is opt-in, never opt-out.
func (c PersonalConfig) EnabledResolved() bool {
        if c.Enabled == nil {
                return false
        }
        return *c.Enabled
}

// UIConfig controls transcript presentation.
type UIConfig struct {
        // AutoExpandToolOutput starts file-editing rows expanded, so a diff is
        // visible without a keypress. Read/grep rows stay collapsed to one line and
        // bash rows stay a tail preview regardless — they are the highest-volume
        // rows and their bodies are one Enter away. Individual rows can always be
        // collapsed or opened, and the choice sticks for the session. Defaults to
        // true; set `auto_expand_tool_output: false` to opt out.
        AutoExpandToolOutput *bool `yaml:"auto_expand_tool_output"`
}

// AutoExpandToolOutput resolves the setting, defaulting to true when the key is
// absent so a fresh config shows tool output without extra setup.
func (c UIConfig) AutoExpandToolOutputResolved() bool {
        if c.AutoExpandToolOutput == nil {
                return true
        }
        return *c.AutoExpandToolOutput
}

// WebConfig is the web GUI's section of config.yaml.
type WebConfig struct {
        // AutoNameSessions titles a session with the memory-ops model right
        // after its first exchange (the sidebar and topbar show the generated
        // name instead of the first-message preview). Nil = on.
        AutoNameSessions *bool `yaml:"auto_name_sessions"`
        // DarkMode is the web GUI's persisted theme choice, written by the
        // theme button (and the Settings picker) via PUT /api/theme: true =
        // dark, false = light, nil = no choice — the GUI follows the OS
        // preference. It exists so a choice made in one browser reaches
        // every other one (and the /settings config screen, which reads the
        // file server-side): localStorage is per-browser and dies with it.
        DarkMode *bool `yaml:"dark_mode,omitempty"`
}

// AutoNameSessionsResolved resolves the toggle, defaulting to on: smart
// names are the feature's whole point, and opting out is the explicit act.
func (c WebConfig) AutoNameSessionsResolved() bool {
        if c.AutoNameSessions == nil {
                return true
        }
        return *c.AutoNameSessions
}

func (c *Config) Model() llm.ModelConfig {
        m := *c.defaultEntry()
        if m.SkillPath == "" {
                m.SkillPath = c.SkillPath
        }
        return m
}

// AllModels returns every configured model with the skill path applied — the
// complete set of switchable models.
func (c *Config) AllModels() []llm.ModelConfig {
        all := make([]llm.ModelConfig, len(c.Models))
        copy(all, c.Models)
        for i := range all {
                if all[i].SkillPath == "" {
                        all[i].SkillPath = c.SkillPath
                }
        }
        return all
}

// FindModel returns the configured model whose name matches, so callers can
// switch to it with its own api_key/base_url/context_window.
func (c *Config) FindModel(name string) (llm.ModelConfig, bool) {
        for _, m := range c.AllModels() {
                if m.Name == name {
                        return m, true
                }
        }
        return llm.ModelConfig{}, false
}

// defaultEntry returns the default model entry (DefaultModel by name, else
// the first entry), creating one if the config has no models yet so env-only
// setups can still apply A1_* overrides.
func (c *Config) defaultEntry() *llm.ModelConfig {
        if c.DefaultModel != "" {
                for i := range c.Models {
                        if c.Models[i].Name == c.DefaultModel {
                                return &c.Models[i]
                        }
                }
        }
        if len(c.Models) > 0 {
                return &c.Models[0]
        }
        c.Models = append(c.Models, llm.ModelConfig{})
        return &c.Models[0]
}

// loadConfig reads the config file, applies environment overrides, and fills
// in defaults. A missing file yields a zero Config so env-only setups work.
func loadConfig(global GlobalLayout) (*Config, error) {
        cfg, err := parseConfigFile(global.ConfigFile())
        if err != nil {
                return nil, err
        }
        applyEnvOverrides(cfg)

        if len(cfg.Models) == 0 {
                return nil, fmt.Errorf("missing models (add at least one model in %s)", global.ConfigFile())
        }
        def := cfg.defaultEntry()
        if def.Name == "" {
                return nil, fmt.Errorf("missing model name (set A1_MODEL or models[].name in %s)", global.ConfigFile())
        }
        if def.APIKey == "" {
                return nil, fmt.Errorf("missing api_key (set A1_API_KEY or models[].api_key in %s)", global.ConfigFile())
        }
        if cfg.SkillPath == "" {
                cfg.SkillPath = global.SkillsDir()
        }
        // Push the jev: section to the decision-model package. Every
        // surface that loads config — TUI boot, web GUI boot, the web
        // GUI's save→reload→next-turn flow — funnels through here, so the
        // two wired call sites (fetch classify, evidence verdict) resolve
        // the current settings per decision without their own plumbing.
        // A missing section stores zero settings: the TYPESAFE_* env
        // fallback applies, preserving env-only setups.
        jev.Configure(cfg.Jev)
        return cfg, nil
}

// parseConfigFile reads models, skill_path, and permissions from the YAML
// config file. A missing file yields a zero Config with DefaultPolicy for
// permissions; a malformed file is an error so bad config never silently
// degrades to defaults.
func parseConfigFile(path string) (*Config, error) {
        cfg := &Config{Permissions: permission.DefaultPolicy(), Agents: AgentsConfig{Enabled: true}}

        data, err := os.ReadFile(path)
        if err != nil {
                if os.IsNotExist(err) {
                        return cfg, nil
                }
                return nil, fmt.Errorf("read config %s: %w", path, err)
        }

        // Pointer fields distinguish "key absent" from "zero value", so per-key
        // defaults (and permission.DefaultPolicy) survive decoding and are only
        // overridden by keys that are actually present.
        var raw fileConfig
        if err := yaml.Unmarshal(data, &raw); err != nil {
                return nil, fmt.Errorf("parse %s: %w", path, err)
        }

        for _, m := range raw.Models {
                mc := modelEntryToConfig(m)
                if m.Default && cfg.DefaultModel == "" {
                        cfg.DefaultModel = mc.Name
                }
                cfg.Models = append(cfg.Models, mc)
        }
        if raw.SkillPath != nil {
                cfg.SkillPath = *raw.SkillPath
        }
        if raw.Permissions != nil {
                applyPermissions(&cfg.Permissions, raw.Permissions)
        }
        if raw.Agents != nil {
                if raw.Agents.Enabled != nil {
                        cfg.Agents.Enabled = *raw.Agents.Enabled
                }
                if raw.Agents.Models != nil {
                        cfg.Agents.Models = AgentsRoleModels{
                                Explore: strings.TrimSpace(raw.Agents.Models.Explore),
                                Review:  strings.TrimSpace(raw.Agents.Models.Review),
                                Worker:  strings.TrimSpace(raw.Agents.Models.Worker),
                        }
                }
        }
        if raw.SemanticSearch != nil {
                cfg.SemanticSearch = SemanticSearchConfig{
                        Enabled:        raw.SemanticSearch.Enabled,
                        OllamaBaseURL:  strings.TrimSpace(raw.SemanticSearch.OllamaBaseURL),
                        EmbeddingModel: strings.TrimSpace(raw.SemanticSearch.EmbeddingModel),
                }
        }
        if raw.Jev != nil {
                cfg.Jev = jev.Settings{
                        Provider:      strings.ToLower(strings.TrimSpace(raw.Jev.Provider)),
                        APIKey:        strings.TrimSpace(raw.Jev.APIKey),
                        BaseURL:       strings.TrimSpace(raw.Jev.BaseURL),
                        Model:         strings.TrimSpace(raw.Jev.Model),
                        OllamaBaseURL: strings.TrimSpace(raw.Jev.OllamaBaseURL),
                        TimeoutSec:    raw.Jev.TimeoutSec,
                }
        }
        if raw.Browser != nil {
                cfg.Browser = BrowserConfig{
                        Profiles: append([]string(nil), raw.Browser.Profiles...),
                        Proxy:    strings.TrimSpace(raw.Browser.Proxy),
                        Headless: raw.Browser.Headless,
                }
        }
        if raw.Personal != nil {
                cfg.Personal = PersonalConfig{
                        Enabled:     raw.Personal.Enabled,
                        CalendarDir: ExpandHome(strings.TrimSpace(raw.Personal.CalendarDir)),
                        MailDir:     ExpandHome(strings.TrimSpace(raw.Personal.MailDir)),
                }
                for _, a := range raw.Personal.MailAccounts {
                        cfg.Personal.MailAccounts = append(cfg.Personal.MailAccounts, MailAccount{
                                Name:     strings.TrimSpace(a.Name),
                                Type:     strings.TrimSpace(a.Type),
                                Root:     ExpandHome(strings.TrimSpace(a.Root)),
                                Host:     strings.TrimSpace(a.Host),
                                Port:     a.Port,
                                Username: strings.TrimSpace(a.Username),
                                Password: a.Password,
                                TLS:      a.TLS,
                                Mailbox:  strings.TrimSpace(a.Mailbox),
                        })
                }
        }
        if raw.UI != nil {
                cfg.UI = UIConfig{AutoExpandToolOutput: raw.UI.AutoExpandToolOutput}
        }
        if raw.Web != nil {
                cfg.Web = WebConfig{AutoNameSessions: raw.Web.AutoNameSessions, DarkMode: raw.Web.DarkMode}
        }
        if raw.Personas != nil {
                cfg.Personas = &PersonasConfig{Active: strings.TrimSpace(raw.Personas.Active)}
        }
        if raw.Tools != nil && raw.Tools.MaxToolCalls != nil && *raw.Tools.MaxToolCalls > 0 {
                cfg.Tools.MaxToolCalls = *raw.Tools.MaxToolCalls
        }
        cfg.Checks = raw.Checks
        cfg.Schedules = raw.Schedules
        return cfg, nil
}

func modelEntryToConfig(m modelEntry) llm.ModelConfig {
        cfg := llm.ModelConfig{Name: m.Name, APIKey: m.APIKey, BaseURL: m.BaseURL}
        // api_keys are the credential pool's extras beyond the single api_key
        // (ModelConfig.APIKeys is extras-only; consumers union it with APIKey).
        // api_key stays required — it is the no-pool default and the
        // configserver's validation anchor.
        if len(m.APIKeys) > 0 {
                cfg.APIKeys = append([]string(nil), m.APIKeys...)
        }
        // A built-in preset supplies base_url / context_window / image_enabled / api
        // when the entry omits them; the explicit fields below still win so
        // users can override any default.
        if preset, ok := model.Lookup(m.Name); ok {
                pc := preset.Config
                if cfg.BaseURL == "" {
                        cfg.BaseURL = pc.BaseURL
                }
                if pc.ContextWindow > 0 {
                        cfg.ContextWindow = pc.ContextWindow
                }
                cfg.ImageEnabled = pc.ImageEnabled
                if cfg.API == "" {
                        cfg.API = pc.API
                }
                // Inherit the preset's thinking config so models like deepseek-flash
                // ship with sensible defaults (thinking enabled, high mode).
                cfg.Think = pc.Think
        }
        if m.API != "" {
                cfg.API = m.API
        }
        if m.ContextWindow != nil && *m.ContextWindow > 0 {
                cfg.ContextWindow = *m.ContextWindow
        }
        if m.ImageEnabled != nil {
                cfg.ImageEnabled = *m.ImageEnabled
        }
        if m.ThinkEnabled != nil {
                cfg.Think.Enabled = *m.ThinkEnabled
        }
        if m.ThinkLevel != nil && *m.ThinkLevel != "" {
                mode := llm.ThinkMode(*m.ThinkLevel)
                cfg.Think.Mode = mode
                cfg.Think.Enabled = mode != llm.Off
        }
        if m.PersonalRead != nil {
                cfg.PersonalRead = *m.PersonalRead
        }
        if m.PersonalWrite != nil {
                cfg.PersonalWrite = *m.PersonalWrite
        }
        if cfg.BaseURL == "" {
                cfg.BaseURL = "https://api.openai.com/v1"
        }
        return cfg
}

// fileConfig mirrors the YAML keys in ~/.a1/config.yaml.
type fileConfig struct {
        Models         []modelEntry          `yaml:"models"`
        SkillPath      *string               `yaml:"skill_path"`
        Permissions    *permConfig           `yaml:"permissions"`
        Agents         *agentsConfig         `yaml:"agents"`
        SemanticSearch *semanticSearchConfig `yaml:"semantic_search"`
        Jev            *jevConfig            `yaml:"jev"`
        Browser        *browserConfig        `yaml:"browser"`
        Personal       *personalConfig       `yaml:"personal"`
        UI             *uiConfig             `yaml:"ui"`
        Web            *webConfig            `yaml:"web"`
        Personas       *personasConfig       `yaml:"personas"`
        Tools          *toolsConfig          `yaml:"tools"`
        Checks         []CheckConfig         `yaml:"checks"`
        Schedules      []ScheduleConfig      `yaml:"schedules"`
}

type personasConfig struct {
        Active string `yaml:"active,omitempty"`
}

// toolsConfig mirrors the optional tools block. MaxToolCalls is a pointer so
// an absent key stays unlimited rather than reading as an explicit 0.
type toolsConfig struct {
        MaxToolCalls *int `yaml:"max_tool_calls"`
}

type agentsConfig struct {
        // Enabled is a pointer so omitting the key keeps the default (on).
        Enabled *bool             `yaml:"enabled"`
        Models  *AgentsRoleModels `yaml:"models"`
}

type semanticSearchConfig struct {
        Enabled        bool   `yaml:"enabled"`
        OllamaBaseURL  string `yaml:"ollama_base_url"`
        EmbeddingModel string `yaml:"embedding_model"`
}

type webConfig struct {
        AutoNameSessions *bool `yaml:"auto_name_sessions"`
        DarkMode         *bool `yaml:"dark_mode"`
}

// jevConfig mirrors the jev: block — the decision-model backend shared
// by the judge tool, fetch routing, evidence verdicts, compaction
// boundaries, and the bash risk guard. Pointer fields
// on fileConfig distinguish "no jev: section" (env fallback applies) from
// a present one. Provider is normalized to lowercase at parse.
type jevConfig struct {
        Provider      string `yaml:"provider"`
        APIKey        string `yaml:"api_key"`
        BaseURL       string `yaml:"base_url"`
        Model         string `yaml:"model"`
        OllamaBaseURL string `yaml:"ollama_base_url"`
        TimeoutSec    int    `yaml:"timeout_sec"`
}

type browserConfig struct {
        Profiles []string `yaml:"profiles"`
        Proxy    string   `yaml:"proxy,omitempty"`
        Headless *bool    `yaml:"headless,omitempty"`
}

// personalConfig mirrors the personal: block. Paths are trimmed and
// home-expanded at parse (expandHome), so runtime consumers never see a
// literal ~; the config editor, which round-trips the file directly,
// preserves whatever spelling the user wrote.
type personalConfig struct {
        Enabled      *bool         `yaml:"enabled,omitempty"`
        CalendarDir  string        `yaml:"calendar_dir,omitempty"`
        MailDir      string        `yaml:"mail_dir,omitempty"`
        MailAccounts []MailAccount `yaml:"mail_accounts,omitempty"`
}

type uiConfig struct {
        AutoExpandToolOutput *bool `yaml:"auto_expand_tool_output"`
}

type modelEntry struct {
        Name          string         `yaml:"name"`
        APIKey        string         `yaml:"api_key"`
        APIKeys       stringList     `yaml:"api_keys"`
        BaseURL       string         `yaml:"base_url"`
        ContextWindow *int           `yaml:"context_window"`
        ImageEnabled  *bool          `yaml:"image_enabled"`
        API           llm.RouterType `yaml:"api"`
        Default       bool           `yaml:"default"`
        ThinkEnabled  *bool          `yaml:"think_enabled"`
        ThinkLevel    *string        `yaml:"think_level"`
        PersonalRead  *bool          `yaml:"personal_read,omitempty"`
        PersonalWrite *bool          `yaml:"personal_write,omitempty"`
}

type permConfig struct {
        Mode                permission.Mode `yaml:"mode"`
        WorkspaceOnlyWrites *bool           `yaml:"workspace_only_writes"`
        AskTimeoutSec       *int            `yaml:"ask_timeout_sec"`
        DangerouslyAllowAll *bool           `yaml:"dangerously_allow_all"`
        BashRiskGuard       *bool           `yaml:"bash_risk_guard"`
        Sandbox             *string         `yaml:"sandbox"`
        SandboxNetwork      *string         `yaml:"sandbox_network"`
        Bash                *bashConfig     `yaml:"bash"`
}

type bashConfig struct {
        Default *string     `yaml:"default"`
        Allow   *stringList `yaml:"allow"`
        Deny    *stringList `yaml:"deny"`
}

// applyPermissions merges the file's permissions block over DefaultPolicy.
// An explicitly set list (even an empty one) replaces the default list.
func applyPermissions(p *permission.Policy, raw *permConfig) {
        if raw.Mode != "" {
                p.Mode = raw.Mode
        }
        if raw.WorkspaceOnlyWrites != nil {
                p.WorkspaceOnlyWrites = *raw.WorkspaceOnlyWrites
        }
        if raw.AskTimeoutSec != nil && *raw.AskTimeoutSec > 0 {
                p.AskTimeoutSec = *raw.AskTimeoutSec
        }
        if raw.DangerouslyAllowAll != nil {
                p.DangerouslyAllowAll = *raw.DangerouslyAllowAll
        }
        if raw.BashRiskGuard != nil {
                p.BashRiskGuard = *raw.BashRiskGuard
        }
        if raw.Sandbox != nil {
                p.Sandbox = strings.ToLower(strings.TrimSpace(*raw.Sandbox))
        }
        if raw.SandboxNetwork != nil {
                p.SandboxNetwork = strings.ToLower(strings.TrimSpace(*raw.SandboxNetwork))
        }
        if b := raw.Bash; b != nil {
                if b.Default != nil {
                        p.BashDefault = parseDecision(*b.Default, p.BashDefault)
                }
                // The user's list extends the built-in safe patterns rather than
                // replacing them. Replacing meant one entry — `allow: [make]` — silently
                // discarded the fourteen defaults, so git status, ls, and go test all
                // fell through to `default: ask` and every command started prompting.
                //
                // An explicitly empty list is the opt-out, and YAML distinguishes it from
                // an absent key: absent unmarshals to nil, `[]` to a non-nil zero-length
                // slice. So `allow: []` is the one case that really means "no built-ins".
                if b.Allow != nil {
                        if len(*b.Allow) == 0 {
                                p.BashAllow = nil
                        } else {
                                p.BashAllow = unionStrings(p.BashAllow, *b.Allow)
                        }
                }
                // The deny list unions rather than replaces, because the hard defaults
                // are safety-net patterns (sudo, rm -rf, curl|sh) that exist to be
                // non-overridable. A config that narrows safety must say so explicitly
                // with an empty list.
                if b.Deny != nil {
                        if len(*b.Deny) == 0 {
                                p.BashDeny = nil
                        } else {
                                p.BashDeny = unionStrings(p.BashDeny, *b.Deny)
                        }
                }
        }
}

// unionStrings concatenates the two lists while dropping duplicates from extra,
// so a config's deny patterns add to the hard defaults instead of silently
// discarding sudo / rm -rf / curl|sh. Order is preserved: defaults first, then
// anything the user added, so the first match wins for the common patterns.
func unionStrings(base, extra []string) []string {
        seen := make(map[string]bool, len(base)+len(extra))
        out := make([]string, 0, len(base)+len(extra))
        for _, list := range [][]string{base, extra} {
                for _, s := range list {
                        if seen[s] {
                                continue
                        }
                        seen[s] = true
                        out = append(out, s)
                }
        }
        return out
}

// stringList accepts either a single YAML scalar or a sequence, so both
// `allow: "go test ./..."` and the block list form in the README work.
type stringList []string

func (s *stringList) UnmarshalYAML(node *yaml.Node) error {
        switch node.Kind {
        case yaml.ScalarNode:
                *s = stringList{node.Value}
        case yaml.SequenceNode:
                items := make(stringList, 0, len(node.Content))
                for _, n := range node.Content {
                        items = append(items, n.Value)
                }
                *s = items
        default:
                return errors.New("expected a string or a list of strings")
        }
        return nil
}

func countIndent(line string) int {
        n := 0
        for _, r := range line {
                switch r {
                case ' ':
                        n++
                case '\t':
                        n += 2
                default:
                        return n / 2
                }
        }
        // Treat 2 spaces as one indent level for our hand-rolled parser.
        return n / 2
}

func parseDecision(val string, def permission.Decision) permission.Decision {
        switch strings.ToLower(strings.TrimSpace(val)) {
        case "allow":
                return permission.Allow
        case "deny", "reject":
                return permission.Deny
        case "ask":
                return permission.Ask
        default:
                return def
        }
}

func applyEnvOverrides(c *Config) {
        if v := envWithDeprecation("A1_API_KEY", "PHI_API_KEY"); v != "" {
                c.defaultEntry().APIKey = v
        }
        // A1_API_KEYS extends the default model's credential pool (comma-
        // separated); it never replaces A1_API_KEY, which stays the pool's
        // first key.
        if v := os.Getenv("A1_API_KEYS"); v != "" {
                entry := c.defaultEntry()
                for _, k := range strings.Split(v, ",") {
                        if k = strings.TrimSpace(k); k != "" {
                                entry.APIKeys = append(entry.APIKeys, k)
                        }
                }
        }
        if v := envWithDeprecation("A1_BASE_URL", "PHI_BASE_URL"); v != "" {
                c.defaultEntry().BaseURL = v
        }
        if v := envWithDeprecation("A1_MODEL", "PHI_MODEL"); v != "" {
                c.defaultEntry().Name = v
                c.DefaultModel = v
        }
        if v := envWithDeprecation("A1_SKILL_PATH", "PHI_SKILL_PATH"); v != "" {
                c.SkillPath = v
        }
        if v := envWithDeprecation("A1_THINK_LEVEL", "PHI_THINK_LEVEL"); v != "" {
                entry := c.defaultEntry()
                entry.Think.Mode = llm.ThinkMode(v)
                entry.Think.Enabled = (v != string(llm.Off))
        }
        if v := envWithDeprecation("A1_THINK_ENABLED", "PHI_THINK_ENABLED"); v != "" {
                entry := c.defaultEntry()
                entry.Think.Enabled = v == "true" || v == "1" || v == "yes"
        }
}

// ExpandHome resolves a leading ~ to the user's home directory so config
// paths can stay short (~/.a1/calendar). A ~ followed by anything other
// than a path separator is left alone (no ~user lookups); an empty path or
// one without the prefix is returned unchanged. Errors resolving the home
// dir degrade to the original path — a config typo should not brick boot.
// Exported since chunk 8: the web's mail configurator applies the same
// rule to editor-typed roots, so "~" means the same thing on both sides
// of the wire and there is one rule to test, not two that agree by luck.
func ExpandHome(path string) string {
        if path == "" || path[0] != '~' {
                return path
        }
        if path == "~" {
                if home, err := os.UserHomeDir(); err == nil {
                        return home
                }
                return path
        }
        if rest, ok := strings.CutPrefix(path, "~/"); ok {
                if home, err := os.UserHomeDir(); err == nil {
                        return filepath.Join(home, rest)
                }
        }
        return path
}

// envWithDeprecation reads newKey, falling back to the pre-rename PHI_*
// name for one deprecation cycle (identity hygiene, Phase 4): a hard switch
// would silently ignore exported vars in users' shells and CI, so the old
// name still works but says goodbye on stderr every time it is the one
// actually consumed.
func envWithDeprecation(newKey, oldKey string) string {
        if v := os.Getenv(newKey); v != "" {
                return v
        }
        if v := os.Getenv(oldKey); v != "" {
                fmt.Fprintf(os.Stderr, "warning: %s is deprecated; use %s (renamed with the fork)\n", oldKey, newKey)
                return v
        }
        return ""
}

// SetDangerouslyAllowAll persists permissions.dangerously_allow_all in config.yaml
// ("Allow All for Every Session"). Uses a yaml.Node surgical edit so comments,
// key order, and formatting are preserved — the old map[string]any round-trip
// stripped comments and alphabetized keys.
func SetDangerouslyAllowAll(global GlobalLayout, enabled bool) error {
        return modifyConfigYAML(global, func(root *yaml.Node) {
                perms := getOrCreateMapping(root, "permissions")
                setScalar(perms, "dangerously_allow_all", enabled)
        })
}

// AppendBashAllow appends a regex pattern to permissions.bash.allow in
// config.yaml, creating the section if needed. Duplicates are skipped.
// Uses a yaml.Node surgical edit so comments, key order, and formatting
// are preserved — the old map[string]any round-trip stripped comments
// and alphabetized keys.
func AppendBashAllow(global GlobalLayout, pattern string) error {
        trimmed := strings.TrimSpace(pattern)
        if trimmed == "" {
                return nil
        }
        return modifyConfigYAML(global, func(root *yaml.Node) {
                perms := getOrCreateMapping(root, "permissions")
                bash := getOrCreateMapping(perms, "bash")
                allow := getOrCreateSequence(bash, "allow")
                for _, item := range allow.Content {
                        if item.Value == trimmed {
                                return // duplicate, no-op
                        }
                }
                allow.Content = append(allow.Content, &yaml.Node{
                        Kind:  yaml.ScalarNode,
                        Tag:   "!!str",
                        Value: trimmed,
                })
        })
}

// modifyConfigYAML reads config.yaml as a yaml.Node tree, calls fn to
// surgically modify it, and writes it back. Comments, key order, and
// formatting are preserved because the Node tree carries them as
// HeadComment/FootComment/LineComment fields that survive the round-trip.
// An empty or missing file is treated as a fresh mapping so fn can add
// keys to it.
func modifyConfigYAML(global GlobalLayout, fn func(root *yaml.Node)) error {
        path := global.ConfigFile()
        data, err := os.ReadFile(path)
        if err != nil && !os.IsNotExist(err) {
                return err
        }

        var node yaml.Node
        if len(data) > 0 {
                if err := yaml.Unmarshal(data, &node); err != nil {
                        return fmt.Errorf("parse config.yaml: %w", err)
                }
        }
        // yaml.Unmarshal into a bare yaml.Node wraps the document in a
        // DocumentNode whose Content[0] is the root mapping. Normalize so
        // fn always sees a mapping root.
        root := &node
        if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
                root = root.Content[0]
        }
        if root.Kind == 0 || root.Kind != yaml.MappingNode {
                // Empty file or non-mapping root: create a fresh mapping.
                root = &yaml.Node{Kind: yaml.MappingNode}
        }

        fn(root)

        out, err := yaml.Marshal(root)
        if err != nil {
                return fmt.Errorf("marshal config.yaml: %w", err)
        }
        //nolint:gosec // G306: config.yaml is meant to be user-readable
        return os.WriteFile(path, append(out, '\n'), 0o644)
}

// getOrCreateMapping finds a mapping-valued child of parent named key,
// creating it (empty mapping) if absent. Returns the child node.
func getOrCreateMapping(parent *yaml.Node, key string) *yaml.Node {
        for i := 0; i+1 < len(parent.Content); i += 2 {
                if parent.Content[i].Value == key {
                        child := parent.Content[i+1]
                        if child.Kind != yaml.MappingNode {
                                // Existing key is not a mapping; replace it.
                                child = &yaml.Node{Kind: yaml.MappingNode}
                                parent.Content[i+1] = child
                        }
                        return child
                }
        }
        // Not found: append a new key + empty mapping.
        parent.Content = append(parent.Content,
                &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
                &yaml.Node{Kind: yaml.MappingNode},
        )
        return parent.Content[len(parent.Content)-1]
}

// getOrCreateSequence finds a sequence-valued child of parent named key,
// creating it (empty sequence) if absent. Returns the child node.
func getOrCreateSequence(parent *yaml.Node, key string) *yaml.Node {
        for i := 0; i+1 < len(parent.Content); i += 2 {
                if parent.Content[i].Value == key {
                        child := parent.Content[i+1]
                        if child.Kind != yaml.SequenceNode {
                                child = &yaml.Node{Kind: yaml.SequenceNode}
                                parent.Content[i+1] = child
                        }
                        return child
                }
        }
        parent.Content = append(parent.Content,
                &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
                &yaml.Node{Kind: yaml.SequenceNode},
        )
        return parent.Content[len(parent.Content)-1]
}

// setScalar sets a scalar-valued child of parent named key to value,
// creating it if absent. Existing comments on the key AND value nodes
// are preserved: when replacing the value, we copy HeadComment,
// FootComment, and LineComment from the old value node to the new
// one so inline comments like `dangerously_allow_all: false  # off`
// survive the edit.
func setScalar(parent *yaml.Node, key string, value any) {
        for i := 0; i+1 < len(parent.Content); i += 2 {
                if parent.Content[i].Value == key {
                        old := parent.Content[i+1]
                        new := scalarNode(value)
                        new.HeadComment = old.HeadComment
                        new.FootComment = old.FootComment
                        new.LineComment = old.LineComment
                        parent.Content[i+1] = new
                        return
                }
        }
        parent.Content = append(parent.Content,
                &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
                scalarNode(value),
        )
}

// scalarNode builds a yaml.Node for a bool or string scalar.
func scalarNode(value any) *yaml.Node {
        n := &yaml.Node{Kind: yaml.ScalarNode}
        switch v := value.(type) {
        case bool:
                n.Tag = "!!bool"
                n.Value = strconv.FormatBool(v)
        case int:
                n.Tag = "!!int"
                n.Value = strconv.Itoa(v)
        case string:
                n.Tag = "!!str"
                n.Value = v
        default:
                n.Tag = "!!str"
                n.Value = fmt.Sprintf("%v", v)
        }
        return n
}

// SetMaxToolCalls persists tools.max_tool_calls in config.yaml — the
// per-turn tool-call budget set from the command palette. n <= 0 removes
// the key (unlimited) and prunes an empty tools block. Uses a yaml.Node
// surgical edit so comments, key order, and formatting are preserved,
// mirroring SetDangerouslyAllowAll.
func SetMaxToolCalls(global GlobalLayout, n int) error {
        return modifyConfigYAML(global, func(root *yaml.Node) {
                if n <= 0 {
                        tools := findMapping(root, "tools")
                        if tools == nil {
                                return
                        }
                        removeKey(tools, "max_tool_calls")
                        if len(tools.Content) == 0 {
                                removeKey(root, "tools")
                        }
                        return
                }
                tools := getOrCreateMapping(root, "tools")
                setScalar(tools, "max_tool_calls", n)
        })
}

// findMapping returns the mapping-valued child of parent named key, or nil
// when absent (read-only counterpart to getOrCreateMapping).
func findMapping(parent *yaml.Node, key string) *yaml.Node {
        for i := 0; i+1 < len(parent.Content); i += 2 {
                if parent.Content[i].Value == key && parent.Content[i+1].Kind == yaml.MappingNode {
                        return parent.Content[i+1]
                }
        }
        return nil
}

// SetMailAccounts persists personal.mail_accounts — the dashboard's mail
// configurator (chunk 8). The list is replaced WHOLESALE: the caller sends
// the complete account list it wants on disk, and this function makes it
// so, mirroring how POST /api/config replaces the document. Fields are
// written sparsely (empty keys omitted, tls only when explicitly set), so
// a saved file looks hand-written. An empty list removes the sequence and
// prunes the personal block when it empties (SetMaxToolCalls' pruning
// discipline) — but never removes keys this function does not own:
// mail_dir, calendar_dir, and enabled survive an empty save untouched.
//
// What this function does NOT do is validate: shape is the caller's
// ruling (the webserver's configurator refuses known-bad entries with a
// 400; hand-edited YAML keeps the read path's lenient degrade). And it
// writes passwords as plain scalars on purpose — config.yaml is already
// this project's secret store (the model api_keys live there); the file's
// permissions are the boundary, same as every other credential it holds.
func SetMailAccounts(global GlobalLayout, accts []MailAccount) error {
        return modifyConfigYAML(global, func(root *yaml.Node) {
                if len(accts) == 0 {
                        personal := findMapping(root, "personal")
                        if personal == nil {
                                return
                        }
                        removeKey(personal, "mail_accounts")
                        if len(personal.Content) == 0 {
                                removeKey(root, "personal")
                        }
                        return
                }
                personal := getOrCreateMapping(root, "personal")
                seq := getOrCreateSequence(personal, "mail_accounts")
                seq.Content = nil
                for _, a := range accts {
                        m := &yaml.Node{Kind: yaml.MappingNode}
                        if a.Name != "" {
                                setScalar(m, "name", a.Name)
                        }
                        if a.Type != "" {
                                setScalar(m, "type", a.Type)
                        }
                        if a.Root != "" {
                                setScalar(m, "root", a.Root)
                        }
                        if a.Host != "" {
                                setScalar(m, "host", a.Host)
                        }
                        if a.Port != 0 {
                                setScalar(m, "port", a.Port)
                        }
                        if a.Username != "" {
                                setScalar(m, "username", a.Username)
                        }
                        if a.Password != "" {
                                setScalar(m, "password", a.Password)
                        }
                        if a.TLS != nil {
                                setScalar(m, "tls", *a.TLS)
                        }
                        if a.Mailbox != "" {
                                setScalar(m, "mailbox", a.Mailbox)
                        }
                        seq.Content = append(seq.Content, m)
                }
        })
}

// SetWebDarkMode persists web.dark_mode — the theme button's config-file
// toggle. nil removes the key (the GUI follows the OS preference again) and
// prunes an empty web block; true/false are the explicit choices. Like every
// surgical setter it preserves comments, key order, and sibling keys
// (auto_name_sessions survives a theme flip untouched), mirroring
// SetMaxToolCalls' discipline.
func SetWebDarkMode(global GlobalLayout, dark *bool) error {
        return modifyConfigYAML(global, func(root *yaml.Node) {
                if dark == nil {
                        web := findMapping(root, "web")
                        if web == nil {
                                return
                        }
                        removeKey(web, "dark_mode")
                        if len(web.Content) == 0 {
                                removeKey(root, "web")
                        }
                        return
                }
                web := getOrCreateMapping(root, "web")
                setScalar(web, "dark_mode", *dark)
        })
}

// removeKey deletes the key/value pair named key from a mapping node.
// No-op when the key is absent.
func removeKey(parent *yaml.Node, key string) {
        for i := 0; i+1 < len(parent.Content); i += 2 {
                if parent.Content[i].Value == key {
                        parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
                        return
                }
        }
}
