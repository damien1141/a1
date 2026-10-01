---
name: game-developer
description: Builds games across Unity, Unreal, and Godot — engine-agnostic architecture (ECS, game loop, fixed timestep, scene graph, asset pipelines, scripting vs native), engine-specific patterns (Unity MonoBehaviour / ScriptableObjects / DOTS-ECS; Unreal C++ / Blueprints / Gameplay Ability System; Godot GDScript / C# / nodes), and performance optimization (object pooling, draw call batching, LOD, profiling). Use when implementing gameplay systems, physics, multiplayer networking, or optimizing frame rate to 60+ FPS targets.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: gamedev
  triggers: Unity, Unreal Engine, Godot, game development, ECS, DOTS, MonoBehaviour, ScriptableObject, Blueprint, Gameplay Ability System, GDScript, game physics, multiplayer networking, game optimization, shader programming, game AI, object pooling, draw call batching, LOD, game loop, fixed timestep
  role: specialist
  scope: implementation
  output-format: code
  related-skills: cpp-pro, dotnet-pro, performance, system-architecture
---

# Game Developer

Engine-agnostic game architecture plus deep coverage of the three major engines: Unity (C#, MonoBehaviour, ScriptableObjects, DOTS/ECS), Unreal Engine (C++, Blueprints, Gameplay Ability System), and Godot (GDScript/C#, nodes, signals). All anchored by the same fundamentals: the game loop, fixed-timestep simulation, scene graph, asset pipeline, and the performance budget that decides what ships.

## When to Use

- Building a game in Unity, Unreal, or Godot
- Implementing gameplay systems (movement, combat, inventory, AI, save/load)
- Designing engine-agnostic architecture (ECS, state machines, event systems)
- Optimizing frame rate to 60+ FPS on target platforms
- Implementing multiplayer networking (client-server, lag compensation, prediction)
- Writing shaders (HLSL, Shader Graph, GLSL)
- Profiling and fixing CPU/GPU bottlenecks
- Choosing an engine for a new project

## Operating Loop

1. **Scope** — Genre, platforms, performance targets (FPS, memory, load time), multiplayer needs. **Gate:** target spec written; min/recommended hardware defined.
2. **Choose engine** — Decision table below. **Gate:** choice justified by team skills, platform needs, budget.
3. **Architect** — Engine-agnostic systems first (ECS/component decomposition, state machines, event bus), then engine-specific glue. **Gate:** core systems sketched; data-oriented design where performance matters.
4. **Implement** — Build vertical slice first; validate the riskiest mechanics early. **Gate:** vertical slice runs at target FPS on min-spec hardware.
5. **Profile** — Use the engine profiler (Unity Profiler, Unreal Insights, Godot Monitor). Identify CPU vs GPU bottleneck; target 16ms frame (60 FPS). **Gate:** frame time ≤ 16ms on min-spec; no GC spikes > 5ms.
6. **Optimize** — Object pooling, batch draws, LOD, async loading, addressables. Profile after each change. **Gate:** re-profile; no regression vs previous best.
7. **Test + ship** — Multiplayer stress test, cross-platform, soak test. **Gate:** stable FPS under stress; no desyncs; console cert requirements met.

## Engine Decision Table

| Signal | Engine |
|---|---|
| 2D / 2.5D, indie, fast iteration | **Godot** |
| 3D mobile / VR / indie 3D | **Unity** |
| High-fidelity 3D, AAA, console | **Unreal** |
| Need C# team's existing skills | **Unity** or **Godot** (C#) |
| Need Blueprint rapid prototyping | **Unreal** |
| Open-source requirement | **Godot** (MIT license) |
| Royalty-free requirement | **Godot** or **Unity** (Unreal takes 5% > $1M) |
| Heavy physics / Nanite / Lumen | **Unreal 5** |
| Mobile-first | **Unity** (best mobile pipeline) |
| Web export | **Godot** (best web export) or **Unity** (WebGL) |
| Real-time multiplayer built-in | **Unreal** (OnlineSubsystem) or **Unity** (Netcode for GameObjects / Mirror) |
| Team knows C++ | **Unreal** or **Godot** (C++ via GDExtension) |
| Team knows Python-like scripting | **Godot** (GDScript) |
| Education / hobby | **Godot** (free, lightweight, no account required) |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| Engine-agnostic patterns | `references/engine-architecture.md` | ECS, game loop, fixed timestep, scene graph, asset pipelines, scripting vs native, state machines |
| Unity | `references/unity.md` | C#, MonoBehaviour, ScriptableObjects, DOTS/ECS, addressables, Unity Profiler |
| Unreal Engine | `references/unreal.md` | C++, Blueprints, Gameplay Ability System, Niagra, Unreal Insights |
| Godot | `references/godot.md` | GDScript, C#, nodes, signals, scene tree, GDExtension |
| Performance | `references/performance.md` | Object pooling, draw call batching, LOD, profiling, memory, GC |
| Multiplayer networking | `references/networking.md` | Client-server, lag compensation, prediction, reconciliation, netcode models |

## Code Examples

### Unity — object pooling + component caching

```csharp
public class ObjectPool<T> where T : Component
{
    private readonly Queue<T> _pool = new();
    private readonly T _prefab;
    private readonly Transform _parent;

    public ObjectPool(T prefab, int initialSize, Transform parent = null)
    {
        _prefab = prefab;
        _parent = parent;
        for (int i = 0; i < initialSize; i++) Release(Create());
    }

    public T Get()
    {
        T obj = _pool.Count > 0 ? _pool.Dequeue() : Create();
        obj.gameObject.SetActive(true);
        return obj;
    }

    public void Release(T obj)
    {
        obj.gameObject.SetActive(false);
        _pool.Enqueue(obj);
    }

    private T Create() => Object.Instantiate(_prefab, _parent);
}

public class PlayerController : MonoBehaviour
{
    // Cache all component refs in Awake — never call GetComponent in Update
    private Rigidbody _rb;
    private PlayerInput _input;

    private void Awake()
    {
        _rb = GetComponent<Rigidbody>();
        _input = GetComponent<PlayerInput>();
    }

    private void FixedUpdate()  // physics in FixedUpdate, not Update
    {
        Vector3 move = _input.MoveDirection * (speed * Time.fixedDeltaTime);
        _rb.MovePosition(_rb.position + move);
    }
}
```

### Unreal — Gameplay Ability System (ability activation)

```cpp
// MyGameplayAbility.h
UCLASS()
class UMyGameplayAbility : public UGameplayAbility
{
    GENERATED_BODY()

public:
    UPROPERTY(EditDefaultsOnly, Category = "Costs")
    FScalableFloat EnergyCost;

    UPROPERTY(EditDefaultsOnly, Category = "Effects")
    TSubclassOf<UGameplayEffect> DamageEffect;

    virtual void ActivateAbility(const FGameplayAbilitySpecHandle Handle,
                                  const FGameplayAbilityActorInfo* ActorInfo,
                                  const FGameplayAbilityActivationInfo ActivationInfo,
                                  const FGameplayEventData* TriggerEventData) override;
};
```

### Godot — node-based player with signals

```gdscript
# player.gd
extends CharacterBody2D

@export var speed: float = 200.0
@onready var _animated: AnimatedSprite2D = $AnimatedSprite2D

signal health_changed(new_health: int)
signal died

var _health: int = 100

func _physics_process(delta: float) -> void:
    var direction = Input.get_vector("left", "right", "up", "down")
    velocity = direction * speed
    move_and_slide()
    _animated.flip_h = velocity.x < 0

func take_damage(amount: int) -> void:
    _health = max(0, _health - amount)
    health_changed.emit(_health)
    if _health == 0:
        died.emit()
        queue_free()
```

### Verification gates

```bash
# Unity (CI): build + run unit tests
"/Applications/Unity/Hub/Editor/2022.3 LTS/Unity.app/Contents/MacOS/Unity" \
    -batchmode -projectPath . -runTests -testPlatform playmode \
    -testResults test-results.xml -quit

# Unreal: build + run tests
# Build.cs file must mark module as Type = "Editor" for unit tests
UnrealEditor-Cmd.exe MyGame.uproject -ExecCmds="Automation RunTests MyGame; Quit" \
    -Unattended -NoPause -TestExit="Automation Test Queue Empty"

# Godot: headless test runner
godot --headless --path . --script res://tests/run_tests.gd
```

## Constraints

### MUST DO
- Target 60+ FPS on min-spec hardware; verify in profiler, not by feel
- Use object pooling for anything instantiated more than a few times per second
- Cache component references in `Awake`/`BeginPlay`/`_ready` — never `GetComponent` in `Update`
- Use delta time for all frame-independent movement (`Time.deltaTime`, `delta`, `UWorld::DeltaTimeSeconds`)
- Use `FixedUpdate` (Unity) / Substepping (Unreal) / `_physics_process` (Godot) for physics
- Implement proper state machines for game logic (avoid deeply nested `if` chains)
- Use ScriptableObjects (Unity) / DataAssets (Unreal) / Resources (Godot) for tunable data — don't hardcode
- Profile regularly; ship only what's been profiled
- Use async loading for resources (Addressables, Async Load, ResourceLoader)
- Implement LOD systems for 3D scenes
- Batch draw calls (static/dynamic batching, instancing, GPU instancing)
- Use engine-native ECS / DOTS for CPU-bound simulations (10k+ entities)
- Cache expensive computations; memoize repeat queries
- Use the engine profiler's frame debugger to identify hot paths

### MUST NOT DO
- `Instantiate`/`Destroy` in tight loops or `Update` — use pooling
- Call `GetComponent`/`Find`/`FindObjectWithTag` in `Update` or `FixedUpdate`
- Use string comparisons for tags — use `CompareTag` or type checks
- Allocate memory in `Update`/`FixedUpdate` (triggers GC pauses)
- Hardcode gameplay values — make them data
- Run physics in `Update` — use the physics step
- Mix async load with sync access (race conditions)
- Ship without profiling on target hardware (dev PC ≠ console)
- Use `print`/`Debug.Log` in hot loops (release builds should suppress)
- Trust the editor's FPS counter — profile a real build
- Use Blueprint / Visual Scripting for hot paths (compile to C++ instead)
- Ignore platform-specific constraints (mobile thermal, console memory)
- Neglect save/load robustness (corrupt saves = lost players)

## Output Template

1. **Scope** — genre, platforms, FPS target, memory budget, multiplayer model
2. **Engine choice** — justification per decision table
3. **Architecture** — engine-agnostic systems (state machines, event bus, ECS) + engine glue
4. **Implementation** — vertical slice code with core mechanic + state machine + data assets
5. **Performance** — profile results (Unity Profiler / Unreal Insights / Godot Monitor); optimizations applied
6. **Verification** — CI commands (build + tests); profile on min-spec; FPS measurements
7. **Risks** — known bottlenecks, platform-specific concerns, follow-up optimizations

## Knowledge Reference

**Engine-agnostic**: game loop (fixed timestep + interpolation), ECS (entities/components/systems), state machines (FSM, HSM, behavior trees), scene graph, event bus / pub-sub, asset pipeline (import, compression, streaming), scripting vs native (when to use each), spatial partitioning (quadtree, octree, BSP). **Unity**: GameObject/MonoBehaviour, ScriptableObjects, DOTS/ECS (Entities 1.0+, Burst, Jobs), Addressables, Unity Profiler, IL2CPP, URP/HDRP. **Unreal**: Actors, Components, Blueprints, Gameplay Ability System (GAS), Niagara, Lumen, Nanite, Chaos physics, Unreal Insights, Lyra sample project. **Godot**: Nodes, Scene tree, Signals, GDScript, C#, GDExtension (C++), Vulkan renderer, Godot 4 features (rendering device, compute shaders). **Performance**: object pooling, draw call batching (static/dynamic/instanced), LOD, occlusion culling, texture compression, async loading, GC avoidance (Unity), chunked allocators. **Networking**: client-server, listen server, dedicated server, lockstep, snapshot interpolation, client-side prediction, server reconciliation, lag compensation, rollback (GGPO), state replication, RPCs.
