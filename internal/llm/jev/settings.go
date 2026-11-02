package jev

import (
        "context"
        "errors"
        "strings"
        "sync/atomic"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
)

// Provider names for Settings.Provider. An empty provider means
// "unconfigured": the resolver falls back to the environment (a
// TYPESAFE_API_KEY enables the api provider, exactly the pre-config
// behavior), and when that too is absent JEV stays off.
const (
        // ProviderAPI is the hosted TypeSafe System One decision model.
        ProviderAPI = "api"
        // ProviderOllama is a local Ollama chat model interrogated with the
        // same questions under a strict JSON contract (see ollama.go).
        ProviderOllama = "ollama"
)

// Settings is the `jev:` section of config.yaml. The host resolves it from
// the config file and pushes it here with Configure; empty fields fall
// back to the TYPESAFE_* environment variables on the api provider and to
// OLLAMA_HOST on the ollama provider, so an env-only setup keeps working
// with no jev: section at all.
//
//      jev:
//        provider: ollama            # api | ollama; absent = env fallback / off
//        model: qwen3:4b             # api: model alias; ollama: local model tag
//        api_key: ...                # api provider (falls back to TYPESAFE_API_KEY)
//        base_url: ...               # api provider, optional
//        ollama_base_url: ...        # ollama provider, default http://127.0.0.1:11434
//        timeout_sec: 20             # optional per-call ceiling override
type Settings struct {
        // Provider selects the backend: ProviderAPI or ProviderOllama. Empty
        // means unconfigured (env fallback, then off).
        Provider string `yaml:"provider"         json:"provider,omitempty"`
        // APIKey authenticates the api provider; falls back to
        // TYPESAFE_API_KEY.
        APIKey string `yaml:"api_key,omitempty"  json:"apiKey,omitempty"`
        // BaseURL overrides the api provider endpoint; falls back to
        // TYPESAFE_BASE_URL and then DefaultBaseURL.
        BaseURL string `yaml:"base_url,omitempty" json:"baseUrl,omitempty"`
        // Model names the decision model: the alias on the api provider, the
        // local model tag on the ollama provider. Falls back to
        // TYPESAFE_DEFAULT_MODEL / DefaultModel on api; required on ollama
        // (pulling a local model is a user decision, never a default).
        Model string `yaml:"model,omitempty"    json:"model,omitempty"`
        // OllamaBaseURL targets the local Ollama server; falls back to the
        // OLLAMA_HOST environment variable and then http://127.0.0.1:11434.
        OllamaBaseURL string `yaml:"ollama_base_url,omitempty" json:"ollamaBaseUrl,omitempty"`
        // TimeoutSec overrides the per-call ceiling at both wired call sites
        // (fetch classify, evidence verdict) when > 0. Zero keeps the
        // provider-tuned defaults (seconds on the hosted API, tens of seconds
        // on a local model).
        TimeoutSec int `yaml:"timeout_sec,omitempty" json:"timeoutSec,omitempty"`
}

// ErrNotConfigured is returned by the asker NewConfiguredAsker hands out
// when no provider resolves — the wired call sites treat it like any other
// miss and fall back to their deterministic paths.
var ErrNotConfigured = errors.New("jev: no decision model configured (set jev.provider in config.yaml or TYPESAFE_API_KEY)")

// configured holds the Settings pushed by the host process. project's
// config loader pushes on every load, so a web-settings save reloads it
// and the next decision sees the new values.
var configured atomic.Value // Settings

// Configure replaces the configured settings. An empty Settings means "no
// jev: section in the file" — the env fallback still applies, preserving
// env-only setups byte-for-byte.
func Configure(s Settings) { configured.Store(s) }

// CurrentSettings returns the configured settings (zero when never
// pushed). It is a snapshot: resolution against the environment happens
// per call, in resolveAsker.
func CurrentSettings() Settings {
        if s, ok := configured.Load().(Settings); ok {
                return s
        }
        return Settings{}
}

// Asker is the backend-agnostic decision-model seam: one state value, a
// set of named questions, one answer per question. Both wired call sites
// (fetch classify, evidence verdict) depend on it, which is what lets the
// provider swap without touching them.
type Asker interface {
        SystemOneWithOptions(ctx context.Context, state any, questions Questions, opts Options) (Response, error)
}

