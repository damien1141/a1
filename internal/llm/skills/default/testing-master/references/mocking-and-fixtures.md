# Mocking, Fixtures, Parametrize, Snapshots

## The Core Principle

> **Test what the code does, not what the mocks do.**

When tests verify mock behaviour instead of actual functionality, they provide false confidence while catching zero real bugs. Mocks are secondary evidence; the real assertion is on the system under test.

## What to Mock

Mock only **trust boundaries** — the edges where your code meets code you don't own:

- External APIs (HTTP, gRPC)
- Database (in unit tests; use a real DB in integration tests)
- Clock / time
- Filesystem (in unit tests)
- Random number generators
- Email / SMS / push providers

Do **not** mock:
- The unit under test
- Pure collaborators whose logic you want to exercise
- Value objects, dataclasses, DTOs

If you mock everything, the test verifies nothing.

## Mocking Patterns by Framework

### Vitest / Jest
```typescript
import { vi, describe, it, expect, beforeEach } from 'vitest';
import { UserService } from './UserService';
import type { UserRepository } from './UserRepository';

describe('UserService', () => {
  let mockRepo: vi.Mocked<UserRepository>;
  let service: UserService;

  beforeEach(() => {
    mockRepo = { findById: vi.fn(), save: vi.fn() };
    service = new UserService(mockRepo);
  });

  it('returns user when found', async () => {
    mockRepo.findById.mockResolvedValue({ id: '1', name: 'Alice' });
    const user = await service.getUser('1');
    expect(user?.name).toBe('Alice');                 // primary: real output
    expect(mockRepo.findById).toHaveBeenCalledWith('1'); // secondary: boundary call
  });

  it('throws NotFoundError when user is missing', async () => {
    mockRepo.findById.mockResolvedValue(null);
    await expect(service.getUser('1')).rejects.toThrow(NotFoundError);
  });
});
```

### pytest
```python
from unittest.mock import AsyncMock, Mock
import pytest
from app.user_service import UserService

@pytest.fixture
def mock_repo() -> Mock:
    return Mock()

@pytest.fixture
def service(mock_repo: Mock) -> UserService:
    return UserService(mock_repo)

async def test_get_user_returns_user(service: UserService, mock_repo: Mock) -> None:
    mock_repo.find_by_id = AsyncMock(return_value={"id": "1", "name": "Alice"})
    user = await service.get_user("1")
    assert user["name"] == "Alice"
    mock_repo.find_by_id.assert_awaited_once_with("1")
```

### Go — table-driven with interfaces
```go
type UserRepository interface {
    FindByID(ctx context.Context, id string) (User, error)
}

type fakeRepo struct{ user User; err error }
func (f *fakeRepo) FindByID(_ context.Context, _ string) (User, error) {
    return f.user, f.err
}

func TestUserService_GetUser(t *testing.T) {
    tests := []struct {
        name    string
        repo    UserRepository
        want    string
        wantErr bool
    }{
        {"found", &fakeRepo{user: User{ID: "1", Name: "Alice"}}, "Alice", false},
        {"missing", &fakeRepo{err: ErrNotFound}, "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            s := &UserService{repo: tt.repo}
            got, err := s.GetUser(context.Background(), "1")
            if (err != nil) != tt.wantErr { t.Fatalf("err = %v", wantErr %v", err, tt.wantErr) }
            if !tt.wantErr && got.Name != tt.want { t.Errorf("got %q want %q", got.Name, tt.want) }
        })
    }
}
```

## Fixtures

Fixtures provide setup, teardown, and shared dependencies. Each test gets a fresh fixture instance — no shared mutable state.

### pytest fixtures
```python
@pytest.fixture
def db_session():
    """Fresh in-memory SQLite session per test."""
    engine = create_engine("sqlite:///:memory:")
    Base.metadata.create_all(engine)
    session = Session(engine)
    yield session
    session.close()

def test_create_user(db_session: Session) -> None:
    db_session.add(User(email="a@x.com"))
    db_session.commit()
    assert db_session.query(User).count() == 1
```

### Vitest setup/teardown
```typescript
beforeEach(() => {
  mockRepo = { findById: vi.fn(), save: vi.fn() };
});
afterEach(() => vi.clearAllMocks());
```

### Playwright fixtures
```typescript
import { test as base } from '@playwright/test';
export const test = base.extend<{ authenticatedPage: Page }>({
  authenticatedPage: async ({ page }, use) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('user@test.com');
    await page.getByLabel('Password').fill('pass');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await page.waitForURL(/dashboard/);
    await use(page);
  },
});
```

## Factories (complete mock objects)

Never ship partial mocks that miss downstream fields. Use factories to produce complete objects with sensible defaults.

