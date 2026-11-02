package toolmanager

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// BunTool describes an external runtime acquired through a package manager
// (bun) rather than a GitHub release asset: no asset to unpack, just a
// global install pinned to a version. The browser download stays the
// tool's own explicit setup step — a 150+ MiB fetch is never a package
// install side effect.
type BunTool struct {
	// Name is the tool identifier and the binary it installs.
	Name string
	// Package is the npm package name when it differs from Name.
	Package string
	// Version pins the install; "latest" tracks upstream.
	Version string
	// Setup is the command the user runs after install to fetch the
	// tool's own runtime (documented in errors, not run automatically).
	Setup string
}

// BunTools is the registry of package-managed external runtimes.
var BunTools = map[string]BunTool{
	"betterwright": {
		Name:    "betterwright",
		Package: "betterwright",
		// Pinned to the version the a1 adapter targets; bump with the
		// CLI contract, not opportunistically. Keep in sync with
		// BETTERWRIGHT_VERSION in the Makefile (make betterwright-install
		// installs this same pin).
		Version: "2.9.0",
		Setup:   "betterwright setup",
	},
}

// installTimeout bounds `bun install -g`.
const installTimeout = 5 * time.Minute

// runInstall is the exec seam for tests.
var runInstall = func(ctx context.Context, bun string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bun, args...) //nolint:gosec // G204: fixed binary, registry-controlled args
	return cmd.CombinedOutput()
}

// InstallBunTool globally installs a package-managed tool at its pinned
// version and returns the resolved binary path. The tool's own runtime
// setup (e.g. `betterwright setup` for the browser download) stays a
// separate, explicit step — this function installs the CLI only.
func InstallBunTool(ctx context.Context, tool string) (string, error) {
	cfg, ok := BunTools[tool]
	if !ok {
		return "", fmt.Errorf("unknown bun-managed tool %q", tool)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		return "", fmt.Errorf("bun is required to install %s — install it from https://bun.com: %w", tool, err)
	}
	pkg := cfg.Package
	if pkg == "" {
		pkg = cfg.Name
	}
	spec := pkg + "@" + cfg.Version
	if cfg.Version == "latest" {
		spec = pkg + "@latest"
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	out, err := runInstall(ctx, bun, []string{"install", "-g", spec})
	if err != nil {
		return "", fmt.Errorf("bun install -g %s: %w: %s", spec, err, string(out))
	}
	bin, err := exec.LookPath(cfg.Name)
	if err != nil {
		return "", fmt.Errorf("%s installed but not on PATH (restart your shell?): %w", cfg.Name, err)
	}
	return bin, nil
}
