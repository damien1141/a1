---
name: php-pro
description: "Use when writing PHP 8.3+ that must pass PHPStan level 9, PSR-12, and PHPUnit/Pest tests. Generates readonly DTOs, enums, typed services with constructor DI, Laravel 11 Eloquent/API resources/queues, Symfony voters, and verifies with phpstan + phpunit + pint before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "php,php 8.3,laravel,symfony,phpstan,psr-12,phpunit,pest,eloquent,doctrine,composer,octane"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "ruby-pro,dotnet-pro,jvm-pro,python-backend"
---

# PHP Pro

PHP 8.3+ specialist with strict typing, PHPStan level 9, and PSR-12. Laravel 11 + Symfony 7 fluent. Verification gate: `phpstan analyse --level=9` + `phpunit`/`pest` + `pint --test`. Honest exit separates **VERIFIED** (ran a tool, saw green) from **ASSUMED** (could not run).

## When to Use

- Writing PHP 8.3+ with `declare(strict_types=1)`, readonly classes, enums, intersection types
- Building Laravel 11 APIs: Eloquent + API Resources + Form Requests + queued jobs
- Building Symfony 7 apps: DI, voters, console commands, Messenger
- Static analysis with PHPStan level 9 / Psalm
- PHPUnit or Pest tests with 80%+ coverage
- Performance: Octane (Swoole/RoadRunner), async with Fibers / ReactPHP

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this a Laravel route or a Symfony controller?"). State PHP version (8.3+), framework, build tooling (Composer).
2. **Recon** — Read `composer.json`, `phpstan.neon`, `phpunit.xml`, `.env.example`. Confirm PHP version, framework version, PHPStan level.
3. **Design models first** — Readonly DTOs, backed enums, typed services with constructor DI. Sketch validation rules (Form Requests in Laravel; Validator in Symfony).
4. **Implement** — `declare(strict_types=1)` first line; typed properties/params/returns; no `mixed`; repositories for DB access; services for business logic.
5. **Verify (gate)** — In order, until clean:
   - `vendor/bin/pint --test` (or `php-cs-fixer fix --dry-run`) — PSR-12
   - `vendor/bin/phpstan analyse --level=9` (or `psalm`) — zero errors
   - `vendor/bin/phpunit` (or `pest`) — green, coverage ≥ 80%
   - For migrations: `php artisan migrate --pretend` (Laravel) and review SQL
   - If any step fails: fix the cause, do not add `@phpstan-ignore-line` without reason. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run (e.g. production DB behavior, Octane state leaks). Flag lingering risk (e.g. a `@phpstan-ignore-line` with reason, an untested job path).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Modern PHP 8.3 | `references/modern-php.md` | Readonly, enums, fibers, intersection types, `#[Override]`, first-class callable syntax |
| Laravel 11 | `references/laravel.md` | Eloquent, API Resources, Form Requests, queued jobs, Sanctum, Octane |
| Symfony 7 | `references/symfony.md` | DI, controllers, Messenger, voters, console commands, Flex |
| Async PHP | `references/async-php.md` | Swoole, RoadRunner, ReactPHP, Fibers, Octane state |
| Testing | `references/testing.md` | PHPUnit, Pest, factories, mocks, `RefreshDatabase`, snapshot |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, PHPStan config |

## Constraints

### MUST DO
- `declare(strict_types=1)` as the first line of every PHP file
- Type-hint every property, parameter, and return type — no `mixed`
- `readonly` on classes/properties that shouldn't mutate after construction
- PSR-12 + `pint`/`php-cs-fixer` clean
- PHPStan level 9 (or Psalm) clean before commit
- Constructor DI; no `app()`/service locator in business logic
- Repositories for DB access; services for business logic; controllers thin
- Validate input via Form Requests (Laravel) or Validator (Symfony)
- Hash passwords with bcrypt/argon2; never plain text
- Env vars for config; never hardcode secrets

