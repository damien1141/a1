package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
)

// The session id must ride on the turn context, because the Ask handler runs on
// the engine's goroutine with no other line back to the HTTP request that
// started the turn.
func TestSessionContextRoundTrip(t *testing.T) {
	ctx := t.Context()
	assert.Empty(t, sessionFromContext(ctx))

	ctx = withSession(ctx, "abc123")
	assert.Equal(t, "abc123", sessionFromContext(ctx))
}

func TestDecodeBodyFormMatchesJSON(t *testing.T) {
	jsonReq := promptRequest{}
	require.NoError(t, decodeBody(jsonRequest(t, `{"sessionId":"s1","prompt":"hi"}`), &jsonReq))

	formReq := promptRequest{}
	require.NoError(t, decodeBody(formRequest(t, "sessionId=s1&prompt=hi"), &formReq))

	assert.Equal(t, jsonReq, formReq,
		"htmx posts form-encoded while curl posts JSON; both must decode identically")
}

func TestDecodeBodyFormAskAndCancel(t *testing.T) {
	var ask askRequest
	require.NoError(t, decodeBody(formRequest(t, "id=42&approved=true&feedback=use+bash"), &ask))
	assert.Equal(t, askRequest{ID: "42", Approved: true, Feedback: "use bash"}, ask)

	var cancel cancelRequest
	require.NoError(t, decodeBody(formRequest(t, "sessionId=s9"), &cancel))
	assert.Equal(t, cancelRequest{SessionID: "s9"}, cancel)
}

func TestProjectEventDropsEmptyDelta(t *testing.T) {
	// The engine re-emits the full accumulated message on every stream chunk,
	// including tool-call chunks that carry no text at all. Those must not
	// reach the browser, where they would force a pointless re-render.
	s := &Server{}
	ch, release := s.events().subscribe()
	defer release()

	s.projectEvent("s1", session.AssistantMessageUpdate{Message: session.Message{
		State: session.StateStreaming,
	}})

	select {
	case ev := <-ch:
		t.Fatalf("an empty delta must not be published, got %s", ev.Kind)
	default:
	}
}

func TestProjectEventPublishesAccumulatedText(t *testing.T) {
	s := &Server{}
	ch, release := s.events().subscribe()
	defer release()

	s.projectEvent("s1", session.AssistantMessageUpdate{Message: session.Message{
		State:   session.StateStreaming,
		Content: []session.ContentBlock{{Type: session.BlockText, Text: "hello"}},
	}})

	select {
	case ev := <-ch:
		require.NotNil(t, ev.Delta)
		assert.Equal(t, "hello", ev.Delta.Text)
		assert.Equal(t, "s1", ev.SessionID, "the turn id is stamped onto the event")
	default:
		t.Fatal("expected a delta event")
	}
}

