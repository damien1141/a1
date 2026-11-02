# Debugging Tools by Language

Quick reference for setting up and driving an interactive debugger per language. Prefer a debugger over `print` / `console.log` — you can ask new questions at the breakpoint without re-running.

## Quick Picker

| Language | Debugger | Start | In-code breakpoint |
|---|---|---|---|
| Python | pdb / ipdb / pudb | `python -m pdb script.py` | `breakpoint()` (3.7+) |
| Node / TypeScript | Chrome DevTools / VS Code | `node --inspect-brk script.js` | `debugger;` |
| Go | Delve | `dlv debug ./cmd/app` | `runtime.Breakpoint()` |
| Rust | gdb / lldb (rust-gdb / rust-lldb) | `rust-gdb ./target/debug/app` | `panic!()` or `std::intrinsics::breakpoint()` |
| C / C++ | gdb / lldb | `gdb ./app` | `__builtin_trap();` |
| Java / JVM | jdb / IDE | IDE debugger | `System.console().readLine();` |
| Browser JS | Chrome DevTools | open DevTools → Sources | `debugger;` |
| React | React DevTools (browser ext) | install extension | n/a |
| .NET | VS / VS Code / dotnet-dump | `dotnet-dump collect` | `Debugger.Break()` |

## Python — pdb / ipdb / pudb

```bash
# Launch from start
python -m pdb script.py

# Post-mortem on first exception
python -m pdb -c continue script.py

# Drop into pdb at a specific line
python -c "import pdb; pdb.set_trace()" script.py
```

```python
# In code (Python 3.7+)
breakpoint()                                    # drops into pdb

# Older Python
import pdb; pdb.set_trace()
```

### pdb commands
| Command | Action |
|---|---|
| `n` | Next line (step over) |
| `s` | Step into |
| `c` | Continue |
| `l` | List source around current line |
| `p expr` | Print expression |
| `pp expr` | Pretty-print |
| `w` | Where (stack trace) |
| `b 42` | Breakpoint at line 42 |
| `b func` | Breakpoint at function |
| `cl 42` | Clear breakpoint |
| `a` | Print args of current function |
| `q` | Quit |

### ipdb (nicer REPL)
```bash
pip install ipdb
# Drop in by setting PYTHONBREAKPOINT
export PYTHONBREAKPOINT=ipdb.set_trace
```

### pudb (full-screen TUI)
```bash
pip install pudb
# Then in code:
import pudb; pudb.set_trace()
# Or:
pudb script.py
```

### Post-mortem on an uncaught exception
```python
import pdb, traceback, sys
try:
    main()
except Exception:
    traceback.print_exc()
    pdb.post_mortem(sys.exc_info()[2])
```

### Quick prints
```python
print(f"{variable=}")                          # Python 3.8+ — shows name + value
print(f"{user.id=} {user.email=}")

# Rich object inspection
from rich import inspect, pretty
pretty.install()
inspect(obj, methods=True)
```

## Node.js / TypeScript — Chrome DevTools

```bash
# Start with inspector; pause at first line
node --inspect-brk dist/main.js

# With ts-node
node --inspect-brk -r ts-node/register src/main.ts

# Allow CORS for inspector (attach from another machine)
node --inspect=0.0.0.0:9229 dist/main.js
```

In Chrome: open `chrome://inspect` → click "inspect" under Remote Target.

DevTools panels:
- **Sources** — breakpoints, step over/into/out, watch expressions, call stack, scope
- **Console** — REPL in the breakpoint context
- **Memory** — heap snapshots, allocation timeline
- **Profiler** — CPU profile, flame chart

### In-code
```typescript
debugger;                                      // breakpoint — only fires when inspector attached

console.log({ variable });                     // shows name + value
console.table(arrayOfObjects);                 // table format
console.trace('Called from');                  // stack trace
console.dir(obj, { depth: null, colors: true });
console.time('label'); /* ... */ console.timeEnd('label');
```

### Async stack traces
```bash
# Node 12+ includes async stack traces by default
node --async-stack-traces dist/main.js
```

### Memory leak hunt
1. DevTools → Memory → Heap snapshot
2. Take snapshot (baseline)
3. Trigger the suspected leak
4. Take snapshot
5. Compare to baseline; look for growing `Detached` DOM nodes or retained closures

