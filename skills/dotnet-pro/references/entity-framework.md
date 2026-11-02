# Entity Framework Core 8

## DbContext

```csharp
public sealed class AppDbContext(DbContextOptions<AppDbContext> options) : DbContext(options)
{
    public DbSet<Product> Products => Set<Product>();
    public DbSet<Order> Orders => Set<Order>();

    protected override void OnModelCreating(ModelBuilder b)
    {
        b.ApplyConfigurationsFromAssembly(typeof(AppDbContext).Assembly);
        base.OnModelCreating(b);
    }
}
```

Registration:
```csharp
builder.Services.AddDbContext<AppDbContext>(o =>
    o.UseNpgsql(builder.Configuration.GetConnectionString("App")));
```

DbContext lifetime is **Scoped** — one per HTTP request. Never Singleton (not thread-safe).

## Entity + configuration

```csharp
public class Product
{
    public int Id { get; set; }
    public string Name { get; private set; } = "";
    public decimal Price { get; private set; }
    public int CategoryId { get; private set; }
    public Category Category { get; private set; } = null!;
    public List<OrderLine> OrderLines { get; private set; } = [];

    private Product() { }  // EF Core constructor

    public Product(string name, decimal price, int categoryId)
    {
        if (string.IsNullOrWhiteSpace(name)) throw new ArgumentException("name required");
        if (price <= 0) throw new ArgumentException("price must be positive");
        Name = name; Price = price; CategoryId = categoryId;
    }
}

public class ProductConfig : IEntityTypeConfiguration<Product>
{
    public void Configure(EntityTypeBuilder<Product> b)
    {
        b.ToTable("products");
        b.HasKey(x => x.Id);
        b.Property(x => x.Name).HasMaxLength(200).IsRequired();
        b.Property(x => x.Price).HasPrecision(10, 2);
        b.HasOne(x => x.Category).WithMany().HasForeignKey(x => x.CategoryId);
        b.HasIndex(x => x.Name);
    }
}
```

Private setters + private constructor enforce invariants while letting EF materialize entities.

## Migrations

```bash
dotnet ef migrations add InitialCreate --project src/MyApp.Infrastructure --startup-project src/MyApp.Api
dotnet ef database update
```

Review the generated migration file before applying:
- Did it drop any columns? Why?
- Are indexes created correctly?
- Are foreign keys present?

Roll back:
```bash
dotnet ef migrations remove      # if not applied yet
dotnet ef database update <Prev> # if applied
```

## Querying — read patterns

```csharp
// Read-only: AsNoTracking, project to DTO
public async Task<ProductDto?> GetAsync(int id, CancellationToken ct) =>
    await db.Products.AsNoTracking()
        .Where(p => p.Id == id)
        .Select(p => new ProductDto(p.Id, p.Name, p.Price))
        .FirstOrDefaultAsync(ct);

// Avoid N+1: use Include / Select
public async Task<List<OrderDto>> ListOrdersAsync(int customerId, CancellationToken ct) =>
    await db.Orders.AsNoTracking()
        .Where(o => o.CustomerId == customerId)
        .Select(o => new OrderDto(
            o.Id,
            o.Total,
            o.Lines.Select(l => new LineDto(l.Id, l.Product.Name, l.Quantity)).ToList()))
        .ToListAsync(ct);
```

`AsNoTracking()` skips change tracking — 2-3x faster for read-only queries. Project to DTOs in the query to avoid pulling full entities.

## N+1 prevention

```csharp
// BAD — N+1 queries
foreach (var order in db.Orders)
    Console.WriteLine(order.Customer.Name);  // query per order

// GOOD — eager load via Include or Select
var orders = db.Orders.Include(o => o.Customer).ToList();

// GOOD — projection (best — no full entity materialization)
var names = db.Orders.Select(o => o.Customer.Name).ToList();
```

## `Split queries` for multiple collections

```csharp
var orders = await db.Orders
    .Include(o => o.Lines)
    .ThenInclude(l => l.Product)
    .AsSplitQuery()  // separate SQL per collection — avoids cartesian explosion
    .ToListAsync(ct);
```

Default (single query) joins everything; if you have many `Include`s with collections, the result set multiplies. `AsSplitQuery` runs separate queries — often faster.

## Transactions

```csharp
public async Task TransferAsync(int from, int to, decimal amount, CancellationToken ct)
{
    using var tx = await db.Database.BeginTransactionAsync(ct);
    try
    {
        // ... multiple writes ...
        await db.SaveChangesAsync(ct);
        await tx.CommitAsync(ct);
    }
    catch
    {
        await tx.RollbackAsync(ct);
        throw;
    }
}

// Or: single SaveChangesAsync is already a transaction
```

`SaveChangesAsync` is atomic. Use explicit transactions only when you need to span multiple `SaveChangesAsync` calls or include raw SQL.

## Concurrency tokens

```csharp
public class Product
{
    public int Id { get; set; }
    public string Name { get; set; } = "";
    [Timestamp] public byte[] RowVersion { get; set; } = [];
}

// On conflict, SaveChangesAsync throws DbUpdateConcurrencyException
try { await db.SaveChangesAsync(ct); }
catch (DbUpdateConcurrencyException)
{
    // reload, reapply, retry
}
```

## Compiled queries (hot paths)

```csharp
private static readonly Func<AppDbContext, int, CancellationToken, Task<ProductDto?>> _getProduct =
    EF.CompileAsyncQuery((AppDbContext db, int id, CancellationToken ct) =>
        db.Products.AsNoTracking()
            .Where(p => p.Id == id)
            .Select(p => new ProductDto(p.Id, p.Name, p.Price))
            .FirstOrDefault());

public Task<ProductDto?> GetAsync(int id, CancellationToken ct) => _getProduct(db, id, ct);
```

Compiles the query once; reuses the plan. Use only for queries called thousands of times per second.

## Common pitfalls

- `db.Products.ToList()` returning entities — exposes schema, leaks change tracking; project to DTOs
- Forgetting `AsNoTracking()` on reads — slower, more memory
- `Include` everywhere — fetches more than needed; prefer `Select` projection
- `db.Products.FindAsync(id)` after `AsNoTracking()` — finds in change tracker first; for `AsNoTracking` use `Where(...).FirstOrDefaultAsync`
- Migrations out of sync with code — review `__EFMigrationsHistory` table
- DbContext as Singleton — thread-unsafe; must be Scoped
- `SaveChanges()` (sync) — use `SaveChangesAsync(ct)` always
