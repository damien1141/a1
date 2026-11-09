package llm

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
