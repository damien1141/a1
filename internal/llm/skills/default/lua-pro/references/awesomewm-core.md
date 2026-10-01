# AwesomeWM Core

AwesomeWM is a tiling window manager written in C with a Lua configuration API. The config lives in `~/.config/awesome/rc.lua` and is re-evaluated on restart. This reference covers the structure, tags/layouts/rules, signals, theming, and the supporting libraries.

## rc.lua structure

A modern `rc.lua` (AwesomeWM 4.3+) follows this skeleton:

```lua
-- ~/.config/awesome/rc.lua

-- 1. Library requires
local awful = require("awful")
local gears = require("gears")
local wibox = require("wibox")
local beautiful = require("beautiful")
local naughty = require("naughty")
local ruled = require("ruled")
local menubar = require("menubar")

-- Optional community libraries:
local rubato = require("modules.rubato")    -- animations
local bling = require("modules.bling")      -- extras (window swallower, tabbed, etc.)

-- 2. Error handling (startup errors + runtime errors)
require("error_handling")  -- or inline: naughty.notify on awesome.connect_signal("debug::error", ...)

-- 3. Theme
beautiful.init(gears.filesystem.get_configuration_dir() .. "theme/theme.lua")

-- 4. Default terminal, editor, modkey
terminal = "wezterm"
editor = os.getenv("EDITOR") or "nvim"
modkey = "Mod4"   -- Super key

-- 5. Layouts
awful.layout.append_default_layouts({
    awful.layout.suit.tile,
    awful.layout.suit.floating,
    awful.layout.suit.max,
    awful.layout.suit.magnifier,
})

-- 6. Tags
require("tags")   -- or inline tag definitions

-- 7. Menu
mymainmenu = awful.menu({ items = { ... } })

-- 8. Wibar (top bar) with taglist, tasklist, system tray, clock
require("wibar")

-- 9. Keybindings (global + client)
require("keys")

-- 10. Rules (per-client settings)
ruled.client.connect_signal("request::rules", function() ... end)

-- 11. Signals (border color on focus, etc.)
client.connect_signal("focus", function(c) c.border_color = beautiful.border_focus end)

-- 12. Autostart
awful.spawn.with_shell("picom -b")
```

## Tags

Tags are workspaces (virtual desktops). Each screen has a tag list; each client is on one or more tags.

```lua
-- Modern tag creation (ruled.tags + awful.tag):
awful.screen.connect_for_each_screen(function(s)
    awful.tag({ "1", "2", "3", "4", "5" }, s, awful.layout.layouts[1])
end)

-- Or via ruled.tags (AwesomeWM 4.4+):
ruled.tag.append_rule({
    rule = { class = "firefox" },
    properties = { tag = "2", switch_to_tags = true },
})
```

- A client can be on multiple tags: `c:tags({ t1, t2 })`.
- Toggle tag view: `awful.tag.viewtoggle(tag)`.
- Move client to tag: `c:move_to_tag(tag)`.

## Layouts

A layout is an algorithm for arranging clients on a tag. Built-in:

| Layout | Behavior |
|---|---|
| `awful.layout.suit.tile` | Master on left, stack on right |
| `awful.layout.suit.tile.bottom` | Master on top, stack below |
| `awful.layout.suit.tile.top` | Master on bottom, stack above |
| `awful.layout.suit.tile.left` | Master on right, stack on left |
| `awful.layout.suit.fair` | Grid (equal-size cells) |
| `awful.layout.suit.fair.horizontal` | Horizontal grid |
| `awful.layout.suit.max` | One client, fullscreen |
| `awful.layout.suit.max.fullscreen` | One client, true fullscreen |
| `awful.layout.suit.magnifier` | Master centered, others in background |
| `awful.layout.suit.floating` | No tiling, free positioning |
| `awful.layout.suit.spiral` / `spiral.dwindle` | Spiral arrangement |
| `awful.layout.suit.cornernw` / `cornerne` / `cornersw` / `cornerse` | Corner-master |

