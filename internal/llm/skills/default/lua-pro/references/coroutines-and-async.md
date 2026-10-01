# Coroutines and Async

Coroutines are Lua's cooperative concurrency: one thread at a time, switched by explicit `yield`/`resume`. They're the foundation for generators, iterators, async I/O patterns, and Lua's `pcall`-style error isolation across non-trivial control flow.

## What a coroutine is

A coroutine is a `thread` type (NOT an OS thread). It has its own stack and instruction pointer but shares the heap and globals with the main thread. It runs only when you `resume` it; it pauses when it `yield`s.

```lua
local co = coroutine.create(function(a, b)
    print("first resume:", a, b)        -- 1, 2
    local c, d = coroutine.yield(a + b) -- pauses here; resumed with (3, 4)
    print("second resume:", c, d)       -- 3, 4
    coroutine.yield("done")
end)

local ok, first = coroutine.resume(co, 1, 2)   -- prints "first resume: 1 2"
print(first)                                    -- 3 (yield returned a+b)
local ok, second = coroutine.resume(co, 3, 4)   -- prints "second resume: 3 4"
print(second)                                   -- "done"
print(coroutine.status(co))                     -- "suspended" (yielded) or "dead"
```

## The coroutine API

| Function | Description |
|---|---|
| `coroutine.create(f)` | Returns a new coroutine (thread). `f` is the body. |
| `coroutine.resume(co, ...)` | Starts or resumes. Returns `ok, ...` (yielded values or error). |
| `coroutine.yield(...)` | Pauses the coroutine; values are returned to `resume`'s caller. |
| `coroutine.status(co)` | `"suspended"`, `"running"`, `"normal"`, `"dead"`. |
| `coroutine.wrap(f)` | Returns a function that resumes the coroutine. Simpler API, but errors propagate (no `ok` flag). |
| `coroutine.isyieldable()` | True if the current coroutine can yield. |
| `coroutine.running()` | The running coroutine (or nil on main thread). |

## `coroutine.wrap` vs `coroutine.create`

```lua
-- wrap: returns a function; no explicit resume; errors propagate
local iter = coroutine.wrap(function()
    for i = 1, 3 do coroutine.yield(i) end
end)
print(iter())   -- 1
print(iter())   -- 2
print(iter())   -- 3
print(iter())   -- raises an error (coroutine is dead)

-- create: explicit resume; returns (ok, value); errors are caught
local co = coroutine.create(function()
    error("kaboom")
end)
local ok, err = coroutine.resume(co)
print(ok, err)  -- false, "...kaboom..."
```

**Rule**: `wrap` for iterators/generators where errors should propagate; `create` for state machines where you want to handle errors per-resume.

## Generators (the common use)

```lua
local function range(start, stop, step)
    step = step or 1
    return coroutine.wrap(function()
        local i = start
        if step > 0 then
            while i <= stop do coroutine.yield(i); i = i + step end
        else
            while i >= stop do coroutine.yield(i); i = i + step end
        end
    end)
end

for x in range(1, 10, 2) do print(x) end    -- 1 3 5 7 9
```

This is the idiomatic Lua generator pattern. The `for ... in` loop calls the wrapped function until it returns nil (or raises).

## Custom iterators with state

```lua
-- A coroutine-based iterator over a tree, depth-first:
local function tree_dfs(root)
    return coroutine.wrap(function()
        local function walk(node)
            if not node then return end
            walk(node.left)
            coroutine.yield(node.value)
            walk(node.right)
        end
        walk(root)
    end)
end

for v in tree_dfs(root) do print(v) end
```

Without coroutines, this would require an explicit stack. With them, the recursion's natural call stack is reused.

## Cooperative multitasking (the producer/consumer)

```lua
local function producer()
    for i = 1, 5 do
        coroutine.yield("item-" .. i)
    end
end

local function consumer(prod_co)
    while coroutine.status(prod_co) ~= "dead" do
        local ok, item = coroutine.resume(prod_co)
        if ok and item then
            print("consumed:", item)
        end
    end
end

consumer(coroutine.create(producer))
-- consumed: item-1
-- consumed: item-2
-- ...
```

