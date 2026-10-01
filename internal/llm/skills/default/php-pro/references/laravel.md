# Laravel 11

## Project layout

```
app/
├── Models/
├── Http/
│   ├── Controllers/
│   ├── Requests/         # Form Requests
│   └── Resources/        # API Resources
├── Services/
├── Repositories/
├── Jobs/
├── Events/
└── Listeners/
database/
├── migrations/
└── factories/
routes/
├── web.php
└── api.php
```

## Eloquent model

```php
<?php

declare(strict_types=1);

namespace App\Models;

use Illuminate\Database\Eloquent\Builder;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Database\Eloquent\SoftDeletes;

final class Post extends Model
{
    use HasFactory, SoftDeletes;

    protected $fillable = ['title', 'body', 'status', 'user_id'];

    protected function casts(): array
    {
        return [
            'status' => PostStatus::class,        // backed enum
            'published_at' => 'immutable_datetime',
        ];
    }

    public function author(): BelongsTo
    {
        return $this->belongsTo(User::class, 'user_id');
    }

    public function comments(): HasMany
    {
        return $this->hasMany(Comment::class);
    }

    protected function scopePublished(Builder $q): Builder
    {
        return $q->where('status', PostStatus::Published);
    }
}
```

## Eager loading — avoid N+1

```php
// BAD — N+1: query per post.author access
$posts = Post::all();
foreach ($posts as $p) { echo $p->author->name; }  // N queries

// GOOD — eager load
$posts = Post::with('author', 'comments')->get();

// GOOD — constrained eager load
$posts = Post::with(['comments' => fn ($q) => $q->latest()->limit(5)])->get();
```

`with()` issues a single IN-query for the relation. Always use it on collection endpoints that touch relations.

## API Resources

```php
<?php

declare(strict_types=1);

namespace App\Http\Resources;

use Illuminate\Http\Request;
use Illuminate\Http\Resources\Json\JsonResource;

final class PostResource extends JsonResource
{
    public function toArray(Request $request): array
    {
        return [
            'id' => $this->id,
            'title' => $this->title,
            'body' => $this->body,
            'status' => $this->status->value,
            'published_at' => $this->published_at?->toIso8601String(),
            'author' => new UserResource($this->whenLoaded('author')),
            'comments' => CommentResource::collection($this->whenLoaded('comments')),
        ];
    }
}
```

`whenLoaded` avoids N+1 by only serializing relations that were eager-loaded.

## Form Requests

```php
<?php

declare(strict_types=1);

final class CreatePostRequest extends FormRequest
{
    public function rules(): array
    {
        return [
            'title' => ['required', 'string', 'max:200'],
            'body' => ['required', 'string'],
            'status' => ['required', Rule::enum(PostStatus::class)],
        ];
    }

    public function toDto(): CreatePostDTO
    {
        return CreatePostDTO::fromArray($this->validated());
    }
}
```

Form Requests validate + authorize before the controller runs. Controllers stay thin.

## Routes (PHP 8 attribute routing in Laravel 11)

```php
// routes/api.php
use App\Http\Controllers\PostController;
use Illuminate\Support\Facades\Route;

Route::get('/posts', [PostController::class, 'index']);
Route::post('/posts', [PostController::class, 'store'])->middleware(['auth:sanctum']);
Route::get('/posts/{post}', [PostController::class, 'show']);
Route::put('/posts/{post}', [PostController::class, 'update'])->middleware(['auth:sanctum']);
Route::delete('/posts/{post}', [PostController::class, 'destroy'])->middleware(['auth:sanctum', 'can:delete,post']);
```

## Queued jobs

```php
final class PublishPost implements ShouldQueue
{
    use Dispatchable, InteractsWithQueue, Queueable, SerializesModels;

    public int $tries = 3;
    public int $backoff = 60;

    public function __construct(private readonly Post $post) {}

    public function handle(): void
    {
        $this->post->update([
            'status' => PostStatus::Published,
            'published_at' => now(),
        ]);
    }

    public function failed(\Throwable $e): void
    {
        logger()->error('PublishPost failed', ['post' => $this->post->id, 'error' => $e->getMessage()]);
    }
}

// Dispatch
PublishPost::dispatch($post)->onQueue('publishing');
```

Run workers: `php artisan queue:work --queue=publishing --tries=3`. Use Horizon in production for monitoring.

## Sanctum — API tokens / SPA auth

```php
// config/auth.php — use 'sanctum' guard for SPA cookie auth or token auth
// Issue a token:
$token = $user->createToken('api')->plainTextToken;
// Revoke:
$user->tokens()->delete();
```

For SPAs, use Sanctum's cookie-based session. For mobile/CLI, use token auth.

## Octane — Swoole / RoadRunner

```bash
composer require laravel/octane
php artisan octane:install --server=swoole
php artisan octane:start
```

Octane boots the app once and serves many requests per worker — 5-10x throughput. **Watch out for state leaks**: statics, singletons, container bindings persist between requests. Use `Octane::tick()` and `flush()` carefully.

## Common pitfalls

- `Post::all()` then looping relations — N+1; use `with()`
- Mass-assigning without `$fillable` / `$guarded` — `MassAssignmentException` or security risk
- Forgetting `casts()` for enums / dates — gets raw strings
- `auth()->user()` in job constructors — user is null in queue context; capture the ID
- Service container `app()` calls in business logic — use constructor DI
- Octane with shared state across requests — singleton state leaks; use `Octane::tick` and per-request state
- `DB::transaction()` returning false on failure — use `DB::transaction(fn () => ...)` which throws
- `now()` in tests — fake with `Carbon::setTestNow()`