Set per-tag: `tag.layout = awful.layout.suit.tile`.

## Rules (per-client settings)

Rules match clients by class, instance, name, role, type, and apply properties.

```lua
ruled.client.connect_signal("request::rules", function()
    -- All clients: respect size hints by default
    ruled.client.append_rule({
        id = "global",
        rule = {},
        properties = {
            focus = awful.client.focus.filter,
            raise = true,
            screen = awful.screen.preferred,
            placement = awful.placement.no_overlap + awful.placement.no_offscreen,
        },
    })

    -- Firefox to tag 2:
    ruled.client.append_rule({
        rule = { class = "firefox" },
        properties = { tag = "2", switch_to_tags = true },
    })

    -- Gimp as floating, on tag 4:
    ruled.client.append_rule({
        rule_any = { class = { "Gimp", "Inkscape" } },
        properties = { tag = "4", floating = true },
    })

    -- Steam dialogs as floating, below:
    ruled.client.append_rule({
        rule = { class = "Steam", type = "dialog" },
        properties = { floating = true, below = true, focus = false },
    })
end)
```

Find a client's `class`/`instance` via `xprop` (click the window, read `WM_CLASS(STRING)` = `"instance", "class"`).

## Signals

Signals are AwesomeWM's event system. Connect with `obj:connect_signal(name, callback)`.

### Common client signals

| Signal | When |
|---|---|
| `manage` | New client appears |
| `unmanage` | Client closed |
| `focus` | Client gained focus |
| `unfocus` | Client lost focus |
| `property::size` | Client resized |
| `property::position` | Client moved |
| `tagged` | Client added to a tag |
| `untagged` | Client removed from a tag |
| `request::titlebars` | Client requests a titlebar |
| `request::border` | Border calculation (custom border logic) |

### Screen / tag signals

| Signal | When |
|---|---|
| `screen::connect_for_each_screen` | Run for each screen (one-time) |
| `tag::history::update` | Tag view changed |
| `tag::property::selected` | Tag was selected/deselected |

### Global signals

```lua
-- Runtime error → notify
awesome.connect_signal("debug::error", function(err)
    naughty.notify({ title = "Awesome error", text = tostring(err) })
end)

-- Refresh (e.g. on dpi change)
awesome.connect_signal("refresh", function() ... end)

-- Exit
awesome.connect_signal("exit", function() ... end)
```

### Signal safety

Always check the client still exists before heavy logic in a signal callback:

```lua
client.connect_signal("property::size", function(c)
    if not c.valid then return end        -- client may be gone
    -- ... do work
end)
```

`c.valid` is `false` after the client is unmanaged; accessing other fields may error.

## Libraries

| Library | Purpose |
|---|---|
| `awful` | Window manager logic: tags, layouts, rules, keybindings, prompts, widget helpers |
| `gears` | Utilities: timers, object, color, filesystem, string, shape, matrix, cache |
| `wibox` | Widget toolkit: containers, widgets, drawable, layout primitives |
| `naughty` | Notifications |
| `beautiful` | Theme variables (colors, fonts, sizes) |
| `menubar` | dmenu-like app launcher |
| `ruled` | Rule system (4.3+, replaces `awful.rules`) |
| `bling` | Community extras: window swallowing, tabbed layouts, task preview, scratchpads, wallpapers |
| `rubato` | Community animation library (easing) |

## `beautiful` theming with fallbacks

**Always provide fallbacks** for `beautiful` variables. If a theme fails to load, widgets crash the WM otherwise.

```lua
-- WRONG: if beautiful.accent is nil, this becomes nil and crashes the widget
local accent = beautiful.accent

-- RIGHT: defensive fallback
local accent = beautiful.accent or "#8AB4F8"
local font = beautiful.font or "sans 10"
local border_width = beautiful.border_width or dpi(2)
```

Initialize `beautiful` BEFORE using its variables:

```lua
beautiful.init(gears.filesystem.get_configuration_dir() .. "theme/theme.lua")
-- AFTER this line, beautiful.foo is populated from the theme
```

