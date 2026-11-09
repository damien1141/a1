package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "sort"
        "strings"
        "sync/atomic"
        "time"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/telemetry"
        "github.com/damien1141/a1/internal/tools"
)

// scriptCallSeq stamps synthetic call ids for in-script tool calls so
// extension events and telemetry rows stay attributable ("script-7" — the
// 7th RPC call of this process's script executions).
var scriptCallSeq atomic.Int64

// DispatchScript runs one in-script tool call (the script tool's RPC
// server, internal/tools/scripttool) through the normal per-call pipeline:
// extension PreTool, permission extraction + gate check + ask, the tool's
// Run, evidence and mutation recording, extension PostTool. This is the
// load-bearing half of the tool-RPC steal — the context savings come from
// collapsing tool steps, and the safety comes from none of those steps
// bypassing the gate the model's own calls face.
//
// Differences from runOneChecked are deliberate:
//   - no ToolData emit: in-script calls are not model-visible turns; the
//     result JSON carries tool_calls_made + tool_errors instead (hermes's
//     contract — the transcript stays one row per script call).
//   - no claimCall: the RPC server's own budget governs inner calls; the
//     executor allowance prices model-visible calls only.
//   - no loop-guard streaks: the per-turn guardrails watch the model's
//     visible call patterns; a script looping on one failing call burns
//     its RPC budget and returns — the failure rides tool_errors.
//   - no read elision: inner read results never enter the model context
//     verbatim (only what the script prints does), so there is nothing to
//     elide.
func (e *Executor) DispatchScript(ctx context.Context, toolName string, args json.RawMessage) (string, bool) {
        if e == nil {
                return "script dispatch: no executor", false
        }
        tool, found := e.registry[toolName]
        if !found {
                return fmt.Sprintf(
                        "tool '%s' is not available in this session. Available: %s",
                        toolName, strings.Join(e.registryNames(), ", ")), false
        }

        callID := fmt.Sprintf("script-%d", scriptCallSeq.Add(1))
        call := llm.ToolCall{
                ID:   callID,
                Type: "function",
                Function: llm.Function{
                        Name:      toolName,
                        Arguments: string(args),
                },
        }

        e.ext.EmitToolExecutionStart(toolName, callID, args)

        // Extension Pre runs before permission Ask so org policy can deny
        // without prompting the user — same order as model-visible calls.
        newArgs, blocked, reason, _ := e.ext.PreTool(ctx, toolName, callID, args)
        if blocked {
                if reason == "" {
                        reason = "tool execution denied by extension"
                }
                e.ext.EmitToolExecutionEnd(toolName, callID, true)
                return reason, false
        }
        if len(newArgs) > 0 {
                args = newArgs
        }

        if msg, rejected := e.checkScriptPermission(ctx, call, args); rejected {
                e.ext.EmitToolExecutionEnd(toolName, callID, true)
                return msg, false
        }

        if e.telemetry != nil {
                e.telemetry.Record(
                        telemetry.Event{Kind: telemetry.EventToolCall, Tool: toolName, CallID: callID, When: time.Now()},
                )
        }

        result, err := tool.Run(tools.WithToolCallID(ctx, callID), args)

        content, output := "", ""
        if err != nil {
                if ctx.Err() != nil {
                        e.ext.EmitToolExecutionEnd(toolName, callID, true)
                        return "tool call canceled", false
                }
                content = err.Error()
        } else {
                content = result.Content
                output = result.Output
                if output == "" {
                        output = result.Content
                }
        }

        // Evidence ledger: a passing test run inside a script is exactly the
        // evidence the verify-on-stop gate prices — skipping this would make
        // script the verification bypass.
        if err == nil && e.evidence != nil && toolName == "bash" {
                if note := e.recordEvidence(ctx, args, output); note != "" {
                        content = content + "\n\n" + note
                }
        }

        // Mutation tracking: in-script writes must trip the verify-on-stop
        // gate the same way direct writes do, or a script-driven edit session
        // could finish "verified" on stale evidence.
        if err == nil && !tool.Definition.Readable {
                e.recordMutation(call, args)
        }

        newContent, _, _, _ := e.ext.PostTool(ctx, toolName, callID, args, content, err != nil, errorText(err))
        if newContent != "" && newContent != content {
                content = newContent
        }
        e.ext.EmitToolExecutionEnd(toolName, callID, err != nil)
        return content, err == nil
}

// checkScriptPermission mirrors checkPermission for an in-script call:
// extraction, gate check, authority consultation, and the interactive ask
// (serialized under askMu like every other approval). The deny/ask texts
// return to the script as ok=false results, so the model reads them in
// tool_errors even when the script ignores them.
func (e *Executor) checkScriptPermission(ctx context.Context, call llm.ToolCall, args json.RawMessage) (string, bool) {
        req, err := permission.ExtractAt(call.Function.Name, args, e.cwd)
        if err != nil {
                return fmt.Sprintf("permission check failed: %v (arguments: %s)", err, truncateForModel(string(args))), true
        }
        dec, reason := e.gate.Check(ctx, req)
        dec, reason = e.consultAuthority(ctx, dec, reason, req, e.prospectiveGaps(req))
        switch dec {
        case permission.Allow:
                return "", false
        case permission.Deny:
                if reason == "" {
                        reason = "tool execution denied by permissions"
                }
                return reason, true
        case permission.Ask:
                if e.ask == nil {
                        if reason == "" {
                                reason = "tool requires approval but no ask handler is configured"
                        }
                        return reason, true
                }
                res, askErr := func() (permission.AskResult, error) {
                        e.askMu.Lock()
                        defer e.askMu.Unlock()
                        return e.ask(ctx, req, reason)
                }()
                if askErr != nil {
                        return fmt.Sprintf("approval failed: %v", askErr), true
                }
                if !res.Approved {
                        msg := "tool execution rejected by user"
                        if res.Feedback != "" {
                                msg = "This tool call was rejected by the user with feedback: " + res.Feedback
                        }
                        return msg, true
                }
                return "", false
        default:
                return "unknown permission decision", true
        }
}

// registryNames lists registered tool names for refusal messages.
func (e *Executor) registryNames() []string {
        names := make([]string, 0, len(e.registry))
        for name := range e.registry {
                names = append(names, name)
        }
        sort.Strings(names)
        return names
}

func errorText(err error) string {
        if err == nil {
                return ""
        }
        return err.Error()
}
