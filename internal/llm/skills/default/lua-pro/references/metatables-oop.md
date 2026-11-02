# Metatables and OOP

Metatables are how Lua does everything object-oriented: method lookup, operator overloading, prototype inheritance, lazy fields. There's no `class` keyword; there's `setmetatable`.

## What a metatable is

A metatable is a regular table attached to another table (or string/userdata via debug library) that controls its behavior. The keys in a metatable are **metamethod names** (strings starting with `__`); the values are functions (or, for `__index`/`__newindex`, a table).

```lua
local t = setmetatable({}, {
    __index = function(_, k) return "no such key: " .. k end,
})
print(t.foo)           -- "no such key: foo"
```

## Metamethod reference

| Metamethod | Trigger | Example |
|---|---|---|
| `__index` | `t[k]` when `t[k]` is nil | Method lookup, defaults, prototypes |
| `__newindex` | `t[k] = v` when `t[k]` is nil | Intercept writes, logging, lazy init |
| `__add` | `a + b` | `setmetatable({}, {__add = function(a, b) ... end})` |
| `__sub` `__mul` `__div` `__mod` `__pow` `__idiv` `__unm` | arithmetic | `a - b`, `a * b`, `a / b`, `a % b`, `a ^ b`, `a // b`, `-a` |
| `__band` `__bor` `__bxor` `__bnot` `__shl` `__shr` | bitwise (5.3+) | `a & b`, `a | b`, `a ~ b`, `~a`, `a << b`, `a >> b` |
| `__concat` | `a .. b` | Concatenating custom types |
| `__len` | `#t` | Custom length |
| `__eq` `__lt` `__le` | `a == b`, `a < b`, `a <= b` | Value equality for tables |
| `__call` | `t(args)` | Calling a table like a function |
| `__tostring` | `tostring(t)` | `print(t)` formatting |
| `__pairs` `__ipairs` | `for k, v in pairs(t)` | Custom iteration (5.2+/5.3+) |
| `__gc` | garbage collection | Finalizer for userdata (5.4 works for tables too) |
| `__close` | scope exit (`local x <close> = ...`) | Lua 5.4 to-be-closed variables |
| `__mode` | weak table | `k`/`v`/`kv` for weak keys/values |

## `__index` — the workhorse

`__index` can be a function OR a table. As a table, lookups fall through to it.

```lua
-- As a table (prototype pattern):
local defaults = { host = "localhost", port = 8080, timeout = 30 }
local config = setmetatable({ port = 9090 }, { __index = defaults })
print(config.host)      -- "localhost" (from defaults)
print(config.port)      -- 9090        (from config)
print(config.timeout)   -- 30          (from defaults)

-- As a function:
local lazy = setmetatable({}, {
    __index = function(t, k)
        local v = expensive_compute(k)
        rawset(t, k, v)            -- cache for next time
        return v
    end,
})
```

`rawget(t, k)` bypasses `__index` — use to check if a key is REALLY in `t` (not inherited).

## `__newindex` — intercepting writes

```lua
local log_writes = setmetatable({}, {
    __newindex = function(t, k, v)
        print("setting " .. tostring(k) .. " = " .. tostring(v))
        rawset(t, k, v)            -- actually store; rawset bypasses __newindex
    end,
})
log_writes.x = 1                   -- prints: setting x = 1
```

`rawset(t, k, v)` bypasses `__newindex` — use inside a `__newindex` handler to avoid infinite recursion.

## Operator overloading

```lua
local Vec = {}
Vec.__index = Vec
Vec.__add = function(a, b) return Vec.new(a.x + b.x, a.y + b.y) end
Vec.__eq  = function(a, b) return a.x == b.x and a.y == b.y end
Vec.__lt  = function(a, b) return a.x < b.x or (a.x == b.x and a.y < b.y) end
Vec.__le  = function(a, b) return a < b or a == b end
Vec.__tostring = function(v) return "(" .. v.x .. "," .. v.y .. ")" end

function Vec.new(x, y)
    return setmetatable({ x = x, y = y }, Vec)
end

local a, b = Vec.new(1, 2), Vec.new(3, 4)
print(a + b)            -- (4,6)
print(a == Vec.new(1,2)) -- true
```

Note: `__eq` is only called when both operands have the SAME `__eq` metamethod. `__lt` and `__le` must be defined consistently (Lua derives `>` and `>=` from them).

## `__call` — callable tables

```lua
local counter = setmetatable({ n = 0 }, {
    __call = function(self, inc)
        self.n = self.n + (inc or 1)
        return self.n
    end,
})
print(counter())       -- 1
print(counter(5))      -- 6
print(counter())       -- 7
```

Use for: factory functions that need state, functors, memoized calls.

## `__gc` and `__close` (Lua 5.4)

```lua
-- Lua 5.4: __gc works on tables (not just userdata). Set the metatable AFTER
-- the table is created to enable __gc.
local function with_resource(path)
    local r = { f = assert(io.open(path, "r")) }
    setmetatable(r, {
        __gc = function(self)
            if self.f then self.f:close() end
        end,
    })
    return r
end

-- Lua 5.4 to-be-closed variables (deterministic scope exit):
local function read_lines(path)
    local f <close> = assert(io.open(path, "r"))
    -- f's __close metamethod runs when f goes out of scope (here: function return)
    return function() return f:read("*l") end
end
```

