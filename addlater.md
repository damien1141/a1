# Addlater

## P0 — Highest leverage

1. Semantic code search — context efficiency is my biggest bottleneck.
2. Session memory bank — persistent memory eliminates rework.
3. Call graph / dependency tracer — code intelligence cuts through the noise.
4. Batch editor with dependency ordering — coordination safety.
5. Test impact selector — verification feedback loop.
6. Environment Snapshot / Rollback
The agent can try a risky refactor, run tests, and roll back the entire filesystem to the pre-edit state if tests fail. Without this, the agent is terrified of bold changes because a bad edit cascades into 50 broken files with no undo. git stash is a hack; the agent needs a filesystem-level snapshot API.
7. Execution Sandbox with Resource Limits
Not just a shell. A sandboxed execution environment with timeout, memory limit, CPU limit, and network isolation. The agent needs to run cargo build, pytest, and custom scripts that might hang, infinite loop, or eat all RAM without bricking the host. Without this, one bad test script kills the session.
8. Web search using playwrite


## P1 — High leverage

6. ~~File importance ranker~~ — context efficiency.
7. ~~Error pattern library~~ — persistent memory.
8. ~~Decision log~~ — persistent memory.
9. ~~API surface validator~~ — code intelligence.
10. ~~Dead code detector~~ — code intelligence.
11. ~~Test coverage mapper~~ — testing & verification.
12. ~~Refactoring impact analyzer~~ — multi-file coordination.
13. ~~Migration assistant~~ — multi-file coordination.
14. ~~Semantic blame — git intelligence.~~
15. ~~PR meaning extractor — git intelligence.~~
16. ~~Merge conflict predictor — git intelligence.~~
17. ~~Test result interpreter~~ — testing & verification.
18. ~~Property-based test generator~~ — testing & verification.
19. ~~Cross-Repo Dependency Tracker~~
The call graph is intra-repo. This is inter-repo. If repo A imports from repo B, changing B's API should flag A. The agent can't safely refactor a shared library without knowing the blast radius across all consumers.
20. Runtime State Inspector / Debugger
Source-level intelligence is great, but when tests fail, the agent needs to inspect runtime state: variable values, heap snapshots, process state. "What was the value of this variable when the crash happened?" Without this, the agent debugs blind.
21. ~~File Watcher / Change Detector~~
The agent should detect when files change on disk (by the user, by build tools, by git operations) and reload context automatically. Without this, the agent operates on stale data and overwrites your manual edits.

## P2 — Medium leverage

19. REPL / scratchpad — developer experience.
20. Config / env validator — developer experience.
21. ~~Stack trace navigator — developer experience.~~
22. ~~TODO / FIXME collector — developer experience.~~
23. ~~Doc-to-code sync checker~~ — documentation.
24. ~~API doc generator~~ — documentation.
25. ~~Basic vulnerability scanner~~ — security & performance.
26. ~~N+1 query detector~~ — security & performance.
27. ~~Secret scanner~~ — security & performance.

## P3 — Lower leverage

28. ~~Error pattern matcher~~ — workflow & productivity.
29. ~~Project scaffold aware tools~~ — workflow & productivity.
27. ~~Context budget manager~~ — context efficiency. Revamped with fold-based concepts: protected zones, recent zone, growth-gated compression, tier accounting, and fold-health summary.
31. ~~Action Journal / Audit Trail~~
Different from the "decision log." This records every operational action: "I edited file X at line Y because Z. I ran cargo check and got error E. I applied fix F." The agent can replay its own session to debug where it went wrong.
32. ~~Token Budget Allocator~~ — context efficiency. Dynamic allocation of context tokens across tools based on task complexity.
33. ~~Multi-Language Error Pattern Translator~~ — developer experience. Unified translator mapping language-specific errors to actionable fixes.
34. ~~Build System Integration~~ — developer experience. Understands make targets, cmake, pkg-config, meson, cargo, go build tags, npm scripts, and gradle tasks.
