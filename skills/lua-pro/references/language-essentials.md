# Lua 5.4 Language Essentials

Lua is small: 8 types, one data structure (the table), one compilation unit (the chunk), one error mechanism (`error`/`pcall`). Mastery is in the idioms, not the surface area.

## Types

Lua has 8 types: `nil`, `boolean`, `number` (Lua 5.4: integer subtype + float subtype), `string`, `table`, `function`, `userdata`, `thread` (coroutine).

- `nil` is the absence of value. `nil` is falsy. Assigning `nil` to a table key deletes it.
- **Only `false` and `nil` are falsy.** `0`, `""`, `{}` are all truthy. This is the #1 surprise for programmers coming from Python/JS.
- Lua 5.4 introduced an integer subtype. `3` is an integer; `3.0` is a float. `3 == 3.0` is `true` (numeric equality), but `math.type(3) == "integer"` and `math.type(3.0) == "float"`.

## Numbers and integer division

```lua
print(7 / 2)           -- 3.5      (always float division)
print(7 // 2)          -- 3        (integer division, Lua 5.3+)
print(7 % 2)           -- 1        (modulo)
print(2^53)            -- 9.0072e+15 (float exponentiation; no integer ^)
print(2 // 0)          -- error: division by zero (Lua 5.4 raises, earlier returned -inf)
print(math.tointeger(3.0))   -- 3   (converts float to integer if exact)
print(math.maxinteger)       -- 9223372036854775807
print(math.mininteger)       -- -9223372036854775808
```

Lua 5.4 changed integer overflow to wrap (like C unsigned), not convert to float. `math.maxinteger + 1 == math.mininteger`.

## Bitwise operators (Lua 5.3+)

Lua 5.3+ added bitwise operators as syntax (5.2 had `bit32` library only):

```lua
print(0xFF & 0x0F)     -- 15    AND
print(0xF0 | 0x0F)     -- 255   OR
print(0xFF ~ 0x0F)     -- 240   XOR (also unary ~ for NOT: ~0xFF == -256)
print(1 << 4)          -- 16    left shift
print(256 >> 4)        -- 16    right shift (logical, on integers)
```

All operands are converted to integers. Operates on 64-bit integers.

## Strings

- Immutable byte sequences. Length is bytes, not codepoints.
- `string.format` (C printf-like), `string.gsub`, `string.find`, `string.match`, `string.gmatch` (iterator).
- Interned: identical string literals share one copy. Comparing strings is O(1).
- Concatenation `..` is O(n) where n is the result length. Building a big string with `..` in a loop is O(n²) — use `table.concat` instead.

```lua
-- O(n²) — DON'T do this for big strings
local s = ""
for i = 1, 10000 do s = s .. tostring(i) .. "," end

-- O(n) — DO this
local parts = {}
for i = 1, 10000 do parts[i] = tostring(i) end
local s = table.concat(parts, ",")
```

## Tables — the only data structure

A table has an array part and a hash part. `#t` (length operator) returns the array length — defined only for sequences (1..n with no holes).

```lua
local t = { 10, 20, 30 }     -- array part: t[1]=10, t[2]=20, t[3]=30
print(#t)                    -- 3
print(t[0])                  -- nil (Lua arrays are 1-indexed)

local config = { host = "localhost", port = 8080 }  -- hash part
print(config["host"], config.port)                  -- "localhost" "localhost"

-- Mixed:
local mixed = { "first", "second", name = "alice", [100] = "far" }
print(#mixed)                -- 2 (length stops at first nil)
print(mixed[100])            -- "far"
```

### Iteration

```lua
-- Array iteration (1..n, stops at first nil):
for i, v in ipairs(t) do print(i, v) end

-- All keys (no order guarantee, includes non-integer keys):
for k, v in pairs(t) do print(k, v) end

-- Manual iteration with next (rare; pairs is built on this):
local k, v = next(t)
while k ~= nil do
    print(k, v)
    k, v = next(t, k)
end

-- Numeric for (fastest, no iterator overhead):
for i = 1, #t do print(i, t[i]) end
```

### Pre-allocation and `table.move` (Lua 5.3+)

```lua
-- Pre-allocate (LuaJIT/Lua 5.3+ via table.new):
local ok, new = pcall(require, "table.new")
local t = ok and new(0, 100) or {}  -- 0 array, 100 hash slots

-- table.move copies a slice:
local src = { 10, 20, 30, 40, 50 }
local dst = {}
table.move(src, 2, 4, 1, dst)   -- dst = { 20, 30, 40 }

-- table.pack/unpack (Lua 5.2+):
local args = table.pack(1, 2, 3)   -- args = { 1, 2, 3, n = 3 }
local function call(f, ...) f(table.unpack(args, 1, args.n)) end
```

### 1-indexed arrays — gotchas

- `t[0]` is valid but breaks `ipairs` and `#t`. Don't use index 0.
- "Holes" (nil in the middle) make `#t` undefined — it returns any boundary.
- C interop: Lua arrays start at 1; C arrays start at 0. Translate at the FFI boundary.

