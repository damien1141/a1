package tokentool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/llm"
)

const (
	tokenDefaultTotal = 4000
	tokenDefaultLimit = 20
)

var tokenDescription = `Dynamic token budget allocator.

Distributes an available context budget across candidate tools or analysis
passes so the agent can plan high-value work first and avoid wasting budget
on low-yield passes.`

// TokenTool returns the token budget allocator tool definition + handler.
func TokenTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "tokenbudget",
			Description: tokenDescription,
		Params: &llm.FunctionParameters{
			Type: "object",
			Properties: llm.Object{
				"task": llm.Object{
					"type":        "string",
					"description": "Short task description used to weight allocations. Example: rust borrow-checker failure",
				},
				"total": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Total token budget to allocate. Example: 4000 (default: %d)", tokenDefaultTotal),
				},
				"limit": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum allocations to return. Example: 10 (default: %d)", tokenDefaultLimit),
				},
			},
			Required: []string{},
		},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in tokenInput
			_ = json.Unmarshal(input, &in)
			task := strings.TrimSpace(in.Task)
			if task == "" {
				task = "general"
			}
			return fmt.Sprintf("tokenbudget %s", task)
		},
		Run: runToken,
	}
}

type tokenInput struct {
	Task  string `json:"task,omitempty"`
	Total int    `json:"total,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type tokenAllocation struct {
	Tool      string  `json:"tool"`
	Tokens    int     `json:"tokens"`
	Share     float64 `json:"share"`
	Reasoning string  `json:"reasoning"`
}

func runToken(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in tokenInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse tokenbudget arguments: %w", err)
	}

	task := strings.TrimSpace(in.Task)
	if task == "" {
		task = "general"
	}
	total := in.Total
	if total <= 0 {
		total = tokenDefaultTotal
	}
	limit := in.Limit
	if limit <= 0 {
		limit = tokenDefaultLimit
	}

	allocations, err := allocateTokens(task, total, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(allocations) == 0 {
		return tooldef.Result{Content: "No allocations produced", Detail: "0 allocations", Output: "No allocations produced"}, nil
	}

	content := renderTokenResults(ctx, allocations)
	detail := fmt.Sprintf("%d allocations", len(allocations))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func allocateTokens(task string, total, limit int) ([]tokenAllocation, error) {
	weights := buildWeights(task)
	if len(weights) == 0 {
		return nil, nil
	}

	weightSum := 0.0
	for _, w := range weights {
		weightSum += w.Weight
	}
	if weightSum <= 0 {
		return nil, nil
	}

	var allocations []tokenAllocation
	for _, w := range weights {
		rawTokens := int(math.Round(float64(total) * (w.Weight / weightSum)))
		if rawTokens < 1 {
			rawTokens = 1
		}
		allocations = append(allocations, tokenAllocation{
			Tool:   w.Tool,
			Tokens: rawTokens,
			Share:  math.Round(float64(rawTokens)/float64(total)*10000) / 100,
			Reasoning: w.Reasoning,
		})
	}

	sort.Slice(allocations, func(i, j int) bool {
		return allocations[i].Tokens > allocations[j].Tokens
	})
	if len(allocations) > limit {
		allocations = allocations[:limit]
	}

	normalized := normalizeAllocations(allocations, total)
	return normalized, nil
}

type toolWeight struct {
	Tool      string
	Weight    float64
	Reasoning string
}

func buildWeights(task string) []toolWeight {
	taskLower := strings.ToLower(task)
	weights := map[string]toolWeight{
		"grep": {
			Tool:      "grep",
			Weight:    22,
			Reasoning: "cheap high-recall search; default first pass",
		},
		"find": {
			Tool:      "find",
			Weight:    16,
			Reasoning: "locates files before reading",
		},
		"read": {
			Tool:      "read",
			Weight:    24,
			Reasoning: "reads highest-value files",
		},
		"bash": {
			Tool:      "bash",
			Weight:    14,
			Reasoning: "runs cheap verification commands",
		},
		"stack": {
			Tool:      "stack",
			Weight:    10,
			Reasoning: "traces call sites cheaply",
		},
		"impact": {
			Tool:      "impact",
			Weight:    8,
			Reasoning: "checks blast radius after reading",
		},
		"context": {
			Tool:      "context",
			Weight:    6,
			Reasoning: "fold-health check late in pass",
		},
	}

	boost := func(name string, delta float64, reason string) {
		w, ok := weights[name]
		if !ok {
			weights[name] = toolWeight{Tool: name, Weight: delta, Reasoning: reason}
			return
		}
		w.Weight += delta
		w.Reasoning = reason
		weights[name] = w
	}

	cut := func(name string, delta float64) {
		w, ok := weights[name]
		if !ok {
			return
		}
		w.Weight -= delta
		if w.Weight < 1 {
			w.Weight = 1
		}
		w.Reasoning = "deferred after cheaper passes"
		weights[name] = w
	}

	switch {
	case strings.Contains(taskLower, "rust") || strings.Contains(taskLower, "borrow") || strings.Contains(taskLower, "lifetime"):
		boost("bash", 18, "runs cargo check for rust diagnostics")
		boost("read", 12, "reads Cargo.toml and failing modules")
		cut("context", 4)
	case strings.Contains(taskLower, "python") || strings.Contains(taskLower, "type") || strings.Contains(taskLower, "import"):
		boost("bash", 12, "runs pyright or mypy")
		boost("grep", 8, "finds imports and type aliases")
	case strings.Contains(taskLower, "build") || strings.Contains(taskLower, "cmake") || strings.Contains(taskLower, "make"):
		boost("bash", 20, "runs build-system commands")
		boost("read", 6, "reads build files for targets")
		weights["build"] = toolWeight{Tool: "build", Weight: 18, Reasoning: "build-system semantics pass"}
	case strings.Contains(taskLower, "error") || strings.Contains(taskLower, "diagnostic") || strings.Contains(taskLower, "stack"):
		boost("stack", 14, "traces error origin")
		boost("read", 6, "reads failing code paths")
	case strings.Contains(taskLower, "security") || strings.Contains(taskLower, "vuln") || strings.Contains(taskLower, "injection"):
		boost("grep", 10, "finds risky API usage")
		weights["vuln"] = toolWeight{Tool: "vuln", Weight: 20, Reasoning: "security-focused scan"}
		cut("context", 2)
	case strings.Contains(taskLower, "test") || strings.Contains(taskLower, "coverage"):
		boost("bash", 10, "runs tests and coverage")
		weights["coverage"] = toolWeight{Tool: "coverage", Weight: 14, Reasoning: "coverage mapping"}
		weights["test"] = toolWeight{Tool: "test", Weight: 12, Reasoning: "test-output interpretation"}
	case strings.Contains(taskLower, "doc") || strings.Contains(taskLower, "api"):
		boost("grep", 8, "finds exported symbols")
		weights["apidoc"] = toolWeight{Tool: "apidoc", Weight: 18, Reasoning: "API doc stubs"}
		cut("impact", 3)
	case strings.Contains(taskLower, "git") || strings.Contains(taskLower, "blame") || strings.Contains(taskLower, "history"):
		boost("grep", 6, "finds changed paths")
		weights["git"] = toolWeight{Tool: "git", Weight: 18, Reasoning: "git intelligence pass"}
	case strings.Contains(taskLower, "refactor") || strings.Contains(taskLower, "migration"):
		boost("impact", 14, "checks blast radius")
		boost("stack", 8, "finds callers")
		weights["migration"] = toolWeight{Tool: "migration", Weight: 12, Reasoning: "multi-file coordination"}
	}

	var result []toolWeight
	for _, w := range weights {
		if w.Weight < 1 {
			w.Weight = 1
		}
		result = append(result, w)
	}
	return result
}

func normalizeAllocations(allocations []tokenAllocation, total int) []tokenAllocation {
	if len(allocations) == 0 {
		return allocations
	}

	allocated := 0
	for _, a := range allocations {
		allocated += a.Tokens
	}

	diff := total - allocated
	if diff == 0 {
		return allocations
	}

	if diff > 0 {
		allocations[0].Tokens += diff
		allocations[0].Share = math.Round(float64(allocations[0].Tokens) / float64(total) * 100) / 100
		return allocations
	}

	for i := 0; i < len(allocations)-1; i++ {
		if allocations[i].Tokens + diff < 1 {
			continue
		}
		allocations[i].Tokens += diff
		break
	}
	return allocations
}

func renderTokenResults(ctx context.Context, allocations []tokenAllocation) string {
	var sb strings.Builder
	for _, a := range allocations {
		sb.WriteString(fmt.Sprintf("%d\t%.1f%%\t%s\t%s\n", a.Tokens, a.Share, a.Tool, a.Reasoning))
	}
	return sb.String()
}
