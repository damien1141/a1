package memchunk

// This file is the benchmark's ground truth: a small, hand-labelled set of
// turns, the facts a correct extractor must keep, the statements it must reject,
// and a seeded bank for the dedupe pass to match against.
//
// The labels are the oracle. They were written by hand, from the perspective of
// "what would I be annoyed to find missing / annoyed to find stored", and they
// are deliberately adversarial in ways the old whole-message pipeline handled
// badly:
//
//   - a real preference buried in the same turn as a stack trace (the mixed
//     case that used to lose the preference entirely)
//   - near-duplicates of seeded memories (the dedupe pass's whole purpose)
//   - confident-sounding but transient statements ("the build failed")
//   - reasoning-block content that is genuinely worth keeping

// FixtureTurn is one turn of conversation plus the surface its prose came from.
type FixtureTurn struct {
	Text string
	Kind Kind
}

// FixtureTurns returns the labelled conversation.
func FixtureTurns() []FixtureTurn {
	return []FixtureTurn{
		// T1 — the mixed-content case. A real preference and a real environment
		// fact share the turn with a stack trace. The old pipeline judged the
		// whole message at once and dropped all three.
		{Text: "I prefer tabs over spaces, and I run Arch btw. Here's the crash:\n\n" +
			"```\npanic: runtime error: index out of range\n" +
			"goroutine 1 [running]:\nmain.main()\n\t/src/main.go:42 +0x1d\n```\n" +
			"Also the build fails on your machine but works on mine, so I'm not sure.",
			Kind: KindProse},

		// T2 — durable project decisions, stated plainly.
		{Text: "We settled on SQLite for the vault layer. The schema is owned by the " +
			"subsystem, not by core migrations. That decision is final.",
			Kind: KindProse},

		// T3 — near-duplicate of the seeded memory "user prefers tabs over spaces".
		// The dedupe pass must fold this, not store it twice.
		{Text: "Just to be clear about my editor setup: I really do prefer tabs to spaces.",
			Kind: KindProse},

		// T4 — pure junk: a log dump. Nothing here is a memory.
		{Text: "2024-01-15 10:22:01 ERROR failed to connect to 127.0.0.1:5432\n" +
			"2024-01-15 10:22:02 ERROR retrying in 5s\n" +
			"2024-01-15 10:22:07 ERROR retrying in 5s\n" +
			"2024-01-15 10:22:12 ERROR gave up after 3 attempts",
			Kind: KindProse},

		// T5 — reasoning block carrying a durable user fact. This is the signal
		// that was thrown away before ReasoningContent was wired in.
		{Text: "The user mentioned earlier they work late on weekdays and prefer " +
			"short answers in the morning. Worth noting for tone.",
			Kind: KindReasoning},

		// T6 — a command with its output. Never a memory.
		{Text: "go test ./internal/memstore/ -run TestInsert\n" +
			"--- FAIL: TestInsert (0.01s)\n" +
			"    store_test.go:88: expected 1 row, got 0",
			Kind: KindProse},
	}
}

// FixtureExpected lists the facts a correct run MUST store. Each entry is a
// distinctive substring of the expected statement, so a merged or truncated
// chunk that still carries the fact counts as recalled.
func FixtureExpected() []string {
	return []string{
		"prefer tabs",           // T1, buried in the mixed turn
		"arch",                  // T1, environment detail
		"sqlite",                // T2, project decision
		"schema is owned",       // T2, project decision, specific wording
		"work late on weekdays", // T5, reasoning block
		"short answers",         // T5, reasoning block
	}
}

// FixtureJunk lists markers of statements a correct run must NOT store. Matching
// is substring, lowercase.
func FixtureJunk() []string {
	return []string{
		"panic: runtime error", // T1 stack trace
		"goroutine",            // T1 stack trace
		"index out of range",   // T1 stack trace
		"failed to connect",    // T4 log
		"retrying in",          // T4 log
		"gave up after",        // T4 log
		"--- fail",             // T6 test output
		"expected 1 row",       // T6 test output
		"build fails on your",  // T1, transient state
	}
}

// FixtureBank is the pre-seeded memory set the dedupe pass matches against. The
// first entry is a near-duplicate of T3 on purpose: a run that stores T3 instead
// of folding it into this row is a dedupe failure.
func FixtureBank() []ExistingMemory {
	return []ExistingMemory{
		{ID: 1, Content: "User prefers tabs over spaces for indentation."},
		{ID: 2, Content: "Project uses SQLite for the vault layer."},
		{ID: 3, Content: "User runs tests before committing."},
	}
}
