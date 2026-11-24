package agent

import "github.com/damien1141/a1/internal/skipdetect"

// fastPathSkip is the package-local name for the shared skip detector. The
// implementation lives in internal/skipdetect so the memory-chunk extractor can
// reuse it without importing this package (which would be a cycle: memchunk is
// consumed by agent). One detector, two callers — the whole point of lifting it.
func fastPathSkip(msg string) string {
	return skipdetect.FastPathSkip(msg)
}
