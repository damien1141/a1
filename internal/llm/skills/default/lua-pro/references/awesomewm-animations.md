# AwesomeWM Animations (Rubato)

Rubato is the standard animation library for AwesomeWM. It's powerful but has sharp edges: the NaN trap, the missing-target trap, and the timer-for-transitions anti-pattern. This reference absorbs the awesomewm source skill's verbatim patterns — they are battle-tested.

## The NaN trap

If a `rubato` animation is interrupted (rapid hovering, tag switching, focus thrash), it can momentarily yield `NaN`, `inf`, or negative numbers.

- **NEVER pass `pos` directly to a widget dimension** (`forced_height`, `forced_width`, `margins`).
- **NEVER rely on `math.max()` or `math.min()` to filter `NaN`.** In Lua, `math.max(10, NaN)` returns `NaN`. `math.min(NaN, 100)` returns `NaN`. NaN propagates through every arithmetic op.
- **Correct clamp**: `if pos and pos == pos and pos > 0 then ... end`. The `pos == pos` check is `false` for `NaN` (NaN is not equal to itself in Lua, same as IEEE 754 everywhere).

```lua
subscribed = function(pos)
    -- WRONG: math.max doesn't filter NaN
    bg.forced_height = math.max(dpi(6), pos)             -- NaN passes through

    -- WRONG: not checking pos is a number
    if pos > 0 then bg.forced_height = pos end           -- errors if pos is NaN

    -- RIGHT: pos == pos is false for NaN
    if pos and pos == pos and pos > 0 then
        bg.forced_height = math.max(dpi(6), math.min(pos, dpi(40)))
    end
end
```

