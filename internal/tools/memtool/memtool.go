// Package memtool exposes the memory system (remember, recall, revise,
// forget) as agent tools. These tools are only registered when the
// memory-enabled flag is set via the Ctrl+K palette.
package memtool

import (
        "context"
        "encoding/json"
        "fmt"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// Tools returns the four memory tools, backed by the given store. The
// store is opened once at session start and shared across all four
// tools — they all hit the same SQLite database.
func Tools(store *memstore.Store) []tooldef.Tool {
        if store == nil {
                return nil
        }
        return []tooldef.Tool{
                rememberTool(store),
                recallTool(store),
                reviseTool(store),
                forgetTool(store),
        }
}

// ---------- remember ----------

type rememberInput struct {
        Content  string  `json:"content"`
        Kind     string  `json:"kind"`
        Scope    string  `json:"scope"`
        Valence  *float64 `json:"valence,omitempty"`
        Salience *float64 `json:"salience,omitempty"`
        Thread   string  `json:"thread,omitempty"`
}

var rememberDesc = `Save a durable memory. Use for things worth remembering across sessions:
identity facts, preferences, task state, thread progress (beats).

kind: fact | preference | task | reference | identity | beat | affect | bit
scope: user (personal, any session) | global (project-independent) | project (this repo)
valence: -1..1 (how positive/negative; default 0)
salience: 0..1 (emotional weight; default 0.3)
thread: stem name if kind=beat (e.g. "job_hunt")`

func rememberTool(store *memstore.Store) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "remember",
                        Description: rememberDesc,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "content": llm.Object{
                                                "type":        "string",
                                                "description": "2-3 sentence gist of what to remember.",
                                        },
                                        "kind": llm.Object{
                                                "type":        "string",
                                                "description":  "fact | preference | task | reference | identity | beat | affect | bit",
                                        },
                                        "scope": llm.Object{
                                                "type":        "string",
                                                "description":  "user | global | project",
                                        },
                                        "valence": llm.Object{
                                                "type":        "number",
                                                "description":  "-1..1, emotional valence. Default 0.",
                                        },
                                        "salience": llm.Object{
                                                "type":        "number",
                                                "description":  "0..1, emotional weight. Default 0.3.",
                                        },
                                        "thread": llm.Object{
                                                "type":        "string",
                                                "description": "Thread stem if kind=beat (e.g. 'job_hunt').",
                                        },
                                },
                                Required: []string{"content", "kind", "scope"},
                        },
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in rememberInput
                        _ = json.Unmarshal(input, &in)
                        c := strings.TrimSpace(in.Content)
                        if len(c) > 40 {
                                c = c[:40] + "..."
                        }
                        return fmt.Sprintf("remember %s: %s", in.Kind, c)
                },
                Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in rememberInput
                        if err := json.Unmarshal(input, &in); err != nil {
                                return tooldef.Result{}, fmt.Errorf("failed to parse remember arguments: %w", err)
                        }
                        if strings.TrimSpace(in.Content) == "" {
                                return tooldef.Result{}, fmt.Errorf("content is required")
                        }
                        in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
                        in.Scope = strings.ToLower(strings.TrimSpace(in.Scope))
                        if in.Kind == "" {
                                return tooldef.Result{}, fmt.Errorf("kind is required: fact|preference|task|reference|identity|beat|affect|bit")
                        }
                        if in.Scope == "" {
                                in.Scope = "user"
                        }

                        sal := 0.3
                        if in.Salience != nil {
                                sal = *in.Salience
                        }
                        nt := memstore.NewTrace{
                                Content:  in.Content,
                                Kind:    in.Kind,
                                Scope:   in.Scope,
                                Salience: sal,
                                Ctx:     memstore.EncodingCtx{},
                        }
                        if in.Valence != nil {
                                nt.Valence = in.Valence
                        }
                        if in.Thread != "" {
                                t := in.Thread
                                nt.Thread = &t
                        }

                        id, merged, err := store.Insert(ctx, nt)
                        if err != nil {
                                return tooldef.Result{}, err
                        }
                        msg := fmt.Sprintf("Memory #%d saved (kind=%s, scope=%s)", id, in.Kind, in.Scope)
                        if merged {
                                msg = fmt.Sprintf("Memory #%d merged into existing (kind=%s, scope=%s)", id, in.Kind, in.Scope)
                        }
                        return tooldef.Result{Content: msg, Detail: in.Kind, Output: msg}, nil
                },
        }
}

// ---------- recall ----------

type recallInput struct {
        Query           string `json:"query"`
        K               int    `json:"k,omitempty"`
        IncludeArchived bool   `json:"include_archived,omitempty"`
}

var recallDesc = `Retrieve memories matching a query. Returns ranked results with
confidence scores. Use this when you need to recall what was discussed or
decided in past sessions.

Results are ranked by activation (recency + relevance + salience), not
raw similarity. Each result includes: id, content, confidence (P), date.

k: max results (default 6, max 20)`

