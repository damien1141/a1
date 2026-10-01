# AwesomeWM Widgets

The widget template pattern is the load-bearing idiom for `taglist`/`tasklist`/custom widgets. Get it right or your WM stutters on every hover. This reference absorbs and expands the awesomewm source skill's verbatim patterns — those are battle-tested.

## Widget basics

A `wibox.widget` is anything with `:draw()` and `:layout()`. Built-in widgets live in `wibox.widget.*`:

| Widget | Purpose |
|---|---|
| `wibox.widget.textbox` | Text label |
| `wibox.widget.imagebox` | Image/icon |
| `wibox.widget.progressbar` | Progress bar |
| `wibox.widget.slider` | Slider input |
| `wibox.widget.checkbox` | Toggle |
| `wibox.widget.graph` | Line/area graph |
| `wibox.widget.systray` | System tray |
| `wibox.widget.calendar` | Calendar widget |

Containers wrap widgets:

| Container | Purpose |
|---|---|
| `wibox.container.background` | Background color, border, shape |
| `wibox.container.margin` | Padding |
| `wibox.container.constraint` | Min/max width/height |
| `wibox.container.place` | Align child within fixed box (THE wrapper for animated children) |
| `wibox.container.rotate` | Rotate child |
| `wibox.container.scroll` | Auto-scrolling text |
| `wibox.layout.fixed.horizontal` / `.vertical` | Stack in one direction |
| `wibox.layout.flex.horizontal` / `.vertical` | Equal-flex expand |
| `wibox.layout.ratio.horizontal` / `.vertical` | Custom ratios |
| `wibox.layout.align.horizontal` / `.vertical` | Three-part: left/center/right |

## Layout principle: fixed wrappers for dynamic children

**Critical**: dynamic sizing (animations) inside fixed layouts causes the entire bar/widget to shift and jump.

If a child widget animates its size (e.g., a pill growing from 10px to 32px), it MUST be placed inside a wrapper with `forced_height` and `forced_width` equal to the MAXIMUM possible size of the child.

- Use `wibox.container.place` as this wrapper. It keeps the animated child centered without altering the parent layout's geometry.
- `wibox.container.constraint` is a weaker alternative — it caps size but doesn't center.
- `wibox.container.margin` is NOT a substitute; margins can collapse to 0 and the bar still jumps.

## Font bounding boxes — do not animate margins

Do not animate the `margins` of `wibox.container.margin` around text or icon fonts. Shrinking the bounding box of a text glyph will cause AwesomeWM to:

- distort it into an ellipse, OR
- wrap it (which then breaks the layout), OR
- clip it.

Animate **opacity** or **background colors** instead of physical text size.

## `awful.widget.taglist` / `tasklist` — the widget template pattern

`taglist` and `tasklist` take a `widget_template` that defines how each entry renders. The template uses `create_callback` (runs once per entry on creation) and `update_callback` (runs on every state change).

```lua
-- A taglist indicator pill that animates its height on selection
local dpi = require("beautiful.xresources").apply_dpi
local rubato = require("modules.rubato")

local taglist = awful.widget.taglist({
    screen = s,
    filter = awful.widget.taglist.filter.all,
    layout = { layout = wibox.layout.fixed.horizontal, spacing = dpi(4) },
    widget_template = {
        {
            {
                id = "background_role",
                widget = wibox.container.background,
            },
            left = dpi(2), right = dpi(2),
            widget = wibox.container.margin,
        },
        -- Wrapper to absorb size changes:
        widget = wibox.container.place,
        forced_height = dpi(40),
        forced_width = dpi(10),

        create_callback = function(self, c3, index, objects)
            -- 1. Fetch child safely and CACHE it (get_children_by_id is expensive)
            local bg = self:get_children_by_id("background_role")[1]
            if not bg then return end
            self._bg = bg

            -- 2. Set initial state
            local h = dpi(8)
            bg.forced_height = h

            -- 3. Initialize Rubato safely (pos + target + NaN clamp)
            --    See references/awesomewm-animations.md for the NaN trap.
            self._anim = rubato.timed{
                pos = h, target = h,
                subscribed = function(pos)
                    if pos and pos == pos and pos > 0 then
                        bg.forced_height = math.max(dpi(6), math.min(pos, dpi(40)))
                    end
                end,
            }
        end,

        update_callback = function(self, c3, index, objects)
            -- 1. Update colors instantly
            if not (self._bg and self._anim) then return end
            local is_selected = c3.selected
            self._bg.bg = is_selected
                and (beautiful.accent or "#8AB4F8")
                or  "#00000000"
            -- 2. Glide to new height using self._anim.target
            self._anim.target = is_selected and dpi(32) or dpi(8)
        end,
    },
})
```

## Caching `get_children_by_id`

`get_children_by_id` walks the widget tree. Calling it in `update_callback` (which fires on every tag change, hover, focus) is expensive.

**Cache the reference in `create_callback`**:

```lua
create_callback = function(self, c3, index, objects)
    self._bg = self:get_children_by_id("background_role")[1]   -- one-time lookup
    self._text = self:get_children_by_id("text_role")[1]
end,

update_callback = function(self, c3, index, objects)
    if not self._bg then return end
    self._bg.bg = ...           -- use cached reference
    if self._text then self._text.text = c3.name end
end,
```

The `_bg` field lives on the widget instance (`self`) and persists across callbacks. Prefix with `_` to signal "private cached field".

