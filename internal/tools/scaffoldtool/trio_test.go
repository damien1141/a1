package scaffoldtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

func runScaffoldAt(t *testing.T, root string, limit int) string {
	t.Helper()
	raw, err := json.Marshal(scaffoldInput{Path: root, Limit: limit})
	require.NoError(t, err)
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	return out.Content
}

func mkdirFixture(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, rel := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		writeFile(t, filepath.Dir(abs), filepath.Base(rel), "fixture\n")
	}
}

// oracle: hand-written golden literals — the exact rendered output for fixed
// fixture trees, walk order and all, so detector drift (new recognized files,
// reordering, message edits, path handling) shows up as a diff.
func TestRunScaffold_RendersGoldenOutput_Differential(t *testing.T) {
	t.Run("full stack at the root", func(t *testing.T) {
		root := t.TempDir()
		mkdirFixture(t, root,
			".gitlab-ci.yml", "Dockerfile", "Makefile", "docker-compose.yml",
			"go.mod", "go.sum", "package.json", "requirements.txt", "yarn.lock",
		)
		want := ".gitlab-ci.yml\t[ci]\tGitLab CI detected; use gitlab-runner or CI pipeline\n" +
			"Dockerfile\t[deployment]\tDockerfile present; use docker build and docker run\n" +
			"Makefile\t[build]\tMakefile present; inspect targets with make help or make list\n" +
			"docker-compose.yml\t[deployment]\tDocker Compose detected; use docker compose up/down\n" +
			"go.mod\t[language]\tGo module detected; use go build, go test, go vet\n" +
			"go.sum\t[language]\tGo module checksums present\n" +
			"package.json\t[language]\tNode.js project detected; use npm, yarn, or pnpm\n" +
			"requirements.txt\t[language]\tPython project detected; use pip, poetry, or pdm\n" +
			"yarn.lock\t[language]\tNode.js lockfile detected; use the corresponding package manager\n"
		assert.Equal(t, want, runScaffoldAt(t, root, 50))
	})

	t.Run("nested files keep their relative path", func(t *testing.T) {
		root := t.TempDir()
		mkdirFixture(t, root, "go.mod", "sub/Makefile")
		want := "go.mod\t[language]\tGo module detected; use go build, go test, go vet\n" +
			"sub/Makefile\t[build]\tMakefile present; inspect targets with make help or make list\n"
		assert.Equal(t, want, runScaffoldAt(t, root, 50))
	})

	t.Run("pruned directories contribute nothing", func(t *testing.T) {
		root := t.TempDir()
		mkdirFixture(t, root, "go.mod", "vendor/package.json", "node_modules/index.js", ".git/config")
		want := "go.mod\t[language]\tGo module detected; use go build, go test, go vet\n"
		assert.Equal(t, want, runScaffoldAt(t, root, 50))
	})

	t.Run("limit keeps the first hints in walk order", func(t *testing.T) {
		root := t.TempDir()
		mkdirFixture(t, root,
			".gitlab-ci.yml", "Dockerfile", "Makefile", "docker-compose.yml",
			"go.mod", "go.sum", "package.json", "requirements.txt", "yarn.lock",
		)
		want := ".gitlab-ci.yml\t[ci]\tGitLab CI detected; use gitlab-runner or CI pipeline\n" +
			"Dockerfile\t[deployment]\tDockerfile present; use docker build and docker run\n"
		assert.Equal(t, want, runScaffoldAt(t, root, 2))
	})
}
