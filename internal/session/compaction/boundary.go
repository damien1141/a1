package compaction

import (
	"context"
	"slices"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// Semantic boundary refinement.
//
// The positional cut (findCutPoint) knows token budgets but not tasks: it
// will happily land between an assistant's tool call and its result, and
// the mid-turn machinery (turn-prefix summary) patches the orphaned half
// afterwards — a second summarizer call running in parallel with the
// history summary, compensating for a cut that could have been clean.
//
// When a decision model is configured, the candidate boundaries nearest
// the positional cut are classified — "does the work just before this
// boundary reach a natural completion?" — and the cut moves to the
// closest clean boundary within the window. One classification round
// trip; when it picks a turn boundary, the turn-prefix summary (and its
// LLM call) disappears entirely.
//
// Fail-open by construction: no classifier, no candidates, a classifier
// error, or nothing classified clean all leave the positional cut exactly
// as it was. The classifier can only improve where the cut lands; it can
// never block, delay beyond its own timeout, or break compaction.

const (
	// boundaryWindowEachSide bounds how many candidate boundaries on each
	// side of the positional cut are offered to the classifier. Small on
	// purpose: the refinement is a nudge, not a re-plan, and every hop
	// away from the positional cut trades budget exactness for semantic
	// cleanliness.
	boundaryWindowEachSide = 3

	// boundaryKeptFloorDiv is the fraction of keepRecentTokens that must
	// survive a newer-boundary move. The budget exists to keep the agent
	// oriented in recent context; folding most of it away to chase a
	// clean boundary trades a small summarization win for disorientation.
	boundaryKeptFloorDiv = 2

	// boundaryTextTail bounds the preceding-message excerpt sent to the
	// classifier; boundaryTextHead bounds the following-message excerpt.
	// Enough text to judge completion, not enough to tax a classifier
	// built for yes/no answers.
	boundaryTextTail = 1200
	boundaryTextHead = 400
)

// BoundaryCandidate is one alternative cut position offered to the
// classifier. KeptTokens is the constraint the refinement enforces; the
// excerpts are the evidence the classifier judges.
type BoundaryCandidate struct {
	// Index is the candidate's position in the entry slice.
	Index int
	// KeptTokens is the estimated token size of the kept zone when cutting
	// here (entries [Index, endIndex)).
	KeptTokens int
	// Preceding is the tail of the last message that would be summarized.
	Preceding string
	// Following is the head of the first message that would be kept.
	Following string
	// PrecedingRole and FollowingRole are the roles of those messages,
	// because "follows is a fresh user request" is a strong cleanness
	// signal the classifier should see.
	PrecedingRole llm.Role
	FollowingRole llm.Role
}

// BoundaryClassifier decides which candidate cut boundaries are clean
// fold points. Implementations ask a decision model; the contract is that
// only returned indices are considered clean, and an error means "no
// opinion" — callers fall back to the positional cut.
type BoundaryClassifier interface {
	ClassifyBoundaries(ctx context.Context, candidates []BoundaryCandidate) (map[int]bool, error)
}

// refineCutPoint moves the cut to the closest classifier-approved boundary
// within the window, subject to the kept-zone constraints, and returns the
// positional result unchanged whenever the classifier offers nothing
// usable. settings.boundaryClassifier nil short-circuits before any work.
func refineCutPoint(
	ctx context.Context,
	entries []session.MessageEntry,
	startIndex, endIndex int,
	settings Settings,
	positional CutPointResult,
) CutPointResult {
	clf := settings.boundaryClassifier
	if clf == nil {
		return positional
	}
	candidates := boundaryCandidates(entries, startIndex, endIndex, settings, positional)
	if len(candidates) == 0 {
		return positional
	}
	clean, err := clf.ClassifyBoundaries(ctx, candidates)
	if err != nil {
		debuglog.Logf("compaction: boundary classifier unavailable, positional cut stands: %v", err)
		return positional
	}
	best := -1
	bestDistance := 0
	for _, cand := range candidates {
		if !clean[cand.Index] {
			continue
		}
		distance := cand.Index - positional.firstKeptEntryIndex
		if distance < 0 {
			distance = -distance
		}
		// Nearest wins; on a tie, the older boundary keeps more context —
		// the conservative side, with the INV5 ceiling already enforced.
		if best == -1 || distance < bestDistance {
			best = cand.Index
			bestDistance = distance
		}
	}
	if best == -1 {
		return positional
	}
	return cutPointResultAt(entries, best, startIndex)
}

// boundaryCandidates collects the cut points within the window of the
// positional cut that satisfy the kept-zone constraints, oldest first.
// The positional cut itself is not a candidate: it is the default that
// stands unless something else wins.
func boundaryCandidates(
	entries []session.MessageEntry,
	startIndex, endIndex int,
	settings Settings,
	positional CutPointResult,
) []BoundaryCandidate {
	cutPoints := collectCutPoints(entries, startIndex, endIndex)
	pos := slices.Index(cutPoints, positional.firstKeptEntryIndex)
	if pos < 0 {
		return nil
	}
	lo := pos - boundaryWindowEachSide
	if lo < 0 {
		lo = 0
	}
	hi := pos + boundaryWindowEachSide + 1
	if hi > len(cutPoints) {
		hi = len(cutPoints)
	}

	maxKept := settings.MaxKeptTokens()
	keptFloor := settings.KeepRecentTokens() / boundaryKeptFloorDiv
	var out []BoundaryCandidate
	for _, idx := range cutPoints[lo:hi] {
		if idx == positional.firstKeptEntryIndex {
			continue
		}
		kept := keptZoneEstimate(entries, idx, endIndex)
		if idx < positional.firstKeptEntryIndex {
			// Older boundary: the kept zone grows. The INV5 ceiling still
			// binds (maxKept <= 0 means no ceiling, matching capKeptZone).
			if maxKept > 0 && kept > maxKept {
				continue
			}
		} else {
			// Newer boundary: the kept zone shrinks. Keep at least half
			// the recent budget so the agent stays oriented.
			if keptFloor > 0 && kept < keptFloor {
				continue
			}
		}
		preceding, precedingRole := messageBefore(entries, idx, startIndex)
		following, followingRole := messageAt(entries, idx)
		out = append(out, BoundaryCandidate{
			Index:         idx,
			KeptTokens:    kept,
			Preceding:     truncateTail(preceding, boundaryTextTail),
			Following:     truncateHead(following, boundaryTextHead),
			PrecedingRole: precedingRole,
			FollowingRole: followingRole,
		})
	}
	return out
}

// messageBefore returns the content and role of the last message entry
// strictly before index, walking backward past non-message entries.
func messageBefore(entries []session.MessageEntry, index, startIndex int) (string, llm.Role) {
	for i := index - 1; i >= startIndex; i-- {
		e := entries[i]
		if e.GetType() != session.EntryMessage {
			continue
		}
		msg := e.(session.SessionMessageEntry).Message
		return msg.Content, msg.Role
	}
	return "", ""
}

// messageAt returns the content and role of the message entry at index
// (cut points are always message entries; anything else yields empties).
func messageAt(entries []session.MessageEntry, index int) (string, llm.Role) {
	e := entries[index]
	if e.GetType() != session.EntryMessage {
		return "", ""
	}
	msg := e.(session.SessionMessageEntry).Message
	return msg.Content, msg.Role
}

// truncateTail keeps the last limit bytes of s.
func truncateTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

// truncateHead keeps the first limit bytes of s.
func truncateHead(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
