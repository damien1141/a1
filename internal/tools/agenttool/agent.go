package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/llm"
)

const agentSummaryLimit = 12000 // bytes, keep parent context small

const agentLaunchGuidance = `Launch a specialized sub-agent. Pick a role:

- explore (default): no write/edit; full bash except hard denies. Multi-hop recon / unknown location — keep tool noise out of this context.
- review: same tools as explore; diffs, checks, quality/security report — do not edit.
- worker: read/write/edit + bash; implement a scoped, self-contained change and verify. Workers above the depth floor can spawn their own worker sub-agents. Not for open-ended exploration.

When NOT to use any sub-agent:
- Exact file path already known — use read yourself
- Exact symbol like "class Foo" — use grep yourself
- Tiny local edit — edit/write yourself

How to use:
1. Use agent_spawn to launch a job, then agent_wait for its summary. For parallel jobs, spawn all first, then wait each.
2. Stateless: put a highly detailed, self-contained prompt and say what the final summary must include. (Exception: warm_from_job reuses a worker's prior session.)
3. You only receive the final summary plus the structured handoff (files touched, check states). Summarize for the user if needed.
4. Explore/review sub-agents cannot spawn further agents; workers can, within cheaper step budgets.
5. Verify before relying on a worker's edits in follow-up work.
6. Sub-agents inherit the checks you declared with the contract tool — a worker is held to that finish line.`

// AgentDeps wires sub-agent tools to a process-level [job.Manager].
// ParentID/WorkDir are read at call time (session may change via /resume).
// Depth is the spawning engine's fleet depth (0 = top-level parent);
// FleetContract (nil = none) supplies the task-contract checks children
// inherit (Bet 04).
type AgentDeps struct {
	Manager       *job.Manager
	ParentID      func() string
	WorkDir       func() string
	Depth         int
	FleetContract func() []string
}

// AgentTools returns agent_spawn / list / wait / cancel. Depth comes from
// AgentDeps (the spawning engine's fleet depth); ParentID comes from
// ParentID(), not model args.
func AgentTools(deps AgentDeps) []tooldef.Tool {
	if deps.Manager == nil {
		return nil
	}
	if deps.ParentID == nil {
		deps.ParentID = func() string { return "" }
	}
	if deps.WorkDir == nil {
		deps.WorkDir = func() string { return "" }
	}
	return []tooldef.Tool{
		agentSpawnTool(deps),
		agentListTool(deps),
		agentWaitTool(deps),
		agentCancelTool(deps),
	}
}

func agentSpawnTool(deps AgentDeps) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "agent_spawn",
			Description: fmt.Sprintf(agentLaunchGuidance+`

Starts asynchronously and returns job_id immediately. Use agent_wait for the summary. Best for parallel jobs.

Concurrency: at most %d sub-agents run at once; further spawns join a FIFO queue (up to %d) and start as slots free.`, deps.Manager.MaxConcurrent(), deps.Manager.MaxQueue()),
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"prompt": llm.Object{
						"type":        "string",
						"description": "Self-contained task. Include context, scope, and exactly what the final summary must return. The sub-agent cannot ask follow-ups.",
					},
					"description": llm.Object{
						"type":        "string",
						"description": "Very short label for the UI / job list (e.g. \"find auth config\").",
					},
					"role": llm.Object{
						"type":        "string",
						"description": "explore (default) | review | worker. See tool description for when to pick each.",
						"enum":        []string{"explore", "review", "worker"},
					},
					"workdir": llm.Object{
						"type":        "string",
						"description": "Working directory for the sub-agent (default: parent session cwd).",
					},
					"warm_from_job": llm.Object{
						"type":        "string",
						"description": "Reuse a finished worker job's session (job_id from a previous agent_wait) so it skips re-reading the repo. Requires role=worker.",
					},
					"timeout_sec": llm.Object{
						"type":        "integer",
						"description": "Optional run timeout in seconds for the job itself (not wait).",
					},
				},
				Required: []string{"prompt"},
			},
		},
		DetailFromArgs: spawnDetail,
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			in, err := parseSpawnInput(input)
			if err != nil {
				return tooldef.Result{}, err
			}
			role, err := job.ParseRole(in.Role)
			if err != nil {
				return tooldef.Result{}, err
			}
			warm := strings.TrimSpace(in.WarmFromJob)
			if warm != "" && role != job.RoleWorker {
				return tooldef.Result{}, errors.New(
					"agent_spawn: warm_from_job requires role=worker (warm resume is the worker pool)",
				)
			}
			wd := strings.TrimSpace(in.WorkDir)
			if wd == "" {
				wd = deps.WorkDir()
			}
			var contract []string
			if deps.FleetContract != nil {
				contract = deps.FleetContract()
			}
			req := job.SpawnRequest{
				Prompt:          in.Prompt,
				Description:     in.Description,
				ParentID:        deps.ParentID(),
				ParentToolUseID: tooldef.ToolCallID(ctx),
				Depth:           deps.Depth,
				Role:            role,
				WorkDir:         wd,
				WarmFrom:        warm,
				Contract:        contract,
			}
			if in.TimeoutSec > 0 {
				req.Timeout = time.Duration(in.TimeoutSec) * time.Second
			}
			info, err := deps.Manager.Spawn(ctx, req)
			if err != nil {
				return tooldef.Result{}, err
			}
			body := mustJSON(map[string]any{
				"job_id":      info.ID,
				"status":      info.Status,
				"role":        info.Role,
				"dir":         info.Dir,
				"result_path": info.ResultPath,
			})
			return tooldef.Result{Content: body, Detail: roleDetail(string(info.Role), info.ID), Output: body}, nil
		},
	}
}

