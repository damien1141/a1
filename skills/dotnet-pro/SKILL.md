---
name: dotnet-pro
description: "Use when writing C# 12 / .NET 8 applications that must build clean under dotnet build -WarnAsError and pass dotnet test. Generates minimal APIs with CancellationToken, EF Core 8 with migrations, MediatR CQRS, record DTOs, JWT auth, and verifies with dotnet build + test + format before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "c#,.net,dotnet,asp.net core,ef core,mediatr,minimal api,linq,records,jwt,xunit"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "jvm-pro,golang-pro,api-design,system-architecture"
---

# .NET Pro

C# 12 + .NET 8 specialist. Nullable-enabled, async-with-`CancellationToken`, minimal-API-first, with a verification gate of `dotnet build -WarnAsError` + `dotnet test` + `dotnet format --verify`. Honest exit separates **VERIFIED** (ran a dotnet command, saw green) from **ASSUMED** (could not run).

## When to Use

- Building ASP.NET Core 8 APIs (Minimal or Controller-based) with EF Core 8
- Designing clean-architecture / CQRS apps with MediatR
- Using modern C# 12: records, primary constructors, collection expressions, pattern matching, `required` members
- Implementing async I/O with `CancellationToken` propagation end-to-end
- JWT/OAuth2 authentication, authorization policies
- Containerized cloud-native services (health checks, OpenTelemetry, AOT)

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this a query (read) or command (write)?"). State the .NET version (.NET 8) and C# language version (12).
2. **Recon** — Read `.csproj` files, `Program.cs`, `appsettings.json`, `Directory.Build.props`. Confirm nullable enabled, language version, NuGet packages.
3. **Design models first** — Records for DTOs, domain entities with private setters; commands/queries separated via MediatR. Sketch validation (FluentValidation).
4. **Implement** — Minimal API endpoints with `CancellationToken`; EF Core queries with `AsNoTracking()`; constructor injection; `await` every I/O.
5. **Verify (gate)** — In order, until clean:
   - `dotnet format --verify-no-changes` (or apply with `dotnet format`)
   - `dotnet build -WarnAsError` (configuration: Debug or Release per project)
   - `dotnet test --collect:"XPlat Code Coverage"` — all green, coverage met
   - For EF migrations: `dotnet ef migrations add <Name>` then review generated SQL
   - If any step fails: fix the cause, do not suppress warnings with `#pragma`. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each dotnet command + summary. **ASSUMED**: list what you believe but did not run (e.g. production perf, real DB behavior with seed data). Flag lingering risk (e.g. an `#pragma warning disable` with reason, an untested edge case, a migration needing manual SQL).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Modern C# 12 | `references/modern-csharp.md` | Records, primary constructors, pattern matching, collection expressions, `required`, `Span<T>` |
| ASP.NET Core | `references/aspnet-core.md` | Minimal APIs, middleware, DI, routing, problem details |
| EF Core 8 | `references/entity-framework.md` | DbContext, migrations, relationships, query optimization, `AsNoTracking` |
| Architecture & CQRS | `references/architecture.md` | Clean architecture, MediatR, domain events, vertical slice |
| Auth & cloud-native | `references/auth-cloud.md` | JWT, OAuth2, Identity, Docker, health checks, OpenTelemetry, AOT |
| Testing | `references/testing.md` | xUnit, `WebApplicationFactory`, Moq, FluentAssertions, Testcontainers |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, analyzer config |

## Constraints

