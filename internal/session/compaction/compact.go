package compaction

import (
        "context"
        "errors"
        "slices"
        "strings"
        "sync"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

// CompactionPreparation holds everything Compact needs: the entries to
// summarize and the file operations captured from the summarized history.
// Recent messages stay in the session tree via FirstKeptEntryId.
type CompactionPreparation struct {
        FirstKeptEntryId    string
        MessagesToSummarize []llm.Message
        TurnPrefixMessages  []llm.Message
        IsMidTurnCut        bool
        TokensBefore        int
        PreviousSummary     string
        // ReserveTokens is the headroom compaction keeps from the context window;
        // summaries are capped at a fraction of it.
        ReserveTokens int
        // ContextWindow is the window the settings were derived from; 0 =
        // unknown. Compact uses it to cap the serialized conversation handed
        // to the summarizer, so the summary call itself cannot overflow the
        // same window that triggered the compaction — the failure mode where
        // a marginal fit makes auto-compaction "sometimes not work".
        ContextWindow int
        // EvidenceState is the harness-rendered evidence record content
        // (protected checks, current failures, the turn contract's residual
        // set) appended verbatim to the summary after the file-operations
        // block. Empty — the common case for plain chat sessions — appends
        // nothing. Populated by the engine at runCompact time; the compaction
        // package only wraps and strips it.
        EvidenceState string
        // TODO: wire into Compact / AppendCompaction so hook preserveData
        // survives across compaction rounds (currently collected but unused).
        PreviousPreserveData map[string]any
        FileOps              FileOperation
}

// PrepareCompact analyzes pathEntries and settings to decide what to
// summarize and what to keep. It returns an empty preparation when the
// session was already compacted.
//
// ctx bounds the semantic boundary classification, when settings carries
// a boundary classifier; without one, PrepareCompact is pure and ctx is
// unused.
func PrepareCompact(
        ctx context.Context,
        pathEntries []session.MessageEntry,
        settings Settings,
) (*CompactionPreparation, error) {
        // A session with no path entries has nothing to compact: the cut logic
        // below indexes pathEntries[cutPoint.firstKeptEntryIndex], which panics
        // on an empty slice - /compact on a brand-new session crashed the engine
        // before this guard existed.
        if len(pathEntries) == 0 {
                return &CompactionPreparation{}, nil
        }
        // already compacted, skip
        if pathEntries[len(pathEntries)-1].GetType() == session.EntryCompaction {
                return &CompactionPreparation{}, nil
        }

        // find the last compaction entry
        preCompactionIndex := -1
        for i := range slices.Backward(pathEntries) {
                entry := pathEntries[i]
                if entry.GetType() == session.EntryCompaction {
                        preCompactionIndex = i
                        break
                }
        }

        // [preCompactionIndex + 1, end)
        start := preCompactionIndex + 1
        end := len(pathEntries)
        cutPoint := findCutPoint(pathEntries, start, end, settings)
        // Semantic refinement (fail-open): with a classifier attached, the cut
        // moves to the nearest clean boundary; every miss leaves the positional
        // cut exactly as computed above.
        cutPoint = refineCutPoint(ctx, pathEntries, start, end, settings, cutPoint)

        firstKeptEntry := pathEntries[cutPoint.firstKeptEntryIndex]
        if firstKeptEntry.GetID() == "" {
                return nil, errors.New("session needs migration")
        }

        firstKeptEntryID := firstKeptEntry.GetID()

        historyEnd := cutPoint.firstKeptEntryIndex
        if cutPoint.isMidTurnCut {
                historyEnd = cutPoint.turnStartIndex
        }

        // Runtime-context riders (clock, weather, memories, job notices) are
        // consumed with the turn they belong to, never summarized: they are
        // ambient per-turn context, not task state, and a zone that reduces to
        // a rider alone is not a conversation worth a summarizer round trip.
        // The strip also guards the empty-preparation early return below: a
        // rider-only history zone now correctly reports nothing to summarize.
        var messagesToSummarize []llm.Message
        for i := start; i < historyEnd; i++ {
                msg := getMessageFromEntry(pathEntries[i])
                if msg != nil && !msg.Runtime {
                        messagesToSummarize = append(messagesToSummarize, *msg)
                }
        }

        // Messages for turn prefix summary (if splitting a turn)
        var turnPrefixMessages []llm.Message
        if cutPoint.isMidTurnCut {
                for i := cutPoint.turnStartIndex; i < cutPoint.firstKeptEntryIndex; i++ {
                        msg := getMessageFromEntry(pathEntries[i])
                        if msg != nil && !msg.Runtime {
                                turnPrefixMessages = append(turnPrefixMessages, *msg)
                        }
                }
        }

        // Nothing falls outside keepRecentTokens: the cut point is the first entry,
        // so there is nothing to summarize. Persisting a summary here would replace
        // the previous summary with "No prior history." (and only add a message to
        // the context), so report an empty preparation instead; the caller skips
        // compaction on an empty FirstKeptEntryId.
        if len(messagesToSummarize) == 0 && len(turnPrefixMessages) == 0 {
                return &CompactionPreparation{}, nil
        }

        previousSummary := ""
        var previousPreserveData map[string]any
        if preCompactionIndex >= 0 {
                prevCompaction := pathEntries[preCompactionIndex].(session.CompactionEntry)
                previousSummary = prevCompaction.Compaction.Summary
                previousPreserveData = prevCompaction.Compaction.PreserveData
        }
        // The previous summary's evidence record described the ledger as of
        // that fold; it is stale by definition here. Strip it so the update
        // prompt sees only narrative summary text — the harness appends a
        // fresh record to the new summary (verified evidence, not folded
        // copies, stays the recurrence state).
        previousSummary = stripEvidenceRecord(previousSummary)

        fileOps := extractFileOperations(messagesToSummarize, pathEntries, preCompactionIndex)

        lastUsage := getLastAssistantUsage(pathEntries)
        // ContextTokens, not TotalTokens: a provider that omits the total still has
        // usable buckets, and the marker must match what the readout shows.
        tokenBefore := lastUsage.ContextTokens()

        return &CompactionPreparation{
                FirstKeptEntryId:     firstKeptEntryID,
                MessagesToSummarize:  messagesToSummarize,
                TurnPrefixMessages:   turnPrefixMessages,
                TokensBefore:         tokenBefore,
                PreviousSummary:      previousSummary,
                PreviousPreserveData: previousPreserveData,
                FileOps:              *fileOps,
                IsMidTurnCut:         cutPoint.isMidTurnCut,
                ReserveTokens:        settings.reverseTokens,
        }, nil
}

// Compact generates a summary for preparation via llm and returns the
// session.Compaction to persist.
func Compact(
        ctx context.Context,
        preparation CompactionPreparation,
        llm llm.Compactor,
) (session.Compaction, error) {
        var (
                summary string
                err     error
        )
        if preparation.IsMidTurnCut && len(preparation.TurnPrefixMessages) > 0 {
                summary, err = summarizeMidTurnCut(ctx, preparation, llm)
        } else {
                summary, err = summarizeHistory(ctx, preparation, llm)
        }
        if err != nil {
                return session.Compaction{}, err
        }

        readFiles, modifiedFiles := computeFileLists(&preparation.FileOps)
        fileOperations := formatFileOperations(readFiles, modifiedFiles)
        if fileOperations != "" {
                summary += "\n\n" + fileOperations
        }

        // Evidence record (Phase 4): appended verbatim after everything the
        // summarizer wrote. Harness-written state must not pass through the
        // model — a summarized copy could drop or distort the failure set the
        // next recurrence round depends on.
        if record := wrapEvidenceRecord(preparation.EvidenceState); record != "" {
                summary += "\n\n" + record
        }

        return session.Compaction{
                Summary:          summary,
                FirstKeptEntryID: preparation.FirstKeptEntryId,
                TokensBefore:     preparation.TokensBefore,
                Details: session.CompactionDetails{
                        ReadFiles:     readFiles,
                        ModifiedFiles: modifiedFiles,
                },
        }, nil
}

// summarizeMidTurnCut runs history and turn-prefix summaries in parallel,
// then joins them when the cut falls inside a turn.
func summarizeMidTurnCut(
        ctx context.Context,
        preparation CompactionPreparation,
        llm llm.Compactor,
) (string, error) {
        var (
                historySummary       string
                turnPrefixSummary    string
                historySummaryErr    error
                turnPrefixSummaryErr error
        )

        var wg sync.WaitGroup
        wg.Add(2)

        go func() {
                defer wg.Done()
                if len(preparation.MessagesToSummarize) == 0 {
                        historySummary = "No prior history."
                        return
                }
                maxTokens := summarizationCap(preparation.ReserveTokens, historySummaryRatio)
                historySummary, historySummaryErr = generateSummary(
                        ctx,
                        llm,
                        preparation.MessagesToSummarize,
                        preparation.PreviousSummary,
                        maxTokens,
                        summarizerInputChars(preparation.ContextWindow, maxTokens),
                )
        }()

        go func() {
                defer wg.Done()
                maxTokens := summarizationCap(preparation.ReserveTokens, turnPrefixSummaryRatio)
                turnPrefixSummary, turnPrefixSummaryErr = generateTurnPrefixSummary(
                        ctx,
                        llm,
                        preparation.TurnPrefixMessages,
                        maxTokens,
                        summarizerInputChars(preparation.ContextWindow, maxTokens),
                )
        }()

        wg.Wait()

        if historySummaryErr != nil {
                return "", historySummaryErr
        }
        if turnPrefixSummaryErr != nil {
                return "", turnPrefixSummaryErr
        }

        return historySummary + "\n\n---\n\n**Turn Context (mid-turn cut):**\n\n" + turnPrefixSummary, nil
}

// summarizeHistory generates a single summary over MessagesToSummarize.
func summarizeHistory(
        ctx context.Context,
        preparation CompactionPreparation,
        llm llm.Compactor,
) (string, error) {
        if len(preparation.MessagesToSummarize) == 0 {
                return "No prior history.", nil
        }
        maxTokens := summarizationCap(preparation.ReserveTokens, historySummaryRatio)
        return generateSummary(
                ctx,
                llm,
                preparation.MessagesToSummarize,
                preparation.PreviousSummary,
                maxTokens,
                summarizerInputChars(preparation.ContextWindow, maxTokens),
        )
}

// getLastAssistantUsage reads usage from the entry, not llm.Message: the
// latter is json:"-" and so reads as zero for every reloaded session.
func getLastAssistantUsage(entries []session.MessageEntry) llm.Usage {
        for i := range slices.Backward(entries) {
                entry := entries[i]
                if entry.GetType() == session.EntryMessage {
                        msgEntry := entry.(session.SessionMessageEntry)
                        if msgEntry.Message.Role == llm.RoleAssistant {
                                return msgEntry.Usage
                        }
                }
        }
        return llm.Usage{
                TotalTokens: 0,
        }
}

func getMessageFromEntry(entry session.MessageEntry) *llm.Message {
        if entry.GetType() == session.EntryMessage {
                msgEntry := entry.(session.SessionMessageEntry)
                return &msgEntry.Message
        }
        return nil
}

const toolResultMaxChars = 500

// summaryOmissionMarker names the front-trim SerializeConversationCapped
// performs under budget; a silent drop would look like the summarizer's
// failure, and the model must know the omission is the harness's.
const summaryOmissionMarker = "[... older history omitted to fit the summarizer's context ...]"

// truncateForSummary truncates content to at most maxChars runes, appending "..." if truncated.
func truncateForSummary(content string, maxChars int) string {
        runes := []rune(content)
        if len(runes) <= maxChars {
                return content
        }
        return string(runes[:maxChars]) + "..."
}

// SerializeConversation formats messages as a single string for summary prompts:
// [User]: ..., [Assistant]: ..., [Assistant tool calls]: ..., [Tool result]: ...
func SerializeConversation(messages []llm.Message) string {
        return strings.Join(serializeParts(messages), "\n\n")
}

// serializeParts renders each message as its own bracketed part; both
// SerializeConversation and the capped variant share the rendering so their
// outputs stay byte-identical below the cap.
func serializeParts(messages []llm.Message) []string {
        var parts []string
        for _, msg := range messages {
                switch msg.Role {
                case llm.RoleUser:
                        if msg.Content != "" {
                                parts = append(parts, "[User]: "+msg.Content)
                        }
                case llm.RoleAssistant:
                        if msg.Content != "" {
                                parts = append(parts, "[Assistant]: "+msg.Content)
                        }
                        if len(msg.ToolCalls) > 0 {
                                var callStrs []string
                                for _, tc := range msg.ToolCalls {
                                        callStrs = append(callStrs, tc.Function.Name+"("+tc.Function.Arguments+")")
                                }
                                parts = append(parts, "[Assistant tool calls]: "+strings.Join(callStrs, "; "))
                        }
                case llm.RoleTool:
                        if msg.Content != "" {
                                parts = append(parts, "[Tool result]: "+truncateForSummary(msg.Content, toolResultMaxChars))
                        }
                }
        }
        return parts
}

// SerializeConversationCapped is SerializeConversation with a character
// budget: when the full serialization exceeds maxChars (<= 0: uncapped),
// the OLDEST entries are dropped until it fits and a marker names the
// omission. The newest material is kept because it abuts the kept zone the
// summary must stay continuous with; the oldest is the most superseded. A
// marked, lossy input beats a summary call that overflows and loses
// everything — that is precisely the failure this exists to prevent on
// small-window models.
func SerializeConversationCapped(messages []llm.Message, maxChars int) string {
        parts := serializeParts(messages)
        full := strings.Join(parts, "\n\n")
        if maxChars <= 0 || len(full) <= maxChars {
                return full
        }
        budget := maxChars - len(summaryOmissionMarker) - 2
        if budget < 0 {
                budget = 0
        }
        kept := 0
        used := 0
        for i := len(parts) - 1; i >= 0; i-- {
                cost := len(parts[i]) + 2 // the "\n\n" join
                if used+cost > budget {
                        break
                }
                used += cost
                kept++
        }
        switch {
        case kept == 0:
                // Even the newest part alone exceeds the budget: keep its
                // tail (the end is the most recent content) and mark.
                tail := parts[len(parts)-1]
                if len(tail) > budget && budget > 0 {
                        tail = tail[len(tail)-budget:]
                }
                return summaryOmissionMarker + "\n\n" + tail
        case kept == len(parts):
                // Join-accounting slack: everything fit after all.
                return full
        }
        return summaryOmissionMarker + "\n\n" + strings.Join(parts[len(parts)-kept:], "\n\n")
}
