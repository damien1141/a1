# Framework Reference Cards

Quick syntax and pattern reference for the five most common test runners. Load this card when you need to write tests in a specific language.

## pytest (Python)

### Anatomy
```python
# tests/test_pricing.py
import pytest
from app.pricing import calculate_discount

@pytest.fixture
def premium_user() -> dict:
    return {"id": "1", "tier": "premium"}

def test_premium_discount(premium_user: dict) -> None:
    assert calculate_discount(price=100, user=premium_user) == 90

@pytest.mark.parametrize("price,expected", [(100, 90), (0, 0), (200, 180)])
def test_premium_discount_table(price: int, expected: int, premium_user: dict) -> None:
    assert calculate_discount(price=price, user=premium_user) == expected

@pytest.mark.asyncio
async def test_async_discount(premium_user: dict) -> None:
    result = await calculate_discount_async(price=100, user=premium_user)
    assert result == 90

def test_raises_on_negative(premium_user: dict) -> None:
    with pytest.raises(ValueError, match="non-negative"):
        calculate_discount(price=-1, user=premium_user)
```

### Fixtures — scope, params, teardown
```python
@pytest.fixture(scope="session")           # one per test session
def app_config() -> AppConfig: ...

@pytest.fixture(scope="function")          # default — fresh per test
def db_session():
    session = Session(engine)
    yield session                          # yield = provide + teardown
    session.close()

@pytest.fixture(params=["premium", "standard"])
def user_tier(request) -> str:
    return request.param                    # test runs once per param
```

### Markers
```python
@pytest.mark.slow                           # custom marker
def test_large_dataset(): ...

@pytest.mark.skip(reason="flaky on CI")
def test_known_flaky(): ...

@pytest.mark.skipif(sys.platform == "win32", reason="POSIX only")
def test_unix_paths(): ...
```
```toml
# pyproject.toml
[tool.pytest.ini_options]
addopts = "-ra --strict-markers"
markers = ["slow: marks tests as slow"]
```

### Run
```bash
pytest -q                                  # quiet
pytest tests/test_pricing.py -k premium    # by name pattern
pytest --cov=app --cov-branch --cov-fail-under=80
pytest -x                                  # stop on first failure
pytest --lf                                 # last-failed only
pytest -p no:cacheprovider                  # disable cache
```

### Mocking
```python
from unittest.mock import AsyncMock, Mock, patch, MagicMock

m = Mock()
m.method.return_value = 42
m.method(1, 2)
m.method.assert_called_once_with(1, 2)

with patch("app.pricing.requests.get") as mock_get:
    mock_get.return_value.json.return_value = {"price": 100}
    ...

# Async
repo = AsyncMock()
repo.find_by_id.return_value = {"id": "1"}
await repo.find_by_id("1")
repo.find_by_id.assert_awaited_once_with("1")
```

## Vitest (TypeScript / JavaScript)

Vitest is the modern Vite-native test runner; it's API-compatible with Jest for the common cases and significantly faster.

### Anatomy
```typescript
// src/pricing.test.ts
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { calculateDiscount } from './pricing';

describe('calculateDiscount', () => {
  beforeEach(() => { /* setup */ });

  it('applies premium discount', () => {
    expect(calculateDiscount({ price: 100, userTier: 'premium' })).toBe(90);
  });

  it.each([
    [100, 90], [0, 0], [200, 180],
  ])('price %i → %i', (price, expected) => {
    expect(calculateDiscount({ price, userTier: 'premium' })).toBe(expected);
  });

  it('throws on negative price', () => {
    expect(() => calculateDiscount({ price: -1, userTier: 'premium' }))
      .toThrow(/non-negative/);
  });
});
```

### Async
```typescript
it('fetches user', async () => {
  await expect(fetchUser('1')).resolves.toEqual({ id: '1', name: 'Alice' });
  await expect(fetchUser('missing')).rejects.toThrow(NotFoundError);
});
```

