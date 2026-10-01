# Auth & Cloud-Native

## JWT authentication

```csharp
builder.Services.AddAuthentication(JwtBearerDefaults.AuthenticationScheme)
    .AddJwtBearer(opts =>
    {
        opts.TokenValidationParameters = new TokenValidationParameters
        {
            ValidateIssuer = true,
            ValidateAudience = true,
            ValidateLifetime = true,
            ValidateIssuerSigningKey = true,
            ValidIssuer = builder.Configuration["Jwt:Issuer"],
            ValidAudience = builder.Configuration["Jwt:Audience"],
            IssuerSigningKey = new SymmetricSecurityKey(
                Encoding.UTF8.GetBytes(builder.Configuration["Jwt:Key"]!))
        };
    });

builder.Services.AddAuthorization(opts =>
{
    opts.AddPolicy("admin", p => p.RequireRole("admin"));
    opts.AddPolicy("over18", p => p.RequireClaim("age", "18", "19", "20", "21"));
});

var app = builder.Build();
app.UseAuthentication();
app.UseAuthorization();
```

## Issuing a JWT

```csharp
public sealed class TokenService(IOptions<JwtOptions> opts)
{
    public string Issue(User user, IEnumerable<string> roles)
    {
        var claims = new List<Claim>
        {
            new(ClaimTypes.NameIdentifier, user.Id.ToString()),
            new(ClaimTypes.Name, user.Email),
        };
        claims.AddRange(roles.Select(r => new Claim(ClaimTypes.Role, r)));

        var key = new SymmetricSecurityKey(Encoding.UTF8.GetBytes(opts.Value.Key));
        var creds = new SigningCredentials(key, SecurityAlgorithms.HmacSha256);
        var token = new JwtSecurityToken(
            issuer: opts.Value.Issuer,
            audience: opts.Value.Audience,
            claims: claims,
            expires: DateTime.UtcNow.AddHours(1),
            signingCredentials: creds);
        return new JwtSecurityTokenHandler().WriteToken(token);
    }
}
```

- Use `HmacSha256` for symmetric keys; RSA/ECDSA for asymmetric
- Short-lived access tokens (15-60 min) + refresh tokens
- Never store secrets in `appsettings.json` — use env vars or Azure Key Vault / AWS Secrets Manager

## OAuth2 / OpenID Connect (external IdP)

```csharp
builder.Services.AddAuthentication(opts =>
{
    opts.DefaultScheme = CookieAuthenticationDefaults.AuthenticationScheme;
    opts.DefaultChallengeScheme = OpenIdConnectDefaults.AuthenticationScheme;
})
.AddCookie()
.AddOpenIdConnect(opts =>
{
    opts.Authority = builder.Configuration["Oidc:Authority"];
    opts.ClientId = builder.Configuration["Oidc:ClientId"];
    opts.ClientSecret = builder.Configuration["Oidc:ClientSecret"];
    opts.ResponseType = "code";
    opts.SaveTokens = true;
    opts.Scope.Add("openid");
    opts.Scope.Add("profile");
});
```

Use Auth0, Okta, Azure AD, or Keycloak as the IdP. Your app doesn't store passwords.

## Authorization attributes

```csharp
[Authorize]
public class AdminController : Controller { }

[Authorize(Policy = "admin")]
public IActionResult Delete(int id) { /* ... */ }

// Minimal API
app.MapDelete("/products/{id}", DeleteProduct)
   .RequireAuthorization("admin");
```

## Health checks

```csharp
builder.Services.AddHealthChecks()
    .AddNpgSql(builder.Configuration.GetConnectionString("App")!)
    .AddUrlGroup(new Uri("https://api.example.com/ping"), "external-api")
    .AddCheck<MemoryHealthCheck>("memory");

app.MapHealthChecks("/health");
// /health returns 200 OK if all checks pass
```

For detailed output (per-check status):
```csharp
app.MapHealthChecks("/health/detail", new HealthCheckOptions
{
    ResponseWriter = UIResponseWriter.WriteHealthCheckUIResponse
});
```

## OpenTelemetry

```csharp
builder.Services.AddOpenTelemetry()
    .WithTracing(tp => tp
        .AddAspNetCoreInstrumentation()
        .AddHttpClientInstrumentation()
        .AddNpgsqlInstrumentation()
        .AddOtlpExporter(o => o.Endpoint = new Uri("http://otel-collector:4317")))
    .WithMetrics(mp => mp
        .AddAspNetCoreInstrumentation()
        .AddRuntimeInstrumentation()
        .AddOtlpExporter());

builder.Logging.AddOpenTelemetry(o => o.AddOtlpExporter());
```

Sends traces, metrics, and logs to an OTLP collector (Jaeger, Tempo, Honeycomb, Datadog, etc.).

## Serilog

```csharp
builder.Host.UseSerilog((ctx, cfg) => cfg
    .ReadFrom.Configuration(ctx.Configuration)
    .Enrich.FromLogContext()
    .Enrich.WithMachineName()
    .WriteTo.Console(formatter: new CompactJsonFormatter()));

// Inject ILogger<T> everywhere
```

## Docker

```dockerfile
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
WORKDIR /src
COPY ["MyApp.Api/MyApp.Api.csproj", "MyApp.Api/"]
RUN dotnet restore "MyApp.Api/MyApp.Api.csproj"
COPY . .
WORKDIR /src/MyApp.Api
RUN dotnet publish -c Release -o /app /p:UseAppHost=false

FROM mcr.microsoft.com/dotnet/aspnet:8.0
WORKDIR /app
COPY --from=build /app .
USER $APP_UID
ENTRYPOINT ["dotnet", "MyApp.Api.dll"]
```

- Multi-stage build — small final image
- Run as non-root (`USER $APP_UID`)
- Use `--platform` for multi-arch

## Native AOT

```xml
<PropertyGroup>
  <PublishAot>true</PublishAot>
</PropertyGroup>
```

```bash
dotnet publish -c Release -r linux-x64
```

- Smaller binary, faster startup, lower memory
- Limitations: no reflection-heavy code (configure with `rd.xml`), no JIT-emit, some NuGet packages incompatible
- Best for CLIs and short-lived functions; less suitable for long-running web apps

## Configuration sources

```csharp
builder.Configuration
    .AddJsonFile("appsettings.json", optional: false)
    .AddJsonFile($"appsettings.{builder.Environment.EnvironmentName}.json", optional: true)
    .AddEnvironmentVariables()
    .AddUserSecrets<Program>(optional: true)  // dev only
    .AddAzureKeyVault(/* ... */);             // production
```

Order matters — later sources override earlier. Env vars win in production.

## Common pitfalls

- Storing JWT signing key in `appsettings.json` — use env var or secret manager
- `UseAuthentication()` after `UseAuthorization()` — must be the other way: authentication first
- No `app.UseHttpsRedirection()` in production behind TLS-terminating LB — causes redirect loops
- Missing health check for DB — LB sends traffic to a node with broken DB
- Long-lived JWTs without refresh tokens — can't revoke before expiry
- Building with `PublishAot=true` without testing reflection — runtime crashes
