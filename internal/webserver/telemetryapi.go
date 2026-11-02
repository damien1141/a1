// telemetryapi.go serves the Bet 07 telemetry surfaces: the JSON rollup at
// /api/telemetry and the server-rendered dashboard at /stats. Both are
// loopback-only like the rest of the chat API (no extra auth) and read the
// vault's SQLite store — the dashboard needs no frontend rebuild, it is
// plain HTML from the Go binary.
package webserver

import (
	"database/sql"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/telemetry"
)

// telemetryOnce lazily installs the telemetry sink on the vault's database
// the first time a telemetry surface is hit. Installing here (not in the
// server constructor) keeps server.go untouched: the sink is observation,
// and a degraded vault simply serves an empty dashboard.
var telemetryOnce sync.Once

// telemetryDB returns the vault's database after the one-time sink install,
// or nil when the vault is degraded (file layout): both surfaces then
// render their empty states.
func (s *Server) telemetryDB() *sql.DB {
	if s == nil || s.vault == nil {
		return nil
	}
	v := s.vault
	telemetryOnce.Do(func() {
		if err := telemetry.AttachDB(v.DB()); err != nil {
			// Surfaced nowhere beyond the empty dashboard; the chat surface
			// must never break because observation failed to start.
			_ = err
		}
	})
	return v.DB()
}

// handleTelemetry serves the machine-readable rollup: per-tool aggregates
// (calls, ok, errors, mean latency), the most recent errors, and per-
// session turn token totals.
func (s *Server) handleTelemetry(w http.ResponseWriter, _ *http.Request) {
	db := s.telemetryDB()
	if db == nil {
		writeJSON(w, http.StatusOK, telemetryReport{
			Tools:  []telemetry.ToolAggregate{},
			Errors: []telemetry.ToolRunRow{},
			Turns:  []telemetry.TurnSummaryRow{},
		})
		return
	}
	aggs, err := telemetry.ToolAggregates(db)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "telemetry: "+err.Error())
		return
	}
	errs, err := telemetry.RecentErrors(db, 10)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "telemetry: "+err.Error())
		return
	}
	turns, err := telemetry.TurnSummary(db)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "telemetry: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, telemetryReport{Tools: aggs, Errors: errs, Turns: turns})
}

// telemetryReport is the /api/telemetry payload.
type telemetryReport struct {
	Tools  []telemetry.ToolAggregate  `json:"tools"`
	Errors []telemetry.ToolRunRow     `json:"recent_errors"`
	Turns  []telemetry.TurnSummaryRow `json:"turns"`
}

// handleStatsDashboard renders the /stats HTML page: dark theme, one
// <style> block, no JS, no frontend dependency. Token totals come from the
// eval-materialized turns table (the durable source of truth), tool
// latency from the sink's live rows.
func (s *Server) handleStatsDashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	var b strings.Builder
	b.WriteString(statsPageHead)
	db := s.telemetryDB()
	if db == nil {
		b.WriteString("<p class='empty'>vault not running — telemetry is empty (file layout).</p>")
		b.WriteString("</main></body></html>")
		_, _ = w.Write([]byte(b.String()))
		return
	}

	aggs, aggErr := telemetry.ToolAggregates(db)
	errs, _ := telemetry.RecentErrors(db, 10)
	turns, turnErr := telemetry.TurnSummary(db)
	if aggErr != nil || turnErr != nil {
		b.WriteString("<p class='empty'>telemetry store unavailable.</p>")
		b.WriteString("</main></body></html>")
		_, _ = w.Write([]byte(b.String()))
		return
	}

	b.WriteString("<h2>Tools</h2>")
	if len(aggs) == 0 {
		b.WriteString("<p class='empty'>no tool runs recorded yet.</p>")
	} else {
		b.WriteString("<table><tr><th>tool</th><th>calls</th><th>ok</th><th>errors</th><th>mean latency</th></tr>")
		for _, a := range aggs {
			b.WriteString("<tr><td>" + html.EscapeString(a.Tool) + "</td><td>" +
				strconv.Itoa(a.Calls) + "</td><td>" + strconv.Itoa(a.OK) + "</td><td>" +
				strconv.Itoa(a.Errors) + "</td><td>" +
				strconv.FormatFloat(a.MeanLatency, 'f', 1, 64) + " ms</td></tr>")
		}
		b.WriteString("</table>")
	}

	b.WriteString("<h2>Recent errors</h2>")
	if len(errs) == 0 {
		b.WriteString("<p class='empty'>none — nice.</p>")
	} else {
		b.WriteString("<table><tr><th>tool</th><th>error</th></tr>")
		for _, e := range errs {
			b.WriteString("<tr><td>" + html.EscapeString(e.Tool) + "</td><td><code>" +
				html.EscapeString(truncateStatsText(e.Err, 120)) + "</code></td></tr>")
		}
		b.WriteString("</table>")
	}

	b.WriteString("<h2>Turn token spend</h2>")
	if len(turns) == 0 {
		b.WriteString("<p class='empty'>no turns materialized yet — run <code>a1 eval</code> to score stored sessions.</p>")
	} else {
		b.WriteString("<table><tr><th>session</th><th>model</th><th>turns</th><th>total tokens</th></tr>")
		for _, t := range turns {
			b.WriteString("<tr><td>" + html.EscapeString(truncateStatsText(t.SessionID, 16)) + "</td><td>" +
				html.EscapeString(t.Model) + "</td><td>" + strconv.Itoa(t.Turns) + "</td><td>" +
				strconv.FormatInt(t.TotalTok, 10) + "</td></tr>")
		}
		b.WriteString("</table>")
	}

	b.WriteString("</main></body></html>")
	_, _ = w.Write([]byte(b.String()))
}

func truncateStatsText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// statsPageHead is the dashboard's static prefix: dark theme variables
// matching the chat UI's palette direction, zero JS.
const statsPageHead = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>a1 stats</title>
<style>
  :root { color-scheme: dark; }
  body {
    margin: 0; padding: 2rem;
    background: #0e1116; color: #d7dde6;
    font: 14px/1.5 ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  }
  main { max-width: 52rem; margin: 0 auto; }
  h1 { font-size: 1.2rem; letter-spacing: .08em; color: #8ea2c6; }
  h2 { font-size: 1rem; margin-top: 2rem; color: #8ea2c6; }
  table { border-collapse: collapse; width: 100%; margin-top: .5rem; }
  th, td { text-align: left; padding: .35rem .6rem; border-bottom: 1px solid #232a35; }
  th { color: #6f7d94; font-weight: normal; }
  td code { color: #e08c7a; }
  .empty { color: #6f7d94; }
  a { color: #7aa2f7; text-decoration: none; }
</style>
</head>
<body>
<main>
<h1>a1 · telemetry</h1>
`
