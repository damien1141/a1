# Async PHP — Swoole, RoadRunner, ReactPHP, Fibers

## Why async PHP

Standard PHP-FPM boots the app per request. For high-throughput APIs, that overhead dominates. Async runtimes boot once and serve many requests per worker:

| Runtime | Model | Use |
|---|---|---|
| Swoole | C extension, coroutines | Laravel Octane, Highload APIs |
| RoadRunner | Go-based PSR-7 server | Symfony, Spiral |
| ReactPHP | Event loop in userland PHP | Long-running daemons, custom servers |
| FrankenPHP | C-based, embeds Go server | Modern, container-friendly |
| Fibers | Native PHP 8.1+ coroutines | Building blocks for async libs |

## Swoole coroutines

```php
use Swoole\Coroutine as Co;

Co\run(function () {
    $results = [];
    Co\go(function () use (&$results) { $results[] = file_get_contents('https://a'); });
    Co\go(function () use (&$results) { $results[] = file_get_contents('https://b'); });
    Co::wait();  // implicit at end of run
});
```

Swoole hooks standard PHP I/O (`file_get_contents`, `pdo`, `curl`) so they yield instead of blocking. Existing sync code can become concurrent without rewrites.

## Laravel Octane

```bash
composer require laravel/octane
php artisan octane:install --server=swoole
php artisan octane:start --workers=4 --task-workers=16
```

Octane boots Laravel once per worker. Throughput jumps 5-10x. **But state leaks**: anything stored on a static, singleton, or service container persists between requests.

### Octane-safe patterns

```php
// BAD — state leaks across requests
class Counter
{
    private static int $count = 0;
    public function inc(): void { self::$count++; }
}

// GOOD — reset on each request (Octane listener)
class Counter
{
    private int $count = 0;
    public function inc(): void { $this->count++; }
    public function reset(): void { $this->count = 0; }
}

// In AppServiceProvider::boot()
Event::listen(RequestReceived::class, fn () => app(Counter::class)->reset());
```

Octane fires `RequestReceived`, `TickReceived`, `TaskReceived` events. Use them to reset state.

### Octane concurrency

```php
use Laravel\Octane\Facades\Octane;

[$users, $posts] = Octane::concurrently([
    fn () => User::all(),
    fn () => Post::all(),
]);
```

Runs both queries in parallel Swoole coroutines.

## RoadRunner

```yaml
# .rr.yaml
server:
    command: "php worker.php"
http:
    address: "0.0.0.0:8080"
    pool:
        num_workers: 4
```

```php
// worker.php
use App\Kernel;
use Spiral\RoadRunner\Http\PSR7Worker;

$worker = PSR7Worker::create($relay, $psrFactory, $psrStreamFactory, $psrUploadFactory);
$kernel = new Kernel();

while ($req = $worker->waitRequest()) {
    $res = $kernel->handle($req);
    $worker->respond($res);
}
```

RoadRunner keeps a pool of PHP workers, each handling many requests. Symfony runs unmodified.

## ReactPHP — event loop

```php
use React\EventLoop\Loop;
use React\Http\Browser;

$loop = Loop::get();
$client = new Browser($loop);

$client->get('https://api.example.com/users')->then(
    function (Psr\Http\Message\ResponseInterface $res) {
        echo (string) $res->getBody();
    },
    function (Exception $e) {
        echo 'Error: ' . $e->getMessage();
    }
);

$loop->run();
```

Userland async — no extension required. Slower than Swoole but portable.

## Fibers (PHP 8.1+)

```php
$fiber = new Fiber(function (): void {
    $value = Fiber::suspend('paused');
    echo "resumed with: $value\n";
});

$result = $fiber->start();     // 'paused'
$fiber->resume('hello');      // prints "resumed with: hello"
```

Fibers are the low-level primitive. ReactPHP v3 and AMP v3 use them to provide async/await syntax on top of event loops:

```php
// AMP v3 async/await
use function Amp\async;
use function Amp\Future\await;

$futures = [
    async(fn () => fetch('https://a')),
    async(fn () => fetch('https://b')),
];
[$a, $b] = await($futures);
```

## When async helps

- High-throughput APIs with significant framework boot overhead
- Long-lived connections (WebSockets, SSE) — PHP-FPM can't hold these
- Fan-out I/O (parallel HTTP calls, parallel DB queries)

## When async hurts

- Single-script CLIs — boot cost is one-shot
- CPU-bound work — async is for I/O; for CPU, use processes
- Apps with global state — Octane leaks; audit carefully

## Common pitfalls

- Statics and singletons on Octane — leaks across requests; reset via `RequestReceived` listener
- `static` caches in libraries — `Symfony Cache` is Octane-safe; hand-rolled caches may not be
- Database connection pool exhaustion — Swoole workers each open a pool; tune `max_connections`
- `session_start()` in async — Swoole hooks don't always cover sessions; use the framework's session abstraction
- Forgetting to disable `zend.assertions` and `opcache.revalidate_freq` in production — major perf hit
- Blocking calls (sleep, sync file I/O) inside Swoole — they block the whole worker; use `Co::sleep`
