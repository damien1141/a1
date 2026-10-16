package tooldef

import (
	"context"
	"encoding/json"

	"github.com/damien1141/a1/internal/llm"
)

// Result is what a tool returns to the model / UI.
type Result struct {
	// Content is sent back to the model as the tool message body.
	Content string
	// Detail identifies the call for the whole run — the url, path, or query.
	// Set it from DetailFromArgs when it comes from arguments; a tool must not
	// overwrite it at completion, because the transcript row needs one stable
	// identity from start to finish.
	Detail string
	// Summary is the one-line outcome, shown beside the identity once the call
	// resolves ("200 OK · 42.1 KB · 1.2s"). Empty hides it. This is where a
	// completion stat belongs; Detail stays the identity.
	Summary string
	// Output is the display body for the TUI (may equal Content).
	Output string
	// Expanded asks the TUI to start the tool row open.
	Expanded bool
}

// Handler runs a tool given raw JSON arguments.
type Handler func(ctx context.Context, input json.RawMessage) (Result, error)

// Tool is a schema + implementation pair.
type Tool struct {
	Definition llm.ToolDefinition
	Run        Handler
	// DetailFromArgs extracts a one-line detail for the UI before execution.
	DetailFromArgs func(input json.RawMessage) string
}

// Definitions extracts LLM schemas from tools.
func Definitions(tools []Tool) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, len(tools))
	for i, t := range tools {
		out[i] = t.Definition
	}
	return out
}

// Registry maps tool name → Tool.
type Registry map[string]Tool

// NewRegistry indexes tools by name.
func NewRegistry(tools []Tool) Registry {
	m := make(Registry, len(tools))
	for _, t := range tools {
		m[t.Definition.Name] = t
	}
	return m
}
