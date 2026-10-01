# ASP.NET Core Minimal APIs

## Minimal API skeleton

```csharp
var builder = WebApplication.CreateBuilder(args);

builder.Services.AddEndpointsApiExplorer();
builder.Services.AddSwaggerGen();
builder.Services.AddScoped<IProductService, ProductService>();
builder.Services.AddDatabase(builder.Configuration);

var app = builder.Build();

if (app.Environment.IsDevelopment())
{
    app.UseSwagger();
    app.UseSwaggerUI();
}

app.MapGet("/products/{id:int}", GetProduct)
   .WithName("GetProduct")
   .WithOpenApi();

app.Run();
```

## Endpoint with full handler signature

```csharp
static async Task<IResult> GetProduct(
    int id,
    IProductService svc,
    CancellationToken ct)
{
    var result = await svc.GetByIdAsync(id, ct);
    return result.IsSuccess
        ? Results.Ok(result.Value)
        : Results.NotFound(result.Error);
}
```

Minimal APIs infer DI from method parameters: `IProductService`, `CancellationToken`, `HttpContext`, `ClaimsPrincipal`, `[FromRoute]`, `[FromQuery]`, `[FromBody]`.

## `Results.TypedResults` — typed responses

```csharp
app.MapGet("/users/{id}", async (int id, IUserService svc, CancellationToken ct) =>
{
    var user = await svc.GetAsync(id, ct);
    return user is null
        ? TypedResults.NotFound()
        : TypedResults.Ok(user);
})
.Produces<UserDto>()
.Produces(404);
```

`TypedResults.Ok(user)` returns `Results.Ok<UserDto>` — the return type is concrete, which improves OpenAPI generation and unit testing.

## Problem details (RFC 9457)

```csharp
builder.Services.AddProblemDetails(opts =>
{
    opts.CustomizeProblemDetails = ctx =>
    {
        ctx.ProblemDetails.Extensions["traceId"] = ctx.HttpContext.TraceIdentifier;
    };
});

app.UseExceptionHandler();
app.UseStatusCodePages();

// In an endpoint:
return Results.Problem(
    title: "Validation failed",
    statusCode: 400,
    detail: "Name is required");
```

`AddProblemDetails()` + `UseExceptionHandler()` gives you RFC 9457 errors for all unhandled exceptions and status codes.

## Middleware pipeline

```csharp
app.UseExceptionHandler();     // first
app.UseHttpsRedirection();
app.UseCors();
app.UseAuthentication();
app.UseAuthorization();
app.MapEndpoints();            // last
```

Order matters. Authentication before Authorization. Exception handler first to catch everything.

Custom middleware:
```csharp
app.Use(async (ctx, next) =>
{
    var sw = Stopwatch.StartNew();
    try { await next(); }
    finally { log.LogInformation("{Path} {Status} in {Ms}ms", ctx.Request.Path, ctx.Response.StatusCode, sw.ElapsedMilliseconds); }
});
```

## Dependency injection

```csharp
// Register
builder.Services.AddScoped<IProductService, ProductService>();
builder.Services.AddSingleton<IClock, SystemClock>();
builder.Services.AddTransient<IEmailSender, SmtpEmailSender>();

// Constructor injection (primary constructor C# 12)
public class ProductService(IProductRepo repo, ILogger<ProductService> log) { /* ... */ }
```

Lifetimes:
- `Singleton` — one instance for the app; must be thread-safe
- `Scoped` — one per request (HTTP request scope)
- `Transient` — new instance every time

Never inject a Singleton into a Scoped/Transient that holds it (captive dependency). Inject `IServiceScopeFactory` and create a scope if needed.

## Validation with FluentValidation

```csharp
public class CreateProductValidator : AbstractValidator<CreateProductCommand>
{
    public CreateProductValidator()
    {
        RuleFor(x => x.Name).NotEmpty().MaximumLength(200);
        RuleFor(x => x.Price).GreaterThan(0);
    }
}

builder.Services.AddValidatorsFromAssemblyContaining<CreateProductValidator>();

// In endpoint
app.MapPost("/products", async (CreateProductCommand cmd, IValidator<CreateProductCommand> validator, CancellationToken ct) =>
{
    var validation = await validator.ValidateAsync(cmd, ct);
    if (!validation.IsValid)
        return Results.ValidationProblem(validation.ToDictionary());
    // ...
});
```

## Routing + groups

```csharp
var products = app.MapGroup("/products")
    .WithTags("Products")
    .WithOpenApi();

products.MapGet("/", ListProducts);
products.MapGet("/{id:int}", GetProduct);
products.MapPost("/", CreateProduct);
```

Groups let you share tags, filters, and metadata across endpoints.

## Configuration

```csharp
public sealed record AppOptions(int Port, string[] AllowedHosts);

builder.Services.Configure<AppOptions>(builder.Configuration.GetSection("App"));

// Inject
public class Svc(IOptions<AppOptions> opts) { /* opts.Value.Port */ }

// IOptionsMonitor for hot-reload
public class Svc2(IOptionsMonitor<AppOptions> opts) { /* opts.CurrentValue */ }
```

## Common pitfalls

- `.Result` / `.Wait()` in async — deadlock in ASP.NET classic sync context (less of an issue in .NET 8, still forbidden)
- Forgetting `CancellationToken` — caller can't cancel the request
- Returning EF entities directly — leaks schema, may cause serialization cycles; map to DTOs
- Service locator (`serviceProvider.GetService<T>()`) — use constructor injection
- `async void` event handlers — exceptions crash the process; use `async Task` and fire-and-forget with care
- Registering DbContext as Singleton — must be Scoped (default with `AddDbContext`)
