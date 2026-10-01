package permissiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/engineaccess"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

var permissionDescription = `Inspect the current session's APPA permission state.

Use this tool to debug permission behavior: check the current mode and
session allow-all state. All inspection is read-only.`

// PermissionTool returns the permission inspection tool definition + handler.
func PermissionTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "permission",
			Description: permissionDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "Action to perform: status, toggle_allow_all",
					},
				},
				Required: []string{"action"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in permissionInput
			_ = json.Unmarshal(input, &in)
			return fmt.Sprintf("permission %s", in.Action)
		},
		Run: runPermission,
	}
}

type permissionInput struct {
	Action string `json:"action"`
}

func runPermission(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in permissionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse permission arguments: %w", err)
	}

	action := strings.TrimSpace(in.Action)
	if action == "" {
		action = "status"
	}

	eng := engineaccess.ActiveEngine()
	if eng == nil {
		return tooldef.Result{}, fmt.Errorf("permission: no active engine")
	}

	switch action {
	case "status":
		return runPermissionStatus(eng)
	case "toggle_allow_all":
		return runPermissionToggleAllowAll(eng)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown permission action %q", action)
	}
}

func runPermissionStatus(eng engineaccess.Engine) (tooldef.Result, error) {
	mode := permission.ModeOf(eng.Gate())
	if mode == "" {
		mode = permission.ModeInteractive
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("mode: %s\n", mode))
	if g, ok := eng.Gate().(*permission.SessionAllowGate); ok && g.Enabled != nil {
		sb.WriteString(fmt.Sprintf("allow_all_session: %v\n", g.Enabled.Load()))
	} else {
		sb.WriteString("allow_all_session: false\n")
	}

	content := sb.String()
	return tooldef.Result{Content: content, Detail: "permission status", Output: content}, nil
}

func runPermissionToggleAllowAll(eng engineaccess.Engine) (tooldef.Result, error) {
	g := eng.Gate()
	if g == nil {
		return tooldef.Result{}, fmt.Errorf("permission: no gate configured")
	}

	sg, ok := g.(*permission.SessionAllowGate)
	if !ok {
		return tooldef.Result{}, fmt.Errorf("permission: session allow-all toggle not supported for gate type %T", g)
	}
	if sg.Enabled == nil {
		return tooldef.Result{}, fmt.Errorf("permission: gate has no session allow-all toggle")
	}

	next := !sg.Enabled.Load()
	sg.Enabled.Store(next)

	content := fmt.Sprintf("allow_all_session: %v\n", next)
	return tooldef.Result{Content: content, Detail: "toggled allow-all", Output: content}, nil
}
