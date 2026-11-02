package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed default
var defaultFS embed.FS

// Default returns the embedded default skills filesystem.
// Files are rooted at the package directory, so entries appear as
// "SKILL.md", "agent-orchestration/SKILL.md", etc.
func Default() fs.FS {
	return defaultFS
}

// InstallDefault copies the embedded default skills into skillDir if the
// directory is empty or does not exist. Returns the number of files copied.
// Existing files are never overwritten — user skills take precedence.
func InstallDefault(skillDir string) (int, error) {
	if skillDir == "" {
		return 0, nil
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return 0, err
	}

	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return 0, err
	}
	if len(entries) > 0 {
		return 0, nil
	}

	copied := 0
	err = fs.WalkDir(defaultFS, "default", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "default" {
			return nil
		}
		rel, err := filepath.Rel("default", path)
		if err != nil {
			return err
		}
		dest := filepath.Join(skillDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := fs.ReadFile(defaultFS, path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		copied++
		return nil
	})
	if err != nil {
		return copied, err
	}
	return copied, nil
}
