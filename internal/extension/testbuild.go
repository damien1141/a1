package extension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// Materialize writes a PXB extension module under dir and builds its binary.
// mainGo must be a complete package main that uses the ext/a1 SDK.
// dir becomes the extension directory (contains a1.yaml + binary).
func Materialize(ctx context.Context, dir, name, version, mainGo string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	phiRoot, err := moduleRoot()
	if err != nil {
		return err
	}
	mod := extensionGoMod(name, filepath.Join(phiRoot, "ext", "go"))
	//nolint:gosec // G306: throwaway generated test module files
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(mod),
		0o644,
	); err != nil {
		return err
	}
	//nolint:gosec // G306: throwaway generated test module files
	if err := os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte(mainGo),
		0o644,
	); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}
	bin := "extbin"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd = exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	yaml := fmt.Sprintf("name: %s\nversion: %q\nexec: ./%s\n", name, version, bin)
	//nolint:gosec // G306: throwaway generated test module files
	return os.WriteFile(
		filepath.Join(dir, "a1.yaml"),
		[]byte(yaml),
		0o644,
	)
}

// extensionGoMod renders the go.mod of a scratch extension module. The SDK
// path rides the replace directive as a double-quoted string: go.mod tokens
// split on whitespace, so an unquoted checkout path like
// ".../workspace-584a6da6 (1)/download/a1_extracted" (a browser's
// duplicate-download suffix) used to break every extension-building test at
// "usage: replace module/path ..." — quoting is always emitted, and a quoted
// path is valid go.mod wherever the tree lives.
func extensionGoMod(module, sdkDir string) string {
	return fmt.Sprintf(
		"module %s\n\ngo 1.26\n\nrequire github.com/damien1141/a1/ext/go v0.0.0\n\nreplace github.com/damien1141/a1/ext/go => %s\n",
		module,
		strconv.Quote(filepath.ToSlash(sdkDir)),
	)
}

func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("extension: runtime.Caller failed")
	}
	// internal/extension/testbuild.go → repo root
	return filepath.Abs(filepath.Join(filepath.Dir(file), "../.."))
}