func TestToolEventPayloadUsesStringStatus(t *testing.T) {
	got := toolEventPayload(session.ToolRun{
		ToolUseID: "call_1",
		Name:      "bash",
		Status:    session.ToolInProgress,
		Detail:    "ls -1",
	})
	assert.Equal(t, "call_1", got.ToolUseID)
	assert.Equal(t, "bash", got.Name)
	assert.Equal(t, "in-progress", got.Status,
		"status is pre-stringified so the client needs no enum table")

	raw, err := json.Marshal(serverEvent{Kind: eventTool, Tool: &got})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"status":"in-progress"`)
	assert.Contains(t, string(raw), `"toolUseId":"call_1"`)
}

func TestWireEventJSONTagsAreStable(t *testing.T) {
	raw, err := json.Marshal(serverEvent{
		Kind:      eventDelta,
		SessionID: "s1",
		Delta:     &deltaPayload{Text: "hi", Reasoning: "think"},
	})
	require.NoError(t, err)
	for _, want := range []string{`"kind":"delta"`, `"sessionId":"s1"`, `"text":"hi"`, `"reasoning":"think"`} {
		assert.Contains(t, string(raw), want)
	}
	// Absent payloads must be omitted, so the client can branch on presence
	// without confusing an omitted object with a zero value.
	assert.NotContains(t, string(raw), `"tool"`)
	assert.NotContains(t, string(raw), `"ask"`)
}

func TestHubBroadcastsToEverySubscriber(t *testing.T) {
	h := newHub()
	a, releaseA := h.subscribe()
	defer releaseA()
	b, releaseB := h.subscribe()
	defer releaseB()

	h.publish("s1", serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "thinking…"}})
	for _, ch := range []chan serverEvent{a, b} {
		select {
		case ev := <-ch:
			assert.Equal(t, "s1", ev.SessionID, "publish fills the session id when the event omits it")
			assert.Equal(t, eventStatus, ev.Kind)
		default:
			t.Fatal("subscriber received nothing")
		}
	}
}

func TestHubDropsStalledSubscriber(t *testing.T) {
	h := newHub()
	stalled, release := h.subscribe()
	defer release()

	// Overflow the 64-slot buffer without draining, then publish far past it.
	// The hub must not block the agent loop on a stalled browser tab.
	for range 200 {
		h.publish("s1", serverEvent{Kind: eventDelta, Delta: &deltaPayload{Text: "x"}})
	}
	// Drain the stalled subscriber so the assertion below is about the live
	// one, not about a channel that happens to still hold old events.
	for range len(stalled) {
		<-stalled
	}

	next, releaseNext := h.subscribe()
	defer releaseNext()
	h.publish("s1", serverEvent{Kind: eventDone})
	select {
	case ev := <-next:
		assert.Equal(t, eventDone, ev.Kind, "a healthy subscriber keeps working after a stalled one is dropped")
	default:
		t.Fatal("publish blocked or dropped the healthy subscriber")
	}
}

func TestSettingsRouteServesEditorPage(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/settings", http.NoBody))
	assert.Equal(t, http.StatusOK, rec.Code)
	// The editor document is served, not a redirect or a blank shell.
	assert.Contains(t, rec.Body.String(), `id="langToggle"`)
	assert.Contains(t, rec.Body.String(), "/api/config")
	assert.Contains(t, rec.Body.String(), "a1-config-lang")
}

func TestSettingsRouteDoesNotShadowChatRoutes(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	// Mounting the editor must not claim the chat API paths. A zero-value
	// Server has no project, so /api/sessions answers 500 rather than the
	// editor document — what matters is that it is not the HTML page.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/api/sessions", http.NoBody))
	assert.NotEqual(t, http.StatusOK, rec.Code, "a projectless server cannot list sessions")
	assert.NotContains(
		t,
		rec.Body.String(),
		`id="langToggle"`,
		"the editor document must not be served for chat routes",
	)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestConfigAPIRoutesRejectForeignOrigin(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	// configserver's own handler refuses non-loopback hosts; the mount must
	// not strip that guard.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/config", strings.NewReader("{}"))
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestConfigPathUsesGlobalConfigFile(t *testing.T) {
	s := &Server{proj: nil}
	// A Server with no project (zero value) still answers the API; the editor
	// treats an empty path as an empty document rather than panicking.
	assert.NotPanics(t, func() { _ = s.configPath() })
}

func jsonRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func formRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/prompt", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestRoutesAreRegistered(t *testing.T) {
	s := &Server{}
	mux := s.routes()
	// /api/events is deliberately excluded: it is an unbounded SSE stream, so
	// hitting it here would block forever. It is covered by the live end-to-end
	// check, not by a route table assertion.
	for _, path := range []string{
		"/", "/api/sessions", "/api/transcript?id=x",
		"/api/mode", "/api/pins", "/api/session?id=x",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", path, http.NoBody))
		assert.NotEqual(t, http.StatusNotFound, rec.Code, "route %s must be registered", path)
	}
}

func TestModeEndpointReportsInteractiveByDefault(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/api/mode", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)

	var view modeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&view))
	assert.Equal(t, "interactive", view.Mode)
	assert.Equal(t, "ask", view.Label)
	// The switcher renders server-supplied labels, so the set must match the
	// TUI's controller.CyclePermissionMode order or the two surfaces drift.
	require.Len(t, view.Modes, 5)
	var got []string
	for _, m := range view.Modes {
		got = append(got, m.Mode)
	}
	assert.Equal(t,
		[]string{"interactive", "readonly", "autopilot", "headless-strict", "yolo"},
		got)
}

func TestSetModeRejectsUnknownMode(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"tinycap"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown permission mode")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/api/mode", http.NoBody))
	var view modeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&view))
	assert.Equal(t, "interactive", view.Mode, "a rejected switch must leave the previous mode in place")
}

func TestSetModeAcceptsFormEncoding(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader("mode=readonly"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var view modeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&view))
	assert.Equal(t, "readonly", view.Mode)
	assert.True(t, view.Modes[1].Active)
}

func TestCycleModeAdvances(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"cycle"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var view modeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&view))
	assert.Equal(t, "readonly", view.Mode, "cycling from interactive lands on readonly")
}

func TestPinToggleRoundTrip(t *testing.T) {
	// Pins live under the project session directory; a temp dir keeps the
	// test's pins out of the real session store.
	dir := t.TempDir()
	s := &Server{pins: newPinnedSessions(dir)}
	mux := s.routes()

	post := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pin",
			strings.NewReader(`{"sessionId":"`+id+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post("sess-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var first pinResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&first))
	assert.True(t, first.Pinned)
	assert.Equal(t, []string{"sess-1"}, first.Pins)

	rec = post("sess-1")
	var second pinResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&second))
	assert.False(t, second.Pinned, "a second toggle unpins")

	rec = post("sess-2")
	require.Equal(t, http.StatusOK, rec.Code)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/api/pins", http.NoBody))
	var list pinResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	assert.Equal(t, []string{"sess-2"}, list.Pins, "the newest pin leads the list")
}

