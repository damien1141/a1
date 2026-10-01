# Performance Reference

Object pooling, draw call batching, LOD, profiling, memory, and GC — the techniques that get a game from "runs on dev PC" to "stable 60 FPS on min-spec hardware." Engine-agnostic patterns + engine-specific tooling.

## Performance Budget

Define before optimizing:

| Platform | Target FPS | Frame budget | Memory |
|---|---|---|---|
| Console (PS5/XSX) | 60 | 16.6ms | 12–16GB |
| Console (Switch) | 30–60 | 16.6–33ms | 4GB |
| PC (min spec) | 60 | 16.6ms | 8GB |
| Mobile (mid) | 30–60 | 16.6–33ms | 2–4GB |
| Mobile (low-end) | 30 | 33ms | 1–2GB |
| VR (Quest 3) | 72–90 | 11–13ms | 6–8GB |

**Rule:** Profile on min-spec hardware, not your dev machine. A 5x-faster dev PC hides 5x the problems.

## Profiling Tools

| Engine | Tool |
|---|---|
| **Unity** | Window → Analysis → Profiler; Frame Debugger; Memory Profiler (package) |
| **Unreal** | Unreal Insights (`unrealinsights.exe`); Stat commands (`stat unit`, `stat gpu`) |
| **Godot** | Debugger → Profiler; Debugger → Monitors; `--print-fps` |
| **RenderDoc** | Cross-engine GPU frame capture (free) |
| **Pix (Windows)** | Microsoft's GPU profiler for DX12 |
| **Xcode Instruments** | macOS / iOS CPU + GPU profiler |

### Reading a Profile

1. **Identify the bottleneck**: CPU or GPU?
   - CPU: `Update`, `FixedUpdate`, `Tick`, `AI`, `Physics.Simulate`
   - GPU: `Camera.Render`, `RenderPass`, draw calls
2. **Find the spike**: longest frame section
3. **Drill into the spike**: which function?
4. **Measure before and after each fix**

**Iron rule:** one change at a time. Otherwise you don't know what helped.

## Object Pooling

Avoid runtime `Instantiate`/`Destroy` (Unity), `SpawnActor`/`Destroy` (Unreal), `instantiate`/`queue_free` (Godot). They cause:
- Allocation (GC pressure)
- Fragmentation
- Frame hitches

```csharp
// Unity
public class Pool<T> where T : Component
{
    private readonly Queue<T> _pool = new();
    private readonly T _prefab;

    public Pool(T prefab, int initialSize)
    {
        _prefab = prefab;
        for (int i = 0; i < initialSize; i++)
        {
            var item = Object.Instantiate(prefab);
            item.gameObject.SetActive(false);
            _pool.Enqueue(item);
        }
    }

    public T Get()
    {
        T item = _pool.Count > 0 ? _pool.Dequeue() : Object.Instantiate(_prefab);
        item.gameObject.SetActive(true);
        return item;
    }

    public void Release(T item)
    {
        item.gameObject.SetActive(false);
        _pool.Enqueue(item);
    }
}
```

**Pool anything instantiated more than a few times per second:** bullets, enemies, particles, damage numbers, audio sources.

## Draw Call Batching

Each draw call has CPU overhead (state setup). Reducing draw calls is often more important than reducing triangles.

### Unity

| Technique | Saves | When |
|---|---|---|
| **Static batching** | Many | Static geometry (mark "Static" in inspector) |
| **Dynamic batching** | Some | Small meshes (<300 verts); auto |
| **GPU instancing** | Big | Same mesh + material, many instances (trees, grass) |
| **SRP Batcher** | Big | URP/HDRP; batches by shader, not by mesh |
| **Texture atlasing** | Some | Combine textures to reduce material switches |

```csharp
// Enable GPU instancing on a material
material.enableInstancing = true;
```

### Unreal

- **Instance Static Mesh** component — many instances of one mesh (foliage, debris)
- **Hierarchical Instanced Static Mesh (HISM)** — adds LOD + culling
- **Nanite** — virtualized geometry; auto-LODs at runtime
- **Merge Actors** tool — combine static meshes at build time

### Godot

- **MultiMeshInstance2D/3D** — same mesh + material, many instances
- Use for grass, particles, repeated props

### Set-Pass Calls

A "set-pass call" = a shader/material switch. Reducing these is the goal. Tools:
- Unity Frame Debugger shows set-pass count
- Unreal `stat scenerendering` shows `Relevant Primitives` and `Draws`
- Godot `--render-driver opengl3 --verbose` shows draw calls

**Target:** <1000 draw calls on mobile, <3000 on PC.

## LOD (Level of Detail)

Swap to lower-detail meshes at distance.

```
LOD0 (0-10m):    10,000 tris    (hero detail)
LOD1 (10-30m):   3,000 tris
LOD2 (30-80m):   800 tris
LOD3 (80m+):     billboard (always faces camera)
```

| Engine | How |
|---|---|
| **Unity** | LOD Group component; assign LOD meshes |
| **Unreal** | Static Mesh editor → LOD Settings; or Nanite (auto) |
| **Godot** | LOD Group node; or `geometry_lod_distance` in mesh |

### LOD Generation

- **Auto-generate** in engine importers (decimate + simplify)
- **Manual** for hero assets (artist-authored)
- **Nanite** (Unreal 5) — auto-decimates at runtime; no LODs needed

## Occlusion Culling

Don't render what's behind walls.