### MUST DO
- Enable nullable reference types everywhere: `<Nullable>enable</Nullable>`
- Treat warnings as errors: `<TreatWarningsAsErrors>true</TreatWarningsAsErrors>`
- File-scoped namespaces, primary constructors (C# 12)
- `async`/`await` for all I/O; **always accept and forward `CancellationToken`**
- Constructor injection; never service locator
- Records for DTOs and value types
- `AsNoTracking()` on read-only EF queries
- Map EF entities to DTOs before returning from APIs — never expose entities
- `IOptions<T>` for strongly-typed config bound to records
- `dotnet format` clean

### MUST NOT DO
- Use `.Result` / `.Wait()` / `.GetAwaiter().GetResult()` in async code (deadlock risk)
- Disable nullable warnings with `!` without justification
- Skip `CancellationToken` on async methods
- Expose EF Core entities in API responses
- Use field injection (`[Inject]` on fields) — use constructors
- Hardcode config (use `appsettings.json` + env vars + `IOptions<T>`)
- Use legacy .NET Framework patterns
- Use `var x = new List<int>()` when `List<int> x = new()` reads clearer (target-typed `new`)
- Catch and swallow exceptions silently

## Code Examples

### Minimal API with CancellationToken + Result
```csharp
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddScoped<IProductService, ProductService>();
var app = builder.Build();

app.MapGet("/products/{id:int}", async (
    int id, IProductService svc, CancellationToken ct) =>
{
    var result = await svc.GetByIdAsync(id, ct);
    return result.IsSuccess
        ? Results.Ok(result.Value)
        : Results.NotFound(result.Error);
})
.WithName("GetProduct")
.WithOpenApi();

app.Run();
```

### Record DTO + Result pattern
```csharp
public readonly record struct Result<T>(T? Value, string? Error, bool IsSuccess)
{
    public static Result<T> Ok(T v) => new(v, null, true);
    public static Result<T> Fail(string e) => new(default, e, false);
}

public record ProductDto(int Id, string Name, decimal Price);
public record CreateProductCommand(string Name, decimal Price);
```

### EF Core DbContext with primary constructor (C# 12)
```csharp
public sealed class AppDbContext(DbContextOptions<AppDbContext> options) : DbContext(options)
{
    public DbSet<Product> Products => Set<Product>();

    protected override void OnModelCreating(ModelBuilder b) =>
        b.ApplyConfigurationsFromAssembly(typeof(AppDbContext).Assembly);
}

public async Task<ProductDto?> GetAsync(int id, CancellationToken ct) =>
    await db.Products.AsNoTracking()
        .Where(p => p.Id == id)
        .Select(p => new ProductDto(p.Id, p.Name, p.Price))
        .FirstOrDefaultAsync(ct);
```

### MediatR query handler
```csharp
public record GetProductQuery(int Id) : IRequest<Result<ProductDto>>;

public sealed class GetProductHandler(AppDbContext db)
    : IRequestHandler<GetProductQuery, Result<ProductDto>>
{
    public async Task<Result<ProductDto>> Handle(GetProductQuery q, CancellationToken ct)
    {
        var dto = await db.Products.AsNoTracking()
            .Where(p => p.Id == q.Id)
            .Select(p => new ProductDto(p.Id, p.Name, p.Price))
            .FirstOrDefaultAsync(ct);
        return dto is null
            ? Result<ProductDto>.Fail("not found")
            : Result<ProductDto>.Ok(dto);
    }
}
```

### `appsettings.json` + `IOptions<T>`
```json
{ "App": { "Port": 8080, "AllowedHosts": ["example.com"] } }
```
```csharp
public sealed record AppOptions(int Port, string[] AllowedHosts);

builder.Services.Configure<AppOptions>(builder.Configuration.GetSection("App"));
// Inject IOptions<AppOptions> where needed
```

## Output Template

When delivering a .NET feature: domain models + DTOs (records) → handlers/services → endpoints (`Program.cs`) → DbContext/migrations → test file → `dotnet` deltas → verification block (`dotnet build`, `dotnet test`) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

C# 12 · .NET 8 · ASP.NET Core Minimal APIs · EF Core 8 · MediatR · FluentValidation · `record`/`struct`/`readonly struct` · pattern matching · `required`/`init` · collection expressions · `Span<T>`/`Memory<T>` · `CancellationToken` · `IOptions<T>`/`IConfiguration` · JWT/OAuth2 · Serilog · xUnit · `WebApplicationFactory` · Testcontainers · OpenTelemetry · `dotnet format` · `dotnet ef` · Native AOT
