# Unity Reference

Production patterns for Unity (2022 LTS / Unity 6): C#, MonoBehaviour, ScriptableObjects, DOTS/ECS, Addressables, and the Profiler. Read alongside `engine-architecture.md` for fundamentals.

## Project Structure

```
Assets/
├── _Project/                    # your game (prefixed to sort first)
│   ├── Art/                     # models, textures, materials
│   ├── Audio/
│   ├── Prefabs/
│   ├── Scenes/
│   ├── Scripts/
│   │   ├── Runtime/             # gameplay code (assemblies)
│   │   │   ├── Player/
│   │   │   ├── Combat/
│   │   │   └── UI/
│   │   └── Editor/              # custom inspector / tools
│   ├── ScriptableObjects/       # data assets
│   └── Settings/                # URP assets, input actions, etc.
├── Plugins/                     # third-party
└── TextMesh Pro/                # built-in packages
```

Use **Assembly Definitions** (.asmdef) to split code into modules. Speeds compile times and enforces dependencies.

## Lifecycle Methods

```csharp
public class PlayerController : MonoBehaviour
{
    // Order of execution (each frame):
    // Awake → OnEnable → Start → (FixedUpdate → Update → LateUpdate) → render

    private void Awake() { }        // once, before Start; use for component caching + init
    private void OnEnable() { }     // when enabled; subscribe to events here
    private void OnDisable() { }    // when disabled; unsubscribe to avoid leaks
    private void Start() { }        // once, after all Awakes; use for cross-object init
    private void FixedUpdate() { }  // physics — fixed timestep (default 0.02s = 50Hz)
    private void Update() { }       // gameplay — every frame, variable delta
    private void LateUpdate() { }   // after all Updates; camera follow, animations
    private void OnDestroy() { }    // cleanup
}
```

**Rule:** `FixedUpdate` for physics, `Update` for input/gameplay, `LateUpdate` for camera follow.

## Component Caching

```csharp
// WRONG — GetComponent every frame
void Update() {
    var rb = GetComponent<Rigidbody>();  // 100ns+ per call
    rb.MovePosition(...);
}

// RIGHT — cache in Awake
private Rigidbody _rb;

void Awake() {
    _rb = GetComponent<Rigidbody>();
}

void FixedUpdate() {
    _rb.MovePosition(...);
}
```

For frequently accessed components, use `[SerializeField] private` references assigned in the inspector:

```csharp
[SerializeField] private Rigidbody _rb;
[SerializeField] private Animator _animator;
[SerializeField] private Collider _collider;
```

Never `public` for fields — use `[SerializeField] private` to expose to inspector without exposing to other classes.

## ScriptableObjects (Data-Driven Design)

```csharp
[CreateAssetMenu(fileName = "NewWeapon", menuName = "Game/Weapon")]
public class WeaponData : ScriptableObject
{
    [Header("Stats")]
    public float damage = 10f;
    public float fireRate = 2f;        // shots per second
    public float range = 50f;

    [Header("Visuals")]
    public Sprite icon;
    public GameObject projectilePrefab;

    [Header("Audio")]
    public AudioClip fireSound;

    [Tooltip("Damage falloff curve over distance (0-1 normalized)")]
    public AnimationCurve damageFalloff = AnimationCurve.Linear(0, 1, 1, 0.5f);
}
```

**Why ScriptableObjects:**
- Designers tune values without code changes
- Different weapons = different assets (no inheritance)
- Build-time data (no JSON parsing at runtime)
- Shareable across scenes and prefabs

### Runtime Sets (event-driven architecture)

```csharp
[CreateAssetMenu(fileName = "RuntimeSet", menuName = "Game/RuntimeSet")]
public class GameObjectRuntimeSet : ScriptableObject
{
    private readonly List<GameObject> _items = new();
    public IReadOnlyList<GameObject> Items => _items;

    public void Add(GameObject go)
    {
        if (!_items.Contains(go)) _items.Add(go);
    }

    public void Remove(GameObject go)
    {
        if (_items.Contains(go)) _items.Remove(go);
    }
}

// On a component:
public class Enemy : MonoBehaviour
{
    [SerializeField] private GameObjectRuntimeSet _allEnemies;

    void OnEnable() => _allEnemies.Add(gameObject);
    void OnDisable() => _allEnemies.Remove(gameObject);
}
```

Now any system can query "all enemies" without `FindObjectsOfType` (which is slow).

## Object Pooling

```csharp
using System.Collections.Generic;
using UnityEngine;

public class ObjectPool : MonoBehaviour
{
    [SerializeField] private GameObject _prefab;
    [SerializeField] private int _initialSize = 16;
    [SerializeField] private Transform _parent;

    private readonly Queue<GameObject> _pool = new();

    private void Awake()
    {
        for (int i = 0; i < _initialSize; i++)
        {
            var go = Instantiate(_prefab, _parent);
            go.SetActive(false);
            _pool.Enqueue(go);
        }
    }

    public GameObject Get(Vector3 position, Quaternion rotation)
    {
        GameObject go = _pool.Count > 0 ? _pool.Dequeue() : Instantiate(_prefab, _parent);
        go.transform.SetPositionAndRotation(position, rotation);
        go.SetActive(true);
        return go;
    }

    public void Release(GameObject go)
    {
        go.SetActive(false);
        _pool.Enqueue(go);
    }
}

// Usage: pool a projectile
public class Projectile : MonoBehaviour
{
    [SerializeField] private float _lifetime = 3f;
    [SerializeField] private ObjectPool _pool;
    private float _expireTime;

    void OnEnable() => _expireTime = Time.time + _lifetime;

    void Update()
    {
        if (Time.time >= _expireTime)
            _pool.Release(gameObject);
    }
}
```