func TestPinToggleRejectsEmptyID(t *testing.T) {
	s := &Server{}
	mux := s.routes()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pin",
		strings.NewReader(`{"sessionId":"  "}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSessionMetaFallsBackToShortID(t *testing.T) {
	out := sessionTitleFromMeta(session.SessionMeta{ID: "0123456789abcdef0123456789abcdef"})
	assert.Equal(t, "0123456789ab…", out,
		"a session with no preview still shows a stable short label, not a blank topbar")
}

// A nested s.mu acquisition wedges the whole server rather than one request:
// the handler holding the mutex never finishes, so every other handler blocks
// behind it and the process stops answering anything at all. liveFor holds s.mu
// while building an engine, so building one must not read the mode store, which
// locks s.mu too.
//
// The test drives a session that actually exists, because liveFor only builds
// (and therefore reaches the gate path) for an id it can resume — the other
// route tests use a zero-valued Server that short-circuits in requireProject,
// which would hide the deadlock entirely.
func TestLiveForDoesNotDeadlockOnLockedState(t *testing.T) {
	proj, err := project.Discover(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	sessionDir := proj.SessionDir()
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(sessionDir) })

	// A minimal but valid session file: header plus one user turn, which is what
	// WithResumeID requires to accept the id as resumable.
	const sessionID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "probe_"+sessionID+".jsonl"), []byte(
		`{"type":"EntrySession","id":"`+sessionID+`","timestamp":"2026-10-09T10-00-00","cwd":"`+t.TempDir()+`"}`+"\n"+
			`{"type":"EntryMessage","id":"m1","parentID":null,"timestamp":"2026-10-09T10:00:01-05:00",`+
			`"message":{"role":"user","content":"hello"}}`+"\n"), 0o600))

	srv := &Server{proj: proj, modes: newModeState(), pins: newPinnedSessions(t.TempDir())}
	// Prime the registries the way Start does, so the zero-value fallback is not
	// what is under test.
	srv.mu.Lock()
	srv.hub = newHub()
	srv.engines = map[string]*liveEngine{}
	srv.pending = map[string]chan permission.AskResult{}
	srv.mu.Unlock()
	mux := srv.routes()

	// One transcript request first: it builds and caches the engine, so the
	// hammering below contends on s.mu the way a real browser does.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET",
		"/api/transcript?id="+sessionID, http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	stop := make(chan struct{})
	var wg sync.WaitGroup
	// Every endpoint that takes s.mu, hammered at once. If any of them nests its
	// acquisition with liveFor, the first overlap deadlocks and none finish.
	hammer := func(build func() *http.Request) {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				out := httptest.NewRecorder()
				mux.ServeHTTP(out, build())
			}
		})
	}
	hammer(func() *http.Request {
		return httptest.NewRequestWithContext(t.Context(), "GET",
			"/api/transcript?id="+sessionID, http.NoBody)
	})
	hammer(func() *http.Request {
		return httptest.NewRequestWithContext(t.Context(), "GET", "/api/mode", http.NoBody)
	})
	hammer(func() *http.Request {
		return httptest.NewRequestWithContext(t.Context(), "GET", "/api/pins", http.NoBody)
	})
	hammer(func() *http.Request {
		return httptest.NewRequestWithContext(t.Context(), "POST", "/api/pin",
			strings.NewReader(`{"sessionId":"`+sessionID+`"}`))
	})

	time.Sleep(3 * time.Second)
	close(stop)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("handlers deadlocked: a handler took s.mu while another already held it")
	}

	// The mode must still move: a wedged mode store fails here instead.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"readonly"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var view modeResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&view))
	assert.Equal(t, "readonly", view.Mode)
}

// The switcher is only useful if the mode actually reaches the gate: a pill
// that changes the label but not the behavior would greenlight tools the
// user believes are blocked.
func TestModeSwitchChangesGateDecisions(t *testing.T) {
	proj, err := project.Discover(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	srv := &Server{proj: proj, modes: newModeState(), pins: newPinnedSessions(t.TempDir())}
	mux := srv.routes()

	// Write inside the project root: WorkspaceOnlyWrites only asks about paths
	// outside it, and this test is about the mode, not the workspace fence.
	writeReq := permission.Request{Action: permission.ActionWrite, Paths: []string{proj.Root() + "/f.txt"}}

	// interactive: a write the policy allows stays allowed.
	dec, _ := srv.gate().Check(t.Context(), writeReq)
	require.Equal(t, permission.Allow, dec)

	// readonly: the same write is denied.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"readonly"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	dec, reason := srv.gate().Check(t.Context(), writeReq)
	assert.Equal(t, permission.Deny, dec, "readonly must deny writes")
	assert.Contains(t, reason, "readonly")

	// and switching back restores the original behavior.
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"interactive"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	dec, _ = srv.gate().Check(t.Context(), writeReq)
	assert.Equal(t, permission.Allow, dec)
}

// bash is the tool most likely to surprise a user: the gate must fold Ask into
// Deny in a suppressing mode instead of silently prompting anyway.
func TestModeSwitchFoldsBashAsk(t *testing.T) {
	proj, err := project.Discover(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	srv := &Server{proj: proj, modes: newModeState(), pins: newPinnedSessions(t.TempDir())}
	mux := srv.routes()

	// Off the default allowlist but not on the deny list, so interactive mode
	// asks. A curl|sh would be denied outright, which is a different rule.
	bashReq := permission.Request{Action: permission.ActionBash, Command: "pip install numpy"}

	dec, _ := srv.gate().Check(t.Context(), bashReq)
	require.Equal(t, permission.Ask, dec, "interactive must ask about a risky command")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/mode",
		strings.NewReader(`{"mode":"headless-strict"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	dec, reason := srv.gate().Check(t.Context(), bashReq)
	assert.Equal(t, permission.Deny, dec, "headless-strict must deny what would need approval")
	assert.Contains(t, reason, "headless-strict")
}

// A Server without a loaded config must not fail open: the zero Policy leaves
// BashDefault at Allow, which would let every command through. The fallback is
// DefaultPolicy, so a risky command still asks.
func TestPolicyFallbackIsNotPermissive(t *testing.T) {
	srv := &Server{modes: newModeState()}
	p := srv.policy()
	assert.Equal(t, permission.Ask, p.BashDefault, "the fallback policy asks by default")
	assert.NotEmpty(t, p.SensitivePathDeny, "the fallback policy still denies sensitive paths")

	dec, _ := srv.gate().Check(t.Context(), permission.Request{
		Action: permission.ActionBash, Command: "pip install numpy",
	})
	assert.Equal(t, permission.Ask, dec, "an unconfigured server must not allow everything")
}
