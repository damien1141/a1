package permission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// CallHash returns a stable SHA-256 hex digest for the exact rendered call.
// It binds the ruling to tool name, paths/command, and input labels so the
// authority cannot approve a different call by accident.
func CallHash(req Request) string {
	h := sha256.New()
	fmt.Fprintf(h, "tool:%s\n", req.Tool)
	fmt.Fprintf(h, "action:%s\n", req.Action)
	if len(req.Paths) > 0 {
		sorted := make([]string, len(req.Paths))
		copy(sorted, req.Paths)
		sort.Strings(sorted)
		for _, p := range sorted {
			fmt.Fprintf(h, "path:%s\n", p)
		}
	}
	if req.Command != "" {
		fmt.Fprintf(h, "cmd:%s\n", req.Command)
	}
	for _, lab := range req.InputLabels {
		fmt.Fprintf(h, "trust:%d\n", lab.Trust)
		for _, r := range lab.ReaderSet {
			fmt.Fprintf(h, "reader:%s\n", r)
		}
		for _, e := range lab.EffectTokens {
			fmt.Fprintf(h, "effect:%s\n", e)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Authority evaluates a single call-scoped ruling for an exact call hash.
type Authority interface {
	Authorize(ctx context.Context, callHash string, req Request) (Decision, string)
}

// AuthorityFunc adapts a function to Authority.
type AuthorityFunc func(ctx context.Context, callHash string, req Request) (Decision, string)

// Authorize calls fn.
func (f AuthorityFunc) Authorize(ctx context.Context, callHash string, req Request) (Decision, string) {
	return f(ctx, callHash, req)
}
