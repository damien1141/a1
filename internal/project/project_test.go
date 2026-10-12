package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoverInTempHome runs Discover("") with HOME redirected to a temp dir so
// tests never touch the real ~/.a1.
func discoverInTempHome(t *testing.T) *Project {
	t.Helper()
	home := t.TempDir()
	// os.UserHomeDir uses HOME on Unix and USERPROFILE on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p, err := Discover("")
	require.NoError(t, err)
	return p
}

func TestDiscoverCreatesGlobalDirs(t *testing.T) {
	p := discoverInTempHome(t)

	for _, dir := range []string{
		p.Global().Root(),
		p.Global().BinDir(),
		p.Global().SkillsDir(),
		p.Global().ExtensionsDir(),
		p.Global().SessionBase(),
		p.Global().JobsDir(),
	} {
		info, err := os.Stat(dir)
		assert.NoErrorf(t, err, "expected dir %q to exist", dir)
		if err == nil {
			assert.Truef(t, info.IsDir(), "expected %q to be a directory", dir)
		}
	}
}

func TestExtensionsDirPath(t *testing.T) {
	p := discoverInTempHome(t)
	assert.Equal(t, filepath.Join(p.Global().Root(), "extensions"), p.Global().ExtensionsDir())
}

func TestLookBinPrefersBinDir(t *testing.T) {
	p := discoverInTempHome(t)
	fake := filepath.Join(p.Global().BinDir(), "rg")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755))

	got, err := p.Global().LookBin("rg")
	require.NoError(t, err)
	assert.Equal(t, fake, got)
}

func TestLookBinFindsWindowsExe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows binaries are installed with an .exe suffix")
	}
	p := discoverInTempHome(t)
	exe := filepath.Join(p.Global().BinDir(), "fd.exe")
	require.NoError(t, os.WriteFile(exe, []byte("MZ"), 0o755))

	got, err := p.Global().LookBin("fd")
	require.NoError(t, err)
	assert.Equal(t, exe, got)
}

func TestLookBinFallsBackToPATH(t *testing.T) {
	p := discoverInTempHome(t)
	got, err := p.Global().LookBin("sh")
	if err != nil {
		t.Skip(err.Error())
	}
	require.NotEmpty(t, got)
	assert.NotContains(t, got, p.Global().BinDir())
}

func TestProjectDirs(t *testing.T) {
	p := discoverInTempHome(t)
	assert.Equal(t, filepath.Join(p.Root(), ".a1", "extensions"), p.ExtensionsDir())
	assert.Equal(t, filepath.Join(p.Root(), ".a1", "mcp.json"), p.MCPConfigFile())
}

func TestMemoryDir(t *testing.T) {
	p := discoverInTempHome(t)
	// The memory bank lives under ~/.a1/sessions/memory/<session>.jsonl, so the
	// directory must be separate from the session JSONL store (~/.a1/session).
	assert.Equal(t, filepath.Join(p.Global().Root(), "sessions", "memory"), p.Global().MemoryDir())
}

func TestLoadConfigDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()

	assert.Equal(t, "deepseek-chat", cfg.Model().Name)
	assert.Equal(t, "sk-test", cfg.Model().APIKey)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Model().BaseURL)
	assert.Equal(t, p.Global().SkillsDir(), cfg.SkillPath)
	// Model() carries the skill path for agent.NewEngine.
	assert.Equal(t, p.Global().SkillsDir(), cfg.Model().SkillPath)
}