### Mocking
```typescript
const mockFn = vi.fn();
mockFn.mockReturnValue(42);
mockFn.mockResolvedValue('async');
mockFn.mockRejectedValue(new Error('boom'));
mockFn.mockImplementation((x) => x * 2);

vi.mock('./database', () => ({
  query: vi.fn(),
}));

vi.spyOn(console, 'log').mockImplementation(() => {});

const spy = vi.spyOn(obj, 'method');
expect(spy).toHaveBeenCalledWith('1');
vi.restoreAllMocks();   // in afterEach
```

### Run
```bash
vitest run                                # one-shot
vitest                                    # watch mode
vitest run --coverage                     # with coverage
vitest run -t 'premium'                   # by name pattern
vitest run --reporter=verbose
```

### Jest compatibility
Most `describe/it/expect` code from Jest works unchanged in Vitest. Replace `jest.fn()` with `vi.fn()`, `jest.mock()` with `vi.mock()`, `jest.spyOn()` with `vi.spyOn()`.

## Jest (TypeScript / JavaScript)

Still common in legacy codebases. Vitest is preferred for new work.

### Anatomy
```typescript
describe('calculateDiscount', () => {
  beforeAll(async () => { /* once before all */ });
  beforeEach(() => { /* before each */ });
  afterEach(() => { jest.clearAllMocks(); });
  afterAll(async () => { /* once after all */ });

  it('applies premium discount', () => {
    expect(calculateDiscount({ price: 100, userTier: 'premium' })).toBe(90);
  });
});
```

### Mocking
```typescript
jest.mock('./database', () => ({ query: jest.fn() }));
jest.spyOn(console, 'log').mockImplementation(() => {});
const mockFn = jest.fn().mockResolvedValue('async');
```

### Run
```bash
jest                                       # watch (default)
jest --ci                                  # one-shot
jest --coverage
jest path/to/file                          # one file
jest -t 'premium'                          # by test name
jest --findRelatedTests src/file.ts        # only tests affected by changes
```

## go test (Go)

### Anatomy
```go
// pricing_test.go
package pricing

import "testing"

func TestCalculateDiscount(t *testing.T) {
    got := CalculateDiscount(100, "premium")
    want := 90
    if got != want {
        t.Errorf("CalculateDiscount(100, premium) = %d, want %d", got, want)
    }
}

func TestCalculateDiscount_Table(t *testing.T) {
    tests := []struct {
        name     string
        price    int
        tier     string
        want     int
        wantErr  bool
    }{
        {"premium", 100, "premium", 90, false},
        {"standard", 100, "standard", 100, false},
        {"zero", 0, "premium", 0, false},
        {"negative", -1, "premium", 0, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CalculateDiscount(tt.price, tt.tier)
            if (err != nil) != tt.wantErr {
                t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
            }
            if !tt.wantErr && got != tt.want {
                t.Errorf("got %d, want %d", got, tt.want)
            }
        })
    }
}
```

### Subtests, parallel, skip
```go
func TestParallel(t *testing.T) {
    t.Parallel()                            // opt-in to parallel
    // ...
}

func TestSkip(t *testing.T) {
    if testing.Short() { t.Skip("long test") }
    // ...
}
```

### Run
```bash
go test ./...                              # all packages
go test -run TestCalculate -v ./pricing    # by name, verbose
go test -race ./...                        # race detector
go test -cover ./...                       # coverage
go test -coverprofile=c.out ./...          # coverage to file
go tool cover -html=c.out                  # browse coverage
go test -count=10 ./pricing                # repeat for flaky hunting
go test -fuzz=FuzzParse -fuzztime=10s      # fuzzing (Go 1.18+)
```

### Testify (assertions library, optional)
```go
import "github.com/stretchr/testify/assert"
import "github.com/stretchr/testify/require"

assert.Equal(t, 90, got)                   // continues on failure
require.Equal(t, 90, got)                  // stops on failure
require.ErrorIs(t, err, ErrNegative)
```

### Fuzzing (built-in, Go 1.18+)
```go
func FuzzParseURL(f *testing.F) {
    f.Add("https://example.com/path")
    f.Fuzz(func(t *testing.T, raw string) {
        u, err := ParseURL(raw)
        if err != nil { return }            // skip invalid inputs
        if got := u.String(); got != raw {
            t.Errorf("roundtrip failed: %q → %q", raw, got)
        }
    })
}
```

## cargo test (Rust)