## Go — Delve

```bash
# Build and debug
dlv debug ./cmd/server

# Attach to running process
dlv attach <pid>

# Debug a specific test
dlv test ./pkg/...

# Headless mode for editors
dlv debug --headless --listen=:2345 --api-version=2 ./cmd/server
```

### Delve commands
| Command | Action |
|---|---|
| `break main.go:55` / `b main.go:55` | Set breakpoint |
| `break main.handleRequest` | Breakpoint at function |
| `continue` / `c` | Continue |
| `next` / `n` | Next line (step over) |
| `step` / `s` | Step into |
| `stepout` | Step out |
| `print myVar` / `p myVar` | Print variable |
| `goroutines` / `grs` | List goroutines |
| `goroutine 5` | Switch to goroutine 5 |
| `goroutines -with-user-code` | Filter to user code |
| `stack` / `bt` | Stack trace |
| `locals` | Print local variables |
| `args` | Print function args |
| `clear main.go:55` | Clear breakpoint |
| `exit` / `q` | Quit |

### Race detector (catches data races)
```bash
go test -race ./...
go run -race ./cmd/server
```

### Quick prints
```go
log.Printf("%+v", variable)                    // with field names
fmt.Printf("%#v\n", variable)                  // Go syntax representation

// Spew for complex structures
import "github.com/davecgh/go-spew/spew"
spew.Dump(variable)
```

## Rust — gdb / lldb

```bash
# Install pretty-printers
rustup component add rust-src
# Use rust-gdb or rust-lldb wrappers (auto-load pretty printers)

rust-gdb ./target/debug/app
rust-lldb ./target/debug/app
```

### gdb commands (apply to lldb with slightly different syntax)
| gdb | lldb | Action |
|---|---|---|
| `break main.rs:42` | `b main.rs:42` | Breakpoint |
| `run` / `r` | `run` | Run |
| `next` / `n` | `n` | Step over |
| `step` / `s` | `s` | Step into |
| `print x` / `p x` | `p x` | Print |
| `bt` | `bt` | Backtrace |
| `continue` / `c` | `c` | Continue |
| `frame 2` | `frame select 2` | Switch frame |
| `info locals` | `frame variable` | Local vars |