func TestLoadConfigEnvOverrides(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: file-model
    api_key: file-key
skill_path: /from/file
`), 0o644))

	t.Setenv("PHI_MODEL", "env-model")
	t.Setenv("PHI_API_KEY", "env-key")
	t.Setenv("PHI_BASE_URL", "https://env.example/v1")
	t.Setenv("PHI_SKILL_PATH", "/from/env")

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Equal(t, "env-model", cfg.Model().Name)
	assert.Equal(t, "env-key", cfg.Model().APIKey)
	assert.Equal(t, "https://env.example/v1", cfg.Model().BaseURL)
	assert.Equal(t, "/from/env", cfg.SkillPath)
}

func TestLoadConfigMissingAPIKey(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte("models:\n  - name: x\n"), 0o644))
	t.Setenv("PHI_API_KEY", "")

	err := p.LoadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}

func TestLoadConfigConfigFileMissing(t *testing.T) {
	// Env-only setup: no config file, all values from environment.
	p := discoverInTempHome(t)
	t.Setenv("PHI_MODEL", "env-model")
	t.Setenv("PHI_API_KEY", "env-key")

	require.NoError(t, p.LoadConfig())
	assert.Equal(t, "env-model", p.Config().Model().Name)
	assert.Equal(t, "interactive", string(p.Config().Permissions.Mode))
}

func TestLoadConfigPermissions(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  mode: headless-strict
  ask_timeout_sec: 30
  workspace_only_writes: true
  bash:
    default: ask
    allow:
      - '^echo\b'
    deny:
      - '\bsudo\b'
`), 0o644))

	require.NoError(t, p.LoadConfig())
	perm := p.Config().Permissions
	assert.Equal(t, "headless-strict", string(perm.Mode))
	assert.Equal(t, 30, perm.AskTimeoutSec)
	// The lists extend the built-ins: `^echo\b` is already a default and the
	// union keeps one copy of it, and `\bsudo\b` likewise. What must not happen
	// is the config's list *replacing* the defaults, which is what made a
	// single-entry allow list turn git status and go test into prompts.
	assert.Contains(t, perm.BashAllow, `^echo\b`)
	assert.Equal(t, 1, countPattern(perm.BashAllow, `^echo\b`), "a duplicate default is not added twice")
	assert.Greater(t, len(perm.BashAllow), 1, "the other built-in safe patterns survive")
	assert.Contains(t, perm.BashDeny, `\bsudo\b`)
	assert.Greater(t, len(perm.BashDeny), 1, "the hard deny defaults survive")
	assert.True(t, p.Config().Agents.Enabled) // default on when agents: absent
}

func TestLoadConfigAgentsEnabled(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
agents:
  enabled: true
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.True(t, p.Config().Agents.Enabled)
}

func TestLoadConfigAgentsDisabled(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
agents:
  enabled: false
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.False(t, p.Config().Agents.Enabled)
}

func TestLoadConfigAgentsModelsOmitsEnabledKeepsDefaultOn(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
agents:
  models:
    explore: m
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.True(t, p.Config().Agents.Enabled)
	assert.Equal(t, "m", p.Config().Agents.Models.Explore)
}

func TestLoadConfigAgentsRoleModels(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: parent
    api_key: k
    default: true
  - name: cheap
    api_key: k
  - name: strong
    api_key: k
agents:
  enabled: true
  models:
    explore: cheap
    review: strong
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Equal(t, "cheap", cfg.Agents.Models.Explore)
	assert.Equal(t, "strong", cfg.Agents.Models.Review)
	assert.Empty(t, cfg.Agents.Models.Worker)
}

func TestLoadConfigAgentsRoleModelUnknownKept(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: parent
    api_key: k
agents:
  models:
    explore: missing-model
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.Equal(t, "missing-model", p.Config().Agents.Models.Explore)
	_, ok := p.Config().FindModel("missing-model")
	assert.False(t, ok)
}

