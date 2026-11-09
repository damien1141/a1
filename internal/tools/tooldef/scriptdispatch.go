package tooldef

import (
	"context"
	"encoding/json"
)

// ScriptDispatchFunc runs one in-script tool call through the owning
// executor's normal per-call pipeline (extension pre/post, permission gate
// with ask, evidence and mutation recording) and returns the model-visible
// content plus whether the call succeeded. This is the load-bearing seam of
// the script tool's RPC: the context savings come from collapsing tool
// steps, and the safety comes from none of those steps bypassing the gate
// the model's own calls face.
type ScriptDispatchFunc func(ctx context.Context, tool string, args json.RawMessage) (content string, ok bool)

type scriptDispatchKey struct{}

// WithScriptDispatch attaches the dispatcher to a tool-run context. The
// executor installs it on every call it runs, so the script tool's child
// process — reached via the RPC server — lands back in the same executor
// that gated the script itself.
func WithScriptDispatch(ctx context.Context, fn ScriptDispatchFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, scriptDispatchKey{}, fn)
}

// ScriptDispatchFrom returns the dispatcher installed on ctx, or nil when
// no executor wired one (a bare Run, or a tool invoked outside the
// pipeline). The script tool refuses to execute in that case rather than
// strand the script's tool calls.
func ScriptDispatchFrom(ctx context.Context) ScriptDispatchFunc {
	fn, _ := ctx.Value(scriptDispatchKey{}).(ScriptDispatchFunc)
	return fn
}
