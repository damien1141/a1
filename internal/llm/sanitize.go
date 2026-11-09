package llm

import (
	"encoding/json"
	"strings"
)

// RepairToolCallArguments returns tool-call arguments that are safe to
// serialize back to a provider. Valid JSON stays byte-for-byte verbatim;
// anything a strict gateway would reject — blank, "null", a stream truncated
// mid-arguments, fragments concatenated by an accumulator glitch — becomes
// "{}".
//
// The distinction matters only on the wire. A session may legitimately hold
// a malformed call (the executor already refused it and answered with a
// corrective tool result); replaying that history verbatim sends
// `arguments: "{\"path\": \"/home/…"` — invalid JSON — to the gateway, and a
// strict one answers a bare 400 ("Provider returned error") that no amount of
// compaction can fix, because the poisoned message rides in the compaction
// kept zone. Repairing at request-build time keeps the session record
// faithful while every request stays parseable.
func RepairToolCallArguments(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" || trimmed == "null" {
		return "{}"
	}
	if json.Valid([]byte(trimmed)) {
		return args
	}
	return "{}"
}

// SanitizeMessages repairs the tool-call arguments of every message so the
// whole slice can be serialized for a provider. The input is returned
// unchanged when nothing needs repair (the common case); otherwise a shallow
// copy with repaired ToolCalls is built and the input is left untouched.
func SanitizeMessages(messages []Message) []Message {
	dirty := false
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if RepairToolCallArguments(tc.Function.Arguments) != tc.Function.Arguments {
				dirty = true
			}
		}
	}
	if !dirty {
		return messages
	}
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		tcs := make([]ToolCall, len(out[i].ToolCalls))
		copy(tcs, out[i].ToolCalls)
		for j := range tcs {
			tcs[j].Function.Arguments = RepairToolCallArguments(tcs[j].Function.Arguments)
		}
		out[i].ToolCalls = tcs
	}
	return out
}