func TestLoadConfigScalarOrInlineListForms(t *testing.T) {
	// The old line scanner only understood block lists (and treated an inline
	// sequence as one literal string); real YAML handles scalar and flow forms.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    allow: "go test ./..."
    deny: ['rm -rf', 'sudo']
`), 0o644))

	require.NoError(t, p.LoadConfig())
	perm := p.Config().Permissions
	// Both forms extend the built-in list. A config that names its own patterns
	// should add to the safe defaults, not discard them — the old replace
	// semantics meant `allow: make` alone turned git status and go test into
	// prompts.
	assert.Contains(t, perm.BashAllow, "go test ./...", "the scalar form still parses")
	assert.Contains(t, perm.BashDeny, "rm -rf")
	assert.Contains(t, perm.BashDeny, "sudo", "the flow form still parses")
	assert.Contains(t, perm.BashDeny, `\bsudo\b`, "and the hard defaults survive")
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	// A malformed config must fail loudly instead of silently degrading to
	// defaults (the old line scanner ignored malformed lines).
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(
		"models:\n  - name: m\n    api_key: k\npermissions: [unclosed\n",
	), 0o644))

	require.Error(t, p.LoadConfig())
}

func TestLoadConfigModelsFlat(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: p
    api_key: pk
    base_url: https://primary.example/v1
    context_window: 1000
    default: true
  - name: a1
    api_key: ak1
    base_url: https://a1.example/v1
  - name: a2
    api_key: ak2
    base_url: https://a2.example/v1
    context_window: 2000
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	require.Len(t, cfg.Models, 3)
	assert.Equal(t, "p", cfg.Models[0].Name)
	assert.Equal(t, "ak1", cfg.Models[1].APIKey)
	assert.Equal(t, 2000, cfg.Models[2].ContextWindow)
	assert.Equal(t, "p", cfg.DefaultModel)

	// Model() returns the entry marked default, with the skill path applied.
	m := cfg.Model()
	assert.Equal(t, "p", m.Name)
	assert.Equal(t, p.Global().SkillsDir(), m.SkillPath)

	// Models() lists every entry with the skill path applied.
	models := cfg.AllModels()
	require.Len(t, models, 3)
	for _, mm := range models {
		assert.Equal(t, p.Global().SkillsDir(), mm.SkillPath)
	}

	// FindModel returns the full per-model connection config.
	a2, ok := cfg.FindModel("a2")
	require.True(t, ok)
	assert.Equal(t, "https://a2.example/v1", a2.BaseURL)
	_, ok = cfg.FindModel("nope")
	assert.False(t, ok)
}

func TestLoadConfigDefaultFallsBackToFirst(t *testing.T) {
	// No entry marked default → the first entry wins.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: first
    api_key: k1
  - name: second
    api_key: k2
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Empty(t, cfg.DefaultModel)
	assert.Equal(t, "first", cfg.Model().Name)
}

func TestLoadConfigAppliesDeepSeekPresets(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-flash
    api_key: sk-flash
  - name: deepseek-v4-pro
    api_key: sk-pro
    context_window: 50000
  - name: custom-model
    api_key: sk-custom
    base_url: https://custom.example/v1
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()

	// deepseek-flash: only name + api_key → preset fills the rest.
	flash, ok := cfg.FindModel("deepseek-flash")
	require.True(t, ok)
	assert.Equal(t, "https://api.deepseek.com", flash.BaseURL)
	assert.Equal(t, 1_000_000, flash.ContextWindow)
	assert.True(t, flash.ImageEnabled, "flash supports image input")

	// deepseek-v4-pro: explicit context_window overrides the preset; image
	// stays off because v4-pro has no image understanding.
	pro, ok := cfg.FindModel("deepseek-v4-pro")
	require.True(t, ok)
	assert.Equal(t, "https://api.deepseek.com", pro.BaseURL)
	assert.Equal(t, 50_000, pro.ContextWindow, "explicit context_window overrides preset")
	assert.False(t, pro.ImageEnabled, "v4-pro has no image understanding")

	// Unknown names keep the generic OpenAI fallback and no preset.
	custom, ok := cfg.FindModel("custom-model")
	require.True(t, ok)
	assert.Equal(t, "https://custom.example/v1", custom.BaseURL)
	assert.Zero(t, custom.ContextWindow)
	assert.False(t, custom.ImageEnabled)
}

func TestAppendBashAllowFourSpaceIndent(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  dangerously_allow_all: false
  bash:
    default: ask
    allow: []
    deny: []
`), 0o644))

	// A non-empty allow list extends the built-in safe patterns rather than
	// replacing them: a config listing one pattern used to silently discard all
	// fourteen defaults and turn git status and go test into prompts.
	require.NoError(t, AppendBashAllow(p.Global(), "^make build\\b"))
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	assert.Contains(t, cfg.Permissions.BashAllow, "^make build\\b", "the appended pattern is allowed")
	assert.Greater(t, len(cfg.Permissions.BashAllow), 1, "the built-in defaults survive")

	// Duplicate append should be a no-op: present once, not twice.
	require.NoError(t, AppendBashAllow(p.Global(), "^make build\\b"))
	cfg2, err := loadConfig(p.Global())
	require.NoError(t, err)
	assert.Equal(t, 1, countPattern(cfg2.Permissions.BashAllow, "^make build\\b"))
	assert.Equal(t, len(cfg.Permissions.BashAllow), len(cfg2.Permissions.BashAllow),
		"a duplicate append adds nothing")
}

