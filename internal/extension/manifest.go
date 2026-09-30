package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest describes a PXB extension subprocess (phi.yaml beside the binary).
type Manifest struct {
	Name        string   `yaml:"name"                  json:"name"`
	Version     string   `yaml:"version,omitempty"     json:"version,omitempty"`
	Exec        string   `yaml:"exec"                  json:"exec"`
	Args        []string `yaml:"args,omitempty"        json:"args,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Enabled     *bool    `yaml:"enabled,omitempty"     json:"enabled,omitempty"`
}

// IsEnabled defaults to true.
func (m Manifest) IsEnabled() bool {
	return m.Enabled == nil || *m.Enabled
}

// ReadManifest loads phi.yaml or phi.yml from dir.
func ReadManifest(dir string) (Manifest, error) {
	for _, name := range []string{"phi.yaml", "phi.yml"} {
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Manifest{}, err
		}
		var m Manifest
		if err := yaml.Unmarshal(b, &m); err != nil {
			return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := m.Validate(path); err != nil {
			return Manifest{}, err
		}
		if m.Name == "" {
			m.Name = filepath.Base(dir)
		}
		if m.Exec == "" {
			return Manifest{}, fmt.Errorf("%s: exec is required", path)
		}
		return m, nil
	}
	return Manifest{}, os.ErrNotExist
}

// Validate checks the manifest for obvious schema problems.
func (m Manifest) Validate(path string) error {
	switch {
	case m.Name != "" && !validIdentifier(m.Name):
		return fmt.Errorf("%s: invalid name %q", path, m.Name)
	case m.Exec != "" && (filepath.IsAbs(m.Exec) || strings.Contains(m.Exec, "..")):
		return fmt.Errorf("%s: exec must be relative to the extension directory", path)
	case len(m.Args) > 64:
		return fmt.Errorf("%s: too many args (%d)", path, len(m.Args))
	}
	return nil
}

func validIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
