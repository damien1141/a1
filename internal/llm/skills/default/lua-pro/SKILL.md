---
name: lua-pro
description: "Use when writing Lua 5.4+ or AwesomeWM rc.lua/wibox widgets. Generates idiomatic code with local discipline, metatable OOP, coroutines, Rubato animations (NaN-clamped), DPI-scaled widgets, defensive beautiful fallbacks, and verifies with luacheck + busted/luaunit + WM-restart error check before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "Lua,AwesomeWM,rc.lua,wibox,awful,gears,rubato,Luarocks,metatables,coroutines,naughty,beautiful,bling,LuaJIT,luaunit,busted,luacheck"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "rigorous-coding"
---

# Lua Pro

Lua 5.4+ and AwesomeWM. Tables are the only data structure, `local` is a discipline, metatables are how OOP works, and coroutines are how async works. AwesomeWM runs on a single thread — bad UI code locks the WM, so animations are NaN-clamped, hot loops cache `get_children_by_id`, and `collectgarbage("collect")` is forbidden in callbacks. Honest exit separates **VERIFIED** (`luacheck .` clean, test suite green, WM restarted with no stack trace in `~/.xsession-errors`) from **ASSUMED**.

## When to Use

- Writing Lua 5.4+ scripts, modules, or libraries (NeoVim plugins, Lapis web, Redis scripts, game logic)
- Using Lua 5.4 features: integer division `//`, bitwise ops, `goto`/labels, generalized `for`
- Implementing OOP via metatables, custom iterators via coroutines, or async via `coroutine.yield`/`resume`
- Authoring or editing AwesomeWM `rc.lua` — tags, layouts, rules, signals, widgets, keybindings
- Building AwesomeWM widgets with `wibox`, `awful.widget.taglist`/`tasklist`, Rubato animations
- Packaging a Lua module via Luarocks; writing busted/luaunit tests; configuring `.luacheckrc`

Skip for: pure configuration files in a different language, or Lua 5.1/LuaJIT-only environments where 5.4 features (integer division, `goto`) are unavailable — flag the version constraint.

## Operating Loop

1. **Scope** — Name the artifact (script, module, `rc.lua` section, widget). State the Lua version (`lua -v` must show 5.4+ unless LuaJIT/5.1 is the explicit target). State the load-bearing unknown: "is this AwesomeWM or generic Lua?" (shared syntax, different APIs and traps).
2. **Recon** — Read module structure, `rockspec`, `.luacheckrc`, `.busted`. For AwesomeWM: `tail -50 ~/.xsession-errors`, read `~/.config/awesome/rc.lua`, check `beautiful` theme path, confirm Rubato is installed (`luarocks list | grep rubato`).
3. **Design tables first** — Lua data = tables. Decide: array part or hash part? Pre-allocate if size known. Define `local M = {}` and its exports. For OOP: pick the metatable pattern (single class, inheritance chain, or prototype via `__index`).
4. **Implement** — `local` everything. Use `:` for methods, `.` for plain functions. Use `pcall`/`xpcall` around external calls. For AwesomeWM widgets: follow the widget template pattern (`references/awesomewm-widgets.md`), NaN-clamp Rubato subscriptions, cache `get_children_by_id` in `create_callback`, never `collectgarbage("collect")` in callbacks.
5. **Verify (gate)** — In order, until clean:
   - `lua -v` → 5.4+ (or LuaJIT if explicit target)
   - `luacheck .` → 0 warnings
   - `busted` (or `lua -l luaunit tests/*.lua`) → all pass
   - For AwesomeWM: restart WM (`Mod+Ctrl+r` or `awesome -r`) + `tail -50 ~/.xsession-errors` → no stack trace
   - For Luarocks packages: `luarocks make` clean, `luarocks test` passes
   - If any step fails: fix the cause. No `--ignore` luacheck globals without a written reason.
