The Blueprint for the Final 8 (Go-Specific Architecture):

1. ~~Semantic Code Search (The Vector Index)~~

     The Engine: ollama- let the user pick which embedding model via config web editor
     The Storage: sqlite-vec (SQLite vector extension). Zero external dependencies. Zero network calls. Pure local storage.
     The Protocol: Chunk files into 50-line windows. Embed each chunk. Store with file path + line range. Query by embedding the search string and finding the nearest neighbor.
     The Go Lib: ollama-api-go for the local API + sqlite-vec Go bindings.
     make configurable via the config web server

2. ~~Session Memory Bank (The Persistent State)~~

     The Architecture: Don't overthink this. It is a key-value store keyed by session ID.
     The Storage: bbolt (pure Go embedded key-value DB) or just structured JSON files in ~/.config/.a1/sessions/.
     The Schema: { "session_id": "...", "timestamp": "...", "type": "decision"|"error"|"context", "content": "..." }.
     The Protocol: On every tool call, write to the bank. On session start, load the last N entries into the system prompt context.

3. ~~Call Graph / Dependency Tracer (The AST Engine)~~

     The Engine: tree-sitter (gotreesitter, pure Go, no CGO). It parses 40+ languages into ASTs.
     The Go Lib: github.com/odvcencio/gotreesitter + grammars.
     The Protocol: Parse every file. Find import statements. Build a directed graph: File A → File B. Store the graph in memory. Query: "Who imports purity.go?"
     Build variants: default lightweight parser (Go/Python/Rust/JS) or `-tags treesitter` for full tree-sitter grammar support.

4. Batch Editor with Dependency Ordering (The Topological Sort)

     The Architecture: You already have the call graph from #3. Run a topological sort on it. If File A imports File B, edit B first.
     The Go Lib: gonum/graph/topo (Go's standard graph library) or just implement Kahn's algorithm in 20 lines of Go.
     The Protocol: Agent says "Edit files X, Y, Z." Sort them. Apply edits in order. Verify after each edit.

5. Test Impact Selector (The Inverse Graph)

     The Architecture: Invert the call graph. Map test files to the source files they test (via import tracing).
     The Protocol: Agent edits purity.go. Look up all test files that import purity.go. Run only those tests.
     The Go Lib: Same tree-sitter graph, inverted.

6. Environment Snapshot / Rollback (The Safety Net)

     The Architecture: Use git.
     The Protocol: Before a batch edit, create a temporary git branch: harness-snapshot-<timestamp>. Commit the current state. Apply edits. Run tests. If tests fail, git checkout the snapshot branch. If tests pass, merge. Delete the temp branch.
     The Go Lib: go-git/go-git.

7. enhance the permission layer: 
first we need to be able to open the harness in a specific directory
the default mode is limit read and write to that directory only like kilo code does it
add a yolo (gives eveery permission to the agent) mode that can be switched on or off by the user 
in default mode agent can request to access a directory outside the sandbox prompting the suer yes no or always allow
7.5. UX tab to switch thinking modes on the fly- shift tab to cycle permission modes

8. Web Search using Playwright (The Fetcher) - install Playwright browser into .config/.a1/pw/browser_go_here | the agent has its own sandboxed browser on the host machine so the agent cant take any drastic measures

9. Runtime State Inspector / Debugger Source-level intelligence is great, but when tests fail, the agent needs to inspect runtime state: variable values, heap snapshots, process state.
"What was the value of this variable when the crash happened?" Without this, the agent debugs blind.

10. REPL / scratchpad — developer experience. a global place for the llm to write notes stuff should be located in .config/.a1/scratchpad the agent can make any amount of notes in here- maybe use a db since itd be easier to query not sure

11. Config / env validator — developer experience.

12. ~~UX up arrow key puts the previous input in the box- kind of like a terminal~~

13. command parity with claude code (maybe not sure on this one)


The Sequence (How to Build Them):

    Memory Bank (#2) - Everything depends on persistent state.
    Call Graph (#3) - Batch Editor and Test Impact both need it.
    Snapshot/Rollback (#6) - You need this before testing risky refactors.
    Semantic Search (#1) - Heaviest to build, highest context efficiency.
    Batch Editor (#4) + Test Impact (#5) - Depend on Call Graph.
    Sandbox (#7) - Independent.
    Web Search (#8) - Independent.