// configuredAsker resolves the provider on every call. Construction-time
// gating (Configured) decides whether a call site wires JEV at all, but a
// long-lived engine should see config edits without a restart, so the
// concrete client is rebuilt from current settings + environment per
// decision. The rebuild is two string reads and a struct literal — the
// network round-trip dominates either way.
type configuredAsker struct{}

// NewConfiguredAsker returns the settings-resolving Asker. It never
// returns nil; call sites gate on Configured() first, and a mid-session
// deconfiguration surfaces as ErrNotConfigured (a plain miss).
func NewConfiguredAsker() Asker { return configuredAsker{} }

func (configuredAsker) SystemOneWithOptions(
        ctx context.Context,
        state any,
        questions Questions,
        opts Options,
) (Response, error) {
        asker := resolveAsker()
        if asker == nil {
                return Response{}, ErrNotConfigured
        }
        return asker.SystemOneWithOptions(ctx, state, questions, opts)
}

// resolveAsker turns current settings plus the environment into a
// concrete asker. Nil means off. Resolution order:
//
//      api     — Settings.APIKey, else TYPESAFE_API_KEY; no key anywhere → off
//      ollama  — Settings.Model required; base URL: Settings.OllamaBaseURL,
//                else OLLAMA_HOST, else http://127.0.0.1:11434
//      ""      — TYPESAFE_API_KEY present → api (the pre-config behavior);
//                otherwise off
//      other   — off, with a debuglog note naming the bad provider
func resolveAsker() Asker {
        s := CurrentSettings()
        switch strings.ToLower(strings.TrimSpace(s.Provider)) {
        case ProviderAPI:
                client, err := NewClient(Config{APIKey: s.APIKey, BaseURL: s.BaseURL, Model: s.Model})
                if err != nil {
                        debuglog.Logf("jev: api provider not usable: %v", err)
                        return nil
                }
                return client
        case ProviderOllama:
                model := strings.TrimSpace(s.Model)
                if model == "" {
                        debuglog.Logf("jev: ollama provider requires jev.model (the local model tag)")
                        return nil
                }
                return NewOllamaClient(OllamaConfig{BaseURL: s.OllamaBaseURL, Model: model})
        case "":
                // Unconfigured: the env-only fallback that predates the jev:
                // section. A key in the environment keeps the api provider live.
                client, err := NewClient(Config{})
                if err != nil {
                        return nil
                }
                return client
        default:
                debuglog.Logf("jev: unknown provider %q (want %q or %q)", s.Provider, ProviderAPI, ProviderOllama)
                return nil
        }
}

// Configured reports whether a decision model resolves right now. The
// wired call sites check it before wiring (and the fetch router before
// every classification) so an unconfigured JEV costs nothing.
func Configured() bool { return resolveAsker() != nil }

// ollamaTimeoutHint is the per-call ceiling the ollama provider suggests:
// a local model generating a tiny JSON verdict can take seconds on modest
// hardware, where the hosted API answers in well under one. The hosted
// provider suggests nothing (zero) and the call sites keep their tuned
// 3s/4s defaults.
const ollamaTimeoutHint = 20 * time.Second

// TimeoutHint returns the current provider's suggested per-call ceiling;
// zero means "no opinion — keep the caller's default".
func TimeoutHint() time.Duration {
        if providerIsOllama() {
                return ollamaTimeoutHint
        }
        return 0
}

// EffectiveTimeout resolves the per-call ceiling for a call site: start
// from the site's own default, raise it to the provider's hint when the
// backend is slower (a local model), then apply an explicit timeout_sec
// override either way — so a user on fast hardware can lower the Ollama
// ceiling too, not just raise it.
func EffectiveTimeout(site time.Duration) time.Duration {
        if s := CurrentSettings(); s.TimeoutSec > 0 {
                return time.Duration(s.TimeoutSec) * time.Second
        }
        if hint := TimeoutHint(); hint > site {
                return hint
        }
        return site
}

// providerIsOllama reports whether the resolved provider (settings, then
// env fallback) is ollama.
func providerIsOllama() bool {
        s := CurrentSettings()
        switch strings.ToLower(strings.TrimSpace(s.Provider)) {
        case ProviderOllama:
                return true
        case ProviderAPI, "":
                return false
        default:
                return false
        }
}
