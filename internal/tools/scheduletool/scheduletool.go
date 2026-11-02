// Package scheduletool exposes the Clock's model-facing surface (Bet 01):
// a single `schedule` tool with an action enum (create | list | cancel).
// One facade, not three tools — the model's tool-selection head is the
// scarcest resource in the system (see the roadmap's anti-goals).
package scheduletool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/schedule"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const scheduleGuidance = `Create, list, or cancel durable wake-up schedules (they survive restarts and fire while a daemon like a1 serve is running).

Pick ONE trigger per schedule:
- cron: 5-field expression, minute granularity — "30 3 * * *" (03:30 daily), "*/20 * * * *" (every 20 min), "0 22 * * 1-5" (22:00 weekdays).
- fixed_rate: duration like "15m" or "2h" — fires repeatedly.
- one_time: RFC3339 timestamp like "2026-12-01T09:00:00Z" — fires once, then disables.

When to schedule instead of spawning: work that must happen later or repeatedly (nightly checks, watch-dog passes, follow-ups after a deploy). Use agent_spawn for work that should start NOW.

Wake-ups run as jobs: they inherit the task contract you declared with the contract tool (your finish line survives the night) and default to role=worker — headless-strict, so anything needing approval is denied. Give the prompt everything: a wake-up cannot ask follow-ups.