## Never `collectgarbage("collect")` in callbacks

```lua
-- WRONG — forces a global GC pause, severe stutter on hover
update_callback = function(self, c3, ...)
    self._bg.bg = ...
    collectgarbage("collect")    -- NEVER in callbacks
end

-- RIGHT — let Lua's incremental GC handle it
update_callback = function(self, c3, ...)
    self._bg.bg = ...
end
```

AwesomeWM runs on a single thread. `collectgarbage("collect")` is a full stop-the-world pause. In a hot callback (hover, focus, tag switch), it causes visible lag.

If you must control GC (rare), tune the incremental collector at startup:

```lua
-- rc.lua, near the top:
collectgarbage("setpause", 110)    -- default 200; lower = more frequent
collectgarbage("setstepmul", 1000) -- default 200; higher = larger steps
```

## Custom widgets

For a custom widget, derive from `wibox.widget.base.make_widget`:

```lua
local my_widget = {}
my_widget.__index = my_widget

function my_widget.new(args)
    local w = wibox.widget.base.make_widget(nil, "my_widget", {enable_properties = true})
    gears.table.crush(w, my_widget, true)
    w._private.text = args.text or ""
    return w
end

function my_widget:draw(_, cr, width, height)
    cr:set_source_rgb(1, 0, 0)
    cr:rectangle(0, 0, width, height)
    cr:fill()
end

function my_widget:layout(_, width, height)
    -- return a list of {widget, x, y, w, h} for children
    return nil
end

function my_widget:set_text(text)
    if self._private.text == text then return end
    self._private.text = text
    self:emit_signal("widget::redraw_needed")
    self:emit_signal("widget::layout_changed")
end

function my_widget:get_text()
    return self._private.text
end

return setmetatable(my_widget, { __call = function(_, ...) return my_widget.new(...) end })
```

The `widget::redraw_needed` and `widget::layout_changed` signals are how a widget tells AwesomeWM to re-render. Emit them on any state change that affects the visual.

## Building a wibar

```lua
local function make_wibar(s)
    s.mywibox = awful.wibar({
        position = "top",
        screen = s,
        height = dpi(36),
        bg = beautiful.wibar_bg or "#1e1e2e",
        fg = beautiful.wibar_fg or "#cdd6f4",
    })

    s.mywibox:setup({
        layout = wibox.layout.align.horizontal,
        expand = "none",
        {   -- left
            layout = wibox.layout.fixed.horizontal,
            s.mytaglist,
            s.mypromptbox,
        },
        {   -- middle (centered)
            layout = wibox.layout.fixed.horizontal,
            my_clock,
        },
        {   -- right
            layout = wibox.layout.fixed.horizontal,
            wibox.widget.systray(false),
            my_volume,
            my_battery,
            s.mylayoutbox,
        },
    })
end

awful.screen.connect_for_each_screen(make_wibar)
```

`wibox.layout.align.horizontal` with `expand = "none"` gives you the classic 3-part bar (left / center / right) where the center is exactly its content width.

## Reactive widgets (signals + refresh)

```lua
-- A battery widget that updates on UDisks2 signal
local battery = wibox.widget.textbox()
local function update_battery()
    awful.spawn.easy_async("acpi -b", function(out)
        local pct = out:match("(%d+)%%")
        local charging = out:match("Charging") and "+" or ""
        battery.text = "BAT " .. (pct or "?") .. charging
    end)
end

gears.timer({
    timeout = 30,
    autostart = true,
    callback = update_battery,
}):start()
update_battery()   -- initial

-- Or subscribe to a dbus signal:
awesome.connect_signal("signal::battery", update_battery)
```

For external state, the pattern is: a timer (or dbus signal) drives an `easy_async` shell command, which updates a `textbox.text`. Heavy commands in the timer are fine if the timeout is ≥1s.

## Clickable widgets

```lua
local button = wibox.widget{
    {
        text = "click me",
        widget = wibox.widget.textbox,
    },
    widget = wibox.container.background,
}

button:connect_signal("button::press", function(_, _, _, button)
    if button == 1 then   -- left click
        awful.spawn("firefox")
    end
end)

-- Hover effect:
button:connect_signal("mouse::enter", function()
    button.bg = beautiful.accent or "#8AB4F8"
end)
button:connect_signal("mouse::leave", function()
    button.bg = beautiful.bg_normal or "#00000000"
end)
```

## Pitfalls

- **Forgetting the fixed wrapper around animated children**: bar jumps/stutters. Always `wibox.container.place` with `forced_height`/`forced_width` set to the max.
- **`get_children_by_id` in `update_callback`**: severe lag on hover. Cache in `create_callback`.
- **`collectgarbage("collect")` in callbacks**: stutter. Don't.
- **Animating text margins**: glyph distortion or wrap. Animate opacity/background instead.
- **Forgetting `:emit_signal("widget::redraw_needed")`**: custom widget doesn't refresh on state change.
- **Calling `easy_async` synchronously**: blocks the WM. Always use the callback.
- **Timer not `:start()`ed**: timer created but never fires. Set `autostart = true` or call `:start()`.
- **Timer leak**: a timer with a callback that references a destroyed widget → error on next tick. Stop timers when the widget is destroyed (`widget:connect_signal("widget::finalized", function() timer:stop() end)` — Lua 5.4 / AwesomeWM 4.4+).