6. **Exit** — Report. **VERIFIED**: list each command + result. **ASSUMED**: list what you believe but did not check (e.g. "smooth on HiDPI" — visual, not verified; "works under LuaJIT" — not tested if you ran PUC Lua 5.4). Flag AwesomeWM-specific risks (a NaN can collapse a widget on rapid hover).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Lua 5.4 language essentials: types, `local`, tables, `ipairs`/`pairs`, varargs, multiple returns, tail calls, `goto`/labels, integer division `//`, bitwise ops | `references/language-essentials.md` | Writing Lua syntax, choosing iteration, using 5.4 numeric/bitwise features |
| Metatables, metamethods, OOP via `__index`, classes, inheritance, `:` vs `.`, operator overloading | `references/metatables-oop.md` | Building classes, prototypes, custom operators, implementing inheritance |
| Coroutines, `coroutine.create`/`resume`/`yield`, generators, cooperative multitasking, `pcall`/`xpcall` error handling | `references/coroutines-and-async.md` | Async patterns, generators, iterators, error isolation |
| Modules, `package.path`, `require` semantics, lazy loading, `local M = {}` pattern, Luarocks, rockspecs, C extensions | `references/modules-and-luarocks.md` | Building a module, packaging via Luarocks, writing a rockspec, C FFI extensions |
| AwesomeWM core: rc.lua structure, tags/layouts/rules, signals, awful/gears/naughty/bling, beautiful theming with fallbacks, DPI scaling, 8-digit hex colors | `references/awesomewm-core.md` | Editing rc.lua, configuring tags/layouts/rules, theming, DPI |
| AwesomeWM widgets: wibox, widget template pattern, `create_callback`/`update_callback`, caching `get_children_by_id`, fixed wrappers for dynamic children | `references/awesomewm-widgets.md` | Building taglist/tasklist widgets, animated widgets, hot-path performance |
| Rubato animations: pos+target init, the NaN trap (`pos == pos`), no timers for transitions, destroy timers on widget death, asymmetric timings | `references/awesomewm-animations.md` | Animating widget size/opacity, fixing collapsed widgets, Rubato subscription safety |
| Testing & lint: busted, luaunit, `.luacheckrc` config, Luarocks test framework, AwesomeWM restart verification | `references/testing-and-lint.md` | Writing tests, configuring luacheck, setting up CI for Lua |

## Constraints

### MUST DO
- **`local` everything.** Globals leak across modules, break sandboxing, and silently override each other. Use `local function`, `local M = {}`, `local x = ...`.
- **Cache globals in hot loops.** `local pairs, ipairs, table_insert = pairs, ipairs, table.insert` at the top of hot files — saves a global lookup per call.
- **Use `:` for method calls, `.` for plain functions.** `obj:method()` passes `obj` as implicit `self`; `obj.method()` does not. Mixing them up is the #1 Lua bug.
- **NaN-clamp Rubato subscriptions** in AwesomeWM: `if pos and pos == pos and pos > 0 then ... end`. `math.max(10, NaN)` returns `NaN` in Lua.
- **Initialize Rubato with both `pos` AND `target`** set to the same initial value. If only `pos`, the default `target` is `0` and the widget animates to 0 and disappears.
- **Cache `get_children_by_id` in `create_callback`** as `self._bg = self:get_children_by_id("background_role")[1]`, then use `self._bg` in `update_callback`. `get_children_by_id` is expensive in hot loops.
- **Defensive `beautiful` fallbacks**: `beautiful.accent or "#8AB4F8"`. If a theme fails to load, widgets crash the WM otherwise.
- **Scale with `dpi()`** for all pixel sizes in AwesomeWM. Never hardcode pixel values (breaks on HiDPI).
- **8-digit hex colors** `#RRGGBBAA` for opacity in AwesomeWM — saves CPU vs animating opacity directly.
- **`pcall`/`xpcall` around external calls** (file I/O, `require` of untrusted module, network). Lua errors propagate by longjmp; unprotected calls abort the calling coroutine/WM.
- **`luacheck .` clean** before commit. Configure AwesomeWM globals in `.luacheckrc` (`awful`, `gears`, `wibox`, `naughty`, `beautiful`, `screen`, `client`, `tag`, `mouse`, `root`).
- **Honest exit**: VERIFIED (ran `luacheck`, ran tests, restarted WM with no error) vs ASSUMED (visual smoothness, cross-Lua-version compat).

### MUST NOT DO
- **No globals.** `function foo()` (no `local`) creates `_G.foo` and pollutes every module. Always `local function foo()`.
- **No `collectgarbage("collect")` in AwesomeWM callbacks** (`update_callback`, `create_callback`, signal handlers). Forces a global GC pause → severe stutter on hover/focus.
- **No `gears.timer` to delay UI state changes** in AwesomeWM. Set `rubato.target` and let easing handle the transition natively.
- **No `math.max`/`math.min` to filter NaN** in Rubato subscriptions. Use `pos == pos` (NaN is not equal to itself in Lua).
- **No animating `margins` around text/icon fonts** in AwesomeWM. Distorts glyph or wraps it. Animate opacity or background colors instead.
- **No deep `get_children_by_id` in `update_callback`.** Cache in `create_callback`.
- **No `module()` function** (deprecated since Lua 5.2). Use `local M = {}` ... `return M`.
- **No assuming Lua 5.4 features under LuaJIT.** LuaJIT is 5.1 + some 5.2/5.3 extensions; integer/float distinction differs. Test under LuaJIT if targeting it (NeoVim, OpenResty, LÖVE).
- **No silent `nil` indexing.** `t.x.y` where `t.x` is `nil` → "attempt to index a nil value". Validate with `t.x and t.x.y`.
- **No claiming "works in AwesomeWM" without restarting the WM** and checking `~/.xsession-errors` for stack traces.