### Race + memory safety
Rust's borrow checker catches most memory bugs at compile time. For the rest:
- `cargo build --debug` (don't strip symbols)
- AddressSanitizer: `RUSTFLAGS="-Z sanitizer=address" cargo +nightly build`
- MemorySanitizer: `RUSTFLAGS="-Z sanitizer=memory" cargo +nightly build`
- ThreadSanitizer: `RUSTFLAGS="-Z sanitizer=thread" cargo +nightly build`
- Valgrind: `valgrind --leak-check=full ./target/debug/app`

### Quick prints
```rust
dbg!(variable);                                 // prints to stderr with file:line
eprintln!("{:?}", variable);
eprintln!("{:#?}", variable);                   // pretty-print
```

## C / C++ — gdb / lldb

```bash
gdb ./app
lldb ./app

# With arguments
gdb --args ./app --flag value

# Core dump
gdb ./app core.dump
```

### Sanitizers (compile-time)
```bash
# AddressSanitizer — use-after-free, buffer overflow
gcc -fsanitize=address -g app.c -o app

# UndefinedBehaviourSanitizer
gcc -fsanitize=undefined -g app.c -o app

# ThreadSanitizer — data races
gcc -fsanitize=thread -g app.c -o app

# MemorySanitizer — uninitialized reads
clang -fsanitize=memory -g app.c -o app
```

## Browser JavaScript — Chrome DevTools

Open with `F12` / `Cmd+Opt+I`.

| Panel | Use for |
|---|---|
| **Elements** | DOM inspection, live CSS editing |
| **Console** | REPL, log inspection |
| **Sources** | Breakpoints, step debugging, watch expressions |
| **Network** | Request waterfall, payloads, headers, timing |
| **Performance** | CPU profile, flame chart |
| **Memory** | Heap snapshots, allocation timeline |
| **Application** | Storage (cookies, localStorage, IndexedDB), service workers |
| **Lighthouse** | Perf, a11y, SEO audit |

### Breakpoint types
- **Line of code** — pause at a line
- **DOM** — pause when a node changes
- **XHR / fetch** — pause when a URL pattern is requested
- **Event listener** — pause on click / keydown / etc.
- **Exception** — pause on caught or uncaught exceptions
- **Function** — pause whenever a function is called

### `debug(function)` — break on function call
```javascript
// In DevTools console
debug(window.fetch);                            // pauses on every fetch() call
```

## React DevTools

Browser extension (Chrome / Firefox) plus standalone for React Native.

| Tab | Use for |
|---|---|
| **Components** | Component tree, props, state, hooks, context |
| **Profiler** | Record renders; flame chart of commit times |

### Common debug moves
- Inspect a component's state and props in the tree
- "Why did this render?" — record with the profiler, click a commit, see what changed
- Highlight updates — see which components re-rendered
- Hook inspection — see `useState` / `useReducer` / `useEffect` values per component

### useState / useEffect debugging
```typescript
// Log every render with what changed
useEffect(() => {
  console.log('[MyComponent] rendered', { count, user });
}, [count, user]);

// Log every state change
useEffect(() => {
  console.log('[MyComponent] count changed', count);
}, [count]);
```

## VS Code — Universal Debug Config

```json
// .vscode/launch.json
{
  "version": "0.2.0",
  "configurations": [
    {
      "type": "node",
      "request": "launch",
      "name": "Debug TypeScript",
      "program": "${workspaceFolder}/src/main.ts",
      "preLaunchTask": "tsc: build",
      "outFiles": ["${workspaceFolder}/dist/**/*.js"]
    },
    {
      "type": "python",
      "request": "launch",
      "name": "Debug Python",
      "program": "${workspaceFolder}/main.py",
      "console": "integratedTerminal",
      "justMyCode": true
    },
    {
      "type": "go",
      "request": "launch",
      "name": "Debug Go",
      "program": "${workspaceFolder}/cmd/server"
    },
    {
      "type": "lldb",
      "request": "launch",
      "name": "Debug Rust",
      "program": "${workspaceFolder}/target/debug/app"
    },
    {
      "type": "chrome",
      "request": "launch",
      "name": "Debug Frontend",
      "url": "http://localhost:3000",
      "webRoot": "${workspaceFolder}/src"
    }
  ]
}
```

## Time-Travel Debuggers

| Tool | Language | Feature |
|---|---|---|
| **rr** | C/C++/Rust | Record + reverse-execute under gdb |
| **replay.io** | Node/browser | Record + replay with DevTools |
| **WinDbg Time Travel** | Windows native | Record + reverse step |
| **Java Flight Recorder** | JVM | Record events for later analysis |

### rr (C/C++/Rust)
```bash
rr record ./app                                # record
rr replay                                      # replay under gdb
(rr) break main.c:42
(rr) continue
(rr) reverse-continue                          # run backwards to last hit
(rr) reverse-step                              # step backwards
```

## When to Use a Debugger vs Logs

| Situation | Use |
|---|---|
| Local development, can reproduce easily | Debugger |
| Bug only in CI / production | Logs (with structured fields) |
| Heisenbug (disappears under debugger) | Logs (debugger changes timing) |
| Need to inspect many variables interactively | Debugger |
| Need a permanent record of what happened | Logs |
| Async / event-driven timing bug | Logs (debugger pauses change timing) |

## Quick Reference

| Need | Tool |
|---|---|
| Breakpoint in code | `breakpoint()` (Py), `debugger;` (JS), `runtime.Breakpoint()` (Go) |
| Print with name | `print(f"{x=}")` (Py), `console.log({x})` (JS), `dbg!(x)` (Rust), `log.Printf("%+v", x)` (Go) |
| Stack trace | `console.trace()`, `traceback.print_stack()`, `runtime.Stack()` |
| Inspect object | `console.dir(obj)`, `dir(obj)`, `inspect(obj, methods=True)` |
| Step through | IDE or CLI debugger (see above) |
| Race detection | `go test -race`, TSan (C/C++/Rust), manual audit (JS) |
| Memory leak | Chrome Memory tab, `tracemalloc`/`memray` (Py), `pprof heap` (Go), valgrind (C/Rust) |
| Time-travel | rr (C/C++/Rust), replay.io (Node) |
