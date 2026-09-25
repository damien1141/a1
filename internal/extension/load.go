package extension

import (
	"context"
	"os"
	"path/filepath"

	ext "github.com/damien1141/a1/ext/go"
	"github.com/damien1141/a1/internal/debuglog"
)

// Load discovers PXB extensions and spawns each subprocess.
func Load(userDir, projectDir string) (*Runner, []Warning, error) {
	found, warns, err := Discover(userDir, projectDir)
	if err != nil {
		return nil, warns, err
	}
	if len(found) == 0 {
		return &Runner{warns: warns}, warns, nil
	}

	logDir := extensionLogDir(userDir)
	cwd, _ := os.Getwd()

	r := &Runner{}
	for _, d := range found {
		proc, err := StartProc(context.Background(), d.Manifest, d.Path, logDir, cwd, "")
		if err != nil {
			warns = append(warns, Warning{Path: d.Path, Message: err.Error()})
			debuglog.Logf("extension: load %s: %v", d.Path, err)
			continue
		}
		api := ext.NewAPI()
		proc.BuildAPI(api)
		r.apis = append(r.apis, api)
		r.procs = append(r.procs, proc)
		r.loaded = append(r.loaded, d)
	}
	r.warns = warns
	return r, warns, nil
}

func extensionLogDir(userDir string) string {
	if userDir == "" {
		return filepath.Join(os.TempDir(), "phi-ext-logs")
	}
	// ~/.a1/extensions → ~/.a1/logs
	return filepath.Join(filepath.Dir(userDir), "logs")
}