## Code Examples

### Module with metatable OOP (Lua 5.4)
```lua
-- luapro/stack.lua
local M = {}
local Stack = {}
Stack.__index = Stack

function M.new(n)
    return setmetatable({ items = table.new(n or 0, 0), n = 0 }, Stack)
end

function Stack:push(v)
    self.n = self.n + 1
    self.items[self.n] = v
end

function Stack:pop()
    if self.n == 0 then return nil end
    local v = self.items[self.n]
    self.items[self.n] = nil            -- release reference
    self.n = self.n - 1
    return v
end

function Stack:size()
    return self.n
end

return M
-- Usage: local Stack = require("luapro.stack"); local s = Stack.new(); s:push(42)
```

### Coroutine as a generator (Lua 5.4)
```lua
local function range(start, stop, step)
    step = step or 1
    return coroutine.wrap(function()
        for i = start, stop > start and stop or start, step do
            coroutine.yield(i)
        end
    end)
end

for v in range(1, 10) do
    print(v)                            -- 1 2 3 ... 10
end
```

### pcall around external call (Lua 5.4)
```lua
local ok, mod = pcall(require, "optional.dependency")
if not ok then
    mod = nil                            -- or a fallback module
end

local function safe_read(path)
    local f, err = io.open(path, "r")
    if not f then return nil, err end
    local content = f:read("*a")
    f:close()
    return content
end
```

### AwesomeWM widget template with Rubato (NaN-clamped)
See `references/awesomewm-widgets.md` for the full pattern. The load-bearing shape:
```lua
local widget_template = {
    { id = "background_role", widget = wibox.container.background },
    widget = wibox.container.place,                 -- FIXED WRAPPER for animated child
    forced_height = dpi(40), forced_width = dpi(10),
    create_callback = function(self, c3, index, objects)
        local bg = self:get_children_by_id("background_role")[1]
        if not bg then return end
        self._bg = bg                              -- CACHE once; use in update_callback
        local h = dpi(8)
        bg.forced_height = h
        self._anim = rubato.timed{                 -- pos AND target BOTH init
            pos = h, target = h,
            subscribed = function(pos)
                -- NaN CLAMP: pos == pos is false for NaN; math.max doesn't filter NaN
                if pos and pos == pos and pos > 0 then
                    bg.forced_height = math.max(dpi(6), math.min(pos, dpi(40)))
                end
            end,
        }
    end,
    update_callback = function(self, c3, index, objects)
        if not (self._bg and self._anim) then return end
        self._bg.bg = c3.selected and (beautiful.accent or "#8AB4F8") or "#00000000"
        self._anim.target = c3.selected and dpi(32) or dpi(8)   -- NO timer; easing handles it
    end,
}
```

### `.luacheckrc` for AwesomeWM
```lua
-- .luacheckrc
std = "lua54"
cache = true
globals = { "awesome", "client", "screen", "tag", "mouse", "root",
    "awful", "gears", "wibox", "naughty", "beautiful", "bling", "rubato", "dpi" }
max_line_length = 120
```

## Output Template

1. **Lua version & environment** — PUC Lua 5.4.x / LuaJIT / AwesomeWM + version
2. **Files** — module path, `rc.lua` section, `.luacheckrc` deltas, rockspec
3. **Verification block**:
   ```
   $ lua -v
   Lua 5.4.6
   $ luacheck .
   Total: 0 warnings / 0 errors in 3 files
   $ busted
   3 successes / 0 failures / 0 errors / 0 pending : 0.02 seconds
   ```
4. **For AwesomeWM**: `awesome -r` (restart) + `tail -50 ~/.xsession-errors` → no stack trace
5. **Exit report** — VERIFIED / ASSUMED / lingering risks (e.g. "NaN-clamp tested by rapid tag switching; visual smoothness on HiDPI NOT verified")

## Knowledge Reference

Lua 5.4 · `local` discipline · tables (array + hash) · `ipairs`/`pairs`/`next` · 1-indexed · multiple returns · varargs · tail calls · integer division `//` · bitwise ops · `goto`/labels · metatables · `__index`/`__newindex`/`__call` · OOP via metatables · `:` vs `.` · coroutines · `pcall`/`xpcall` · `local M = {}` · `require` · `package.path` · Luarocks · rockspec · `table.new`/`table.move` · AwesomeWM · rc.lua · tags/layouts/rules · signals · `awful`/`gears`/`wibox`/`naughty`/`bling` · `beautiful` · DPI · Rubato · NaN trap · `get_children_by_id` caching · `create_callback`/`update_callback` · `#RRGGBBAA` · `collectgarbage` (never in callbacks) · busted · luaunit · luacheck