| Engine | How |
|---|---|
| **Unity** | Window → Rendering → Occlusion Culling → Bake |
| **Unreal** | Auto (visibility bucket); or place Occlusion Volumes |
| **Godot** | `OccluderInstance3D` + `ArrayOccluder` |

For 2D: layer-based culling (only render what's near the camera).

## Texture Compression

| Format | Use |
|---|---|
| **ASTC** | Modern mobile (iOS + Android); best quality/size |
| **ETC2** | Older mobile (fallback) |
| **BC1/BC3/BC7** | Desktop (Windows/Mac/Linux); BC7 is highest quality |
| **PVRTC** | Legacy iOS (replaced by ASTC) |
| **Uncompressed** | UI / text; when quality is critical |

**Rule:** always compress textures for shipping. Uncompressed 4K texture = 16MB; BC7 = 4MB; ASTC 6x6 = 0.9MB.

## Memory Management

### Unity (C# GC)

The GC is the #1 cause of mobile hitches. Mitigations:

```csharp
// Avoid allocations in Update/FixedUpdate:
// WRONG — allocates a new array every frame
void Update() {
    var enemies = FindObjectsOfType<Enemy>();  // allocates + slow
}

// RIGHT — cache
private static readonly List<Enemy> _enemies = new();
void Update() {
    _enemies.Clear();
    // populate from a runtime set
}

// Avoid:
// - LINQ (allocates lambdas + enumerators)
// - String concatenation in hot paths (use StringBuilder)
// - `new` in Update
// - Foreach on List<T> in older Unity (allocates iterator; fixed in 2018+)

// Use:
// - Object pooling
// - `Stack<T>` / `Dictionary<T,U>` reused (not reallocated)
// - `GC.Collect()` at known idle moments (loading screens)
```

### Unreal (Manual + GC)

UObjects are garbage-collected; raw pointers are manual. Rules:
- `UPROPERTY()` on every UObject pointer in a class — or GC will collect it
- `TWeakObjectPtr<T>` for non-owning references
- `TSharedPtr<T>` / `TSharedRef<T>` for non-UObject shared ownership
- `MakeShared<T>()` / `MakeShareable()` for allocation
- Avoid raw `new`/`delete` for UObjects — use `NewObject<T>()` / `SpawnActor<T>()`

### Godot (Ref-counted)

- `RefCounted` objects (most engine types) — automatic ref counting
- `Object` (manual management) — call `free()` or use `queue_free()`
- Nodes in tree — managed by tree; remove with `queue_free()` (safe) or `free()` (immediate)

## GPU Performance

Common GPU bottlenecks + fixes:

| Bottleneck | Symptom | Fix |
|---|---|---|
| **Fill rate** | Heavy post-processing; large transparent quads | Reduce postFX quality; cut transparent overdraw |
| **Texture bandwidth** | 4K textures everywhere | Compress; use mip maps; reduce texture count |
| **Shader complexity** | Long fragment shaders | Simplify; use LOD material; precompute |
| **Geometry** | Too many triangles | LOD; Nanite; mesh decimation |
| **Draw calls** | Many small objects | Batch; instance; merge |
| **Overdraw** | Many transparent surfaces | Reduce particle count; use depth prepass |

## CPU Performance

| Bottleneck | Symptom | Fix |
|---|---|---|
| **Physics** | `Physics.Simulate` high | Reduce colliders; simplify collision meshes; lower fixed timestep rate |
| **AI** | Many AI thinking each frame | Tick AI every N frames; use LOD for AI |
| **Animation** | Many animators | Use rig LODs; reduce bone count; bake to FK |
| **Audio** | Many sources | Pool audio sources; use streaming for long clips |
| **GC** | Periodic hitches | Object pooling; avoid allocs in Update |
| **Serialization** | Save/load hitches | Async serialize; binary not JSON |

## Async Loading

Never block a frame on disk I/O.

```csharp
// Unity Addressables
var handle = Addressables.LoadAssetAsync<GameObject>("Boss");
await handle.Task;
var boss = Instantiate(handle.Result);

// Unreal
TSoftObjectPtr<UStaticMesh> MeshPtr = ...;
UStaticMesh* Mesh = MeshPtr.LoadSynchronous();  // BLOCKING - avoid
// Instead:
StreamableManager.LoadAsync(MeshPtr, FStreamableDelegate::CreateLambda([]() { ... }));
```

For level transitions: show a loading screen, async load the new scene, swap.

## Mobile-Specific

| Concern | Mitigation |
|---|---|
| **Thermal throttling** | Profile after 10+ min of play; not just cold start |
| **Memory pressure** | Stream content; unload aggressively; profile resident memory |
| **Battery** | Lower frame rate (30 vs 60) when not in combat |
| **Background app** | Pause heavy work; save state |
| **App size** | Use asset packs (Play) / on-demand resources (iOS) |
| **Slow storage** | Async load; cache aggressively |

## Profiling Workflow

1. **Reproduce the issue** — find the scenario that causes the spike
2. **Profile a release build** — debug builds are misleading (asserts, editor overhead)
3. **Identify CPU vs GPU** — use the right profiler
4. **Find the top 3 functions** — don't fix the 10th-biggest first
5. **Fix one** — smallest change
6. **Re-profile** — verify it actually helped
7. **Repeat** — until you hit the budget

### Anti-patterns

- "Optimizing" by feel (always profile first)
- Fixing the wrong bottleneck (CPU work on a GPU-bound game)
- Premature optimization (don't optimize code that isn't in the top 10)
- Micro-optimizations (1% gains) when macro gains (50%+) are available
- Trusting editor FPS (always profile a build)
