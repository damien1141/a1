package webserver

import (
	"context"
	"net/http"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/permission"
)

// turnKey tags the session id a turn belongs to. The Ask handler runs on the
// engine's goroutine with no direct line back to the HTTP request, so the id
// travels on the context instead of a field on Engine.
type turnKey struct{}

func withSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, turnKey{}, sessionID)
}

func sessionFromContext(ctx context.Context) string {
	id, _ := ctx.Value(turnKey{}).(string)
	return id
}

// loopOpts are the per-turn options for the web surface. No pending skills and
// no image attachments: the composer is plain text, so the defaults hold.
func loopOpts() agent.LoopOpts { return agent.LoopOpts{} }

// permissionAskResult converts the browser's answer into the engine's contract.
func permissionAskResult(approved bool, feedback string) permission.AskResult {
	return permission.AskResult{Approved: approved, Feedback: feedback}
}

// editorPageRequest clones r with its path rewritten to "/". The editor is
// served at /settings for a stable, guessable URL, but configserver's handler
// matches its document on the exact path "/". Cloning keeps the original
// request — and therefore anything a middleware or the mux sees — untouched.
func editorPageRequest(r *http.Request) *http.Request {
	uri := *r.URL
	uri.Path = "/"
	uri.RawPath = ""
	page := r.Clone(r.Context())
	page.URL = &uri
	return page
}