## `local` discipline

```lua
-- GOOD: locals everywhere
local function foo()
    local x = 1
    local function bar() return x + 1 end
    return bar()
end

-- BAD: globals leak
function foo()              -- creates _G.foo
    x = 1                   -- creates _G.x
    function bar() return x + 1 end   -- creates _G.bar
end
```

Why locals matter:

- Performance: local access is a register/array index; global access is a hash lookup on `_G`.
- Sandbox safety: globals are shared across all modules in the same Lua state.
- No accidental shadowing of stdlib names (`print`, `pairs`, `error`).

### Cache globals in hot loops

```lua
-- At the top of a hot file:
local pairs, ipairs, tostring, table_concat = pairs, ipairs, tostring, table.concat

-- Then use the locals:
for k, v in pairs(t) do ... end
```

## Functions: multiple returns, varargs, tail calls

```lua
-- Multiple returns:
local function minmax(t)
    local lo, hi = math.huge, -math.huge
    for _, v in ipairs(t) do
        if v < lo then lo = v end
        if v > hi then hi = v end
    end
    return lo, hi          -- caller: local lo, hi = minmax(t)
end

-- Varargs:
local function printf(fmt, ...)
    io.write(string.format(fmt, ...))
end

-- table.pack to capture varargs as a table (Lua 5.2+):
local function sum(...)
    local args = table.pack(...)
    local total = 0
    for i = 1, args.n do total = total + args[i] end
    return total
end

-- Select:
local function first_two(...)
    return select(1, ...), select(2, ...)   -- or: return (...), (select(2, ...))
end
-- select("#", ...) returns the count

-- Tail call (Lua optimizes to a jump, no stack growth):
local function loop(n)
    if n <= 0 then return "done" end
    return loop(n - 1)    -- tail call: no stack frame added
end
```

A tail call must be the LAST operation. `return f() + 1` is NOT a tail call (the `+ 1` runs after `f` returns).

## `goto` and labels (Lua 5.2+)

```lua
for i = 1, 10 do
    for j = 1, 10 do
        if i + j > 15 then goto continue end   -- skip rest of inner loop body
        -- ... work
        ::continue::
    end
end

-- goto to break out of nested loops:
for i = 1, 10 do
    for j = 1, 10 do
        if found(i, j) then goto done end
    end
end
::done::
```

Constraints: `goto` cannot jump into the scope of a local; cannot jump out of a function. Use sparingly — `break` + flags are usually clearer.

## Generalized `for` (Lua 5.4)

Lua 5.4 added the `for v in t do ... end` form (no iterator function), which iterates `t` via `__iter` metamethod or defaults to `pairs`. Useful for custom containers.

## Truthy/falsy and `and`/`or` idioms

```lua
local x = maybe_nil or "default"       -- default value idiom
local y = cond and "yes" or "no"       -- ternary idiom (CAREFUL if "yes" is false/nil)
-- Safe ternary:
local z = (cond and { "yes" } or { "no" })[1]

-- Boolean conversion:
local bool = not not x                 -- true if x is truthy
```

## Error handling

```lua
-- error(message, level) — raises an error with a stack trace
error("something went wrong")           -- default level 1: error at error() call site
error("from caller", 2)                 -- level 2: blame the caller

-- assert(value, message) — errors if value is nil/false
local f = assert(io.open(path, "r"))    -- idiomatic: error with file message

-- pcall: protected call, returns (ok, result1, result2, ...)
local ok, err = pcall(function()
    return risky_operation()
end)
if not ok then
    log("failed: " .. tostring(err))
end

-- xpcall: with a custom error handler (for stack traces)
local ok, err = xpcall(risky_function, debug.traceback)
```

Lua errors propagate via longjmp. Any unprotected call that errors will unwind the stack to the nearest `pcall`. **In a coroutine, an unhandled error kills the coroutine, not the whole state.**

## Truthy table gotchas

```lua
-- Empty table is truthy:
if {} then print("yes") end          -- "yes"

-- nil indexing is an error:
local t = {}
print(t.x.y)                         -- error: attempt to index a nil value (field 'x')
print(t.x and t.x.y)                 -- nil, no error

-- Default value chain:
local port = config and config.server and config.server.port or 8080
```

## Common pitfalls

- **`#t` on a table with holes**: undefined behavior. Use a `n` field or `table.pack`.
- **Modifying a table during `pairs` iteration**: undefined behavior. Collect keys to delete, delete after.
- **`..` with non-string/non-number**: errors. Wrap with `tostring`.
- **Comparing tables with `==`**: identity comparison, not value. Use a deep-equal helper.
- **Floating-point keys**: `t[1.0]` and `t[1]` are the SAME key (numeric coercion). `t["1"]` is different.
- **`nil` in the middle of an array**: breaks `ipairs` and `#t`. Use a sentinel (`false`) or re-pack.
