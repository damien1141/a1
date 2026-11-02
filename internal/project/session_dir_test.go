package project

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectDirName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cwd  string
		want string
	}{
		{"/Users/foo/bar", "--Users-foo-bar--"},
		{"relative/proj", "--relative-proj--"},
		{".", "--unknown--"},
	}
	for _, tt := range tests {
		t.Run(tt.cwd, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ProjectDirName(tt.cwd))
		})
	}
}

func TestProjectSessionDir(t *testing.T) {
	t.Parallel()
	base := "/tmp/a1-sessions"
	cwd := "/home/dev/app"
	got := ProjectSessionDir(base, cwd)
	assert.Equal(t, filepath.Join(base, "--home-dev-app--"), got)
}

func TestProjectSessionDirMethod(t *testing.T) {
	p := &Project{
		root:   "/Users/foo/a1",
		global: GlobalLayout{root: "/tmp/.a1"},
	}
	assert.Equal(t, filepath.Join("/tmp/.a1/session", "--Users-foo-a1--"), p.SessionDir())
}
