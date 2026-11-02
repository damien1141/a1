# Engine-Agnostic Architecture Reference

The patterns that apply to every game engine: the game loop, fixed timestep, ECS, scene graph, state machines, asset pipelines, and the scripting-vs-native trade-off. Read this first; engine-specific references build on it.

## The Game Loop

Every game runs the same loop, ~60 times per second:

```
while (running) {
    process_input();
    update(delta_time);   // gameplay, physics, AI
    render();              // draw frame
    present();             // swap buffers
}
```

### Fixed Timestep vs Variable Delta

**Variable delta** (use real elapsed time each frame):
- Pro: smooths frame rate variations
- Con: physics becomes non-deterministic; spiral of death (slow frames compound)

**Fixed timestep** (simulation always advances by a fixed amount):
- Pro: deterministic physics; reproducible bugs; stable simulation
- Con: visual jitter (frame rate doesn't match sim rate)

**Hybrid (industry standard):**
```
accumulator += real_delta;
while (accumulator >= FIXED_DT) {
    physics_update(FIXED_DT);   // always 1/60s
    accumulator -= FIXED_DT;
}
render(accumulator / FIXED_DT);  // interpolate between physics steps
```

This gives deterministic simulation + smooth rendering. Unity's `FixedUpdate` uses this; Unreal's substepping does too; Godot's `_physics_process` is fixed.

### Spiral of Death

If a frame takes longer than `FIXED_DT`, the accumulator grows. The next frame runs multiple physics steps — taking even longer. Fix: cap the number of physics steps per frame (e.g., max 5); if you exceed, slow the simulation.

## Entity Component System (ECS)

Traditional OOP: deep inheritance (`Entity → Actor → Character → Player`). Fails at scale.

ECS: composition + data-oriented design.

| Concept | Role |
|---|---|
| **Entity** | An ID (just an integer); no behavior |
| **Component** | Pure data (position, health, mesh); no behavior |
| **System** | Logic that operates on all entities with a matching component set |

```
System: MovementSystem
  Queries: entities with (Position, Velocity)
  Each frame: position += velocity * dt
```

### Why ECS

- **Cache locality** — components stored in contiguous arrays; CPU prefetch works
- **Parallelism** — systems are independent; easy to multithread
- **Composition over inheritance** — mix and match components freely
- **Hot reload** — add/remove components at runtime changes behavior

### ECS in Practice

| Engine | ECS Implementation |
|---|---|
| **Unity** | DOTS / Entities 1.0+ (official, production-ready since 2023) |
| **Unreal** | Mass Entity (Unreal 5.0+) — for crowd simulations |
| **Godot** | No built-in ECS; use the node system (composition via nodes) |
| **Custom engine** | `bevy_ecs`, `entt` (C++), `legion` (Rust) |

For 10k+ entities (crowds, particles, RTS units), ECS is essential. For <1k entities, traditional OOP is fine and easier to reason about.

## State Machines

### Finite State Machine (FSM)

```csharp
public abstract class State
{
    public abstract void Enter();
    public abstract void Tick(float dt);
    public abstract void Exit();
}

public class StateMachine
{
    private State _current;
    public void TransitionTo(State next)
    {
        _current?.Exit();
        _current = next;
        _current.Enter();
    }
    public void Tick(float dt) => _current?.Tick(dt);
}

public class IdleState : State
{
    private readonly Animator _animator;
    public IdleState(Animator animator) => _animator = animator;
    public override void Enter() => _animator.SetTrigger("Idle");
    public override void Tick(float dt) { /* poll transitions */ }
    public override void Exit() { }
}
```

### Hierarchical State Machine (HSM)

States can have sub-states. E.g., `Grounded` → `Standing` / `Crouching` / `Sliding`. Sub-states inherit parent behavior. Useful for character controllers.

### Behavior Trees (AI)

For AI: a tree of nodes (Selector, Sequence, Action, Condition) that's easier to author and debug than nested FSMs. Built into Unreal (Behavior Tree); use a library in Unity (NodeCanvas, Behavior Designer).

## Scene Graph

Tree of objects that gets transformed each frame. Parent transforms propagate to children.

```
Scene
├── Camera
├── Lights
├── Player (transform: position, rotation, scale)
│   ├── Mesh
│   ├── Skeleton (bones)
│   └── WeaponSocket (child of right hand bone)
│       └── Sword
└── Enemies
    └── ...
```

When the player moves, the weapon socket follows automatically. This is how "attach weapon to hand" works.

## Asset Pipeline

Assets go from authoring format (FBX, PNG, WAV) → engine format → packaged.

### Stages

1. **Import** — engine reads source file
2. **Process** — compress, mip, optimize, generate LODs
3. **Cache** — store processed version in `Library/` (Unity), `_DerivedDataCache` (Unreal), `.godot/imported` (Godot)
4. **Reference** — scenes/code reference the asset
5. **Package** — bundle for distribution (.apk, .ipa, .exe + data)

### Best Practices

- **Source files in version control** (FBX, PSD, WAV) — not just the imported versions
- **Use Addressables (Unity) / Primary Data Assets (Unreal) / ResourceLoader (Godot)** for runtime loading
- **Compress textures per-platform** — ASTC for mobile, BC7 for desktop, ETC2 for older mobile
- **Generate LODs in the importer, not in the editor** — reproducible
- **Use asset bundles / pak files** to ship DLC or reduce initial download

### Asset Reference Patterns

- **Direct reference** — `public Mesh swordMesh;` (Unity inspector field). Simple, but loads at scene load.
- **Addressable / soft reference** — load on demand. Use for anything not always needed.
- **Resource folder (Unity)** — convenience, but everything in `Resources/` always loads. Avoid for large assets.
- **Async load** — never block a frame on disk I/O.

## Scripting vs Native Code

| Aspect | Scripting (C#/GDScript/Blueprint) | Native (C++/Burst/HLSL) |
|---|---|---|
| Iteration speed | Fast (hot reload) | Slow (recompile) |
| Performance | 10–100x slower than optimized C++ | Fast |
| Memory | GC (Unity) / ref-counted (Godot) / manual | Manual |
| Use for | Gameplay, prototyping, content-driven logic | Hot paths, math-heavy, custom rendering |
| Team | Designers can write | Engineers only |

**Rule:** prototype in scripting; port hot paths to native. Measure with the profiler first — don't optimize by feel.

### Unity-Specific

- **C# (MonoBehaviour)** — gameplay code
- **C# + Burst + Jobs (DOTS)** — high-performance simulations
- **C++ (Native Plugins)** — heavy compute, custom rendering
- **HLSL (Shader)** — GPU programs

### Unreal-Specific

- **Blueprints** — gameplay, rapid prototyping, designer-facing
- **C++ (Actor/Component)** — engine integration, performance-critical
- **Blueprint Native Events** — C++ base, Blueprint override (best of both)
- **HLSL (Material Editor / USF/USH)** — shaders

### Godot-Specific

- **GDScript** — gameplay, rapid prototyping (Python-like)
- **C#** — Unity migrants, performance-critical gameplay
- **GDExtension (C++)** — engine extensions, hot paths
- **GLSL** — shaders

## Event Bus / Pub-Sub

Decouples systems without hard references:

```csharp
// C# event bus
public static class EventBus
{
    private static readonly Dictionary<Type, List<Delegate>> _handlers = new();

    public static void Subscribe<T>(Action<T> handler)
    {
        if (!_handlers.ContainsKey(typeof(T))) _handlers[typeof(T)] = new List<Delegate>();
        _handlers[typeof(T)].Add(handler);
    }

    public static void Publish<T>(T evt)
    {
        if (_handlers.TryGetValue(typeof(T), out var handlers))
            foreach (var h in handlers) ((Action<T>)h)(evt);
    }
}

// Usage — no direct reference between Player and UI
public struct DamageEvent { public GameObject Target; public int Amount; }

// Player
EventBus.Publish(new DamageEvent { Target = enemy, Amount = 10 });

// UI (subscribes)
EventBus.Subscribe<DamageEvent>(e => UpdateHealthBar(e.Target, e.Amount));
```

Godot has built-in signals; Unreal has delegates. Use them.

## Spatial Partitioning

For "find all enemies within range" queries on large worlds:

| Structure | Use |
|---|---|
| **Uniform grid** | Many similar-size objects (particles, RTS units) |
| **Quadtree (2D) / Octree (3D)** | Variable density, dynamic objects |
| **BSP** | Static geometry (legacy, mostly replaced) |
| **BVH (Bounding Volume Hierarchy)** | Ray tracing, collision broadphase |

Most engines handle this internally (Unity's `Physics.OverlapSphere`, Unreal's `SphereOverlapActors`, Godot's `PhysicsDirectSpaceState3D`). Use the built-in; don't roll your own.

## Game Architecture Patterns

| Pattern | Use |
|---|---|
| **MVC** | UI-heavy games (model = game state, view = rendering, controller = input) |
| **Component (ECS-lite)** | Game objects with mixed behaviors (Unity GameObjects, Godot Nodes) |
| **Singleton** | Managers (AudioManager, GameManager) — but use sparingly; causes hidden coupling |
| **Service Locator** | Decoupled access to subsystems |
| **Object Pool** | Anything instantiated frequently (bullets, enemies, particles) |
| **Observer (events)** | Decouple cause (player died) from effect (UI updates, save game) |
| **Command** | Input → command → undo/redo; replays; networking |
| **Flyweight** | Shared immutable data (textures, meshes shared across instances) |

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Physics in Update | Jittery collision | Use FixedUpdate / substep |
| Variable delta in physics | Non-deterministic simulation | Fixed timestep |
| Deep inheritance hierarchies | Brittle; hard to extend | Composition (ECS / components) |
| Singleton overuse | Hidden coupling | Service locator / DI |
| Hardcoded values | Can't tune without recompile | ScriptableObjects / data assets |
| `Find` in Update | Frame hitches | Cache references |
| `Instantiate`/`Destroy` in loop | GC pauses | Object pooling |
| String-based event names | Typos, no refactor support | Typed events / structs |
| No asset versioning | Lost source files | Source assets in VCS |
| No profiling | "Feels slow" but unknown why | Use the engine profiler |
