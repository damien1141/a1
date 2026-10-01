# Modules and Luarocks

Lua modules are tables returned from a chunk loaded via `require`. There's no `class`, no `module` keyword (deprecated since 5.2). The pattern is uniform: `local M = {}` ... `return M`. Luarocks is the package manager.

## `require` semantics

`require("foo.bar")` does:

1. Checks `package.loaded["foo.bar"]` — if not `nil`, returns it (cached).
2. Otherwise, searches `package.path` for `foo/bar.lua` (or `.so`/`.dll` for C modules) and `package.cpath` for `foo/bar.so`.
3. Runs the chunk. Whatever it returns is stored in `package.loaded["foo.bar"]` and returned.
4. If the chunk returns `nil` or `true`, `package.loaded` is set to `true` (idiomatically: a module that sets globals instead of returning a table).

```lua
-- foo/bar.lua
local M = {}
function M.greet(name) return "hello, " .. name end
return M
```

```lua
-- consumer
local bar = require("foo.bar")
print(bar.greet("world"))    -- "hello, world"
```

`require` is idempotent: the second call returns the cached table without re-running the chunk. This is the primary difference from `dofile` (always re-runs) and `loadfile` (compiles but doesn't run).

## `package.path` and `package.cpath`

```lua
-- Default package.path on Linux Lua 5.4 build:
-- ./?.lua
-- ./?/init.lua
-- /usr/local/share/lua/5.4/?.lua
-- /usr/local/share/lua/5.4/?/init.lua
-- /usr/local/lib/lua/5.4/?.lua
-- /usr/local/lib/lua/5.4/?/init.lua

-- Modify at runtime:
package.path = package.path .. ";./lib/?.lua;./lib/?/init.lua"
package.cpath = package.cpath .. ";./lib/?.so"

-- Or via env var:
-- LUA_PATH="./?.lua;./?/init.lua;;"  ;; means "append the default"
-- LUA_CPATH="./?.so;;"
```

The `?` placeholder is the module name with `.` replaced by `/`. `require("foo.bar")` searches for `foo/bar.lua` then `foo/bar/init.lua`.

## The modern module pattern

```lua
-- mymod.lua
local mymod = {}

-- Local dependencies at top:
local io_open = io.open
local string_format = string.format

-- Local state (private):
local instances = {}

-- Constructor / public API:
function mymod.new(name)
    local self = setmetatable({}, { __index = mymod })
    self.name = name
    instances[#instances + 1] = self
    return self
end

function mymod:greet()
    return string_format("hi, %s", self.name)
end

-- Module-level (static) function:
function mymod.count()
    return #instances
end

return mymod
```

## The deprecated `module()` function

Lua 5.1 had a `module("mymod")` call that auto-created and set `_G.mymod`. **Deprecated since 5.2, removed in 5.4.** Don't use it. The `local M = {}` pattern above replaces it cleanly and works in every Lua version.

If you encounter old code:

```lua
-- OLD (5.1):
module("mymod", package.seeall)
function greet() ... end      -- becomes mymod.greet

-- MODERN:
local M = {}
function M.greet() ... end
return M
```

## Lazy loading

```lua
local M = setmetatable({}, {
    __index = function(t, k)
        local mod = require("mymod." .. k)
        t[k] = mod             -- cache for next access
        return mod
    end,
})

-- First access loads mymod.foo:
M.foo.do_something()
-- Second access uses cached value:
M.foo.do_other()
```

Use for: large module trees where only some submodules are needed per run; plugin systems.

## Requiring a specific Lua version

```lua
local _Lua_version = _VERSION:match("Lua 5%.(%d)")
local major = tonumber(_Lua_version)
if major < 3 then
    error("this module requires Lua 5.3+")
end
```

Or check for specific features:

```lua
-- Integer division (5.3+):
local has_idiv = (1 // 1) == 1

-- Bitwise ops (5.3+):
local has_bnot = pcall(function() return ~0 end)
```

## Luarocks

[Luarocks](https://luarocks.org/) is the Lua package manager. Install: `apt install luarocks` / `brew install luarocks` / from source.

### Installing rocks

```bash
luarocks install luaunit
luarocks install busted
luarocks install luacheck
luarocks install --local penlight     # install to ~/.luarocks (no sudo)
luarocks install --server=https://luarocks.org/dev foo    # from dev manifest
```

### Searching

```bash
luarocks search http
luarocks search --all http
```

### Listing installed rocks

```bash
luarocks list
luarocks list --tree=system
luarocks show luaunit
```

## Writing a rockspec

A rockspec is a Lua file describing how to build and package your module.

```lua
-- mymod-scm-1.rockspec
package = "mymod"
version = "scm-1"           -- scm = git HEAD; or "1.0.0-1"

source = {
    url = "git+https://github.com/you/mymod.git",
    tag = "v1.0.0",         -- omit for scm; or use branch = "main"
}

description = {
    summary = "A short one-line description.",
    detailed = [[
        A longer multi-line description.
        Can span multiple lines.
    ]],
    homepage = "https://github.com/you/mymod",
    license = "MIT",
    maintainer = "Your Name <you@example.com>",
}

dependencies = {
    "lua >= 5.3, < 5.5",    -- Lua version constraint
    "luaunit >= 3.4",       -- runtime dependency
}

build = {
    type = "builtin",
    modules = {
        ["mymod"] = "src/mymod.lua",
        ["mymod.util"] = "src/mymod/util.lua",
        ["mymod.sub"] = "src/mymod/sub.lua",
    },
    install = {
        conf = { ["mymod.cfg"] = "config/cfg.lua" },   -- config files
    },
    copy_directories = { "docs", "tests" },
}
```

### Building and testing a rock

```bash
# In the repo root (with the rockspec):
luarocks lint mymod-scm-1.rockspec        # check rockspec syntax
luarocks make                              # build + install locally
luarocks test                              # run tests (uses .busted or test.lua)
luarocks pack mymod-scm-1.rockspec         # produce .src.rock file
luarocks upload mymod-1.0.0-1.src.rock     # publish (needs API key)
```

### Local development with Luarocks

```bash
# Make a local rock available without installing it system-wide:
luarocks make --local                       # installs to ~/.luarocks
# Ensure ~/.luarocks is on LUA_PATH:
eval $(luarocks path --bin)
```

## C extensions

Lua C modules expose functions via the `luaL_Reg` array and a `luaopen_<modname>` entry point.

```c
/* mycmod.c — a C module for Lua 5.4 */
#include <lua.h>
#include <lauxlib.h>

static int mycmod_add(lua_State *L) {
    lua_Integer a = luaL_checkinteger(L, 1);
    lua_Integer b = luaL_checkinteger(L, 2);
    lua_pushinteger(L, a + b);
    return 1;
}

static const luaL_Reg funcs[] = {
    {"add", mycmod_add},
    {NULL, NULL}
};

int luaopen_mycmod(lua_State *L) {
    luaL_newlib(L, funcs);   /* 5.3+; in 5.1 use luaL_register */
    return 1;
}
```

Build with Luarocks (preferred):

```lua
-- mycmod-1.0-1.rockspec
build = {
    type = "builtin",
    modules = {
        mycmod = { "src/mycmod.c" },
    },
}
```

Or compile manually:

```bash
cc -O2 -fPIC -shared -I/usr/include/lua5.4 -o mycmod.so src/mycmod.c
```

Then in Lua:

```lua
local mycmod = require("mycmod")
print(mycmod.add(2, 3))   -- 5
```

## Common pitfalls

- **Naming mismatch**: `require("foo.bar")` looks for `foo/bar.lua`. The C entry must be `luaopen_foo_bar` (dots become underscores). Mismatched names → "module not found" with a confusing error.
- **Forgetting `return M`** at the end of a module → `require` returns `true` instead of your table → "attempt to index a boolean value" at the call site.
- **Circular requires**: A requires B, B requires A → one of them gets `nil` from `package.loaded` mid-construction. Refactor: extract the shared logic to a third module, or pass dependencies as parameters.
- **Re-running a module**: `require` caches in `package.loaded`. To force a reload: `package.loaded["mymod"] = nil; require("mymod")`. Useful in dev/repl.
- **Path conflicts**: multiple Lua versions installed (5.1, 5.3, 5.4, LuaJIT) each have separate `package.path`/`package.cpath`. A rock installed for 5.3 is not visible to 5.4. Use `luarocks config` to check which Lua it targets.
- **`module()` in old code**: silently breaks in 5.4. Replace with the modern pattern.
- **Globals in modules**: a module that sets `_G.mymod = ...` instead of returning the table works but defeats sandboxing and `require`'s caching. Always return the table.