### Anatomy
```rust
// src/pricing.rs
pub fn calculate_discount(price: i32, tier: &str) -> i32 {
    if tier == "premium" { (price as f64 * 0.9) as i32 } else { price }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn premium_discount() {
        assert_eq!(calculate_discount(100, "premium"), 90);
    }

    #[test]
    #[should_panic(expected = "non-negative")]
    fn rejects_negative() {
        calculate_discount(-1, "premium");
    }

    #[test]
    fn table_driven() {
        for (price, tier, want) in [
            (100, "premium", 90),
            (0,   "premium", 0),
            (200, "standard", 200),
        ] {
            assert_eq!(calculate_discount(price, tier), want);
        }
    }
}
```

### Run
```bash
cargo test                                # all tests
cargo test pricing                        # by name pattern
cargo test -- --nocapture                 # show println! output
cargo test -- --test-threads=4            # parallel
cargo test -- --ignored                   # run #[ignore] tests
cargo test --release                      # in release mode

cargo tarpaulin --out Html --fail-under 80  # coverage
cargo mutmut run                            # mutation testing
```

### Property-based (proptest)
```rust
proptest! {
    #[test]
    fn sum_commutative(ref xs in prop::collection::vec(0i32..1000, 0..100)) {
        let forward: i32 = xs.iter().sum();
        let reverse: i32 = xs.iter().rev().sum();
        prop_assert_eq!(forward, reverse);
    }
}
```

## dotnet test (.NET / C#)

### Anatomy (xUnit)
```csharp
// PricingTests.cs
using Xunit;

public class PricingTests
{
    [Fact]
    public void PremiumDiscount_AppliesTenPercent()
    {
        Assert.Equal(90, Pricing.CalculateDiscount(100, "premium"));
    }

    [Theory]
    [InlineData(100, "premium", 90)]
    [InlineData(0,   "premium", 0)]
    [InlineData(200, "standard", 200)]
    public void TableDriven(int price, string tier, int expected)
    {
        Assert.Equal(expected, Pricing.CalculateDiscount(price, tier));
    }

    [Fact]
    public void NegativePrice_Throws()
    {
        Assert.Throws<ArgumentException>(
            () => Pricing.CalculateDiscount(-1, "premium"));
    }
}
```

### Run
```bash
dotnet test
dotnet test --filter "FullyQualifiedName~PricingTests"
dotnet test --filter "Category=Slow"           # by [Trait("Category","Slow")]
dotnet test --collect:"XPlat Code Coverage"    # coverage via coverlet
dotnet test --logger "trx;LogFileName=results.trx"
```

### Moq (mocking)
```csharp
var repo = new Mock<IUserRepository>();
repo.Setup(r => r.FindById("1"))
    .ReturnsAsync(new User { Id = "1", Name = "Alice" });

var service = new UserService(repo.Object);
var user = await service.GetUser("1");

Assert.Equal("Alice", user.Name);
repo.Verify(r => r.FindById("1"), Times.Once);
```

## Quick Cross-Reference

| Need | pytest | Vitest | Jest | go test | cargo test | dotnet test |
|---|---|---|---|---|---|---|
| Run all | `pytest` | `vitest run` | `jest --ci` | `go test ./...` | `cargo test` | `dotnet test` |
| Filter | `-k name` | `-t name` | `-t name` | `-run name` | `name` (substring) | `--filter name` |
| Watch | `pytest-watch` | `vitest` | `jest` | — | `cargo watch -x test` | `dotnet watch test` |
| Async | `@pytest.mark.asyncio` | native | native | native | native | native |
| Mock | `unittest.mock` | `vi` | `jest` | interfaces + fakes | `mockall` crate | `Moq` |
| Parametrize | `@parametrize` | `it.each` | `it.each` | table-driven | table-driven | `[Theory]+[InlineData]` |
| Coverage | `pytest-cov` | v8 provider | `--coverage` | `-coverprofile` | `cargo-tarpaulin` | coverlet |
| Property | `hypothesis` | `fast-check` | `fast-check` | `testing/quick` | `proptest` | FsCheck (F#) |
| Snapshot | `syrupy` | `toMatchFileSnapshot` | `toMatchSnapshot` | — | `insta` | `Verify` |
