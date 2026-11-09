// serve.go — a1 serve (Bet 01, The Clock): the headless engine host.
//
// The webserver's multi-session machinery (the liveFor engine cache, the
// per-session turn mutex, the SSE hub) already IS a daemon; what the GUI
// lacks for ambient duty is an auth story beyond loopback and a permission
// mode that folds Ask to Deny. StartServe reuses all of it behind a bearer
// token and defaults to headless-strict, then adds the Clock's durable half:
// a job manager whose restart policy parks in-flight jobs as pending (and
// auto-resumes them at boot) and a scheduler firing wake-ups as jobs.
package webserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/schedule"
	"github.com/damien1141/a1/internal/session"
)

// ServePort is where `a1 serve` listens (one above the loopback GUI).
const ServePort = 9002

// ServeOptions configures [StartServe].
type ServeOptions struct {
	// Host binds somewhere other than 127.0.0.1. A non-loopback host
	// REQUIRES a token — refusing the bind otherwise keeps the daemon
	// from quietly exposing an agent to the network.
	Host string
	// Port defaults to ServePort.
	Port int
	// Token is the bearer token. Empty on a loopback bind = no auth
	// (same posture as the GUI); the CLI generates one otherwise.
	Token string
	// Strict starts the daemon in headless-strict: Ask folds to Deny, so
	// an unattended prompt cannot stall on an approval nobody will answer.
	Strict bool
}

// StartServe launches the headless engine host: the full webserver API
// (pages, SSE, chat, personas) behind bearer-token auth, in headless-strict
// by default, with the Clock wired — scheduled wake-ups fire as jobs, and
// jobs parked by a previous daemon restart resume at boot.
func StartServe(ctx context.Context, proj *project.Project, opts ServeOptions) (string, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if proj == nil {
		return "", nil, errors.New("serve: project is required")
	}
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := opts.Port
	if port == 0 {
		port = ServePort
	}
	if port <= 0 || port > 65535 {
		return "", nil, fmt.Errorf("serve: port %d out of range", port)
	}
	if !isLoopbackHost(host) && strings.TrimSpace(opts.Token) == "" {
		return "", nil, errors.New("serve: a non-loopback host requires a token")
	}

	srv, err := newServer(proj)
	if err != nil {
		return "", nil, err
	}

	// Headless-strict is the daemon default: an Ask nobody answers must
	// deny, not hang. --strict=false opts back into the interactive modes
	// for someone running serve as a remote GUI with a human attached.
	if opts.Strict {
		if err := srv.setPermissionMode(permission.ModeHeadlessStrict); err != nil {
			fmt.Fprintf(os.Stderr, "serve: strict mode: %v\n", err)
		}
	}

	if proj.Config().Agents.Enabled {
		srv.wireClock(ctx, proj)
	}

	return listenAndServe(ctx, srv, host, port, srv.tokenAuth(opts.Token)(srv.routes()))
}

// wireClock builds the durable half: the job manager (restart policy =
// RecoverRequeue, metas mirrored into the vault) and the scheduler whose
// fires spawn jobs. Engines built by this server get agent_* + schedule
// tools; pending jobs from a dead daemon resume immediately.
func (s *Server) wireClock(ctx context.Context, proj *project.Project) {
	parent := proj.Config().Model()
	models := proj.Config().Agents.Models
	modelFn := func(role job.Role) llm.ModelConfig {
		var name string
		switch job.NormalizeRole(string(role)) {
		case job.RoleReview:
			name = models.Review
		case job.RoleWorker:
			name = models.Worker
		default:
			name = models.Explore
		}
		if cfg, ok := proj.Config().FindModel(name); ok {
			return cfg
		}
		return parent
	}
	// The parent gate for child branches: the surface's live gate (mode
	// switches included), resolved at spawn time — no lock is held here.
	parentGate := func() permission.Gate { return s.gate() }

	jobs, err := agent.NewJobManager(proj.JobsDir(), s.vault.DB(), parent, modelFn, nil, parentGate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: job manager unavailable: %v\n", err)
		return
	}
	sched, err := schedule.New(s.vault.DB(), func(ctx context.Context, sc schedule.Schedule) error {
		role, rerr := job.ParseRole(sc.Role)
		if rerr != nil {
			role = job.RoleWorker
		}
		_, serr := jobs.Spawn(ctx, job.SpawnRequest{
			Prompt:      sc.Prompt,
			Description: sc.Description,
			ParentID:    session.ScheduleParentPrefix + sc.ID,
			Role:        role,
			WorkDir:     sc.WorkDir,
			Contract:    sc.Contract,
			Timeout:     sc.Timeout,
			Kind:        job.KindSchedule,
			ScheduleID:  sc.ID,
		})
		return serr
	}, schedule.WithLogger(func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "serve: "+format+"\n", args...)
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: scheduler unavailable: %v\n", err)
		return
	}
	if defs, derr := proj.Config().ScheduleDefs(); derr != nil {
		fmt.Fprintf(os.Stderr, "serve: schedules config: %v\n", derr)
	} else if serr := sched.Sync(ctx, defs); serr != nil {
		fmt.Fprintf(os.Stderr, "serve: schedules sync: %v\n", serr)
	}
	s.jobs = jobs
	s.scheduler = sched
	go sched.Run(ctx)

	// Overnight work continues: anything the dead daemon parked as pending
	// rebuilds now (engine from the session store, evidence receipts
	// replayed, prompt continued).
	if rerr := jobs.ResumePending(ctx); rerr != nil {
		fmt.Fprintf(os.Stderr, "serve: resume pending jobs: %v\n", rerr)
	}
}

// tokenAuth wraps the whole mux in a bearer-token check. An empty token
// (loopback, no token configured) passes everything through unchanged.
// EventSource cannot set headers, so the SSE endpoint also accepts
// ?token=<t>; every route accepts it for curl convenience. The comparison
// is constant-time.
func (s *Server) tokenAuth(token string) func(http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimSpace(r.Header.Get("Authorization"))
			got = strings.TrimPrefix(got, "Bearer ")
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				writeStatus(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isLoopbackHost reports whether host binds a loopback interface.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
