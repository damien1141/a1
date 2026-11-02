package snapshottool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/snapshot"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

var snapshotDescription = `Explicit git snapshot tool for safe batch edits.

Creates a temporary git branch at HEAD before risky changes. You can then
rollback to that exact snapshot branch, list all snapshots, or delete them
when they are no longer needed.`

// SnapshotTool returns the explicit snapshot tool definition + handler.
func SnapshotTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "snapshot",
			Description: snapshotDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "Explicit snapshot action: create, rollback, list, delete. Example: create",
					},
					"branch": llm.Object{
						"type":        "string",
						"description": "Explicit snapshot branch name for rollback or delete. Example: harness-snapshot-20250101-120000",
					},
				},
				Required: []string{"action"},
			},
			// Not Readable: create/rollback/delete mutate the working tree and
			// branch refs, so a batch of snapshot calls must run sequentially.
			Readable: false,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in snapshotInput
			_ = json.Unmarshal(input, &in)
			action := strings.TrimSpace(in.Action)
			if action == "" {
				return "snapshot"
			}
			return fmt.Sprintf("snapshot %s", action)
		},
		Run: runSnapshot,
	}
}

type snapshotInput struct {
	Action string `json:"action"`
	Branch string `json:"branch,omitempty"`
}

func runSnapshot(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in snapshotInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse snapshot arguments: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		return tooldef.Result{}, fmt.Errorf("action is required: create, rollback, list, or delete")
	}

	proj := project.GetDefaultProject()
	if proj == nil {
		return tooldef.Result{}, fmt.Errorf("snapshot: project config not loaded")
	}
	root := proj.Root()

	mgr := snapshot.NewManager(root)

	switch action {
	case "create":
		name, err := mgr.Create(ctx)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("create snapshot: %w", err)
		}
		return tooldef.Result{
			Content: "Created explicit snapshot: " + name,
			Detail:  name,
			Output:  "Created explicit snapshot: " + name,
		}, nil
	case "rollback":
		branch := strings.TrimSpace(in.Branch)
		if branch == "" {
			return tooldef.Result{}, fmt.Errorf("rollback requires explicit branch name")
		}
		if err := mgr.Rollback(ctx, branch); err != nil {
			return tooldef.Result{}, fmt.Errorf("rollback failed: %w", err)
		}
		return tooldef.Result{
			Content: "Explicitly rolled back to " + branch,
			Detail:  branch,
			Output:  "Explicitly rolled back to " + branch,
		}, nil
	case "list":
		branches, err := mgr.List(ctx)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("list snapshots: %w", err)
		}
		if len(branches) == 0 {
			return tooldef.Result{
				Content: "No explicit snapshots found",
				Detail:  "0 snapshots",
				Output:  "No explicit snapshots found",
			}, nil
		}
		content := "Explicit snapshot branches:\n" + strings.Join(branches, "\n")
		return tooldef.Result{
			Content: content,
			Detail:  fmt.Sprintf("%d snapshots", len(branches)),
			Output:  content,
		}, nil
	case "delete":
		branch := strings.TrimSpace(in.Branch)
		if branch == "" {
			return tooldef.Result{}, fmt.Errorf("delete requires explicit branch name")
		}
		if err := mgr.Delete(ctx, branch); err != nil {
			return tooldef.Result{}, fmt.Errorf("delete snapshot: %w", err)
		}
		return tooldef.Result{
			Content: "Deleted explicit snapshot: " + branch,
			Detail:  branch,
			Output:  "Deleted explicit snapshot: " + branch,
		}, nil
	default:
		return tooldef.Result{}, fmt.Errorf("unknown action %q: use create, rollback, list, or delete", action)
	}
}