func recallTool(store *memstore.Store) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "recall",
                        Description: recallDesc,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "query": llm.Object{
                                                "type":        "string",
                                                "description": "What to look for — natural language or keywords.",
                                        },
                                        "k": llm.Object{
                                                "type":        "integer",
                                                "description": "Max results. Default 6, max 20.",
                                        },
                                },
                                Required: []string{"query"},
                        },
                        Readable: true,
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in recallInput
                        _ = json.Unmarshal(input, &in)
                        q := strings.TrimSpace(in.Query)
                        if len(q) > 40 {
                                q = q[:40] + "..."
                        }
                        return fmt.Sprintf("recall %q", q)
                },
                Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in recallInput
                        if err := json.Unmarshal(input, &in); err != nil {
                                return tooldef.Result{}, fmt.Errorf("failed to parse recall arguments: %w", err)
                        }
                        if strings.TrimSpace(in.Query) == "" {
                                return tooldef.Result{}, fmt.Errorf("query is required")
                        }
                        k := in.K
                        if k <= 0 {
                                k = 6
                        }
                        if k > 20 {
                                k = 20
                        }

                        results, err := store.Retrieve(ctx, in.Query, memstore.ModeCompanion,
                                memstore.Cues{Hour: float64(time.Now().Hour())}, time.Now())
                        if err != nil {
                                return tooldef.Result{}, err
                        }
                        if len(results) > k {
                                results = results[:k]
                        }
                        if len(results) == 0 {
                                return tooldef.Result{
                                        Content: "No memories matched",
                                        Detail:  "0 memories",
                                        Output:  "No memories matched",
                                }, nil
                        }

                        var sb strings.Builder
                        for _, r := range results {
                                date := r.CreatedAt.Format("2006-01-02")
                                sb.WriteString(fmt.Sprintf("[%s · %s · P=%.2f] #%d: %s\n",
                                        date, r.Kind, r.P, r.ID, r.Content))
                        }
                        content := strings.TrimRight(sb.String(), "\n")
                        return tooldef.Result{
                                Content: content,
                                Detail:  fmt.Sprintf("%d memories", len(results)),
                                Output:  content,
                        }, nil
                },
        }
}

// ---------- revise ----------

type reviseInput struct {
        MemoryID   int64  `json:"memory_id"`
        NewContent string `json:"new_content"`
        Reason     string `json:"reason"`
}

var reviseDesc = `Correct a memory in place. The old content is logged — never lost.
Use this when you learn something that contradicts or updates a prior memory.`

func reviseTool(store *memstore.Store) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "revise",
                        Description: reviseDesc,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "memory_id": llm.Object{
                                                "type":        "integer",
                                                "description": "ID of the memory to revise (from recall results).",
                                        },
                                        "new_content": llm.Object{
                                                "type":        "string",
                                                "description": "The corrected content.",
                                        },
                                        "reason": llm.Object{
                                                "type":        "string",
                                                "description": "Why the correction was made.",
                                        },
                                },
                                Required: []string{"memory_id", "new_content", "reason"},
                        },
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in reviseInput
                        _ = json.Unmarshal(input, &in)
                        return fmt.Sprintf("revise #%d", in.MemoryID)
                },
                Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in reviseInput
                        if err := json.Unmarshal(input, &in); err != nil {
                                return tooldef.Result{}, fmt.Errorf("failed to parse revise arguments: %w", err)
                        }
                        if in.MemoryID <= 0 {
                                return tooldef.Result{}, fmt.Errorf("memory_id is required")
                        }
                        if strings.TrimSpace(in.NewContent) == "" {
                                return tooldef.Result{}, fmt.Errorf("new_content is required")
                        }
                        if err := store.Revise(ctx, in.MemoryID, in.NewContent, in.Reason); err != nil {
                                return tooldef.Result{}, err
                        }
                        msg := fmt.Sprintf("Memory #%d revised", in.MemoryID)
                        return tooldef.Result{Content: msg, Detail: fmt.Sprintf("revise #%d", in.MemoryID), Output: msg}, nil
                },
        }
}

// ---------- forget ----------

type forgetInput struct {
        MemoryID int64  `json:"memory_id,omitempty"`
        Query    string `json:"query,omitempty"`
}

var forgetDesc = `Permanently delete a memory. This is destructive — the memory is
removed from the database and cannot be recovered.

Provide either memory_id (from recall) or query (deletes all matches).`

func forgetTool(store *memstore.Store) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "forget",
                        Description: forgetDesc,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "memory_id": llm.Object{
                                                "type":        "integer",
                                                "description": "ID of the memory to forget (from recall results).",
                                        },
                                        "query": llm.Object{
                                                "type":        "string",
                                                "description": "Delete all memories matching this query.",
                                        },
                                },
                        },
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in forgetInput
                        _ = json.Unmarshal(input, &in)
                        if in.MemoryID > 0 {
                                return fmt.Sprintf("forget #%d", in.MemoryID)
                        }
                        return "forget"
                },
                Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in forgetInput
                        if err := json.Unmarshal(input, &in); err != nil {
                                return tooldef.Result{}, fmt.Errorf("failed to parse forget arguments: %w", err)
                        }
                        if in.MemoryID > 0 {
                                if err := store.Forget(in.MemoryID); err != nil {
                                        return tooldef.Result{}, err
                                }
                                msg := fmt.Sprintf("Memory #%d forgotten", in.MemoryID)
                                return tooldef.Result{Content: msg, Detail: fmt.Sprintf("forget #%d", in.MemoryID), Output: msg}, nil
                        }
                        if strings.TrimSpace(in.Query) != "" {
                                results, err := store.Retrieve(ctx, in.Query, memstore.ModeCompanion,
                                        memstore.Cues{Hour: float64(time.Now().Hour())}, time.Now())
                                if err != nil {
                                        return tooldef.Result{}, err
                                }
                                ids := make([]int64, len(results))
                                for i, r := range results {
                                        ids[i] = r.ID
                                }
                                if len(ids) == 0 {
                                        return tooldef.Result{Content: "No memories matched", Detail: "0 forgotten", Output: "No memories matched"}, nil
                                }
                                if err := store.Forget(ids...); err != nil {
                                        return tooldef.Result{}, err
                                }
                                msg := fmt.Sprintf("Forgot %d memories matching %q", len(ids), in.Query)
                                return tooldef.Result{Content: msg, Detail: fmt.Sprintf("forget %d", len(ids)), Output: msg}, nil
                        }
                        return tooldef.Result{}, fmt.Errorf("provide either memory_id or query")
                },
        }
}
