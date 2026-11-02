package compaction

import (
        "slices"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

// CutPointResult identifies where to cut the session history: the index of
// the first entry to keep and, when the cut falls mid-turn, that turn's
// starting index.
type CutPointResult struct {
        // firstKeptEntryIndex is the index of the first entry to keep.
        firstKeptEntryIndex int
        // turnStartIndex is the user message that opens the turn containing the
        // cut, or -1 when the cut is at a turn boundary.
        turnStartIndex int
        // isMidTurnCut is true when firstKept is not a user message, so the
        // orphaned start of that turn needs a turn-prefix summary.
        isMidTurnCut bool
}

func findCutPoint(
        messageEntries []session.MessageEntry,
        startIndex int,
        endIndex int,
        settings Settings,
) CutPointResult {
        cutPoints := collectCutPoints(messageEntries, startIndex, endIndex)
        if len(cutPoints) == 0 {
                return CutPointResult{
                        firstKeptEntryIndex: startIndex,
                        turnStartIndex:      -1,
                        isMidTurnCut:        false,
                }
        }

        cutIndex := findCutIndex(
                messageEntries,
                startIndex,
                endIndex,
                settings.KeepRecentTokens(),
                cutPoints,
        )
        // Kept-zone ceiling (INV5): when the budget cut leaves more than
        // maxKeptTokens behind — a giant message landed inside the kept
        // window — the cut moves to NEWER boundaries until the kept zone
        // fits. Without this, a big paste or result inside the kept zone
        // survives every compaction: the post-compaction context still
        // overflows, recovery finds nothing new to shrink, and the turn
        // dies on streak exhaustion. A ceiling no boundary can satisfy
        // (the huge entry is at the very tail) leaves the cut as-is: the
        // overflow-recovery window halving takes over from there.
        cutIndex = capKeptZone(messageEntries, endIndex, cutIndex, cutPoints, settings.MaxKeptTokens())

        // Entry at the cut point;
        // used to tell if we cut at a user message (a turn boundary).
        cutEntry := messageEntries[cutIndex]
        isUserMessage := cutEntry.GetType() == session.EntryMessage &&
                cutEntry.(session.SessionMessageEntry).Message.Role == llm.RoleUser

        // [userMessage, cutIndex) is the orphaned turn prefix when cutting mid-turn.
        turnStartIndex := -1
        if !isUserMessage {
                turnStartIndex = findTurnStartIndex(messageEntries, cutIndex, startIndex)
        }

        return CutPointResult{
                firstKeptEntryIndex: cutIndex,
                turnStartIndex:      turnStartIndex,
                isMidTurnCut:        !isUserMessage && turnStartIndex != -1,
        }
}

// findCutIndex walks entries backward from endIndex accumulating each
// message's own estimated size until it reaches keepRecentTokens. It then
// finds the first valid cut point at or after the position where the budget
// was reached. If the budget is never reached, it falls back to the earliest
// candidate cut point.
//
// When the budget is reached inside the newest entries and no message
// boundary exists at or after that position — the signature case is one huge
// tool result at the tail of the turn — the fallback is the NEWEST cut point
// (cut the whole turn that carries the bulk), not the earliest. The earliest
// would keep everything and summarize nothing, producing an empty compaction
// that cannot relieve pressure: overflow recovery would find nothing to
// shrink and the turn would die even though older history is summarizable.
func findCutIndex(
        entries []session.MessageEntry,
        startIndex int,
        endIndex int,
        keepRecentTokens int,
        cutPoints []int,
) int {
        accumulatedTokens := 0
        cutIndex := cutPoints[0]

        for i := endIndex - 1; i >= startIndex; i-- {
                entry := entries[i]
                if entry.GetType() != session.EntryMessage {
                        continue
                }

                messageTokens := estimateMessageTokens(entry.(session.SessionMessageEntry).Message)
                if messageTokens == 0 {
                        continue
                }
                accumulatedTokens += messageTokens

                if accumulatedTokens >= keepRecentTokens {
                        found := false
                        for _, point := range cutPoints {
                                if point >= i {
                                        cutIndex = point
                                        found = true
                                        break
                                }
                        }
                        if !found {
                                // The kept zone would start inside a tool-result tail: no
                                // user/assistant boundary exists at or after the budget
                                // position. Cut at the newest boundary instead — a kept
                                // zone slightly over budget beats an empty compaction.
                                cutIndex = cutPoints[len(cutPoints)-1]
                        }
                        break
                }
        }

        return cutIndex
}

// capKeptZone enforces the kept-zone ceiling: when the entries from
// cutIndex to endIndex estimate larger than maxKept, the cut moves to NEWER
// boundaries until the kept zone fits or the newest cut point is reached.
// Newer, not older, because the kept zone is [cut, end): moving the cut
// forward drops the oversized entry into the summarized zone (where the
// serialization truncates old tool results and the summary compresses the
// rest) instead of carrying it forward verbatim. A ceiling no boundary
// satisfies — the huge entry sits at the tail, after every cut point —
// leaves the cut unchanged: nothing cut placement can do, and the
// overflow-recovery window halving takes over.
func capKeptZone(
        entries []session.MessageEntry,
        endIndex int,
        cutIndex int,
        cutPoints []int,
        maxKept int,
) int {
        if maxKept <= 0 {
                return cutIndex
        }
        pos := slices.Index(cutPoints, cutIndex)
        if pos < 0 {
                return cutIndex
        }
        for pos < len(cutPoints)-1 && keptZoneEstimate(entries, cutPoints[pos], endIndex) > maxKept {
                pos++
        }
        return cutPoints[pos]
}

// keptZoneEstimate sums estimateMessageTokens over the message entries in
// [from, endIndex) — the context size the kept zone would occupy if the
// cut landed at from.
func keptZoneEstimate(entries []session.MessageEntry, from, endIndex int) int {
        total := 0
        for i := from; i < endIndex; i++ {
                if entries[i].GetType() != session.EntryMessage {
                        continue
                }
                total += estimateMessageTokens(entries[i].(session.SessionMessageEntry).Message)
        }
        return total
}

// estimatedImageChars is the stand-in size for one attached image. Images
// arrive base64-encoded, so their string length says nothing about the tokens
// the model spends on them.
const estimatedImageChars = 4800

// estimateMessageTokens estimates one message's size with a chars/4 heuristic.
// The budget is spent per message, so this must never fall back to provider
// usage: an assistant message reports the size of the whole conversation up to
// that turn (and only assistant messages report anything), so accumulating it
// overflows keepRecentTokens on the newest message and collapses the cut to
// the last entry.
func estimateMessageTokens(msg llm.Message) int {
        chars := len(msg.Content) + len(msg.ReasoningContent)
        for _, call := range msg.ToolCalls {
                chars += len(call.Function.Name) + len(call.Function.Arguments)
        }
        chars += len(msg.Images) * estimatedImageChars
        return (chars + 3) / 4
}

// EstimateContextTokens sums estimateMessageTokens over every message entry
// in entries. This is the bytes/4 ruler for the growth gate: the same ruler
// the cut logic uses, the same ruler PostCompactionEstimate produces for
// lastCompactTokens. Feeding a provider-reported ContextTokens() to the
// growth gate mixed units (real tokens vs bytes/4) and broke the growth
// calculation — this function gives the gate a single consistent ruler.
//
// The safety valve (ShouldCompact) stays on provider-reported ContextTokens
// because it compares against the model's actual context window, which is
// in real tokens; only the growth gate (ShouldCompactGated) should use this
// estimate.
func EstimateContextTokens(entries []session.MessageEntry) int {
        total := 0
        for _, e := range entries {
                if e.GetType() != session.EntryMessage {
                        continue
                }
                msg, ok := e.(session.SessionMessageEntry)
                if !ok {
                        continue
                }
                total += estimateMessageTokens(msg.Message)
        }
        return total
}

// collectCutPoints returns the indices of every user/assistant message
// entry in [startIndex, endIndex), in chronological order: the positions
// where a compaction cut may land. Picking among them is findCutIndex's job.
func collectCutPoints(entries []session.MessageEntry, startIndex, endIndex int) []int {
        var cutPoints []int

        for i := startIndex; i < endIndex; i++ {
                e := entries[i]
                if e.GetType() != session.EntryMessage {
                        continue
                }
                msgEntry := e.(session.SessionMessageEntry)
                role := msgEntry.Message.Role
                if role == llm.RoleUser || role == llm.RoleAssistant {
                        cutPoints = append(cutPoints, i)
                }
        }
        return cutPoints
}

// findTurnStartIndex walks backward from entryIndex to find the user message
// that starts the current turn.
func findTurnStartIndex(entries []session.MessageEntry, entryIndex, startIndex int) int {
        for i := entryIndex; i >= startIndex; i-- {
                entry := entries[i]
                ty := entry.GetType()

                if ty == session.EntryMessage {
                        msgEntry := entry.(session.SessionMessageEntry)
                        if msgEntry.Message.Role == llm.RoleUser {
                                return i
                        }
                }
        }
        return -1
}