### TypeScript factory
```typescript
import { faker } from '@faker-js/faker';

export class UserFactory {
  static create(overrides: Partial<User> = {}): User {
    return {
      id: faker.string.uuid(),
      email: faker.internet.email(),
      name: faker.person.fullName(),
      role: 'user',
      createdAt: new Date().toISOString(),
      permissions: ['read'],
      settings: { theme: 'light', notifications: true },
      ...overrides,
    };
  }
  static createMany(count: number, overrides: Partial<User> = {}): User[] {
    return Array.from({ length: count }, () => this.create(overrides));
  }
}
```

### Python factory (factory_boy)
```python
import factory

class UserFactory(factory.Factory):
    class Meta:
        model = User
    email = factory.Faker("email")
    name = factory.Faker("name")
    role = "user"
    created_at = factory.LazyFunction(datetime.utcnow)
```

## Parametrize / Table-Driven Tests

Parametrize lets you run the same test logic against many inputs. Use it for edge cases, boundaries, and equivalence classes.

### pytest
```python
@pytest.mark.parametrize("input,expected", [
    ([], 0),
    ([1], 1),
    ([1, 2, 3], 6),
    ([-1, -2, -3], -6),     # negatives
    ([0, 0, 0], 0),         # zeros
])
def test_sum(input: list[int], expected: int) -> None:
    assert sum(input) == expected
```

### Vitest `it.each` / `describe.each`
```typescript
it.each([
  [[], 0],
  [[1], 1],
  [[1, 2, 3], 6],
  [[-1, -2, -3], -6],
])('sum(%j) === %i', (input, expected) => {
  expect(sum(input)).toBe(expected);
});
```

### Go table-driven
```go
tests := []struct {
    name string
    in   []int
    want int
}{
    {"empty", []int{}, 0},
    {"single", []int{1}, 1},
    {"negatives", []int{-1, -2, -3}, -6},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        if got := sum(tt.in); got != tt.want {
            t.Errorf("sum(%v) = %d, want %d", tt.in, got, tt.want)
        }
    })
}
```

## Snapshot Tests

Snapshot tests capture the serialised output of a function or component and fail on any change. Useful for:
- React component render output
- Serialised API responses (carefully — see warning below)
- Generated configs, SQL, codegen output

### Vitest snapshot
```typescript
it('matches snapshot', () => {
  expect(renderToStaticMarkup(<Card title="Hello" />)).toMatchFileSnapshot('__snapshots__/card.html');
});
```

### Jest snapshot
```typescript
it('renders correctly', () => {
  const tree = renderer.create(<Card title="Hello" />).toJSON();
  expect(tree).toMatchSnapshot();
});
```

### Snapshot warnings
- **Review diffs manually** — snapshots fail on intentional changes; reviewers must read the diff, not blindly update
- **Don't snapshot large objects** — they become write-only; no one reads them
- **Don't snapshot third-party output** — you can't fix it when it changes
- **Use `toMatchInlineSnapshot`** for small snapshots so they live next to the test
- Update with `--update-snapshots` (or `-u`) only after a human has read the diff

## Test Doubles Vocabulary

| Double | Purpose |
|---|---|
| **Stub** | Returns canned answers |
| **Mock** | Records calls; you assert on the calls |
| **Spy** | Wraps real object; records calls but still delegates |
| **Fake** | Working but simplified implementation (in-memory DB) |
| **Dummy** | Passed but never used |

Prefer **fakes** for repositories (in-memory implementations) over mocks — they survive refactors and exercise real logic.

## Anti-Patterns

| Anti-pattern | Symptom | Fix |
|---|---|---|
| Testing the mock | `expect(mock).toHaveBeenCalled()` with no real assertion | Assert on real output; mock calls are secondary |
| Test-only methods in production | `_resetForTesting()` on a class | Use fresh instances per test |
| Over-mocking | Every dependency mocked | Mock only trust boundaries; run real code between them |
| Incomplete mocks | Mock returns `{ id: 1 }` but real object has 10 fields | Use factories with full defaults |
| Tests as afterthought | Test files added weeks after the feature | TDD from the start |
| Order-dependent tests | Test B passes alone, fails after Test A | Reset state in `beforeEach`; never rely on order |

## Quick Reference

| Need | Use |
|---|---|
| One test, many inputs | `parametrize` / `it.each` / table-driven |
| Shared setup | fixtures (`pytest`), `beforeEach` (Vitest) |
| Complete mock objects | factory |
| External API | mock at the HTTP boundary; or use a fake server (e.g. `msw`, `responses`) |
| Time | inject a clock; never `time.now()` in unit tests |
| Randomness | seed the generator |
| Verify call happened | mock + `assert_awaited_once_with` (secondary evidence) |
| Capture render output | snapshot (sparingly, with manual diff review) |
