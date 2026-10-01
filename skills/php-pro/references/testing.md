# Testing PHP

## PHPUnit basics

```php
<?php

declare(strict_types=1);

namespace Tests\Service;

use App\DTO\CreateUserDTO;
use App\Models\User;
use App\Repositories\UserRepositoryInterface;
use App\Services\UserService;
use Mockery;
use PHPUnit\Framework\TestCase;

final class UserServiceTest extends TestCase
{
    private UserRepositoryInterface&Mockery\MockInterface $users;
    private UserService $service;

    protected function setUp(): void
    {
        parent::setUp();
        $this->users = Mockery::mock(UserRepositoryInterface::class);
        $this->service = new UserService($this->users);
    }

    public function test_create_hashes_password(): void
    {
        $dto = new CreateUserDTO('Alice', 'a@b.c', 'secret');
        $this->users->expects('create')
            ->withArgs(function (array $data): bool {
                return $data['name'] === 'Alice'
                    && $data['password'] !== 'secret';  // hashed
            })
            ->andReturn(new User(['name' => 'Alice']));

        $user = $this->service->create($dto);

        self::assertSame('Alice', $user->name);
    }

    protected function tearDown(): void
    {
        Mockery::close();
        parent::tearDown();
    }
}
```

## Pest — modern alternative

```php
<?php

declare(strict_types=1);

uses(\Illuminate\Foundation\Testing\RefreshDatabase::class);

it('creates a published post', function () {
    $user = User::factory()->create();
    $post = Post::factory()->for($user, 'author')->create(['status' => PostStatus::Published]);

    expect($post->status)->toBe(PostStatus::Published);
});

it('returns a published post', function () {
    $user = User::factory()->create();
    $post = Post::factory()->published()->for($user, 'author')->create();

    $this->actingAs($user)
        ->getJson("/api/posts/{$post->id}")
        ->assertOk()
        ->assertJsonPath('data.status', 'published');
});

it('queues publish job', function () {
    Queue::fake();
    $user = User::factory()->create();
    $post = Post::factory()->draft()->for($user, 'author')->create();

    $this->actingAs($user)
        ->postJson("/api/posts/{$post->id}/publish")
        ->assertAccepted();

    Queue::assertPushed(PublishPost::class, fn ($j) => $j->post->is($post));
});
```

Pest is built on PHPUnit; under the hood it's the same engine. Syntax is more declarative.

## Laravel feature tests

```php
class AuthTest extends TestCase
{
    use RefreshDatabase;

    public function test_user_can_login(): void
    {
        $user = User::factory()->create(['password' => bcrypt('secret')]);

        $response = $this->postJson('/api/login', [
            'email' => $user->email,
            'password' => 'secret',
        ]);

        $response->assertOk()
            ->assertJsonStructure(['token', 'user']);
    }

    public function test_invalid_credentials_rejected(): void
    {
        $user = User::factory()->create();

        $this->postJson('/api/login', [
            'email' => $user->email,
            'password' => 'wrong',
        ])->assertUnprocessable();
    }
}
```

## Factories

```php
class PostFactory extends Factory
{
    public function definition(): array
    {
        return [
            'title' => fake()->sentence(),
            'body' => fake()->paragraph(),
            'status' => PostStatus::Draft,
            'user_id' => User::factory(),
        ];
    }

    public function published(): static
    {
        return $this->state(fn () => ['status' => PostStatus::Published, 'published_at' => now()]);
    }
}

// Usage
$post = Post::factory()->published()->create();
$posts = Post::factory()->count(3)->for($user, 'author')->create();
```

## Mockery — typed mocks

```php
$mock = Mockery::mock(UserRepositoryInterface::class);
$mock->expects('find')
    ->with(42)
    ->andReturn(new User(['id' => 42, 'name' => 'Alice']));

$service = new UserService($mock);
$service->getUser(42);
```

Use `Mockery::close()` in `tearDown` (or via Pest's `uses(...)` hook) to verify expectations.

## Snapshot testing — `spatie/phpunit-snapshot-assertions`

```php
class ApiTest extends TestCase
{
    use MatchesSnapshots;

    public function test_product_response(): void
    {
        $response = $this->getJson('/api/products/1');
        $this->assertMatchesJsonSnapshot($response->json());
    }
}
```

First run records; subsequent runs diff. Use for API response stability.

## Coverage

```xml
<!-- phpunit.xml -->
<phpunit>
  <coverage>
    <report>
      <clover outputFile="coverage/clover.xml"/>
      <html outputDirectory="coverage/html"/>
    </report>
  </coverage>
  <source>
    <include><directory>app</directory></include>
    <exclude><directory>app/Http/Resources</directory></exclude>
  </source>
</phpunit>
```

Run: `vendor/bin/phpunit --coverage-text`. Configure `xdebug` or `pcov` for coverage.

Target 80%+ line coverage. Exclude Resources, DTOs (often trivial), and config.

## Pest + PHPStan integration

Pest test files are PHP files — PHPStan checks them too. Use `pestphp/pest-plugin-type-coverage`:

```bash
vendor/bin/pest --type-coverage
```

Reports type coverage of test files (and through them, the SUT).

## Common pitfalls

- `RefreshDatabase` not used in integration tests — tests pollute each other
- `Mockery::close()` missing — expectations silently not verified
- `Queue::fake()` without `Queue::assertPushed` — fake doesn't process, easy to forget assertion
- `auth()->user()` in tests — call `actingAs($user)` first
- Time-based tests without `Carbon::setTestNow()` — flaky
- HTTP tests without `RefreshDatabase` — data leaks between tests
- `$this->withoutExceptionHandling()` to debug — useful in dev, never commit