For production, use **Unity Pooling API** (1.0+ in `UnityEngine.Pool`) — same idea, official.

## DOTS / ECS (High-Performance)

Use DOTS when you have 10k+ entities (crowds, bullets, particles). Skip for typical gameplay.

```csharp
using Unity.Entities;
using Unity.Transforms;
using Unity.Mathematics;
using Unity.Burst;

// Component — pure data, struct, blittable
public struct Velocity : IComponentData
{
    public float3 Value;
}

// System — logic; Burst-compiled + Job-scheduled
[BurstCompile]
public partial struct MovementSystem : ISystem
{
    [BurstCompile]
    public void OnUpdate(ref SystemState state)
    {
        float dt = SystemAPI.Time.DeltaTime;
        foreach (var (transform, velocity) in
                 SystemAPI.Query<RefRW<LocalTransform>, RefRO<Velocity>>())
        {
            transform.ValueRW.Position += velocity.ValueRO.Value * dt;
        }
    }
}
```

**DOTS rules:**
- Components are structs (no references to managed objects)
- Systems are Burst-compiled (`[BurstCompile]`)
- Use `SystemAPI.Query` to iterate
- Run jobs in parallel for big speedups

## Input System (New)

```csharp
using UnityEngine.InputSystem;

public class PlayerInput : MonoBehaviour
{
    [SerializeField] private InputActionAsset _inputAsset;
    private InputAction _moveAction;
    private InputAction _fireAction;

    void Awake()
    {
        var gameplay = _inputAsset.FindActionMap("Gameplay");
        _moveAction = gameplay.FindAction("Move");
        _fireAction = gameplay.FindAction("Fire");
    }

    void OnEnable() { _moveAction.Enable(); _fireAction.Enable(); }
    void OnDisable() { _moveAction.Disable(); _fireAction.Disable(); }

    void Update()
    {
        Vector2 move = _moveAction.ReadValue<Vector2>();
        if (_fireAction.WasPressedThisFrame()) Fire();
    }
}
```

Define actions in an `Input Actions` asset (or use the new Input System component). Don't use the legacy `Input.GetKey` API for new projects.

## Addressables (Asset Management)

```csharp
using UnityEngine.AddressableAssets;
using UnityEngine.Resource.AsyncOperations;

public class LevelLoader : MonoBehaviour
{
    [SerializeField] private AssetReferenceGameObject _bossPrefab;

    public async void SpawnBoss()
    {
        var handle = Addressables.InstantiateAsync(_bossPrefab);
        await handle.Task;
        if (handle.Status == AsyncOperationStatus.Succeeded)
        {
            // boss is spawned
        }
        else
        {
            Debug.LogError($"Failed to load: {handle.OperationException}");
        }
    }

    public void ReleaseInstance(GameObject instance)
    {
        Addressables.ReleaseInstance(instance);
    }
}
```

Use Addressables for anything not always needed. Reduces initial load time + memory.

## Profiling

```csharp
using UnityEngine.Profiling;

public class ExpensiveSystem : MonoBehaviour
{
    void Update()
    {
        Profiler.BeginSample("AI.Think");
        ComputeAI();
        Profiler.EndSample();

        Profiler.BeginSample("Physics.Cast");
        Physics.OverlapSphere(...);
        Profiler.EndSample();
    }
}
```

Open **Window → Analysis → Profiler**. Record a few seconds of gameplay. Look for:
- **CPU spikes > 16ms** (60 FPS budget)
- **GC.Alloc > 0 in hot loops** (causes GC pauses)
- **Physics.Simulate** too high (reduce fixed timestep frequency or simplify colliders)
- **Camera.Render** too high (too many draw calls; batch or cull)

### Frame Debugger

**Window → Analysis → Frame Debugger** — see every draw call. Identify:
- Excess set-pass calls (material switches)
- Missing batching
- Shadow render passes

## Memory Management

```csharp
// Avoid GC allocations in hot loops:
// WRONG — allocates a new list each call
void Update() {
    var enemies = new List<Enemy>(FindObjectsOfType<Enemy>());
}

// RIGHT — reuse a cached list
private static readonly List<Enemy> _enemyBuffer = new();

void Update() {
    _enemyBuffer.Clear();
    // populate from a runtime set, not FindObjectsOfType
}

// Avoid LINQ in hot paths (allocates)
// WRONG
var active = enemies.Where(e => e.IsActive).Select(e => e.transform);

// RIGHT
foreach (var e in enemies) if (e.IsActive) Process(e.transform);

// Avoid string concatenation in hot paths
// Use StringBuilder or string interpolation (compiler optimizes)
```

### IL2CPP vs Mono

- **Mono** — JIT, fast iteration, larger build
- **IL2CPP** — AOT to C++, smaller build, faster execution, required for iOS

Always ship with IL2CPP. Use Mono only for editor iteration.

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| `GetComponent` in Update | Frame hitches | Cache in Awake |
| `Instantiate`/`Destroy` for bullets | GC pauses | Object pool |
| `FindObjectsOfType` in Update | Hitches | Runtime sets / cached references |
| `Resources.Load` for everything | Memory bloat | Addressables |
| String-based input | Slow; typos | New Input System |
| Physics in Update | Jitter | FixedUpdate |
| Public fields | Tight coupling | `[SerializeField] private` |
| LINQ in hot paths | GC allocs | Manual loops |
| Missing `[BurstCompile]` | Slow DOTS | Add the attribute |
| Deep prefab nesting | Slow load | Flatten prefabs |
