# Testing and Lint

Lua has two mainstream test frameworks (`busted` and `luaunit`) and one mainstream linter (`luacheck`). For AwesomeWM, the verification gate also includes "restart the WM and read `~/.xsession-errors`."

## luacheck

[luacheck](https://github.com/mpeterv/luacheck) is the standard Lua linter. Static analysis: unused variables, undefined globals, redefined locals, shadowing, unreachable code, etc.

### Install

```bash
luarocks install luacheck
```

### Run

```bash
luacheck .                          # all .lua files in current dir, recursively
luacheck src/                       # specific dir
luacheck --config .luacheckrc .     # explicit config
luacheck --stdout-format=plain .    # one-line-per-issue format (CI-friendly)
luacheck --codes .                  # show warning codes (W211, etc.)
luacheck --ranges .                 # show byte range of each issue
luacheck -q .                       # quiet: only warnings, not OK
```

Output format:

```
Checking src/stack.lua             2 warnings

    src/stack.lua:14:1: (W211) unused variable `step`
    src/stack.lua:21:5: (W212) unused argument `self`

Total: 2 warnings / 0 errors in 2 files
```

Exit code is non-zero if any warnings — perfect for CI.

### `.luacheckrc`

```lua
-- .luacheckrc (project root)
std = "lua54"           -- or "lua51", "lua53", "luajit", "max" (everything)

cache = true             -- cache standard library definitions
max_line_length = 120

-- Global tables your code is allowed to use (no warning):
globals = {
    "vim",                  -- NeoVim
}

-- Standard library: built-in `std` covers most cases. Add AwesomeWM explicitly:
std = "+luajit"            -- base + LuaJIT extensions, OR
std = "lua54"              -- pure Lua 5.4

-- For AwesomeWM:
files["config/awesome/"] = {
    globals = {
        "awesome", "client", "screen", "tag", "mouse", "root",
        "awful", "gears", "wibox", "naughty", "beautiful",
        "menubar", "ruled", "keygrabber",
    },
    ignore = { "21" },     -- ignore "unused variable" for some rc.lua patterns
}

-- Per-file overrides:
files["spec/"] = {
    globals = { "describe", "it", "assert", "pending", "before_each", "after_each" },
}

-- Ignore specific codes globally (use sparingly):
-- ignore = { "21" }       -- unused variables
-- ignore = { "21", "21" }

-- Don't ignore warnings in tests:
files["src/"] = {
    only = false,
}

-- Allow defining globals in init.lua:
files["src/init.lua"] = {
    allow_defined = true,    -- assignments to globals are allowed
}
```

### Common warning codes

| Code | Meaning | Action |
|---|---|---|
| W001 | operator precedence confusion | add parens |
| W011 | statement has no effect | remove or fix |
| W021 | unused variable | remove or prefix with `_` |
| W022 | unused argument | remove or rename to `_` |
| W042 | empty if statement | remove or fill in |
| W111 | setting non-standard global variable | use `local`, or add to `.luacheckrc` `globals` |
| W112 | mutating non-standard global variable | same |
| W113 | accessing undefined global | add to `globals` or use `local` |
| W211 | unused variable | same as W021 |
| W212 | unused argument | same as W022 |
| W231 | loop variable is unused | rename to `_` |
| W311 | value assigned to a variable is unused | remove the assignment |

### Inline control comments

```lua
local x = compute()  -- luacheck: ignore
-- luacheck: push ignore 21
local unused = 1
-- luacheck: pop

-- luacheck: no max line length
local long_line = "a" .. string.rep("b", 200) .. "c"  -- luacheck: ignore 631
```

Use sparingly — if you have many `-- luacheck: ignore` lines, fix your code or your config.

## busted

[busted](https://lunarmodules.github.io/busted/) is the BDD-style test framework (like RSpec/Mocha). Most popular for pure-Lua modules.

### Install

```bash
luarocks install busted
```

### Write tests

```lua
-- spec/stack_spec.lua
local Stack = require("luapro.stack")

describe("Stack", function()
    local s

    before_each(function()
        s = Stack.new()
    end)

    it("starts empty", function()
        assert.are.equal(0, s:size())
    end)

    it("push and pop maintain size", function()
        s:push(1)
        s:push(2)
        assert.are.equal(2, s:size())
        assert.are.equal(2, s:pop())
        assert.are.equal(1, s:pop())
        assert.is_nil(s:pop())
    end)

    it("handles nil values", function()
        s:push(nil)
        assert.are.equal(1, s:size())    -- size increments; nil is a valid value
        assert.is_nil(s:pop())
        assert.are.equal(0, s:size())
    end)

    it("errors on bad input", function()
        assert.has_error(function()
            error("boom")
        end)
    end)
end)
```

### Run

```bash
busted                       # run all specs in spec/
busted spec/stack_spec.lua   # specific file
busted --pattern="_spec"     # custom pattern
busted -v                    # verbose (show each test)
busted --coverage            # with luacov coverage
busted --helper=spec/helper.lua   # run a setup helper first
```

### Busted API

| Function | Purpose |
|---|---|
| `describe(name, fn)` | Test group |
| `it(name, fn)` | Single test case |
| `pending(name, fn)` | Skipped test (shown but not run) |
| `before_each(fn)` | Runs before each `it` in this `describe` |
| `after_each(fn)` | Runs after each `it` |
| `setup(fn)` | Runs once before all tests in this `describe` |
| `teardown(fn)` | Runs once after all tests |
| `lazy_setup` / `lazy_teardown` | Like setup/teardown but lazy (only if tests run) |
| `finally(fn)` | Runs at end of current `it` (like a defer) |
| `assert.are.equal(a, b)` | `a == b` |
| `assert.are.same(a, b)` | deep equality (tables) |
| `assert.is_true(v)`, `assert.is_nil(v)`, `assert.is_number(v)`, ... | type-specific |
| `assert.has_error(fn, msg)` | fn raises an error |
| `assert.no_error(fn)` | fn does not raise |
| `spy.on(obj, "method")` | wrap a method for spying |
| `mock(obj)` | replace all methods with spies |

## luaunit

[luaunit](https://github.com/bluebird75/luaunit) is a single-file xUnit-style framework. Drop-in (no install), simpler than busted.

### Install

```bash
luarocks install luaunit
```

### Write tests

```lua
-- test_stack.lua
local luaunit = require("luaunit")
local Stack = require("luapro.stack")

TestStack = {}

function TestStack:setUp()
    self.s = Stack.new()
end

function TestStack:tearDown()
    self.s = nil
end

function TestStack:test_starts_empty()
    luaunit.assertEquals(self.s:size(), 0)
end

function TestStack:test_push_pop()
    self.s:push(1)
    self.s:push(2)
    luaunit.assertEquals(self.s:size(), 2)
    luaunit.assertEquals(self.s:pop(), 2)
    luaunit.assertEquals(self.s:pop(), 1)
    luaunit.assertNil(self.s:pop())
end

function TestStack:test_nil_values()
    self.s:push(nil)
    luaunit.assertEquals(self.s:size(), 1)
    luaunit.assertNil(self.s:pop())
end

os.exit(luaunit.LuaUnit.run())
```

### Run

```bash
lua test_stack.lua
lua test_stack.lua -v                # verbose
lua test_stack.lua -o TAP            # TAP output (CI-friendly)
lua test_stack.lua -p test_push      # pattern filter
lua test_stack.lua --output=junit -n test-results.xml   # JUnit XML
```

### luaunit API

| Function | Purpose |
|---|---|
| `luaunit.assertEquals(a, b)` | `a == b` |
| `luaunit.assertNotEquals(a, b)` | `a ~= b` |
| `luaunit.assertTrue(v)`, `assertFalse(v)` | boolean |
| `luaunit.assertNil(v)`, `assertNotNil(v)` | nil check |
| `luaunit.assertTableEquals(a, b)` | deep table equality |
| `luaunit.assertStrContains(str, sub)` | string contains |
| `luaunit.assertErrorMsgContains(substr, fn, ...)` | fn errors with substring |
| `luaunit.assertError(fn, ...)` | fn raises any error |

## busted vs luaunit — which?

| | busted | luaunit |
|---|---|---|
| Style | BDD (describe/it) | xUnit (test classes) |
| Install | Luarocks (multiple files) | Single file (can vendor) |
| Async | Built-in coroutines | Manual |
| Mocks/spies | Built-in | Limited |
| Output formats | TAP, JSON, JUnit | TAP, JUnit, text |
| Use when | Pure-Lua modules, BDD preference | Embedding in a binary, single-file simplicity |

For a Luarocks-published module, **busted is the modern default**. For a vendored test setup (e.g., inside a game, AwesomeWM config, or embedded device), **luaunit** is simpler.

## Luarocks test integration

A rockspec can declare tests:

```lua
-- mymod-1.0-1.rockspec
test_dependencies = {
    "busted >= 2.2",
}

test = {
    type = "command",
    script = "busted",
}
```

Then:

```bash
luarocks test            # runs busted
luarocks test mymod-1.0-1.rockspec
```

## AwesomeWM verification gate

For AwesomeWM configs, the verification is different: you cannot run `busted` on `rc.lua` directly (it has too many side effects). The gate is:

1. **Syntax check**: `awesome -c ~/.config/awesome/rc.lua --check` (does NOT execute, just parses).
2. **Restart the WM**: `Mod4 + Ctrl + r` (or `awesome -r` from a TTY).
3. **Read `~/.xsession-errors`**:
   ```bash
   tail -100 ~/.xsession-errors | grep -E "error|traceback"
   ```
   A stack trace means your edit broke something. The WM may still be running (it caught the error via the `debug::error` signal) but a widget is broken.
4. **Interactive check**: switch tags, open a client, hover widgets — verify visually that nothing's collapsed or stuttering.
5. **For animated widgets**: rapid-fire hover/focus changes (the NaN trap test). If a widget collapses to 0 or freezes, your NaN clamp is missing or wrong.

### AwesomeWM unit testing (for widget libraries, not rc.lua)

For testable widget logic, extract pure-Lua helpers and test them with busted:

```lua
-- src/helpers.lua
local M = {}
function M.clamp_pos(pos, min, max)
    if not (pos and pos == pos and pos > 0) then return min end
    return math.max(min, math.min(pos, max))
end
return M

-- spec/helpers_spec.lua
local helpers = require("src.helpers")
describe("clamp_pos", function()
    it("returns min for NaN", function()
        assert.are.equal(6, helpers.clamp_pos(0/0, 6, 40))
    end)
    it("returns min for nil", function()
        assert.are.equal(6, helpers.clamp_pos(nil, 6, 40))
    end)
    it("clamps negative to min", function()
        assert.are.equal(6, helpers.clamp_pos(-5, 6, 40))
    end)
    it("clamps over-max to max", function()
        assert.are.equal(40, helpers.clamp_pos(50, 6, 40))
    end)
end)
```

## CI setup (GitHub Actions)

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        lua_version: ["5.3", "5.4", "luajit"]
    steps:
      - uses: actions/checkout@v4
      - uses: leafo/gh-actions-lua@v10
        with:
          luaVersion: ${{ matrix.lua_version }}
      - uses: leafo/gh-actions-luarocks@v4
      - run: luarocks install luacheck
      - run: luarocks install busted
      - run: luacheck .
      - run: busted
```

## Common pitfalls

- **`assert.are.equal(a, b)` uses `==`** which is identity for tables. Use `assert.are.same(a, b)` for deep table equality.
- **`assert.has_error` requires the function to actually error**, not just return an error value. Wrap your error-returning function: `assert.has_error(function() error(my_func()) end)`.
- **busted tests run in random order by default**. Use `busted --shuffle=off` to disable if order matters (it usually shouldn't — fix your tests instead).
- **`.luacheckrc` not picked up**: luacheck looks for it in the current directory or any parent. Run from the project root, or pass `--config /path/to/.luacheckrc`.
- **Globals from busted (`describe`, `it`) flagged by luacheck**: add them to `files["spec/"].globals` in `.luacheckrc`.
- **AwesomeWM `--check` doesn't catch runtime errors**: it's parse-only. A widget that crashes on a nil `beautiful.foo` passes `--check` but breaks at restart.
- **`~/.xsession-errors` persists across sessions**: tail the END, not the beginning. Old errors from previous restarts are still there.
