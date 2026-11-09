package agent

import (
        "os"
        "path/filepath"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/llm"
        llmclient "github.com/damien1141/a1/internal/llm/client"
        "github.com/damien1141/a1/internal/memfsck"
        "github.com/damien1141/a1/internal/project/model"
        "github.com/damien1141/a1/internal/session"
)

// The Diary (Bet 06): the designed-but-unwired loops, wired. Valence,
// episodes, memfsck, and the dedicated memory-ops model all live here so
// the inlet/outlet changes stay one-liners.

// updateSessionValence computes the session's mood from turn outcomes —
// evidence regressions pull it down, a certified contract pulls it up —
// and stores it for the next retrieval's mood-congruency cue. Before
// this, every cue carried Valence 0 and selectBundle's §3.5 override was
// unreachable dead code.
func (engine *Engine) updateSessionValence() {
        v := 0.0
        if l := engine.evidence; l != nil {
                if regs := len(l.Regressions()); regs > 0 {
                        v -= 0.2 * float64(regs)
                }
                if checks := engine.turnContract.snapshot(); len(checks) > 0 {
                        if len(l.Residual(checks)) == 0 {
                                v += 0.3 // turn certified complete
                        }
                }
        }
        if v > 1 {
                v = 1
        }
        if v < -1 {
                v = -1
        }
        engine.sessionValence.Store(v)
}

// loadSessionValence returns the stored mood (0 before the first turn).
func (engine *Engine) loadSessionValence() float64 {
        if v, ok := engine.sessionValence.Load().(float64); ok {
                return v
        }
        return 0
}

// episodeCaps bound the episodic recall block: parent context, so each
// episode keeps its load-bearing head only.
const (
        episodeRecallCount  = 2
        episodeRecallBudget = 2000 // bytes per episode
)

// recordEpisode lands one compaction summary as a vault episode row
// (fire-and-forget; a missed episode only means one fewer recall
// candidate).
func recordEpisode(engine *Engine, summary string) {
        v := session.DefaultVault()
        if v == nil || engine == nil {
                return
        }
        summary = strings.TrimSpace(summary)
        if summary == "" {
                return
        }
        project := ""
        if f := engine.SessionFile(); f != "" {
                project = filepath.Dir(f)
        }
        if project == "" {
                return
        }
        if err := v.PutEpisode(project, "compaction", summary); err != nil {
                debuglog.Logf("episode: %v", err)
        }
}

// recallEpisodes renders the "last time this happened" block the memory
// inlet splices in: the most recent episode summaries recorded for this
// project. Empty string when there is nothing to recall (fresh
// projects, or the vault is not live) — pre-Bet 06 sessions are
// byte-identical.
func recallEpisodes(engine *Engine, project string) string {
        v := session.DefaultVault()
        if v == nil || project == "" {
                return ""
        }
        episodes, err := v.RecentEpisodes(project, episodeRecallCount)
        if err != nil || len(episodes) == 0 {
                return ""
        }
        var b strings.Builder
        b.WriteString("Recent episodes from earlier sessions in this project (what happened last time):\n")
        for _, ep := range episodes {
                content := ep.Content
                if len(content) > episodeRecallBudget {
                        content = content[:episodeRecallBudget] + "…"
                }
                b.WriteString("\n<episode at=\"")
                b.WriteString(ep.At.Format(time.RFC3339))
                b.WriteString("\">\n")
                b.WriteString(content)
                b.WriteString("\n</episode>")
        }
        return b.String()
}

// runMemfsckAtSessionStart reconciles <cwd>/memory with its MEMORY.md
// index once per engine. A missing memory dir is a silent no-op — the
// pass is for projects that keep markdown notes, not a new requirement.
func (engine *Engine) runMemfsckAtSessionStart() {
        if engine == nil {
                return
        }
        engine.memfsckOnce.Do(func() {
                cwd := engine.SessionCwd()
                if cwd == "" {
                        return
                }
                root := filepath.Join(cwd, "memory")
                if _, err := os.Stat(root); err != nil {
                        return
                }
                _, rep, err := memfsck.Run(root)
                if err != nil {
                        debuglog.Logf("memfsck: %v", err)
                        return
                }
                if rep != nil && rep.Changed {
                        debuglog.Logf("memfsck: repaired MEMORY.md (added %d, dropped %d, deduped %d)",
                                len(rep.AddedIndex), len(rep.Dropped), rep.Deduped)
                }
        })
}

