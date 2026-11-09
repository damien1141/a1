package agent

import (
        "fmt"
        "strings"

        "github.com/damien1141/a1/internal/llm"
)

// jobNoticeSummaryLimit caps each delivered summary inside the injected
// notice — the full result stays in result.md and in agent_wait's reply.
const jobNoticeSummaryLimit = 800

// buildJobNotices collects finished background jobs addressed to this
// session (the durable outbox's consumer side) and renders them as one
// transient system message. Nil when there is nothing to present.
//
// The message is NOT persisted — like the memory inlet it is rebuilt per
// turn — but unlike memory, each delivery row is CONSUMED by collection:
// a result is presented at exactly one turn boundary, then acked. A job
// that finishes mid-turn reaches the parent either here (next turn) or
// via agent_wait (this turn); the wait tool's ack keeps the two paths
// from double-presenting.
func (engine *Engine) buildJobNotices() *llm.Message {
        if engine == nil || engine.jobs == nil {
                return nil
        }
        deliveries := engine.jobs.CollectNotices(engine.SessionID())
        if len(deliveries) == 0 {
                return nil
        }
        var sb strings.Builder
        sb.WriteString("Background sub-agent job results (delivered at the start of this turn; each is presented exactly once):\n")
        for _, d := range deliveries {
                label := d.Description
                if label == "" {
                        label = d.JobID
                }
                head := fmt.Sprintf("- %s (%s) — %s", label, d.JobID, d.Status)
                if d.Role != "" {
                        head = fmt.Sprintf("- %s (%s, %s) — %s", label, d.JobID, d.Role, d.Status)
                }
                sb.WriteString(head)
                sb.WriteString("\n")
                if d.Error != "" {
                        sb.WriteString("  error: " + strings.TrimSpace(d.Error) + "\n")
                }
                if s := trimNoticeSummary(d.Summary); s != "" {
                        sb.WriteString("  summary: " + s + "\n")
                }
        }
        return &llm.Message{
                Role:    llm.RoleSystem,
                Content: sb.String(),
        }
}

// trimNoticeSummary folds a result summary to one presentable line,
// capped at jobNoticeSummaryLimit bytes on a rune boundary.
func trimNoticeSummary(s string) string {
        s = strings.TrimSpace(s)
        if s == "" {
                return ""
        }
        // Newlines flatten: a notice line is one line.
        s = strings.Join(strings.Fields(s), " ")
        if len(s) > jobNoticeSummaryLimit {
                cut := s[:jobNoticeSummaryLimit]
                // Back off to a rune boundary (the cap may land mid-rune).
                for len(cut) > 0 && (cut[len(cut)-1]&0xC0) == 0x80 {
                        cut = cut[:len(cut)-1]
                }
                s = cut + "…"
        }
        return s
}
