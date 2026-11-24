package llm

import "strings"

// IsLocalBaseURL reports whether baseURL points at a loopback or
// private-network host — the shape of a local LLM server (llama.cpp,
// Ollama, LM Studio, vLLM on the LAN). The harness keys several
// local-friendly behaviors off this: background ops traffic is batched
// away from the conversation path, fleet concurrency collapses to one
// slot, and the context-window probe runs. It deliberately matches
// localOrigin's notion (window.go): loopback or RFC1918 private ranges.
//
// An empty or unparseable baseURL is NOT local — the remote default is
// the safe default for every behavior gated on this predicate.
func IsLocalBaseURL(baseURL string) bool {
	_, ok := localOrigin(baseURL)
	return ok
}

// SameOrigin reports whether two base URLs name the same scheme://host:port.
//
// This is the predicate background ops actually needs. IsLocalBaseURL answers
// "is this a local server", but the hazard it guards — a different-prefix
// request evicting the conversation's prompt cache — only exists when the ops
// call lands on the SAME server the conversation runs on. A persona memory model
// on loopback port 8017 while the conversation runs on 8080 is local, but it is
// a different server: no shared KV cache, no eviction, and gating naming on
// IsLocalBaseURL there disabled the feature for no benefit.
func SameOrigin(a, b string) bool {
	oa, oka := localOrigin(a)
	ob, okb := localOrigin(b)
	if !oka || !okb {
		return false
	}
	return strings.EqualFold(oa, ob)
}