## 8-digit hex colors (`#RRGGBBAA`)

AwesomeWM accepts 8-digit hex for opacity. Use for soft glows instead of animating opacity (CPU-saver):

```lua
local accent_full = beautiful.accent or "#8AB4F8"      -- #RRGGBB
local accent_soft  = (beautiful.accent or "#8AB4F8") .. "33"  -- ~20% opacity
local accent_med   = (beautiful.accent or "#8AB4F8") .. "80"  -- 50% opacity

-- Use:
widget.bg = accent_full
widget.bg = accent_soft   -- soft hover background
```

| Alpha hex | Opacity |
|---|---|
| `FF` | 100% |
| `80` | 50% |
| `40` | 25% |
| `33` | ~20% |
| `1A` | ~10% |
| `00` | 0% (transparent) |

## DPI scaling

Always scale sizes with `dpi()`. Hardcoded pixel values look wrong on HiDPI displays.

```lua
local dpi = require("beautiful.xresources").apply_dpi

local size = dpi(40)           -- 40px on a 1x display, 80px on 2x
widget.forced_height = dpi(40)
widget.forced_width = dpi(10)
```

`dpi()` reads Xft.dpi from X resources. Configure per monitor in `~/.Xresources`:

```
Xft.dpi: 144   # for a 1.5x display
```

Without `dpi()`, your widgets will look tiny on HiDPI and huge on a CRT.

## Error handling in rc.lua

```lua
-- Startup errors (config syntax errors before the WM fully starts):
if awesome.startup_errors then
    naughty.notify({
        preset = naughty.config.presets.critical,
        title = "Oops, there were errors during startup!",
        text = awesome.startup_errors,
    })
end

-- Runtime errors (errors during signal callbacks, etc.):
do
    local in_error = false
    awesome.connect_signal("debug::error", function(err)
        if in_error then return end   -- avoid recursion
        in_error = true
        naughty.notify({
            preset = naughty.config.presets.critical,
            title = "Oops, an error happened!",
            text = tostring(err),
        })
        in_error = false
    end)
end
```

This catches errors but does NOT prevent them. The error still propagates and the signal callback aborts at that point. Use `pcall` inside callbacks for non-critical work.

## Restarting and reloading

- `Mod4 + Ctrl + r` — restart AwesomeWM (reloads rc.lua).
- `Mod4 + Shift + q` — quit AwesomeWM (logs out).
- From a TTY: `awesome -r` (restart) or restart the X session.

After any rc.lua edit, restart and check `~/.xsession-errors` (or `~/.xsession-errors.old`) for stack traces:

```bash
tail -50 ~/.xsession-errors
```

A syntax error in rc.lua drops you into a fallback WM at next login (or fails to start at all). **Always test rc.lua changes before logging out**:

```bash
awesome -c ~/.config/awesome/rc.lua --check   # syntax check (no execution)
# Then restart with Mod4 + Ctrl + r and watch ~/.xsession-errors
```

## Pitfalls

- **Forgetting `require`**: Using `awful.foo` without `local awful = require("awful")` → "attempt to index a nil value (global 'awful')". Luacheck with proper `.luacheckrc` catches this at edit time.
- **Order of operations**: `beautiful.init` must run BEFORE any `beautiful.foo` access. `require("awful")` must run before `awful.layout.suit.tile`.
- **Defensive theming**: every `beautiful.x` access needs `or <default>`. A theme that fails to load should not crash the WM.
- **Signal callbacks running on dead clients**: check `c.valid` before heavy work.
- **`~/.xsession-errors` accumulating**: old errors persist. Tail the end, not the whole file.
- **Modifying rc.lua while logged in**: a syntax error kicks in on next restart and may soft-lock you. Keep a backup `rc.lua.bak` and a TTY open to revert.
- **Theme reload requires WM restart**: editing `theme.lua` does not hot-reload. `Mod4 + Ctrl + r`.
