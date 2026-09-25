package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// GlobalLayout describes the global phi home directory (~/.a1).
type GlobalLayout struct {
	root string
}

// Root returns the global phi home directory (~/.a1).
func (g GlobalLayout) Root() string { return g.root }

// ConfigFile returns the path to the global config file.
func (g GlobalLayout) ConfigFile() string { return filepath.Join(g.root, "config.yaml") }

// BinDir returns the directory for downloaded tool binaries.
func (g GlobalLayout) BinDir() string { return filepath.Join(g.root, "bin") }

// LookBin returns name from BinDir if present, otherwise PATH.
func (g GlobalLayout) LookBin(name string) (string, error) {
	for _, path := range binCandidates(filepath.Join(g.BinDir(), name)) {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not available: install to ~/.a1/bin or PATH", name)
	}
	return p, nil
}

// binCandidates returns the paths to probe inside a single directory.
// Downloads save Windows binaries as "<name>.exe", so an extensionless stat
// misses an fd/rg that is sitting right there in ~/.a1/bin. exec.LookPath
// appends PATHEXT on its own, so only the bin dir needs the extra variant.
func binCandidates(path string) []string {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		return []string{path + ".exe", path}
	}
	return []string{path}
}

// SkillsDir returns the directory for SKILL.md files.
func (g GlobalLayout) SkillsDir() string { return filepath.Join(g.root, "skills") }

// ExtensionsDir returns the directory for Go extension plugins (~/.a1/extensions).
func (g GlobalLayout) ExtensionsDir() string { return filepath.Join(g.root, "extensions") }

// SessionBase returns the root directory for persisted sessions.
func (g GlobalLayout) SessionBase() string { return filepath.Join(g.root, "session") }

// JobsDir returns the directory for sub-agent job artifacts.
func (g GlobalLayout) JobsDir() string { return filepath.Join(g.root, "jobs") }

// SessionDir returns the per-cwd session storage directory
// (~/.a1/session/<encoded-cwd>/).
func (p *Project) SessionDir() string {
	return ProjectSessionDir(p.global.SessionBase(), p.root)
}

// JobsDir returns ~/.a1/jobs for sub-agent job artifacts.
func (p *Project) JobsDir() string {
	return p.global.JobsDir()
}

// ExtensionsDir returns <root>/.a1/extensions, the per-project extensions
// directory (user extensions live under Global().ExtensionsDir()).
func (p *Project) ExtensionsDir() string {
	return filepath.Join(p.root, ".a1", "extensions")
}

// MCPConfigFile returns <root>/.a1/mcp.json, the per-project MCP config
// file (the user config is ~/.a1/mcp.json).
func (p *Project) MCPConfigFile() string {
	return filepath.Join(p.root, ".a1", "mcp.json")
}

// Project is the resolved phi workspace: the current working directory plus
// the global layout and its loaded configuration.
type Project struct {
	root   string
	global GlobalLayout
	config *Config
}

// Root returns the working directory the project was resolved from.
func (p *Project) Root() string { return p.root }

// Global returns the global phi layout (~/.a1).
func (p *Project) Global() GlobalLayout { return p.global }

// Config returns the loaded configuration, or nil before LoadConfig.
func (p *Project) Config() *Config { return p.config }

// LoadConfig reads, env-overrides and finalizes the global configuration.
// The result is cached on the Project until the next LoadConfig call.
func (p *Project) LoadConfig() error {
	cfg, err := loadConfig(p.global)
	if err != nil {
		return err
	}
	p.config = cfg
	return nil
}

// ensureGlobalDirs creates the global phi home directories. It is what makes
// ~/.a1/{bin,skills,extensions,session,jobs} exist from the very first startup.
func ensureGlobalDirs(global GlobalLayout) error {
	dirs := []string{
		global.Root(),
		global.BinDir(),
		global.SkillsDir(),
		global.ExtensionsDir(),
		global.SessionBase(),
		global.JobsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}
	return nil
}

// Discover resolves the phi workspace starting from startDir ("" = cwd) and
// ensures the global directory layout exists.
func Discover(startDir string) (*Project, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	global := GlobalLayout{root: filepath.Join(home, ".a1")}
	if err := ensureGlobalDirs(global); err != nil {
		return nil, err
	}
	return &Project{root: absRoot, global: global}, nil
}
