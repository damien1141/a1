package webserver

import (
        "embed"
        "fmt"
        "html"
        "io"
        "net/http"
        "regexp"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/session"
)

//go:embed dist/*
var distFS embed.FS

// page is the compiled index.html from the Preact frontend.
var page []byte

func init() {
        page, _ = distFS.ReadFile("dist/index.html")
}

// Inline span matchers. They run after html.EscapeString, so the patterns only
// ever match already-escaped text and cannot reintroduce raw HTML.
var (
        inlineCodeRe = regexp.MustCompile("`([^`]+)`")
        bold         = regexp.MustCompile(`\*\*([^*]+)\*\*`)
        italic       = regexp.MustCompile(`\*([^*]+)\*`)
)

// inlineCode renders `code` spans, escaping the delimiter content again so a
// backtick pair inside escaped text still shows literally.
func inlineCode(s string) string {
        return inlineCodeRe.ReplaceAllString(s,
                `<code>$1</code>`)
}

// renderFragment writes an htmx fragment: raw HTML, never JSON, because the
// browser swaps it straight into the DOM.
func renderFragment(w http.ResponseWriter, html string) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _, _ = io.WriteString(w, html)
}

// sessionListFragment renders the sidebar's session rows, pinned first, then
// newest first inside each group.
func sessionListFragment(sessions []session.SessionMeta, pins []string) string {
        var sb strings.Builder
        if len(sessions) == 0 {
                // No Tailwind utilities here: this fragment is swapped in by htmx after
                // load, and the Tailwind browser build only scans the classes present
                // in the initial document, so utility classes would silently no-op.
                sb.WriteString(`<p class="sessions-empty">No sessions yet. Start a new chat.</p>`)
                return sb.String()
        }
        pinned := pinnedSet(pins)
        for _, s := range orderByPin(sessions, pinned) {
                title := html.EscapeString(sessionTitleFromMeta(s))
                fmt.Fprintf(&sb,
                        `<button type="button" class="session-row%[1]s" data-session-id="%[2]s" data-title="%[3]s" data-mtime="%[6]s">`+
                                `<span class="session-pin" aria-hidden="true">%[4]s</span>`+
                                `<span class="session-title">%[3]s</span>`+
                                `<span class="session-meta">%[5]s</span>`+
                                `</button>`,
                        pinClass(pinned[s.ID]),
                        html.EscapeString(s.ID),
                        title,
                        pinGlyph(pinned[s.ID]),
                        html.EscapeString(friendlySessionTime(s)),
                        html.EscapeString(s.Mtime.UTC().Format(time.RFC3339Nano)),
                )
        }
        return sb.String()
}

// pinnedSet indexes the pin list for cheap membership tests.
func pinnedSet(pins []string) map[string]bool {
        out := make(map[string]bool, len(pins))
        for _, p := range pins {
                out[p] = true
        }
        return out
}

// orderByPin puts pinned sessions first, preserving the input order (newest
// first) inside each group.
func orderByPin(sessions []session.SessionMeta, pinned map[string]bool) []session.SessionMeta {
        out := make([]session.SessionMeta, 0, len(sessions))
        for _, s := range sessions {
                if pinned[s.ID] {
                        out = append(out, s)
                }
        }
        for _, s := range sessions {
                if !pinned[s.ID] {
                        out = append(out, s)
                }
        }
        return out
}

func pinClass(pinned bool) string {
        if pinned {
                return " session-pinned"
        }
        return ""
}

// pinGlyph is the row's pin marker: filled for pinned, hollow otherwise.
func pinGlyph(pinned bool) string {
        if pinned {
                return "&#9733;"
        }
        return "&#9734;"
}

// compactionDividerText is the divider every view renders for a compaction
// cut: the reload paths (HTML fragment and JSON transcript) for the stored
// entry, and the live marker event for the cut as it happens. One string,
// three call sites — a session watched live and the same session reloaded
// must draw the same line at the same place.
func compactionDividerText(tokensBefore int) string {
        if tokensBefore <= 0 {
                return "Compacted context"
        }
        return fmt.Sprintf("Compacted context (%d tokens)", tokensBefore)
}

// transcriptFragment renders a stored transcript: user text, assistant text and
// thinking, tool runs, and compaction markers. An empty session renders the
// same start page the client draws, so switching to an unused session and
// clicking "New chat" look identical.
func transcriptFragment(snap session.Snapshot) string {
        if len(snap.Messages) == 0 {
                return emptyStateFragment()
        }
        var sb strings.Builder
        for _, m := range snap.Messages {
                switch m.Role {
                case session.RoleUser:
                        fmt.Fprintf(&sb, `<div class="bubble bubble-user">%s</div>`, renderMarkdown(m.Text))
                case session.RoleAssistant:
                        sb.WriteString(assistantBubble(m))
                case session.RoleCompaction:
                        fmt.Fprintf(&sb, `<div class="bubble bubble-system">%s</div>`,
                                html.EscapeString(compactionDividerText(m.TokensBefore)))
                }
        }
        sb.WriteString(toolsFragment(snap))
        return sb.String()
}

// emptyStateFragment is the landing state for a session with no messages. The
// ids match the client-side start page so both paths light up the same rules.
func emptyStateFragment() string {
        return `<div class="empty-state" id="start-page">` +
                `<h1>What can I do for you?</h1>` +
                `<p>Ask anything about this workspace.</p>` +
                `</div>`
}

