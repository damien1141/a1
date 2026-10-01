# Architecture & CQRS with MediatR

## Clean architecture — layers

```
src/
├── MyApp.Domain/           # Entities, value objects, domain events; no deps
├── MyApp.Application/      # Use cases (commands/queries), DTOs, interfaces
├── MyApp.Infrastructure/   # EF Core, external services, implementations
└── MyApp.Api/              # ASP.NET Core endpoints, Program.cs
```

Dependency direction: `Api → Application → Domain`. `Infrastructure → Application` (implements interfaces defined in Application). Domain has no dependencies.

## Vertical slice — alternative

```
src/
├── Features/
│   ├── Products/
│   │   ├── ProductDtos.cs
│   │   ├── GetProduct.cs       # Query + Handler + Endpoint
│   │   ├── CreateProduct.cs    # Command + Handler + Endpoint
│   │   └── ProductConfig.cs    # EF config
│   └── Orders/
└── MyApp.Api/
    └── Program.cs              # registers feature modules
```

Vertical slice organizes by feature, not by layer. Easier to navigate for small/medium apps; harder to enforce layer constraints.

## MediatR — command/query separation

```csharp
// Query (read)
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
        return dto is null ? Result<ProductDto>.Fail("not found") : Result<ProductDto>.Ok(dto);
    }
}

// Command (write)
public record CreateProductCommand(string Name, decimal Price, int CategoryId)
    : IRequest<Result<int>>;

public sealed class CreateProductHandler(AppDbContext db)
    : IRequestHandler<CreateProductCommand, Result<int>>
{
    public async Task<Result<int>> Handle(CreateProductCommand cmd, CancellationToken ct)
    {
        var product = new Product(cmd.Name, cmd.Price, cmd.CategoryId);
        db.Products.Add(product);
        await db.SaveChangesAsync(ct);
        return Result<int>.Ok(product.Id);
    }
}
```

Registration:
```csharp
builder.Services.AddMediatR(cfg =>
    cfg.RegisterServicesFromAssembly(typeof(GetProductHandler).Assembly));
```

## Pipeline behaviors — cross-cutting

```csharp
public class LoggingBehavior<TRequest, TResponse> : IPipelineBehavior<TRequest, TResponse>
    where TRequest : notnull
{
    private readonly ILogger<LoggingBehavior<TRequest, TResponse>> _log;
    public LoggingBehavior(ILogger<LoggingBehavior<TRequest, TResponse>> log) => _log = log;

    public async Task<TResponse> Handle(TRequest req, RequestHandlerDelegate<TResponse> next, CancellationToken ct)
    {
        _log.LogInformation("Handling {Request}", typeof(TRequest).Name);
        try { return await next(); }
        finally { _log.LogInformation("Handled {Request}", typeof(TRequest).Name); }
    }
}

builder.Services.AddSingleton(typeof(IPipelineBehavior<,>), typeof(LoggingBehavior<,>));
```

Use for logging, validation, transactions, retry, metrics.

## Validation behavior

```csharp
public class ValidationBehavior<TRequest, TResponse> : IPipelineBehavior<TRequest, TResponse>
    where TRequest : notnull
{
    private readonly IEnumerable<IValidator<TRequest>> _validators;
    public ValidationBehavior(IEnumerable<IValidator<TRequest>> v) => _validators = v;

    public async Task<TResponse> Handle(TRequest req, RequestHandlerDelegate<TResponse> next, CancellationToken ct)
    {
        if (!_validators.Any()) return await next();
        var ctx = new ValidationContext<TRequest>(req);
        var results = await Task.WhenAll(_validators.Select(v => v.ValidateAsync(ctx, ct)));
        var failures = results.SelectMany(r => r.Errors).Where(f => f != null).ToList();
        if (failures.Count != 0)
            throw new ValidationException(failures);
        return await next();
    }
}
```

## Domain events

```csharp
public interface IDomainEvent { }

public record ProductCreated(int ProductId) : IDomainEvent;

public class Product
{
    private readonly List<IDomainEvent> _events = [];
    public IReadOnlyList<IDomainEvent> Events => _events;
    public void ClearEvents() => _events.Clear();

    public static Product Create(string name, decimal price)
    {
        var p = new Product(name, price);
        p._events.Add(new ProductCreated(p.Id));
        return p;
    }
}

// In SaveChangesAsync override, dispatch events
```

Domain events decouple side effects (send email, update projection, publish message) from the entity.

## Result pattern — return errors without exceptions

```csharp
public readonly record struct Result<T>(T? Value, string? Error, bool IsSuccess)
{
    public static Result<T> Ok(T v) => new(v, null, true);
    public static Result<T> Fail(string e) => new(default, e, false);
}
```

For **expected** failures (not found, validation, business rule), prefer `Result<T>` over throwing. Reserve exceptions for **unexpected** failures (DB down, network error).

## Project structure conventions

- One file per command/query/handler
- DTOs grouped by feature (`Products/Dtos.cs` or per file)
- EF configs alongside entity (`ProductConfig.cs`)
- Endpoint definitions alongside handlers in vertical slice

## Common pitfalls

- Domain entities with public setters — break invariants; use methods like `Rename(string)`
- Application layer referencing EF Core — couples to persistence; define repository interfaces in Application, implement in Infrastructure
- `async void` in handlers — must be `async Task<TResponse>`
- Throwing exceptions for "not found" — use `Result<T>.Fail` for expected failures
- Handlers with multiple responsibilities — split into smaller handlers
- Domain events dispatched before `SaveChanges` — dispatch after commit, or risk publishing events for rolled-back changes