type spawnInput struct {
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Role        string `json:"role"`
	WorkDir     string `json:"workdir"`
	WarmFromJob string `json:"warm_from_job"`
	TimeoutSec  int    `json:"timeout_sec"`
}

func parseSpawnInput(input json.RawMessage) (spawnInput, error) {
	var in spawnInput
	if err := json.Unmarshal(input, &in); err != nil {
		return spawnInput{}, err
	}
	return in, nil
}

func spawnDetail(input json.RawMessage) string {
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		Role        string `json:"role"`
	}
	_ = json.Unmarshal(input, &in)
	label := strings.TrimSpace(in.Description)
	if label == "" {
		label = truncateRunes(in.Prompt, 80)
	}
	return roleDetail(in.Role, label)
}

// roleDetail is the one-line TUI suffix: "explore · find auth".
func roleDetail(role, rest string) string {
	r := string(job.NormalizeRole(role))
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return r
	}
	return r + " · " + rest
}

func agentListTool(deps AgentDeps) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "agent_list",
			Description: `List sub-agent jobs (newest first). Each row includes status; filter client-side if needed.`,
			Params: &llm.FunctionParameters{
				Type:       "object",
				Properties: llm.Object{},
			},
		},
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			list, err := deps.Manager.HandleList(ctx, input)
			if err != nil {
				return tooldef.Result{}, err
			}
			rows := make([]map[string]any, 0, len(list))
			for _, info := range list {
				rows = append(rows, map[string]any{
					"job_id":      info.ID,
					"status":      info.Status,
					"role":        info.Role,
					"description": info.Description,
					"dir":         info.Dir,
				})
			}
			body := mustJSON(map[string]any{"jobs": rows, "count": len(rows)})
			return tooldef.Result{Content: body, Detail: fmt.Sprintf("%d jobs", len(rows)), Output: body}, nil
		},
	}
}

func agentWaitTool(deps AgentDeps) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "agent_wait",
			Description: `Block until a sub-agent job reaches a terminal status and return its result.md summary.

timeout_sec only limits how long this wait blocks — it does NOT cancel the job.
Use agent_cancel to stop a running job.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"job_id": llm.Object{
						"type":        "string",
						"description": "Job id from agent_spawn.",
					},
					"timeout_sec": llm.Object{
						"type":        "integer",
						"description": "Max seconds to wait (does not cancel the job).",
					},
				},
				Required: []string{"job_id"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				JobID string `json:"job_id"`
			}
			_ = json.Unmarshal(input, &in)
			return in.JobID
		},
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			res, err := deps.Manager.HandleWait(ctx, input)
			if err != nil {
				return tooldef.Result{}, err
			}
			// Durable outbox: the parent just consumed this
			// result directly — ack the delivery row so the
			// next turn boundary does not re-present it.
			if res.Info.Status.Terminal() {
				deps.Manager.AckDelivery(res.Info.ID)
			}
			summary := truncateBytes(res.Summary, agentSummaryLimit)
			body := map[string]any{
				"job_id":      res.Info.ID,
				"status":      res.Info.Status,
				"role":        res.Info.Role,
				"error":       res.Info.Error,
				"result_path": res.Info.ResultPath,
				"summary":     summary,
			}
			// Structured handoff (Bet 04): files the child
			// touched and where every observed check stands —
			// merged as data, not prose, so a fleet parent can
			// act on them without re-reading the child's
			// transcript. Absent for runners without handoff
			// support; the summary above stands alone then.
			if h, ok := job.ReadHandoff(res.Info.Dir); ok {
				body["files_read"] = h.FilesRead
				body["files_modified"] = h.FilesModified
				body["checks"] = h.Checks
			}
			out := mustJSON(body)
			return tooldef.Result{
				Content: out,
				Detail:  roleDetail(string(res.Info.Role), string(res.Info.Status)),
				Output:  out,
			}, nil
		},
	}
}

func agentCancelTool(deps AgentDeps) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "agent_cancel",
			Description: `Cancel a running or starting sub-agent job and wait until it stops.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"job_id": llm.Object{
						"type": "string",
					},
				},
				Required: []string{"job_id"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				JobID string `json:"job_id"`
			}
			_ = json.Unmarshal(input, &in)
			return in.JobID
		},
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			if err := deps.Manager.HandleCancel(ctx, input); err != nil {
				return tooldef.Result{}, err
			}
			body := mustJSON(map[string]any{"ok": true})
			return tooldef.Result{Content: body, Detail: "cancelled", Output: body}, nil
		},
	}
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func truncateBytes(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "\n…(truncated)"
}

func truncateRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	var b strings.Builder
	i := 0
	for _, r := range s {
		if i >= n {
			break
		}
		b.WriteRune(r)
		i++
	}
	b.WriteString("…")
	return b.String()
}
