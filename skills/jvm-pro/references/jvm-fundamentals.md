# JVM Fundamentals — Java 21 + Kotlin 2.x

## Java 21 records

```java
public record ProductDto(long id, String name, BigDecimal price) {}

// Compact constructor for validation
public record Port(int value) {
    public Port {
        if (value < 1 || value > 65535)
            throw new IllegalArgumentException("port out of range: " + value);
    }
}
```

Records give immutable carriers with `equals`/`hashCode`/`toString`. Use for DTOs, value objects, commands.

## Sealed classes (Java 17+)

```java
public sealed interface Shape permits Circle, Square, Triangle {}
public record Circle(double r) implements Shape {}
public record Square(double s) implements Shape {}
public record Triangle(double a, double b, double c) implements Shape {}

public double area(Shape s) {
    return switch (s) {
        case Circle c -> Math.PI * c.r() * c.r();
        case Square sq -> sq.s() * sq.s();
        case Triangle t -> /* heron */ 0;
    };
    // Exhaustive — no default needed
}
```

Sealed types + records + pattern matching = algebraic data types in Java.

## Pattern matching

```java
// Type patterns (Java 16+)
String format(Object o) {
    return switch (o) {
        case Integer i -> String.format("int %d", i);
        case String s -> "str " + s;
        case null -> "null";
        default -> "other";
    };
}

// Record patterns (Java 21)
String describe(ProductDto p) {
    return switch (p) {
        case ProductDto(long id, String name, _) when id < 0 -> "invalid: " + name;
        case ProductDto(long id, String name, BigDecimal price) -> name + " $" + price;
    };
}

// Pattern matching for instanceof (Java 16+)
if (o instanceof String s && s.length() > 5) {
    System.out.println(s.toUpperCase());
}
```

## Virtual threads (Java 21)

```java
// Per-request virtual thread — millions can run concurrently
try (var executor = Executors.newVirtualThreadPerTaskExecutor()) {
    List<Future<String>> futures = urls.stream()
        .map(url -> executor.submit(() -> fetch(url)))
        .toList();
    for (var f : futures) System.out.println(f.get());
}
```

Virtual threads are cheap (kilobytes, not megabytes). Spring Boot 3.2+ enables them with `spring.threads.virtual.enabled=true`.

## `HttpClient` (Java 11+)

```java
HttpClient client = HttpClient.newBuilder()
    .version(HttpClient.Version.HTTP_2)
    .connectTimeout(Duration.ofSeconds(10))
    .build();

HttpRequest req = HttpRequest.newBuilder()
    .uri(URI.create("https://api.example.com/users"))
    .timeout(Duration.ofSeconds(30))
    .header("Accept", "application/json")
    .GET()
    .build();

HttpResponse<String> resp = client.send(req, HttpResponse.BodyHandlers.ofString());
```

Replaces Apache HttpClient for most use cases. Supports sync and async (`sendAsync`).

## Text blocks

```java
String json = """
    {
      "name": "Alice",
      "age": 30
    }
    """;
```

## Switch expressions

```java
String label = switch (status) {
    case PENDING -> "Pending";
    case SHIPPED -> "Shipped";
    case DELIVERED -> "Delivered";
    case CANCELLED -> throw new IllegalStateException("cancelled");
};
```

## Kotlin 2.x essentials

### Data classes
```kotlin
data class Product(val id: Long, val name: String, val price: BigDecimal)

val (id, name, _) = Product(1, "Widget", BigDecimal("9.99"))
val updated = product.copy(price = BigDecimal("14.99"))
```

### Null safety
```kotlin
val name: String? = user?.profile?.name ?: "Anonymous"
user?.email?.let { sendEmail(it) }
val cfg = requireNotNull(System.getenv("CFG")) { "CFG required" }
```

### Scope functions
```kotlin
val request = HttpRequest().apply {
    url = "https://api.example.com"
    headers["Authorization"] = "Bearer $token"
}
val length = name?.let { it.trim().length } ?: 0
val user = create(form).also { log.info("created ${it.id}") }
```

### Extension functions
```kotlin
fun String.isEmail(): Boolean = contains("@") && contains(".")

val valid = "a@b.c".isEmail()
```

### Sealed classes
```kotlin
sealed class UiState<out T> {
    data object Loading : UiState<Nothing>()
    data class Success<T>(val data: T) : UiState<T>()
    data class Error(val msg: String, val cause: Throwable? = null) : UiState<Nothing>()
}

fun render(s: UiState<User>) = when (s) {
    is UiState.Loading -> spinner()
    is UiState.Success -> show(s.data)
    is UiState.Error -> showError(s.msg)
}  // exhaustive — compiler enforces
```

### Coroutines (preview — see kotlin-coroutines.md)
```kotlin
suspend fun fetchUser(id: String): User = withContext(Dispatchers.IO) {
    api.fetch(id)
}

val users = users.map { async { fetchUser(it) } }.awaitAll()
```

### `inline` value classes (Kotlin 2.x)
```kotlin
@JvmInline
value class UserId(val value: Long)

fun find(id: UserId): User?  // distinct from find(OrderId)
```

## Java/Kotlin interop

- Kotlin calls Java: nullable annotations on Java (`@Nullable`/`@NotNull`) improve Kotlin null-safety
- Java calls Kotlin: `KotlinClass.foo()` works; for `suspend` functions, Java sees a `Continuation` parameter — use `kotlinx-coroutines-jdk8` for `Future` adapters
- Avoid `internal` Kotlin visibility in code Java needs to call (Java sees it as public with name mangling)
- `@JvmStatic`, `@JvmOverloads`, `@JvmField` for ergonomic Java interop

## Common pitfalls

- `var x = new ArrayList<>()` (Java) — fine; but `var data = service.fetch()` loses type clarity
- Mutable Java records — records are immutable by design; use `final` fields
- Kotlin `data class` with `var` — defeats the purpose; use `val`
- Java `Optional<List<T>>` — empty list, not empty Optional
- Kotlin `GlobalScope.launch` — no structured concurrency; use a `CoroutineScope`
- Java `synchronized` blocks across virtual threads — pinning; use `ReentrantLock` instead