For a real scheduler, see [LuaLane](https://github.com/luislavena/lanes) or [Copas](https://github.com/lunarmodules/copas). Standard Lua coroutines are not preemptive and do not run in parallel.

## Error handling in coroutines

```lua
local co = coroutine.create(function()
    error("inside coroutine")     -- raises inside the coroutine
end)

local ok, err = coroutine.resume(co)
if not ok then
    print("coroutine failed:", err)   -- "stdin:X: inside coroutine"
end
print(coroutine.status(co))            -- "dead"
```

**An error inside a coroutine does NOT propagate to the main thread** unless you use `coroutine.wrap` (which propagates). `coroutine.resume` catches the error and returns `false, err`.

This is the safe pattern for untrusted or risky code:

```lua
local function safe_call(f, ...)
    local co = coroutine.create(f)
    local ok, result = coroutine.resume(co, ...)
    if not ok then
        return nil, "error: " .. tostring(result)
    end
    return result
end
```

(Equivalent to `pcall(f, ...)` — but the coroutine version lets you yield across the call boundary, which `pcall` does not.)

## `pcall` and `xpcall` (the non-coroutine error isolation)

```lua
-- pcall: protected call, returns (ok, ...)
local ok, result = pcall(function()
    return risky()
end)
if not ok then
    log("failed: " .. tostring(result))
end

-- pcall with arguments:
local ok, result = pcall(io.open, "/etc/passwd", "r")

-- xpcall: with a custom error handler (gets the error before stack unwinds)
local ok, err = xpcall(function()
    error("boom")
end, function(e)
    return debug.traceback("handled: " .. tostring(e), 2)
end)
print(err)   -- "handled: stdin:3: boom\nstack traceback:..."
```

`pcall` cannot be paused (no `coroutine.yield` inside a `pcall` in Lua 5.1 — fixed in 5.2+, but Luarocks/NeoVim sometimes use 5.1 semantics). `xpcall` is the same but lets you build a custom traceback before the stack unwinds.

## Async I/O patterns

Lua has no built-in async I/O. Patterns:

1. **Couper with a C library**: copas, lua-async, or a C-based event loop (libuv binding). The library calls `coroutine.resume` when I/O completes; you `coroutine.yield` before blocking I/O.

2. **Lapis (OpenResty/LuaJIT)**: uses ngx.phases and coroutines; each HTTP request runs in its own coroutine, yielding to the nginx event loop.

3. **LuaSocket + copas**: copas wraps sockets so blocking calls yield; a scheduler resumes them.

```lua
-- copas pattern:
local copas = require("copas")
local socket = require("socket")

local server = socket.bind("127.0.0.1", 8080)
copas.addserver(server, function(conn)
    conn:settimeout(0)
    local line = conn:receive("*l")    -- yields internally; other coroutines run
    conn:send("echo: " .. line .. "\n")
    conn:close()
end)

copas.loop()                           -- runs the scheduler forever
```

## Tail calls and stack discipline

Lua optimizes tail calls (a `return f(...)` that is the last operation) to a jump — no stack growth. This enables recursive patterns that would otherwise overflow:

```lua
-- State machine via tail calls (infinite, no stack overflow):
local function state_a()
    -- do work
    return state_b()       -- tail call
end

local function state_b()
    -- do work
    if done() then return end
    return state_a()       -- tail call
end
```

A tail call must be `return f(args)` with no further operations. `return f() + 1` is NOT a tail call (the `+ 1` runs after `f` returns).

## Pitfalls

- **`coroutine.resume` catches errors silently.** Always check the first return value (`ok`). If you forget and the coroutine errored, you'll get `nil` results and scratch your head.
- **`coroutine.wrap` propagates errors.** If the body errors, the next call raises. Wrap calls in `pcall` if you need to handle it.
- **You can't yield across a C boundary** in some Lua versions. Specifically: yielding inside a C function called from Lua works in Lua 5.2+ but may not in LuaJIT or older Lua 5.1.
- **Coroutines are not threads.** Only one runs at a time. CPU-bound work in one coroutine blocks all others. For parallel CPU work, use LuaLane (multiple OS processes) or a C extension.
- **`coroutine.yield` outside a coroutine is an error.** Use `coroutine.isyieldable()` to check.
- **A dead coroutine can't be resumed.** Resuming returns `false, "cannot resume dead coroutine"`. Check `coroutine.status` first.
- **Coroutines share global state.** No isolation between coroutines — they're all in one Lua state. Use locals and explicit table-passing for "encapsulation".
