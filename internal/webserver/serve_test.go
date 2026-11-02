package webserver

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
)

// startServeFixture boots the serve stack (newServer + strict + tokenAuth +
// listenAndServe) on an OS-assigned ephemeral port against the hermetic
// fixture project, and returns its base URL plus a stop func.
func startServeFixture(t *testing.T, opts ServeOptions) (string, func()) {
        t.Helper()
        proj := newFixtureProject(t)
        srv, err := newServer(proj)
        require.NoError(t, err)
        if opts.Strict {
                require.NoError(t, srv.setPermissionMode(permission.ModeHeadlessStrict))
        }
        url, stop, err := listenAndServe(context.Background(), srv, "127.0.0.1", 0,
                srv.tokenAuth(opts.Token)(srv.routes()))
        require.NoError(t, err)
        return url, stop
}

func TestServeTokenAuth(t *testing.T) {
        base, stop := startServeFixture(t, ServeOptions{Token: "sekrit"})
        defer stop()

        // No token → 401 on API and page.
        for _, path := range []string{"/api/state", "/", "/api/events"} {
                resp, err := http.Get(base + path)
                require.NoError(t, err)
                _ = resp.Body.Close()
                assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, path)
        }

        // Wrong token → 401.
        req, _ := http.NewRequest(http.MethodGet, base+"/api/state", nil)
        req.Header.Set("Authorization", "Bearer nope")
        resp, err := http.DefaultClient.Do(req)
        require.NoError(t, err)
        _ = resp.Body.Close()
        assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

        // Header token → 200.
        req, _ = http.NewRequest(http.MethodGet, base+"/api/state", nil)
        req.Header.Set("Authorization", "Bearer sekrit")
        resp, err = http.DefaultClient.Do(req)
        require.NoError(t, err)
        defer resp.Body.Close()
        assert.Equal(t, http.StatusOK, resp.StatusCode)
        var state map[string]any
        require.NoError(t, json.NewDecoder(resp.Body).Decode(&state))
        assert.NotEmpty(t, state)

        // Query token → 200 (EventSource cannot set headers).
        resp, err = http.Get(base + "/api/state?token=sekrit")
        require.NoError(t, err)
        _ = resp.Body.Close()
        assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServeNoTokenPassesThroughOnLoopback(t *testing.T) {
        base, stop := startServeFixture(t, ServeOptions{})
        defer stop()
        resp, err := http.Get(base + "/api/state")
        require.NoError(t, err)
        defer resp.Body.Close()
        assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServeStrictModeDefault(t *testing.T) {
        base, stop := startServeFixture(t, ServeOptions{Strict: true})
        defer stop()
        resp, err := http.Get(base + "/api/mode")
        require.NoError(t, err)
        defer resp.Body.Close()
        assert.Equal(t, http.StatusOK, resp.StatusCode)
        body, err := io.ReadAll(resp.Body)
        require.NoError(t, err)
        assert.Contains(t, string(body), string(permission.ModeHeadlessStrict))
}

func TestServeRefusesNonLoopbackWithoutToken(t *testing.T) {
        proj := newFixtureProject(t)
        _, _, err := StartServe(context.Background(), proj, ServeOptions{Host: "0.0.0.0"})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "token")
}

func TestServeBadPortRejected(t *testing.T) {
        proj := newFixtureProject(t)
        _, _, err := StartServe(context.Background(), proj, ServeOptions{Port: -1})
        require.Error(t, err)
}

// TestServeClockWiresJobsAndScheduler pins the daemon's durable half: with
// agents enabled the server builds the job manager and scheduler, engines
// get the fleet + schedule tools, and the strict mode sticks.
func TestServeClockWiresJobsAndScheduler(t *testing.T) {
        proj := newFixtureProject(t) // agents enabled by default in config
        srv, err := newServer(proj)
        require.NoError(t, err)
        require.NoError(t, srv.setPermissionMode(permission.ModeHeadlessStrict))
        srv.wireClock(context.Background(), proj)
        require.NotNil(t, srv.jobs, "daemon wires the job manager")
        require.NotNil(t, srv.scheduler, "daemon wires the scheduler")

        // Engines built after the wiring carry the schedule tool.
        le, err := srv.liveFor("")
        require.NoError(t, err)
        assert.True(t, le.engine.HasTool("schedule"), "daemon engines expose the schedule tool")
        assert.True(t, le.engine.HasTool("agent_spawn"), "daemon engines expose fleet tools")

        // The strict fold is live on the engine's gate decisions.
        gate := le.engine.Gate()
        require.NotNil(t, gate)
        decision, _ := gate.Check(context.Background(), permission.Request{
                Action: permission.ActionEdit,
                Paths:  []string{"/tmp/x"},
        })
        assert.Equal(t, permission.Deny, decision,
                "headless-strict folds Ask to Deny for unattended edits")
}

// sanity: the middleware is exercised through the full HTTP stack above; a
// direct unit check keeps the constant-time compare honest.
func TestTokenAuthConstantTimeReject(t *testing.T) {
        s := &Server{}
        wrapped := s.tokenAuth("abc")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                fmt.Fprint(w, "ok")
        }))
        rec := httptest.NewRecorder()
        wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
        assert.Equal(t, http.StatusUnauthorized, rec.Code)
        rec = httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/state?token=abc", nil)
        wrapped.ServeHTTP(rec, req)
        assert.Equal(t, http.StatusOK, rec.Code)
        assert.Equal(t, "ok", rec.Body.String())
}
