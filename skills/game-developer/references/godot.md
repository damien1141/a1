# Godot Reference

Production patterns for Godot 4: GDScript, C#, nodes, signals, scene tree, GDExtension (C++), and the rendering pipeline. Read alongside `engine-architecture.md` for fundamentals.

## Why Godot

- **Free and open source** (MIT license) — no royalties, no account required
- **Lightweight** — single ~100MB download; opens in seconds
- **Excellent 2D** — true 2D coordinate system (not faked 3D)
- **GDScript** — Python-like, designed for games, hot reload
- **C# support** — for Unity migrants (via .NET 8)
- **GDExtension** — write C++/Rust/Python extensions without recompiling the engine
- **Node-based architecture** — composable, designer-friendly

## Project Structure

```
res://                            # project root (virtual filesystem)
├── project.godot                 # project config
├── scenes/
│   ├── player/
│   │   ├── player.tscn           # scene (tree of nodes)
│   │   └── player.gd
│   ├── enemies/
│   └── ui/
├── scripts/                      # autoloads, utilities
├── assets/
│   ├── sprites/
│   ├── audio/
│   └── fonts/
└── addons/                       # third-party plugins
```

## Node System

Everything is a node. Nodes form a tree (the scene). Common node types:

| Node | Use |
|---|---|
| `Node2D` / `Node3D` | Base 2D / 3D node |
| `Sprite2D` / `Sprite3D` | Draw a texture |
| `CharacterBody2D` / `CharacterBody3D` | Player-controlled body (with collision) |
| `RigidBody2D` / `RigidBody3D` | Physics-driven body |
| `StaticBody2D` / `StaticBody3D` | Static collider (walls, floor) |
| `Area2D` / `Area3D` | Trigger zone (no physical response) |
| `AnimationPlayer` | Key-frame animations |
| `Camera2D` / `Camera3D` | View into the scene |
| `CanvasLayer` | UI layer (screen-space) |

### Composition via Nodes

Instead of inheritance, attach child nodes:

```
Player (CharacterBody2D)
├── Sprite2D              # visual
├── CollisionShape2D      # physics
├── Camera2D              # follows player
├── AnimationPlayer       # anims
└── GunSpawn (Marker2D)   # where bullets spawn
```

To add behavior, attach a script to the root node and access children via `$NodeName` (shorthand for `get_node("NodeName")`).

## Signals (Event System)

```gdscript
# player.gd
extends CharacterBody2D

signal health_changed(new_health: int)
signal died

@export var max_health: int = 100
var _health: int

func _ready() -> void:
    _health = max_health

func take_damage(amount: int) -> void:
    _health = max(0, _health - amount)
    health_changed.emit(_health)
    if _health == 0:
        died.emit()
        queue_free()  # destroy this node
```

```gdscript
# game_manager.gd — listen to the signal
extends Node

@onready var _player: CharacterBody2D = $Player

func _ready() -> void:
    _player.health_changed.connect(_on_player_health_changed)
    _player.died.connect(_on_player_died)

func _on_player_health_changed(new_health: int) -> void:
    $UI/HealthBar.value = new_health

func _on_player_died() -> void:
    get_tree().reload_current_scene()
```

**Rule:** prefer signals over direct method calls for cross-node communication. Decouples systems.

## Lifecycle Methods

```gdscript
extends Node2D

func _ready() -> void:           # called once when node enters the tree
    pass

func _enter_tree() -> void:      # called when node is added to tree (before _ready)
    pass

func _exit_tree() -> void:       # called when node is removed from tree
    pass

func _process(delta: float) -> void:  # every frame, variable delta
    pass

func _physics_process(delta: float) -> void:  # physics step, fixed delta
    pass

func _input(event: InputEvent) -> void:       # every input event
    pass

func _unhandled_input(event: InputEvent) -> void:  # input not consumed by UI
    pass
```

**Rule:** `_physics_process` for physics/movement, `_process` for visuals/UI, `_input` for input.

## Player Controller (2D)

```gdscript
# player.gd
extends CharacterBody2D

@export var speed: float = 200.0
@export var jump_velocity: float = -400.0
@export var gravity: float = 980.0

@onready var _animated: AnimatedSprite2D = $AnimatedSprite2D

func _physics_process(delta: float) -> void:
    # Gravity
    if not is_on_floor():
        velocity.y += gravity * delta

    # Jump
    if Input.is_action_just_pressed("jump") and is_on_floor():
        velocity.y = jump_velocity

    # Horizontal movement
    var direction := Input.get_axis("move_left", "move_right")
    velocity.x = direction * speed

    # Flip sprite
    if direction != 0:
        _animated.flip_h = direction < 0
        _animated.play("run")
    else:
        _animated.play("idle")

    move_and_slide()
```

Input actions are defined in **Project Settings → Input Map**.

## Scene Instancing

