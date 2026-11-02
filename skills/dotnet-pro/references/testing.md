# Testing in .NET

## xUnit — the default

```csharp
public class CalculatorTests
{
    [Fact]
    public void Add_returns_sum()
    {
        var calc = new Calculator();
        calc.Add(2, 3).Should().Be(5);
    }

    [Theory]
    [InlineData(1, 2, 3)]
    [InlineData(10, 20, 30)]
    [InlineData(-1, -2, -3)]
    public void Add_sums_correctly(int a, int b, int expected)
    {
        new Calculator().Add(a, b).Should().Be(expected);
    }
}
```

`[Fact]` for single cases, `[Theory]` + `[InlineData]` for parametric. xUnit is the most common; NUnit and MSTest also work.

## FluentAssertions

```csharp
result.Should().Be(5);
list.Should().HaveCount(3).And.ContainInOrder(1, 2, 3);
action.Should().ThrowAsync<ValidationException>();
dto.Should().BeEquivalentTo(new ProductDto(1, "x", 9.99m));
```

More readable than `Assert.Equal(5, result)`.

## `WebApplicationFactory` — integration tests

```csharp
public class ApiTests : IClassFixture<WebApplicationFactory<Program>>
{
    private readonly HttpClient _client;

    public ApiTests(WebApplicationFactory<Program> factory)
    {
        _client = factory.WithWebHostBuilder(b =>
        {
            b.ConfigureServices(services =>
            {
                // Replace real DB with in-memory or Testcontainers
                services.RemoveAll<DbContextOptions<AppDbContext>>();
                services.AddDbContext<AppDbContext>(o => o.UseSqlite("DataSource=:memory:"));
            });
        }).CreateClient();
    }

    [Fact]
    public async Task GetProduct_returns_200_for_existing()
    {
        var resp = await _client.GetAsync("/products/1");
        resp.EnsureSuccessStatusCode();
        var dto = await resp.Content.ReadFromJsonAsync<ProductDto>();
        dto.Should().NotBeNull();
        dto!.Name.Should().Be("Widget");
    }
}
```

`WebApplicationFactory<Program>` boots the whole app in-process. You can override any service registration.

## Testcontainers — real DB in tests

```csharp
public class DbFixture : IAsyncLifetime
{
    public PostgreSqlContainer Container { get; } = new PostgreSqlBuilder()
        .WithImage("postgres:16-alpine")
        .Build();

    public string ConnectionString => Container.GetConnectionString();

    public async Task InitializeAsync()
    {
        await Container.StartAsync();
        // Run migrations
    }

    public Task DisposeAsync() => Container.DisposeAsync().AsTask();
}

public class RepoTests : IClassFixture<DbFixture>
{
    private readonly DbFixture _fx;
    public RepoTests(DbFixture fx) => _fx = fx;

    [Fact]
    public async Task Insert_and_query()
    {
        await using var db = new AppDbContext(/* options from _fx.ConnectionString */);
        // ...
    }
}
```

Testcontainers spins up a real Postgres/Redis/Kafka in Docker. Slower than SQLite-in-memory but catches dialect-specific bugs.

## Moq — mocking

```csharp
var repo = new Mock<IProductRepo>();
repo.Setup(r => r.FindAsync(42, It.IsAny<CancellationToken>()))
    .ReturnsAsync(new Product("Widget", 9.99m, 1));

var svc = new ProductService(repo.Object, Mock.Of<ILogger<ProductService>>());
var dto = await svc.GetAsync(42, default);

dto!.Name.Should().Be("Widget");
repo.Verify(r => r.FindAsync(42, It.IsAny<CancellationToken>()), Times.Once);
```

Mock interfaces, not concrete classes. Moq is the most popular; NSubstitute is a cleaner alternative.

## Snapshot testing — `Verify`

```csharp
[Fact]
public Task Serializes_correctly()
{
    var dto = new ProductDto(1, "Widget", 9.99m);
    return Verifier.Verify(dto);
}
```

`Verify` writes a `.received.txt` and diffs against `.verified.txt`. Run `dotnet test` then `dotnet verify` to accept changes.

## Benchmark.NET

```csharp
[MemoryDiagnoser]
public class Bench
{
    private readonly int[] _data = Enumerable.Range(0, 1000).ToArray();

    [Benchmark]
    public int LinqSum() => _data.Sum();

    [Benchmark]
    public int ForSum()
    {
        int s = 0;
        foreach (var x in _data) s += x;
        return s;
    }
}

// dotnet run -c Release --project MyBench
```

Run benchmarks in Release, with `--filter` to select cases.

## Coverage

```xml
<!-- in test .csproj -->
<ItemGroup>
  <PackageReference Include="coverlet.collector" Version="6.0.*" />
</ItemGroup>
```

```bash
dotnet test --collect:"XPlat Code Coverage"
# Generates TestResults/*/coverage.cobertura.xml
# View with reportgenerator: dotnet reportgenerator -reports:**/coverage.cobertura.xml -targetdir:coverage
```

Target 80%+ line coverage. Branch coverage matters more than line — review uncovered `if`/`switch` paths.

## Test patterns

### Arrange / Act / Assert
```csharp
[Fact]
public void Create_throws_on_empty_name()
{
    // Arrange
    Action act = () => new Product("", 9.99m, 1);

    // Act + Assert
    act.Should().Throw<ArgumentException>()
       .WithMessage("*name*");
}
```

### Builder for test data
```csharp
public class ProductBuilder
{
    private string _name = "Widget";
    private decimal _price = 9.99m;
    private int _categoryId = 1;

    public ProductBuilder WithName(string n) { _name = n; return this; }
    public ProductBuilder WithPrice(decimal p) { _price = p; return this; }
    public Product Build() => new(_name, _price, _categoryId);
}

var p = new ProductBuilder().WithName("Gadget").Build();
```

## Common pitfalls

- `Thread.Sleep` in tests — use `Task.Delay` with cancellation, or fake timers
- Mocking concrete classes — mock interfaces; if you must, mark methods `virtual`
- Testing implementation details — test behavior through public API
- `Assert.Equal` chains — use `FluentAssertions` `Should().Be().And.Be()`
- Shared state between tests — use constructor + `IClassFixture` for shared setup
- `dotnet test` in Debug — benchmarks need Release; tests can be either, but Release catches JIT issues
- Skipping tests via `#if` — use `[Trait("Category", "Slow")]` and filter with `--filter`
