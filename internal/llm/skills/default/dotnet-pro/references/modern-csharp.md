# Modern C# 12

## Records — value-equal, immutable

```csharp
public record ProductDto(int Id, string Name, decimal Price);

// With-expressions — non-destructive mutation
var updated = product with { Price = 99.99m };

// Record struct — value type, mutable by default; use `readonly record struct` for immutability
public readonly record struct Point(int X, int Y);
```

Use records for DTOs, value objects, commands/queries. They give you `Equals`, `GetHashCode`, `ToString`, and `with` for free.

## Primary constructors (C# 12)

```csharp
public class ProductService(IProductRepo repo, ILogger<ProductService> log)
{
    public async Task<ProductDto?> GetAsync(int id, CancellationToken ct) =>
        await repo.FindAsync(id, ct);
}
```

Parameters are in scope for the entire class. No explicit constructor boilerplate.

## Collection expressions (C# 12)

```csharp
int[] xs = [1, 2, 3];
List<int> ys = [4, 5, 6];
Span<int> zs = [7, 8, 9];

// Spread
int[] combined = [.. xs, .. ys, 10];
```

Replaces `new[] { ... }` and `new List<int> { ... }`.

## `required` members (C# 11)

```csharp
public class Config
{
    public required string Host { get; init; }
    public int Port { get; init; } = 8080;
}

var c = new Config { Host = "localhost" };  // Port defaults; Host required
```

`required` forces the caller to set the property via object initializer. `init` makes it settable only at construction.

## Pattern matching

```csharp
// Property patterns
string label = order switch
{
    { Status: OrderStatus.Pending } => "Pending",
    { Status: OrderStatus.Shipped, Total: > 1000 } => "Big shipment",
    { Status: OrderStatus.Shipped } => "Shipped",
    _ => "Unknown"
};

// List patterns (C# 11)
int SumHead(int[] xs) => xs switch
{
    [] => 0,
    [var first, ..] => first,
};

// Type patterns
string Describe(object o) => o switch
{
    int i => $"int: {i}",
    string s => $"string: {s}",
    IEnumerable<object> e => $"seq: {e.Count()}",
    null => "null",
    _ => "other"
};
```

## File-scoped namespaces

```csharp
namespace MyApp.Services;  // single line, no braces

public class UserService { /* ... */ }
```

Always file-scoped in new code. Reduces indentation by one level.

## `Span<T>` and `Memory<T>` — zero-copy slicing

```csharp
void Process(ReadOnlySpan<byte> bytes) { /* ... */ }

byte[] buffer = new byte[1024];
Process(buffer);                  // implicit conversion
Process(buffer.AsSpan(10, 100));  // slice

// stackalloc for small temporary buffers
Span<int> tmp = stackalloc int[16];
```

Use `Span<T>` for sync code, `Memory<T>` for async (Span can't cross `await`). Avoids allocations on hot paths.

## Target-typed `new`

```csharp
List<int> xs = new();           // instead of new List<int>()
Dictionary<string, int> m = new() { ["a"] = 1 };
ProductService svc = new(repo, log);
```

## `init`-only setters

```csharp
public class User
{
    public string Email { get; init; }
    public string Name { get; init; }
}

var u = new User { Email = "a@b.c", Name = "Alice" };
// u.Email = "x";  // compile error — init only
```

## `global using`

```csharp
// GlobalUsings.cs
global using System;
global using System.Collections.Generic;
global using System.Linq;
global using System.Threading;
global using System.Threading.Tasks;
global using Microsoft.AspNetCore.Builder;
global using Microsoft.Extensions.DependencyInjection;
```

Or in `.csproj`:
```xml
<ItemGroup>
  <Using Include="System.Threading" />
</ItemGroup>
```

## Top-level statements (entry point)

```csharp
// Program.cs — entire file is the entry point
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddSwaggerGen();
var app = builder.Build();
app.Run();
```

No `Main` method boilerplate. Compiler synthesizes it. Combine with `global using` for minimal API entry points.

## `nameof`, `nameof<T>`, interpolated strings

```csharp
throw new ArgumentException("bad port", nameof(port));
Console.WriteLine($"User {user.Name} logged in at {DateTime.Now:O}");
```

## Common pitfalls

- `class` where `record` fits — records give value equality for free
- Public setters on DTOs — use `init` or `record`
- `var` for everything — explicit types improve readability for complex generics
- Forgetting `CancellationToken` — every async public method should accept one
- `.Result` on async — deadlock risk in ASP.NET; use `await`
- `IEnumerable<T>` materialization multiple times — use `.ToList()` once if you iterate twice
- `List<T>` as a public API return — use `IReadOnlyList<T>` for immutability
