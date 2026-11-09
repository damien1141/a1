package agent

import (
        "database/sql"
        "errors"

        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
)

// NewJobManager creates a process-level job manager whose runner drives child Engines.
// modelFn may be nil; then model is used as a fixed snapshot for every role.
// When set, modelFn receives the job role so callers can pick per-role models.
// extensionsFn supplies extensions for child engines (may return nil); prefer a live
// getter so TUI reload updates sub-agents too.
//
// parentGate is the parent trajectory's gate. Child branches fork from the
// parent's label and share its effect log, so a sub-agent cannot
// start from an unconstrained top label — which would be an illicit
// laundering path for data the parent already ingested (APPA §6).
//
// db mirrors job metas into the vault's jobs table (Bet 01): nil = files
// only (degraded vault). Recovery is RecoverRequeue — a restart parks
// in-flight jobs as pending instead of failing them; the durable host
// (a1 serve) auto-resumes, everything else leaves them parked for it.
//
// The runner carries the jobs root so worker children can nest (Bet 04):
// explore/review children keep the no-nesting default, workers below the
// depth ceiling get their own manager over the same root.
// localFleetConcurrency caps fleet parallelism when the fleet's model
// runs on a local server. A local LLM server keeps each conversation's
// prompt cache warm only in untouched slots: concurrent children carry
// different prefixes, so on a single-slot server (the common local
// default — llama.cpp --parallel 1, Ollama's per-model context, LM
// Studio's JIT) their interleaved rounds evict each other and EVERY
// child round re-prefills from scratch. Serializing the fleet costs
// ~nothing there anyway — prefill saturates the box, so parallel
// requests were time-slicing the same compute — and it keeps the parent
// conversation's cache eviction to the minimum the work itself demands.
// Returns dflt (0 = job.New's own default) when the model is remote.
func localFleetConcurrency(model llm.ModelConfig, modelFn func(job.Role) llm.ModelConfig, dflt int) int {
        base := model.BaseURL
        if base == "" && modelFn != nil {
                base = modelFn(job.RoleWorker).BaseURL
        }
        if llm.IsLocalBaseURL(base) {
                return 1
        }
        return dflt
}

func NewJobManager(
        root string,
        db *sql.DB,
        model llm.ModelConfig,
        modelFn func(role job.Role) llm.ModelConfig,
        extensionsFn func() *extension.Runner,
        parentGate func() permission.Gate,
) (*job.Manager, error) {
        if root == "" {
                return nil, errors.New("agent: jobs root is required")
        }
        return job.New(job.Options{
                Root: root,
                DB:   db,
                Runner: EngineRunner{
                        Model:        model,
                        ModelFn:      modelFn,
                        ExtensionsFn: extensionsFn,
                        ParentGateFn: parentGate,
                        JobsRoot:     root,
                        DB:           db,
                },
                MaxConcurrent: localFleetConcurrency(model, modelFn, 0),
                Recovery: job.RecoverRequeue,
        })
}