The `pos and` (truthy check) catches `nil`. The `pos == pos` catches `NaN`. The `pos > 0` catches negatives and (if you want) `inf` — adjust per widget (some widgets allow 0, some don't).

## Why `math.max`/`min` doesn't filter NaN

Lua follows IEEE 754: every comparison involving NaN returns `false`. So:

```lua
print(math.max(10, 0/0))     -- NaN    (max can't pick; falls through to NaN)
print(0/0 < 10)              -- false
print(0/0 > 10)              -- false
print(0/0 == 0/0)            -- false   ← this is the trap AND the test
print(0/0 ~= 0/0)            -- true
```

The trick `pos == pos` returns `false` ONLY for NaN (every other number equals itself). Use it.

## Initialize both `pos` AND `target`

Always initialize `rubato.timed` with BOTH `pos = initial_value` AND `target = initial_value`. If you only set `pos`, the default `target` is `0`. If an update fires before the animation fully initializes, the widget animates to 0 and disappears.

```lua
-- WRONG: missing target, will animate to 0 on first update
self._anim = rubato.timed{
    pos = dpi(8),
    subscribed = function(pos) ... end,
}

-- RIGHT: pos and target both set to initial value
local initial = dpi(8)
self._anim = rubato.timed{
    pos = initial,
    target = initial,
    subscribed = function(pos) ... end,
}
```

Even better: set the widget's initial state to match, so there's no flicker:

```lua
local initial = dpi(8)
bg.forced_height = initial                  -- widget starts correct
self._anim = rubato.timed{
    pos = initial,
    target = initial,
    subscribed = function(pos)
        if pos and pos == pos and pos > 0 then
            bg.forced_height = math.max(dpi(6), math.min(pos, dpi(40)))
        end
    end,
}
```

## No timers for transitions

Never use `gears.timer` to delay UI state changes (e.g., waiting 0.1s to fade a button out after release). Instead, instantly set the `rubato.target` to the new state and let the easing library handle the smooth transition natively.

```lua
-- WRONG: timer to delay a fade-out
gears.timer{
    timeout = 0.1,
    autostart = true,
    single_shot = true,
    callback = function()
        my_widget.opacity = 0   -- jumps, no easing
    end,
}:start()

-- RIGHT: set rubato.target, let easing handle the transition
my_anim.target = 0    -- glides smoothly to 0 over the duration
```

Why: timers add latency (you wait `timeout` before anything happens) and produce instant jumps (no easing). Rubato's `target = x` produces a smooth glide over the configured duration.

## Destroy timers on widget death

If you MUST use a `gears.timer` (e.g., bling's task preview has an intentional hover delay), ensure the timer is stopped or collected when the widget is destroyed to prevent memory leaks and signals firing on dead widgets.

```lua
local my_timer = gears.timer{
    timeout = 0.2,
    single_shot = true,
    callback = function() ... end,
}

-- Stop the timer when the widget is finalized (Lua 5.4 / AwesomeWM 4.4+):
my_widget:connect_signal("widget::finalized", function()
    my_timer:stop()
end)
```

For older AwesomeWM, store the timer on the widget (`self._timer`) and check `self._timer.started` before resuming.

## Fixed wrappers for dynamic children

If a child widget animates its size (e.g., a pill growing from 10px to 32px), it MUST be placed inside a wrapper with `forced_height` and `forced_width` equal to the MAXIMUM possible size of the child.

- Use `wibox.container.place` as this wrapper. It keeps the animated child centered without altering the parent layout's geometry.
- Without the wrapper, the parent layout re-flows on every animation frame → bar jumps/stutters.

```lua
widget_template = {
    {
        id = "background_role",
        widget = wibox.container.background,
    },
    -- THIS is the fixed wrapper:
    widget = wibox.container.place,
    forced_height = dpi(40),   -- MAX possible height of the child
    forced_width = dpi(10),    -- MAX possible width
    ...
}
```

## Asymmetric transitions

When a widget goes from State A (10px) to State B (32px), the animation looks smoother if you use asymmetric timings or `intro` values so the "grow" and "shrink" don't look identical.

```lua
self._anim = rubato.timed{
    pos = initial,
    target = initial,
    duration = 0.4,            -- symmetric default

    -- Or asymmetric:
    intro = 0.1,               -- quick start
    outro = 0.3,               -- slower finish
    -- OR use separate rubato.timed for grow vs shrink:
    -- rate = 60,              -- alternative to duration
    subscribed = function(pos) ... end,
}
```

For a button: fast grow (intro = 0.05), slower shrink (outro = 0.25). Feels responsive on press, soft on release.

## Font bounding boxes — do not animate margins

Do NOT animate the `margins` of `wibox.container.margin` around text or icon fonts. Shrinking the bounding box of a text glyph will cause AwesomeWM to distort it into an ellipse or wrap it.

Animate **opacity** or **background colors** instead of physical text size:

```lua
-- WRONG: animating margins around text
text_margin_widget:connect_signal("mouse::enter", function()
    my_anim_margin.target = dpi(2)    -- shrinks text bounding box, distorts glyph
end)

-- RIGHT: animate opacity or background
text_widget:connect_signal("mouse::enter", function()
    self._bg.bg = (beautiful.accent or "#8AB4F8") .. "33"   -- soft glow
    self._opacity_anim.target = 1.0                          -- fade in
end)
```

## Performance: never `collectgarbage("collect")` in callbacks

AwesomeWM runs on a single thread. Bad UI code will lock up the entire window manager. Never call `collectgarbage("collect")` inside `update_callback`, `create_callback`, or signal handlers. This forces a global GC pause and causes severe stuttering/lag on hover or focus changes. Let Lua's incremental GC handle it.

Tune the incremental collector at startup instead (one-time, in rc.lua):

```lua
collectgarbage("setpause", 110)       -- default 200; lower = more frequent collections
collectgarbage("setstepmul", 1000)    -- default 200; higher = bigger steps per byte allocated
```

## Performance: cache `get_children_by_id` in `create_callback`

`get_children_by_id` is expensive. In `update_callback`, if you need to manipulate a child, cache the reference in `create_callback` (e.g., `self.my_bg = self:get_children_by_id("background_role")[1]`) and access `self.my_bg` during updates.

```lua
create_callback = function(self, c3, index, objects)
    -- One-time expensive lookup, cached on self
    self.my_bg = self:get_children_by_id("background_role")[1]
    if not self.my_bg then return end
    -- ... initialize Rubato here, see widget-template pattern
end,

update_callback = function(self, c3, index, objects)
    -- Use cached reference; NO get_children_by_id here
    if not self.my_bg then return end
    self.my_bg.bg = ...
end,
```

## Color strings and CPU

AwesomeWM accepts 8-digit hex colors (`#RRGGBBAA`) for opacity. Use this for soft glows (e.g., `.. "33"` or `.. "1A"`) instead of animating opacity directly if you want to save CPU cycles.

```lua
-- Animating opacity = continuous redraws, CPU-heavy
self._opacity_anim = rubato.timed{
    pos = 0, target = 0,
    subscribed = function(p) self._bg.bg = (beautiful.accent or "#8AB4F8"):sub(1, 7)
                                    .. string.format("%02X", math.floor(p * 255)) end,
}

-- Static alpha = one redraw, much cheaper
self._bg.bg = (beautiful.accent or "#8AB4F8") .. "33"   -- 20% alpha, fixed
```

For a hover effect, a 2-state transition (off → `.. "33"`, on → `.. "FF"`) is often visually indistinguishable from a smooth opacity fade and uses 1/100th the CPU.

## Complete widget-template example (re-emphasized)

```lua
local dpi = require("beautiful.xresources").apply_dpi
local rubato = require("modules.rubato")
local beautiful = require("beautiful")

local widget_template = {
    {
        id = "background_role",
        widget = wibox.container.background,
    },
    -- Fixed wrapper absorbs the child's size animation
    widget = wibox.container.place,
    forced_height = dpi(40),
    forced_width = dpi(10),

    create_callback = function(self, c3, index, objects)
        -- 1. Fetch child safely, cache on self
        local bg = self:get_children_by_id("background_role")[1]
        if not bg then return end
        self._bg = bg

        -- 2. Set initial state
        local h = dpi(8)
        bg.forced_height = h

        -- 3. Initialize Rubato safely: pos + target + NaN clamp
        self._anim = rubato.timed{
            pos = h, target = h,
            intro = 0.05, outro = 0.25,      -- asymmetric: fast grow, soft shrink
            subscribed = function(pos)
                -- NaN trap: pos == pos is false for NaN
                if pos and pos == pos and pos > 0 then
                    bg.forced_height = math.max(dpi(6), math.min(pos, dpi(40)))
                end
            end,
        }
    end,

    update_callback = function(self, c3, index, objects)
        -- 1. Update colors instantly (no animation needed for color)
        if not (self._bg and self._anim) then return end
        local is_selected = c3.selected
        self._bg.bg = is_selected
            and (beautiful.accent or "#8AB4F8")
            or  "#00000000"
        -- 2. Glide to new height using self._anim.target
        --    (NO timer; the easing library handles the transition)
        self._anim.target = is_selected and dpi(32) or dpi(8)
    end,
}
```

## Pitfalls (summary)

- **NaN in `pos`**: clamp with `pos == pos`, not `math.max`/`min`.
- **Missing `target` in init**: widget animates to 0 and disappears.
- **Timer-based transitions**: instant jumps, no easing. Use `rubato.target = x`.
- **Animated margins around text**: glyph distortion.
- **No fixed wrapper around animated children**: bar jumps.
- **`collectgarbage("collect")` in callbacks**: stutter.
- **`get_children_by_id` in `update_callback`**: lag on hover.
- **Symmetric easing for press/release**: feels dead. Asymmetric (fast in, slow out).
- **Leaked timers on destroyed widgets**: errors on next tick. Stop on `widget::finalized`.
