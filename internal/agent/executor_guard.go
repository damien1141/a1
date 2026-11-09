package agent

import (
	"encoding/json"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/toolguard"
)

// Tool-call loop guardrails, ported from hermes-agent's tool_guardrails via
// the side-effect-free internal/toolguard controller. This file is the
// executor's mounting surface: it owns the two install methods and the
// folding of controller decisions into tool results.

// SetLoopGuard attaches the tool-call loop guardrail controller (nil =
// disabled — a bare executor, as tests build). The controller's per-turn
// state is reset via ResetLoopGuard at every user prompt.
func (e *Executor) SetLoopGuard(c *toolguard.Controller) {
	if e == nil {
		return
	}
	e.loopGuard = c
}

// ResetLoopGuard clears the loop-guard controller's per-turn state. The
// streaks are per-turn by design: a new user message is a new experiment, so
// re-reads and re-tries after a prompt are never flagged. Nil-safe for bare
// executors.
func (e *Executor) ResetLoopGuard() {
	if e == nil || e.loopGuard == nil {
		return
	}
	e.loopGuard.Reset()
}

// SetDispatchWhitelist restricts dispatch to the named tools; a call to any
// other tool is refused before budget, permission, or execution. The denier
// renders the refusal (it can name what IS allowed); a nil denier gets a
// default message. Used by constrained sub-engines whose tool surface IS
// the policy — the learning-review fork allows only memory and skill-file
// tools, so an out-of-surface call is a policy refusal, not a permission
// question (an Ask would deadlock an unattended fork).
func (e *Executor) SetDispatchWhitelist(names map[string]bool, denyMessage func(tool string) string) {
	if e == nil {
		return
	}
	e.dispatchWhitelist = names
	e.dispatchDenier = denyMessage
}

// guardRefusal folds one synthetic refusal into the loop guard and returns
// the refusal text with any warn guidance appended. Refusals count as the
// failure the streaks need: some models genuinely believe they sent the
// arguments, so the re-ask is identical and only the accumulating warning
// teaches the model to change course.
func (e *Executor) guardRefusal(call llm.ToolCall, args json.RawMessage, errText string) string {
	if e.loopGuard == nil {
		return errText
	}
	readable := false
	if tool, ok := e.registry[call.Function.Name]; ok {
		readable = tool.Definition.Readable
	}
	d, notice, _ := e.loopGuard.Report(call.Function.Name, string(args), errText, call.ID, true, readable)
	return appendGuardNotes(errText, d, notice)
}

// appendGuardNotes renders guardrail guidance onto a tool result body: warn
// decisions and identical-call/cycle notices ride the result the model reads
// next round.
func appendGuardNotes(content string, d *toolguard.Decision, notice string) string {
	if d != nil && d.Action == "warn" {
		content += "\n\nTool loop warning: " + d.Message
	}
	if notice != "" {
		content += "\n\n" + notice
	}
	return content
}

// guardDispatch refuses a call outside the dispatch whitelist. The bool
// reports whether the call was refused (the message is the refusal result).
func (e *Executor) guardDispatch(call llm.ToolCall, detail string, emit func(session.ToolData) bool) (llm.Message, bool) {
	if e.dispatchWhitelist == nil || e.dispatchWhitelist[call.Function.Name] {
		return llm.Message{}, false
	}
	msg := "tool '" + call.Function.Name + "' is not available in this context"
	if e.dispatchDenier != nil {
		if m := e.dispatchDenier(call.Function.Name); m != "" {
			msg = m
		}
	}
	msg = e.guardRefusal(call, json.RawMessage(call.Function.Arguments), msg)
	_ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, msg, "")})
	return e.toolMessage(call.ID, msg, nil), true
}

// SetLearningSink installs the nudge-counter reset hook the engine's
// learning loop owns: the counters tick on opportunity and reset on actual
// use, and "actual use" is a dispatch that passed every gate and is about
// to run. Nil (a bare executor) disables it.
func (e *Executor) SetLearningSink(fn func(tool string, args json.RawMessage)) {
	if e == nil {
		return
	}
	e.learningSink = fn
}
