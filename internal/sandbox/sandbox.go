// Package sandbox compiles APPA policy into OS-level enforcement for bash.
// The permission gate decides what the model may ask for; the sandbox makes
// the answer cheap — containment substitutes for human approval, so
// autopilot stops being scary and "yolo with walls" becomes a real mode.
//
// The Profile is the manifest: read-only everywhere, writes confined to the
// workspace plus well-known caches, network on a toggle. Backends:
//
//   - Linux: Landlock (kernel 5.13+, no daemon, pure syscalls — fits the
//     lean-deps constraint). Applied on a locked OS thread immediately
//     before fork/exec; the child inherits the ruleset, the parent thread
//     dies with its goroutine, and the harness itself stays unrestricted.
//   - macOS: sandbox-exec ("seatbelt"): the compiled profile wraps the
//     command argv.
//   - else: unsupported — Start degrades to a plain exec and Supported
//     reports false, so the permission gate keeps asking.
//
// Reads stay policy-governed on purpose: Landlock allowlists cannot express
// "read everything except ~/.ssh", so read containment remains the APPA
// gate's job and the sandbox contains what actually escapes a compromised
// child — writes and network egress.
package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Profile is the compiled sandbox manifest.
type Profile struct {
	// ReadWrite lists absolute directories the child may write (and read).
	// Everything else is read-only or invisible-by-permission.
	ReadWrite []string
	// Network reports whether TCP connect/bind stays allowed.
	Network bool
}

// Compile builds the Profile for one bash run: workspace RW, the OS temp
// dir, and the well-known home caches a build needs (go, npm, cargo, bun),
// minus any directory that falls under an APPA sensitive-path deny prefix
// (the manifest never widens what the policy denies). network follows the
// policy's trust: false keeps egress denied — containment first.
func Compile(workspace string, network bool, denyPrefixes []string) Profile {
	home, _ := os.UserHomeDir()
	candidates := []string{workspace, os.TempDir()}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".cache"),
			filepath.Join(home, ".npm"),
			filepath.Join(home, ".cargo"),
			filepath.Join(home, "go"),
			filepath.Join(home, ".bun", "install", "cache"),
			// ~/.a1 narrowed to the subdirectories a build may touch; the
			// config and the credential-ish corners stay outside.
			filepath.Join(home, ".a1", "bin"),
			filepath.Join(home, ".a1", "jobs"),
			filepath.Join(home, ".a1", "browser-profiles"),
			filepath.Join(home, ".a1", "extensions"),
		)
	}
	var rw []string
	for _, dir := range candidates {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			abs, err := filepath.Abs(dir)
			if err != nil {
				continue
			}
			dir = abs
		}
		if underAnyPrefix(dir, denyPrefixes) {
			continue
		}
		// Cleaned and deduped; a nested duplicate is harmless but noisy.
		dir = filepath.Clean(dir)
		if !containsDir(rw, dir) {
			rw = append(rw, dir)
		}
	}
	return Profile{ReadWrite: rw, Network: network}
}

func underAnyPrefix(dir string, prefixes []string) bool {
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "~" {
			if rel, err := filepath.Rel(homeDir(), dir); err == nil &&
				(rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))) {
				return true
			}
			continue
		}
		if strings.HasPrefix(p, "~"+string(os.PathSeparator)) || strings.HasPrefix(p, "~/") {
			if h := homeDir(); h != "" {
				p = filepath.Join(h, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
			}
		}
		if !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if dir == p {
			return true
		}
		if rel, err := filepath.Rel(p, dir); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func containsDir(dirs []string, dir string) bool {
	for _, d := range dirs {
		if d == dir {
			return true
		}
	}
	return false
}

// Start starts cmd under the profile when enforcement is active for this
// host; otherwise it starts cmd unchanged. The caller still owns Wait.
// A Profile of nil means "no sandbox" and degrades to exec.Cmd.Start.
//
// On Linux the start happens on a dedicated locked thread that applies the
// Landlock ruleset first: the fork inherits the restrictions and the thread
// is destroyed when this call returns, taking the ruleset with it. On macOS
// the argv is wrapped in sandbox-exec with the compiled seatbelt profile.
func Start(cmd *exec.Cmd, p *Profile) error {
	if cmd == nil {
		return nil
	}
	if p == nil || !supported() {
		return cmd.Start()
	}
	return startLocked(cmd, p)
}

// Supported reports whether this host has a sandbox backend (Landlock on
// Linux, sandbox-exec on macOS). Callers gate enforcement on it; when
// false, Start degrades to a plain exec and the permission gate keeps
// asking.
func Supported() bool { return supported() }