// assistantBubble renders one assistant message: optional thinking disclosure,
// text blocks in order, then the tool runs it issued.
func assistantBubble(m session.Message) string {
        var sb strings.Builder
        sb.WriteString(`<div class="bubble bubble-assistant">`)
        var think, text strings.Builder
        for _, b := range m.Content {
                switch b.Type {
                case session.BlockThinking:
                        think.WriteString(b.Text)
                case session.BlockText:
                        text.WriteString(b.Text)
                case session.BlockToolUse:
                        text.WriteString("\n\n→ ")
                        text.WriteString(b.Name)
                }
        }
        if t := strings.TrimSpace(think.String()); t != "" {
                // Reasoning is free prose with its own line breaks; preformatted text
                // preserves them, whereas the markdown renderer would collapse each
                // line into its own paragraph.
                fmt.Fprintf(
                        &sb,
                        `<details class="thinking"><summary>Thinking</summary><div class="thinking-body">%s</div></details>`,
                        html.EscapeString(t),
                )
        }
        if t := strings.TrimSpace(text.String()); t != "" {
                sb.WriteString(renderMarkdown(t))
        }
        if m.Usage.Reported() {
                usage := fmt.Sprintf("%d in · %d out", m.Usage.PromptTokens, m.Usage.CompletionTokens)
                if m.Usage.TPS > 0 {
                        usage += fmt.Sprintf(" · %.0f tok/s", m.Usage.TPS)
                }
                fmt.Fprintf(&sb, `<div class="usage">%s</div>`, usage)
        }
        sb.WriteString(`</div>`)
        return sb.String()
}

// toolsFragment renders the tool rows of a snapshot below the transcript so
// they read as a trace of what the agent ran.
func toolsFragment(snap session.Snapshot) string {
        if len(snap.Tools) == 0 {
                return ""
        }
        var sb strings.Builder
        sb.WriteString(`<div class="tool-trace">`)
        for _, run := range snap.Tools {
                status := html.EscapeString(run.Status.String())
                fmt.Fprintf(&sb,
                        `<div class="tool-row tool-%s" data-tool-id="%s">`+
                                `<span class="tool-name">%s</span>`+
                                `<span class="tool-status">%s</span>`+
                                `<span class="tool-detail">%s</span>`+
                                `</div>`,
                        status,
                        html.EscapeString(run.ToolUseID),
                        html.EscapeString(run.Name),
                        status,
                        html.EscapeString(toolDetailText(run)),
                )
        }
        sb.WriteString(`</div>`)
        return sb.String()
}

// toolDetailText picks the most informative one-liner for a tool row.
func toolDetailText(run session.ToolRun) string {
        if run.Detail != "" {
                return run.Detail
        }
        if run.Error != "" {
                return run.Error
        }
        out := strings.TrimSpace(run.Output)
        if len(out) > 160 {
                return out[:160] + "…"
        }
        return out
}

// friendlySessionTime renders the sidebar's meta line: a compact relative
// label instead of the raw filename timestamp plus 32-char session hash the
// row used to show ("2026-10-24T21-43-03 · 44cbf2b0…" reads as machine
// noise, not information — the row already has the title). The session id
// stays available on data-session-id for programmatic use.
func friendlySessionTime(s session.SessionMeta) string {
        t := s.Mtime
        if t.IsZero() {
                return ""
        }
        d := time.Since(t)
        switch {
        case d < 0:
                // Clock skew: fall back to the absolute label.
                return t.Format("Jan 2 15:04")
        case d < time.Minute:
                return "just now"
        case d < time.Hour:
                return fmt.Sprintf("%dm ago", int(d.Minutes()))
        case d < 24*time.Hour:
                return fmt.Sprintf("%dh ago", int(d.Hours()))
        case d < 48*time.Hour:
                return "yesterday"
        case t.Year() == time.Now().Year():
                return t.Format("Jan 2")
        default:
                return t.Format("Jan 2, 2006")
        }
}

// loadPage reads the embedded web UI document. It is compiled into the
// binary, so no file is resolved at runtime and the server cannot fail to
// find its own page on another machine.
func loadPage() ([]byte, error) {
        return page, nil
}

// renderMarkdown is a deliberately small markdown subset renderer: fenced code,
// inline code, bold, italic, and paragraphs. The transcript is machine-generated
// text and code, so pulling in a full markdown dependency would dwarf the
// payload this UI needs.
func renderMarkdown(src string) string {
        var sb strings.Builder
        var code strings.Builder
        inCode := false

        lines := strings.Split(src, "\n")
        for i, line := range lines {
                trimmed := strings.TrimSpace(line)
                switch {
                case strings.HasPrefix(trimmed, "```"):
                        if !inCode {
                                inCode = true
                                code.Reset()
                                continue
                        }
                        inCode = false
                        fmt.Fprintf(&sb, `<pre><code>%s</code></pre>`, html.EscapeString(code.String()))
                        continue
                case inCode:
                        code.WriteString(line)
                        if i+1 < len(lines) {
                                code.WriteString("\n")
                        }
                        continue
                case trimmed == "":
                        continue
                }
                sb.WriteString("<p>")
                sb.WriteString(inlineMarkdown(trimmed))
                sb.WriteString("</p>")
        }
        if inCode {
                fmt.Fprintf(&sb, `<pre><code>%s</code></pre>`, html.EscapeString(code.String()))
        }
        return sb.String()
}

// inlineMarkdown escapes then re-applies `code`, **bold**, and *italic* spans.
func inlineMarkdown(s string) string {
        s = html.EscapeString(s)
        s = bold.ReplaceAllString(s, "<strong>$1</strong>")
        s = italic.ReplaceAllString(s, "<em>$1</em>")
        return inlineCode(s)
}
