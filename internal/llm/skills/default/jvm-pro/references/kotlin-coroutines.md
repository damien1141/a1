# Kotlin Coroutines & Flow

## Structured concurrency — the rule

Every coroutine runs in a `CoroutineScope`. When the scope is cancelled, all child coroutines are cancelled. **Never use `GlobalScope`** in application code.

```kotlin
class UserService(
    private val api: UserApi,
    private val scope: CoroutineScope,
) {
    fun userFlow(id: String): Flow<UiState<User>> = flow {
        emit(UiState.Loading)
        try { emit(UiState.Success(api.fetch(id))) }
        catch (e: IOException) { emit(UiState.Error(e.message ?: "network")) }
    }.flowOn(Dispatchers.IO)
}
```

## Dispatchers

| Dispatcher | Use |
|---|---|
| `Dispatchers.Main` | UI thread (Android, JavaFX) |
| `Dispatchers.IO` | Blocking I/O (DB, file, network with blocking client) |
| `Dispatchers.Default` | CPU-bound work |
| `Dispatchers.Unconfined` | Testing only (rarely correct in production) |

`flowOn(Dispatchers.IO)` runs the upstream on IO. `withContext(Dispatchers.IO) { ... }` switches for a single call.

## `suspend` functions

```kotlin
suspend fun fetchUser(id: String): User = withContext(Dispatchers.IO) {
    api.fetch(id)
}

// Concurrent fan-out
suspend fun fetchAll(ids: List<String>): List<User> = coroutineScope {
    ids.map { async { fetchUser(it) } }.awaitAll()
}
```

`coroutineScope { ... }` waits for all children, cancels siblings on failure.

## `Flow` — cold async stream

```kotlin
fun numbers(): Flow<Int> = flow {
    for (i in 0..10) {
        emit(i)
        delay(100)
    }
}

numbers().collect { println(it) }
```

`Flow` is cold — nothing runs until `collect`. One collector per flow instance.

## `StateFlow` / `SharedFlow` — hot streams

```kotlin
class Counter {
    private val _count = MutableStateFlow(0)
    val count: StateFlow<Int> = _count.asStateFlow()

    fun inc() { _count.update { it + 1 } }
}

// StateFlow: always has a value, last-value-replay, conflated
// SharedFlow: optional replay, multi-cast to all collectors
```

Use `StateFlow` for "current state" (view model state). Use `SharedFlow` for events (one-shot notifications).

## Operators

```kotlin
flow
    .map { it * 2 }
    .filter { it > 5 }
    .debounce(100)
    .distinctUntilChanged()
    .onEach { println("got $it") }
    .launchIn(scope)  // hot — collects in background
```

Most operators mirror `Sequence`/`Stream`. `flowOn` switches dispatcher upstream.

## Cancellation

```kotlin
suspend fun work() {
    while (currentCoroutineContext().isActive) {
        doChunk()
        delay(100)  // cooperative suspension point
    }
}
```

Coroutines cooperate via suspension points (`delay`, `await`, `Flow.collect`). Long CPU work without a suspension point can't be cancelled — sprinkle `yield()` or use `ensureActive()`.

```kotlin
suspend fun work() {
    while (true) {
        ensureActive()  // throws CancellationException if cancelled
        doChunk()
    }
}
```

## `select` — race multiple sources

```kotlin
suspend fun firstResult(a: Deferred<T>, b: Deferred<T>): T = select {
    a.onAwait { it }
    b.onAwait { it }
}
```

## Channels (low-level, prefer Flow)

```kotlin
val ch = Channel<Int>(Channel.BUFFERED)
launch { ch.send(1); ch.close() }
for (v in ch) println(v)
```

Channels are hot — sender and receiver run concurrently. Use `Flow` for cold streams, `Channel` for producer-consumer.

## Exception handling

```kotlin
flow
    .catch { e -> emit(Result.Error(e)) }  // upstream-only
    .onEach { /* won't catch downstream errors */ }
    .collect()

// .catch only catches upstream exceptions
// For complete handling, use flow { } builder with try/catch
```

## `runTest` — testing coroutines

```kotlin
class UserRepoTest {
    @Test
    fun fetches_concurrently() = runTest {
        val repo = UserRepo(fakeApi)
        val users = repo.fetchAll(listOf("1", "2", "3"))
        assertEquals(3, users.size)
    }
}
```

`runTest` provides a virtual clock — `delay(1000)` returns instantly. Use `advanceTimeBy`/`runCurrent` for time-sensitive tests.

## Turbine — Flow testing

```kotlin
@Test
fun emits_loading_then_success() = runTest {
    userRepo.userFlow("1").test {
        assertEquals(UiState.Loading, awaitItem())
        assertEquals(UiState.Success(user), awaitItem())
        awaitComplete()
    }
}
```

Turbine lets you assert each emission in order.

## Ktor server (alternative to Spring)

```kotlin
fun Application.module() {
    routing {
        get("/users/{id}") {
            val id = call.parameters["id"]!!
            val user = userService.find(id)
            call.respond(user)
        }
    }
}
```

For Kotlin-native services, Ktor is lighter than Spring Boot and coroutine-native.

## Common pitfalls

- `GlobalScope.launch` — no parent; never cancelled; memory leak risk
- `runBlocking` in production — blocks the thread; only for `main()` or tests
- `async` without `await` — fire-and-forget; errors silently lost
- Holding a `Mutex` across suspension — works but consider `Semaphore` for fairness
- `Dispatchers.Main` in backend code — only valid on Android/JavaFX
- Swallowing `CancellationException` — breaks structured concurrency; always re-throw
- `Flow` collected multiple times — cold flow re-runs each time; that's expected
- `MutableStateFlow.update` vs `+=` — `+=` is not atomic; use `update`