func countPattern(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

func TestUIAutoExpandDefaultsTrue(t *testing.T) {
	// An absent key means on, so a fresh config shows tool output without setup.
	assert.True(t, UIConfig{}.AutoExpandToolOutputResolved(), "absent defaults to true")

	off := false
	assert.False(t, UIConfig{AutoExpandToolOutput: &off}.AutoExpandToolOutputResolved(),
		"an explicit false opts out")

	on := true
	assert.True(t, UIConfig{AutoExpandToolOutput: &on}.AutoExpandToolOutputResolved(),
		"an explicit true stays on")
}

func TestParseConfigUIAutoExpandRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	// Absent → nil → resolves true.
	require.NoError(t, os.WriteFile(path, []byte("models:\n  - name: m\n    api_key: k\n"), 0o644))
	cfg, err := parseConfigFile(path)
	require.NoError(t, err)
	assert.Nil(t, cfg.UI.AutoExpandToolOutput, "an absent key stays nil so it can round-trip as absent")
	assert.True(t, cfg.UI.AutoExpandToolOutputResolved())

	// Explicit false parses and is honored.
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte("models:\n  - name: m\n    api_key: k\nui:\n  auto_expand_tool_output: false\n"),
			0o644,
		),
	)
	cfg, err = parseConfigFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.UI.AutoExpandToolOutput)
	assert.False(t, cfg.UI.AutoExpandToolOutputResolved(), "an explicit false is honored")
}

// The regression this guards: a config with one allow entry silently discarded
// every built-in safe pattern, so git status, ls, and go test all fell through
// to `default: ask` and every command prompted. The user's entry is meant to
// widen the allowlist, not replace it.
func TestLoadConfigSingleAllowEntryKeepsDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    default: ask
    allow:
      - make
    deny: []
`), 0o644))

	require.NoError(t, p.LoadConfig())
	perm := p.Config().Permissions

	assert.Contains(t, perm.BashAllow, "make", "the user's entry is allowed")
	// The defaults that matter most in day-to-day use.
	for _, pat := range []string{`^ls\b`, `^cat\b`, `^go (test|build|vet|fmt|mod|list|env|version)\b`} {
		assert.Contains(t, perm.BashAllow, pat, "the built-in default %q must survive", pat)
	}
	assert.Len(t, perm.BashAllow, defaultBashAllowCount(t)+1,
		"the built-in defaults plus the one user entry")
}

// An explicitly empty list is the opt-out, and YAML tells it apart from an
// absent key: `allow: []` unmarshals to a non-nil empty slice, so it means
// "no built-ins" rather than "inherit them".
func TestLoadConfigEmptyAllowListOptsOutOfDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    allow: []
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.Empty(t, p.Config().Permissions.BashAllow,
		"an explicit empty list drops the built-in safe patterns")
}

// An absent allow key inherits the defaults rather than clearing them.
func TestLoadConfigAbsentAllowListInheritsDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    default: ask
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.Len(t, p.Config().Permissions.BashAllow, defaultBashAllowCount(t),
		"an absent list inherits the built-in safe patterns")
}

// A config's deny list must not be able to silently drop the hard safety-net
// patterns (sudo, rm -rf, curl|sh) by naming its own.
func TestLoadConfigDenyListExtendsHardDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    deny:
      - 'terraform destroy'
`), 0o644))

	require.NoError(t, p.LoadConfig())
	deny := p.Config().Permissions.BashDeny
	assert.Contains(t, deny, "terraform destroy", "the user's deny pattern is added")
	assert.Contains(t, deny, `\bsudo\b`, "sudo stays denied")
	assert.Contains(t, deny, `\brm\s+-[a-zA-Z]*[rf][a-zA-Z]*\b`, "rm -rf stays denied")
}

// defaultBashAllowCount is how many patterns the built-in list ships, so the
// arithmetic assertions above stay honest if that list changes. It is derived
// from a config with no permissions block — that yields DefaultPolicy — rather
// than a literal, and without importing permission, which imports this package.
func defaultBashAllowCount(t *testing.T) int {
	// A config with no permissions block yields DefaultPolicy. The temp home is
	// written directly rather than through discoverInTempHome, because that
	// helper calls t.Setenv and calling it here would repoint HOME underneath
	// the test already using its own.
	p, err := Discover(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(),
		[]byte("models:\n  - name: m\n    api_key: k\n"), 0o644))
	require.NoError(t, p.LoadConfig())
	return len(p.Config().Permissions.BashAllow)
}