Create returns schedule_id + next fire time. Cancel disables (history stays). List shows id, trigger, next fire, and status.`

// Deps wires the schedule tool to the process scheduler. WorkDir and
// FleetContract are read at call time — the contract a schedule inherits
// is the live task contract at create time (the finish line that survives).
type Deps struct {
	Scheduler     *schedule.Scheduler
	WorkDir       func() string
	FleetContract func() []string
}

// ScheduleTool returns the schedule facade tool (nil deps → nil).
func ScheduleTool(deps Deps) tooldef.Tool {
	if deps.Scheduler == nil {
		return tooldef.Tool{}
	}
	if deps.WorkDir == nil {
		deps.WorkDir = func() string { return "" }
	}
	if deps.FleetContract == nil {
		deps.FleetContract = func() []string { return nil }
	}
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "schedule",
			Description: scheduleGuidance,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "create | list | cancel",
						"enum":        []string{"create", "list", "cancel"},
					},
					"prompt": llm.Object{
						"type":        "string",
						"description": "create only: the fully self-contained prompt the wake-up runs. Include context, scope, and what the result must contain — it cannot ask follow-ups.",
					},
					"description": llm.Object{
						"type":        "string",
						"description": "create only: short label for listings (e.g. \"nightly lint\").",
					},
					"cron": llm.Object{
						"type":        "string",
						"description": "create only: 5-field cron expression (mutually exclusive with fixed_rate / one_time).",
					},
					"fixed_rate": llm.Object{
						"type":        "string",
						"description": "create only: duration like \"15m\" (mutually exclusive with cron / one_time).",
					},
					"one_time": llm.Object{
						"type":        "string",
						"description": "create only: RFC3339 timestamp, fires once (mutually exclusive with cron / fixed_rate).",
					},
					"workdir": llm.Object{
						"type":        "string",
						"description": "create only: working directory for the wake-up (default: this session's cwd).",
					},
					"role": llm.Object{
						"type":        "string",
						"description": "create only: explore | review | worker (default worker — headless-strict, full tools).",
						"enum":        []string{"explore", "review", "worker"},
					},
					"timeout_sec": llm.Object{
						"type":        "integer",
						"description": "create only: run timeout per fire in seconds.",
					},
					"schedule_id": llm.Object{
						"type":        "string",
						"description": "cancel only: the schedule id from create.",
					},
				},
				Required: []string{"action"},
			},
		},
		DetailFromArgs: scheduleDetail,
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			var in scheduleInput
			if err := json.Unmarshal(input, &in); err != nil {
				return tooldef.Result{}, err
			}
			switch strings.ToLower(strings.TrimSpace(in.Action)) {
			case "create":
				return runCreate(ctx, deps, in)
			case "list":
				return runList(deps)
			case "cancel":
				return runCancel(deps, in)
			default:
				return tooldef.Result{}, fmt.Errorf("schedule: unknown action %q (want create, list, or cancel)", in.Action)
			}
		},
	}
}

type scheduleInput struct {
	Action      string `json:"action"`
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Cron        string `json:"cron"`
	FixedRate   string `json:"fixed_rate"`
	OneTime     string `json:"one_time"`
	WorkDir     string `json:"workdir"`
	Role        string `json:"role"`
	TimeoutSec  int    `json:"timeout_sec"`
	ScheduleID  string `json:"schedule_id"`
}

func runCreate(ctx context.Context, deps Deps, in scheduleInput) (tooldef.Result, error) {
	trigger, err := schedule.ParseTrigger(in.Cron, in.FixedRate, in.OneTime)
	if err != nil {
		return tooldef.Result{}, err
	}
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = string(job.RoleWorker) // headless-strict unattended default
	}
	if _, err := job.ParseRole(role); err != nil {
		return tooldef.Result{}, err
	}
	wd := strings.TrimSpace(in.WorkDir)
	if wd == "" {
		wd = deps.WorkDir()
	}
	// The finish line: the live task contract at create time. Scheduled
	// work inherits the fleet's declared checks (Bet 04 task contracts).
	contract := deps.FleetContract()
	sc := schedule.Schedule{
		Prompt:      in.Prompt,
		Description: in.Description,
		Trigger:     trigger,
		WorkDir:     wd,
		Role:        role,
		Contract:    contract,
	}
	if in.TimeoutSec > 0 {
		sc.Timeout = time.Duration(in.TimeoutSec) * time.Second
	}
	created, err := deps.Scheduler.Upsert(ctx, sc)
	if err != nil {
		return tooldef.Result{}, err
	}
	body := map[string]any{
		"schedule_id": created.ID,
		"trigger":     created.Trigger.Summary(),
		"next_run_at": tsOrNever(created.NextRunAt),
		"role":        created.Role,
		"enabled":     created.Enabled,
	}
	if len(created.Contract) > 0 {
		body["contract"] = created.Contract
	}
	if created.NextRunAt.IsZero() {
		body["note"] = "one_time is already in the past; the schedule disables itself on the next scheduler pass"
	}
	return jsonResult(body, created.Description)
}

func runList(deps Deps) (tooldef.Result, error) {
	rows := deps.Scheduler.List()
	body := make([]map[string]any, 0, len(rows))
	for _, sc := range rows {
		body = append(body, map[string]any{
			"schedule_id": sc.ID,
			"trigger":     sc.Trigger.Summary(),
			"next_run_at": tsOrNever(sc.NextRunAt),
			"last_run_at": tsOrNever(sc.LastRunAt),
			"enabled":     sc.Enabled,
			"role":        sc.Role,
			"prompt":      truncate(sc.Prompt, 120),
			"source":      sc.Source,
		})
	}
	out := map[string]any{"schedules": body, "count": len(body)}
	b, _ := json.MarshalIndent(out, "", "  ")
	return tooldef.Result{
		Content: string(b),
		Detail:  fmt.Sprintf("%d schedules", len(body)),
		Output:  string(b),
	}, nil
}

func runCancel(deps Deps, in scheduleInput) (tooldef.Result, error) {
	id := strings.TrimSpace(in.ScheduleID)
	if id == "" {
		return tooldef.Result{}, fmt.Errorf("schedule: cancel requires schedule_id")
	}
	cancelled, err := deps.Scheduler.Cancel(id)
	if err != nil {
		return tooldef.Result{}, err
	}
	return jsonResult(map[string]any{
		"schedule_id": id,
		"enabled":     false,
		"last_run_at": tsOrNever(cancelled.LastRunAt),
	}, "cancelled "+id)
}

func jsonResult(body map[string]any, detail string) (tooldef.Result, error) {
	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return tooldef.Result{}, err
	}
	return tooldef.Result{Content: string(b), Detail: detail, Output: string(b)}, nil
}

func scheduleDetail(input json.RawMessage) string {
	var in scheduleInput
	_ = json.Unmarshal(input, &in)
	switch strings.ToLower(in.Action) {
	case "create":
		label := strings.TrimSpace(in.Description)
		if label == "" {
			label = truncate(in.Prompt, 60)
		}
		which := in.Cron
		if which == "" {
			which = in.FixedRate
		}
		if which == "" {
			which = in.OneTime
		}
		return fmt.Sprintf("create %s · %s", which, label)
	case "cancel":
		return "cancel " + in.ScheduleID
	default:
		return "list schedules"
	}
}

func tsOrNever(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
