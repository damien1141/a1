# Modern PHP 8.3

## `declare(strict_types=1)` — first line, always

```php
<?php

declare(strict_types=1);

function add(int $a, int $b): int
{
    return $a + $b;
}

add('1', '2');  // TypeError under strict_types; coerced without it
```

Without `strict_types=1`, PHP coerces (`'1'` → `1`). With it, type mismatches throw `TypeError`. Always enable in new code.

## Typed properties + constructor promotion

```php
final class User
{
    public function __construct(
        public readonly string $name,
        public readonly string $email,
        private readonly UserRepository $repo,
    ) {}
}
```

Constructor promotion (PHP 8.0+) generates the properties + assignment automatically. `readonly` (8.1+) prevents mutation after construction.

## `readonly class` (PHP 8.2+)

```php
final readonly class Point
{
    public function __construct(
        public float $x,
        public float $y,
    ) {}
}
```

All properties of a `readonly class` are readonly. Perfect for value objects and DTOs.

## Enums (PHP 8.1+)

```php
enum UserStatus: string
{
    case Active = 'active';
    case Inactive = 'inactive';
    case Banned = 'banned';

    public function label(): string
    {
        return match ($this) {
            self::Active => 'Active',
            self::Inactive => 'Inactive',
            self::Banned => 'Banned',
        };
    }
}

$status = UserStatus::from('active');  // UserStatus::Active
$status = UserStatus::tryFrom('xxx') ?? UserStatus::Inactive;
$status->value;  // 'active'
$status->label();  // 'Active'
```

Backed enums serialize to a scalar (`string` or `int`). Pure enums have no backing value.

## Match expression

```php
$label = match ($status) {
    'active' => 'Active',
    'inactive', 'banned' => 'Inactive',
    default => 'Unknown',
};
```

`match` is exhaustive (must have `default` or cover all cases), returns a value, and uses strict comparison (`===`).

## Named arguments

```php
$cfg = new Config(
    host: 'localhost',
    port: 8080,
    debug: true,
);
```

Order-independent. Useful for constructors with many optional params.

## Attributes (PHP 8.0+)

```php
#[Attribute]
final class Route
{
    public function __construct(public string $path, public string $method = 'GET') {}
}

#[Route('/users/{id}', 'GET')]
function getUser(int $id): Response { /* ... */ }

// Read via reflection
$ref = new ReflectionFunction('getUser');
$route = $ref->getAttributes(Route::class)[0]->newInstance();
```

Replaces doc-comment annotations. Used by Symfony, Doctrine, Laravel.

## `#[Override]` (PHP 8.3+)

```php
class Base
{
    public function run(): void {}
}

class Sub extends Base
{
    #[Override]
    public function run(): void { /* ... */ }
}
```

`#[Override]` asserts the method overrides a parent. If the parent signature changes, you get a compile error.

## First-class callable syntax (PHP 8.1+)

```php
$strlen = strlen(...);
$lengths = array_map($strlen, ['hello', 'world']);

// Equivalent to closure:
$strlen = fn(string $s): int => strlen($s);
```

`func(...)` creates a closure referencing `func`. Cleaner than `Closure::fromCallable('func')`.

## Intersection types (PHP 8.1+)

```php
function process(Iterator & Countable $collection): void
{
    foreach ($collection as $item) { /* ... */ }
    echo count($collection);
}
```

The parameter must satisfy all listed types. Useful for fine-grained interfaces.

## DNF types (PHP 8.2+)

```php
function handle((A & B) | null $obj): void {}
```

Disjunctive Normal Form — `(A & B) | null` means "either both A and B, or null".

## Fibers (PHP 8.1+)

```php
$fiber = new Fiber(function (Fiber $fiber): void {
    $value = $fiber->resume('started');
    echo "got: $value\n";
});

$fiber->start();           // prints nothing, suspends
$fiber->resume('hello');   // prints "got: hello"
```

Fibers are cooperative coroutines. ReactPHP, Amp, and Swoole use them for async I/O. Most application code uses async frameworks, not Fibers directly.

## Readonly with arrays

```php
final readonly class Config
{
    /**
     * @param array<string, non-empty-string> $allowedHosts
     */
    public function __construct(
        public array $allowedHosts,
    ) {}
}
```

`readonly` prevents reassignment of `$allowedHosts`, but the array contents can still be mutated. For deep immutability, use `SplFixedArray` / `ReadonlyCollection` from a library, or document the contract.

## Common pitfalls

- Forgetting `declare(strict_types=1)` — silent type coercion bugs
- `mixed` type — too loose; use `object`, `array<TKey, TValue>`, or a typed DTO
- `array` without PHPDoc — PHPStan can't infer key/value types; add `@param array<string, int>` etc.
- Public mutable state — use `readonly` or private + getters
- Magic methods (`__get`/`__set`) — defeats typing; use explicit properties
- `null` defaults where the type doesn't allow null — `?string $name = null` is correct; `string $name = null` is a deprecation warning