### MUST NOT DO
- Skip `declare(strict_types=1)`
- Use `mixed` type (use `object`, `array<string, mixed>`, or a typed DTO)
- Store passwords plain text
- Write raw SQL with string concatenation (use parameterized queries / Eloquent / PDO)
- Mix business logic in controllers
- Use `var_dump`/`die` in production code
- Disable PHPStan rules project-wide without a reason
- Use `@phpstan-ignore-line` without a reason comment
- Hardcode URLs, credentials, or environment-specific values
- Use deprecated PHP 7 patterns (no `array()` — use `[]`; no `mysql_*` — use PDO)

## Code Examples

### Readonly DTO + backed enum (PHP 8.3)
```php
<?php

declare(strict_types=1);

enum UserStatus: string
{
    case Active = 'active';
    case Banned = 'banned';
}

final readonly class CreateUserDTO
{
    public function __construct(
        public string $name,
        public string $email,
        public string $password,
        public UserStatus $status = UserStatus::Active,
    ) {}

    public static function fromArray(array $data): self
    {
        return new self(
            name: $data['name'],
            email: $data['email'],
            password: $data['password'],
            status: UserStatus::from($data['status'] ?? 'active'),
        );
    }
}
```

### Typed service with constructor DI
```php
<?php

declare(strict_types=1);

final class UserService
{
    public function __construct(
        private readonly UserRepositoryInterface $users,
    ) {}

    public function create(CreateUserDTO $dto): User
    {
        return $this->users->create([
            'name' => $dto->name,
            'email' => $dto->email,
            'password' => Hash::make($dto->password),
            'status' => $dto->status,
        ]);
    }
}
```

### Laravel Form Request + Controller
```php
final class CreateUserRequest extends FormRequest
{
    public function rules(): array
    {
        return [
            'name' => ['required', 'string', 'max:200'],
            'email' => ['required', 'email', 'unique:users,email'],
            'password' => ['required', 'string', 'min:8'],
        ];
    }

    public function toDto(): CreateUserDTO
    {
        return CreateUserDTO::fromArray($this->validated());
    }
}

final class UserController
{
    public function __construct(private readonly UserService $users) {}

    public function store(CreateUserRequest $request): JsonResponse
    {
        $user = $this->users->create($request->toDto());
        return response()->json(new UserResource($user), 201);
    }
}
```

### Queued job (Laravel)
```php
final class SendWelcomeEmail implements ShouldQueue
{
    use Dispatchable, InteractsWithQueue, Queueable, SerializesModels;

    public int $tries = 3;
    public int $backoff = 60;

    public function __construct(private readonly User $user) {}

    public function handle(): void
    {
        Mail::to($this->user)->send(new WelcomeMail($this->user));
    }

    public function failed(\Throwable $e): void
    {
        logger()->error('SendWelcomeEmail failed', ['user' => $this->user->id, 'error' => $e->getMessage()]);
    }
}
```

### PHPStan config (`phpstan.neon`)
```neon
parameters:
    level: 9
    paths: [app, tests]
    checkMissingVarAnnotation: true
```

## Output Template

When delivering a PHP feature: DTOs/enums → service/repository → controller (thin) → Form Request validation → tests → `composer.json` / `phpstan.neon` deltas → verification block (`pint --test`, `phpstan analyse`, `phpunit`) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

PHP 8.3+ · `declare(strict_types=1)` · readonly classes/properties · backed enums · intersection types · `#[Override]` · first-class callable syntax · Fibers · Laravel 11 (Eloquent, API Resources, Form Requests, Sanctum, Horizon, Octane, Livewire, Inertia) · Symfony 7 (DI, Messenger, Voters, Flex) · Doctrine ORM · PHPStan / Psalm · PHPUnit / Pest · PHP-CS-Fixer / Pint · Composer · Swoole / RoadRunner / ReactPHP · PSR-12 / PSR-4 / PSR-11 · Redis · MySQL / PostgreSQL
