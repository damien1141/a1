//go:build linux

package sandbox

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAnchorMissing pins which add-rule failures are skippable (the anchor
// directory isn't there — nothing to grant) versus fatal (the sandbox itself
// misbehaved). The skip branch in applyLandlock only runs on Landlock hosts;
// the classification is unit-testable everywhere Linux builds.
func TestAnchorMissing(t *testing.T) {
	assert.True(t, anchorMissing(syscall.ENOENT), "ENOENT is a missing anchor")
	assert.True(t, anchorMissing(&os.PathError{Op: "open", Path: "/gone", Err: syscall.ENOENT}), "wrapped ENOENT counts")
	assert.True(t, anchorMissing(&os.PathError{Op: "open", Path: "/file/x", Err: syscall.ENOTDIR}), "ENOTDIR (component is a file) counts")
	assert.False(t, anchorMissing(syscall.EACCES), "EACCES stays fatal")
	assert.False(t, anchorMissing(syscall.EPERM), "EPERM stays fatal")
	assert.False(t, anchorMissing(syscall.EBADF), "EBADF stays fatal")
	assert.False(t, anchorMissing(errors.New("boom")), "non-errno errors stay fatal")
}