// memoryConsolidationClient returns the client memory-consolidation
// traffic uses (Bet 06): the persona's dedicated MemoryModel when
// configured — an 8B model doing memory ops frees the main LLM and cuts
// latency — else the engine's main client. The dedicated client carries no
// tools (consolidation is a pure text op).
//
// The persona is read through the STORE, not session.activePersona(): the
// default persona is a real persona here. Its MemoryModel is a legitimate
// setting — the persona editor offers the dropdown for every persona — and
// the activePersona() gate (which reports false for the default) silently
// ignored it, routing consolidation AND the web GUI's smart session names
// to the main model even when a memory-ops model was configured.
//
// The model resolves through the catalog (personaModelResolver) first: the
// memory model may live on a different provider than the main model (a
// local 8B against a hosted frontier model), and swapping only the name
// into the MAIN model's config would call the wrong endpoint with the
// wrong key. A catalog miss falls back to the legacy name-swap so a
// memory model outside the catalog still routes somewhere sane.
func (engine *Engine) memoryConsolidationClient() *llmclient.Client {
        if engine == nil {
                return engine.client
        }
        pstore := engine.personaStore()
        if pstore == nil {
                return engine.client
        }
        p, err := pstore.ActivePersona()
        if err != nil || p.MemoryModel == "" || p.MemoryModel == engine.modelCfg.Name {
                return engine.client
        }
        if engine.personaModelResolver != nil {
                if m, ok := engine.personaModelResolver(p.MemoryModel); ok {
                        return llmclient.NewClient(m, model.HooksFor(m.Name), nil, "").SetKeyPool(engine.poolFor(m))
                }
        }
        cfg := engine.modelCfg
        cfg.Name = p.MemoryModel
        return llmclient.NewClient(cfg, engine.hooks, nil, "").SetKeyPool(engine.poolFor(cfg))
}

// OpsClient exposes the memory-ops client to surfaces outside the engine
// (the web server's smart session naming) — the same cheap-second-model
// routing consolidation uses, so background write-backs never spend the
// main model's context.
func (engine *Engine) OpsClient() *llmclient.Client {
        return engine.memoryConsolidationClient()
}

// opsBaseURL returns the endpoint background ops traffic (memory
// consolidation, web session naming) actually hits: the persona's
// memory model when one is routed (resolver or legacy name-swap),
// else the main model. It mirrors memoryConsolidationClient's routing
// without building a client.
func (engine *Engine) opsBaseURL() string {
        if engine == nil {
                return ""
        }
        pstore := engine.personaStore()
        if pstore == nil {
                return engine.modelCfg.BaseURL
        }
        p, err := pstore.ActivePersona()
        if err != nil || p.MemoryModel == "" || p.MemoryModel == engine.modelCfg.Name {
                return engine.modelCfg.BaseURL
        }
        if engine.personaModelResolver != nil {
                if m, ok := engine.personaModelResolver(p.MemoryModel); ok {
                        return m.BaseURL
                }
        }
        // Legacy name-swap: a memory model outside the catalog rides the
        // main config's endpoint under a different name.
        return engine.modelCfg.BaseURL
}

// LocalOpsEndpoint reports whether background ops LLM traffic (memory
// consolidation, session naming) runs against a local server. On a
// local server every different-prefix request evicts the conversation's
// prompt cache — a 15K-token context re-prefills at ~600 tok/s for tens
// of seconds — so surfaces gate opportunistic ops calls on this and the
// engine batches consolidation (memory_outlet.go). A persona memory
// model routed off the main endpoint reports false and keeps per-turn
// behavior.
func (engine *Engine) LocalOpsEndpoint() bool {
        return llm.IsLocalBaseURL(engine.opsBaseURL())
}