Prefer `<close>` over `__gc` for deterministic cleanup — `__gc` runs at GC time (non-deterministic).

## OOP pattern: a class

```lua
-- animal.lua
local Animal = {}
Animal.__index = Animal

function Animal.new(name, sound)
    local self = setmetatable({}, Animal)
    self.name = name
    self.sound = sound
    return self
end

function Animal:speak()
    return self.name .. " says " .. self.sound
end

return Animal
```

```lua
local Animal = require("animal")
local d = Animal.new("Rex", "Woof")
print(d:speak())       -- "Rex says Woof"
```

The `:` method-call syntax desugars `d:speak()` to `Animal.speak(d)` — passing `d` as the implicit `self`. `d.speak(d)` would work too but is uglier.

## Inheritance

```lua
-- dog.lua
local Animal = require("animal")
local Dog = setmetatable({}, { __index = Animal })   -- Dog inherits Animal's methods
Dog.__index = Dog                                     -- instances of Dog use Dog as metatable

function Dog.new(name)
    local self = Animal.new(name, "Woof")             -- call parent constructor
    return setmetatable(self, Dog)                    -- override metatable to Dog
end

function Dog:fetch()
    return self.name .. " fetches the ball"
end

return Dog
```

```lua
local Dog = require("dog")
local d = Dog.new("Rex")
print(d:speak())       -- "Rex says Woof"   (inherited from Animal)
print(d:fetch())       -- "Rex fetches the ball"
```

The two-metatable trick: `Dog` itself inherits from `Animal` (so `Dog.new` falls through to `Animal.new` if undefined), while `Dog` instances use `Dog` as their metatable (so instance method lookup goes through `Dog` first, then `Animal` via `__index` chain).

## Multiple inheritance

```lua
local function multi_inherit(...)
    local parents = { ... }
    local cls = {}
    setmetatable(cls, {
        __index = function(_, k)
            for _, parent in ipairs(parents) do
                local v = parent[k]
                if v ~= nil then return v end
            end
        end,
    })
    cls.__index = cls
    return cls
end
```

Lua has no built-in multiple inheritance; this manual `__index` function does the lookup. Use sparingly — composition (a field that IS-A another object) is usually cleaner than multiple inheritance.

## Prototype pattern (no class)

```lua
local prototype = { x = 0, y = 0, greet = function(self) return "hi from " .. self.x end }

local instance = setmetatable({}, { __index = prototype })
print(instance:greet())    -- "hi from 0"  (x inherited from prototype)
instance.x = 5
print(instance:greet())    -- "hi from 5"  (own x shadows prototype)
```

Use for: simple config objects, singletons, when you don't need a constructor.

## `:` vs `.` — the #1 Lua bug

```lua
function Foo:method() ... end     -- implicit self parameter
-- Equivalent to:
function Foo.method(self) ... end

-- Call:
foo:method()                       -- passes foo as self
-- Equivalent to:
Foo.method(foo)                    -- explicit self
```

Mixing them up:

- Defining with `:` and calling with `.`: `Foo.method()` — `self` is `nil`, errors on first `self.x` access.
- Defining with `.` and calling with `:`: passes the receiver as the first arg, shifting all params by 1 — silent bug.

Convention: **use `:` for methods (functions that operate on a receiver), `.` for plain functions.** Be consistent within a class.

## Weak tables

```lua
local cache = setmetatable({}, { __mode = "v" })   -- weak values
cache[1] = some_object
-- some_object is collectible if cache[1] is the only reference

local memo = setmetatable({}, { __mode = "k" })    -- weak keys
memo[obj] = computed_value
-- if obj is GC'd, the entry is removed

-- Both: { __mode = "kv" }
```

Use for: caches, memoization, observer registries that shouldn't keep objects alive.

## Pitfalls

- **`__eq` requires matching metamethods on both sides.** `mytype == "string"` won't trigger `mytype`'s `__eq` unless string also has one (it doesn't).
- **`__gc` on tables requires the metatable to be set after creation** (Lua 5.4 specific quirk). Setting it inline `setmetatable({}, {__gc=...})` does NOT enable `__gc` — you must set it in a separate call.
- **`__index` chain can loop.** `setmetatable(t, {__index = t})` is infinite recursion on any miss.
- **`rawget`/`rawset` bypass metamethods.** Use them inside `__index`/`__newindex` handlers to avoid recursion.
- **`__len` does not apply to strings** in standard Lua (LuaJIT/Lua 5.4 differ). Use `string.len` or `#`.
- **Operator metamethods only fire if the LEFT operand doesn't support the operator natively.** `1 + mytype` — number `1` supports `+`, so it tries to add `mytype` as a number, errors. Reverse-order lookup is automatic for `+`, `-`, etc., but not for `..` or comparisons.