```gdscript
# spawning a bullet
const BulletScene := preload("res://scenes/bullet.tscn")

func fire() -> void:
    var bullet: CharacterBody2D = BulletScene.instantiate()
    bullet.global_position = $GunSpawn.global_position
    bullet.velocity = (get_global_mouse_position() - bullet.global_position).normalized() * 400
    get_tree().current_scene.add_child(bullet)
```

For pooling, reuse instances instead of `instantiate()`/`queue_free()`.

## C# in Godot

```csharp
// Player.cs
using Godot;

public partial class Player : CharacterBody2D
{
    [Export] public float Speed { get; set; } = 200.0f;

    private AnimatedSprite2D _animated;

    public override void _Ready()
    {
        _animated = GetNode<AnimatedSprite2D>("AnimatedSprite2D");
    }

    public override void _PhysicsProcess(double delta)
    {
        var direction = Input.GetVector("move_left", "move_right", "move_up", "move_down");
        Velocity = direction * Speed;
        MoveAndSlide();
    }
}
```

C# requires the **.NET version of Godot** (separate download). Otherwise identical to GDScript patterns.

## GDExtension (C++ / Rust / Python)

For performance-critical code or porting existing C++ libraries:

1. Use **SConstruct** + `godot-cpp` to build a `.so` / `.dll`
2. Register classes via `GDREGISTER_CLASS`
3. Use in GDScript like any other class

```cpp
// example.cpp
#include <godot_cpp/classes/node2d.hpp>

namespace godot {
class MyCustom : public Node2D {
    GDCLASS(MyCustom, Node2D)

protected:
    static void _bind_methods() {
        ClassDB::bind_method(D_METHOD("get_count"), &MyCustom::get_count);
    }

public:
    int get_count() const { return count_; }
private:
    int count_ = 0;
};
}
```

Use GDExtension when:
- Hot loop performance matters (10x+ speedup over GDScript)
- You need to integrate a C++ library (Box2D, Bullet, custom physics)
- You're porting from Unity/C# and want C#-equivalent performance

For most gameplay, GDScript is fine.

## Rendering Pipeline

| Renderer | Use |
|---|---|
| **Forward+** | Default 3D; high quality; modern |
| **Mobile** | Mobile / web; optimized |
| **Compatibility** (formerly GLES2) | Old hardware / maximum compatibility |

Configure in **Project Settings → Rendering → Renderer**.

### Shaders (Godot Shading Language)

```
// sprite_flash.shader
shader_type canvas_item;

uniform vec4 flash_color : source_color = vec4(1.0);
uniform float flash_amount : hint_range(0.0, 1.0) = 0.0;

void fragment() {
    vec4 base = texture(TEXTURE, UV);
    COLOR = mix(base, flash_color, flash_amount);
}
```

Apply to a Sprite2D via `Material → ShaderMaterial → Shader`. Update uniforms from GDScript:

```gdscript
func flash() -> void:
    var mat: ShaderMaterial = $Sprite2D.material
    mat.set_shader_parameter("flash_amount", 1.0)
    var tween := create_tween()
    tween.tween_property(mat, "shader_parameter/flash_amount", 0.0, 0.3)
```

## Profiling

**Debugger → Monitors** (real-time) or **Debugger → Profiler** (recorded):
- **Frame Time** — total per frame
- **Physics** — physics step
- **Process** — `_process` callbacks
- **Node count** — total nodes (high = slow)
- **Object count** — total objects

### Common Bottlenecks

| Bottleneck | Symptom | Fix |
|---|---|---|
| `instantiate()` + `queue_free()` for bullets | Hitches | Pool |
| `get_node()` in `_process` | Slow | Cache in `_ready` with `@onready` |
| String-based input | Slow + typos | Use Input Map actions |
| No `@onready` | Repeated lookups | Cache references |
| Heavy shader | Slow GPU | Simplify |
| Too many CanvasItems | High draw calls | Use `MultiMeshInstance2D` for repeats |

## Export

```bash
# Headless export for CI
godot --headless --export-release "Linux/X11" game.x86_64
godot --headless --export-release "Web" web/index.html
```

Configure export presets in **Project → Export**. Each platform has its own preset.

### Web Export

Godot has excellent web export (single HTML+JS+CSS). Tips:
- Use the **Compatibility** or **Mobile** renderer for web
- Enable **Thread support** cautiously (some browsers)
- Compress with Brotli for ~70% size reduction

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| `get_node()` in `_process` | Slow | Use `@onready` |
| `queue_free()` for repeated bullets | GC pauses | Pool |
| Forgetting to call `move_and_slide()` | Player doesn't move | Always call after setting velocity |
| No `await` for async | Race conditions | Use `await` with `await get_tree().create_timer(1.0).timeout` |
| String comparisons in `_process` | Slow | Use enums or type checks |
| Missing `super()` overrides | Broken behavior | Call `super._ready()`, `super._process(delta)` |
| `Engine.get_process_frames()` for timing | Wrong | Use `delta` time |
| Heavy `_input` handler | Input lag | Use `_unhandled_input` for game input; let UI handle `_input` |
