# Async Patterns in TypeScript

## async/await — the default

```ts
async function fetchUser(id: string): Promise<User> {
  const res = await fetch(`/api/users/${id}`);
  if (!res.ok) throw new HttpError(res.status, `HTTP ${res.status}`);
  return (await res.json()) as User;
}
```

Rules:
- `async` functions always return a `Promise`; the return type annotation should reflect that
- `await` only inside `async` (top-level await requires ESM modules)
- Never mix `Promise.then` chains with `async/await` in the same function — pick one

## Typed errors

TypeScript does not track thrown types. Encode errors in the return type instead:

```ts
type Result<T, E = Error> =
  | { ok: true; value: T }
  | { ok: false; error: E };

async function safeFetch<T>(url: string): Promise<Result<T, HttpError>> {
  try {
    const res = await fetch(url);
    if (!res.ok) return { ok: false, error: new HttpError(res.status, "http") };
    return { ok: true, value: (await res.json()) as T };
  } catch (e) {
    return { ok: false, error: new HttpError(0, String(e)) };
  }
}

const r = await safeFetch<User>("/api/users/1");
if (r.ok) console.log(r.value.name);
else console.error(r.error.message);
```

## AbortController — cancellation is mandatory

```ts
async function fetchWithTimeout<T>(url: string, ms: number): Promise<T> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), ms);
  try {
    const res = await fetch(url, { signal: ctrl.signal });
    if (!res.ok) throw new HttpError(res.status, "http");
    return (await res.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}
```

For long-lived operations, expose the `signal` parameter so callers can cancel:

```ts
async function loadAll(urls: string[], signal?: AbortSignal): Promise<User[]> {
  return Promise.all(urls.map((u) => fetchJson<User>(u, signal)));
}
```

`fetch` rejects with `AbortError` when aborted. Catch it explicitly:

```ts
try { await loadAll(urls, ctrl.signal); }
catch (e) {
  if (e instanceof DOMException && e.name === "AbortError") return;
  throw e;
}
```

## Concurrent fan-out

| Function | Behavior | Use when |
|---|---|---|
| `Promise.all` | Rejects on first failure; returns array of values | All-or-nothing; you can retry the batch |
| `Promise.allSettled` | Never rejects; returns `{status, value/reason}[]` | Partial success OK; report failures |
| `Promise.any` | Resolves on first success; rejects if all fail | First-wins racing |
| `Promise.race` | Resolves/rejects on first settled | Timeouts, first-response |

```ts
// Partial success
const results = await Promise.allSettled(urls.map((u) => fetchJson<User>(u)));
const ok = results
  .filter((r): r is PromiseFulfilledResult<User> => r.status === "fulfilled")
  .map((r) => r.value);
const failed = results
  .filter((r): r is PromiseRejectedResult => r.status === "rejected")
  .map((r) => r.reason);
```

## Bounded concurrency

```ts
async function mapBounded<T, R>(
  items: readonly T[],
  n: number,
  fn: (item: T, i: number) => Promise<R>,
): Promise<R[]> {
  const ret: R[] = new Array(items.length);
  let i = 0;
  const workers = Array.from({ length: n }, async () => {
    while (i < items.length) {
      const idx = i++;
      ret[idx] = await fn(items[idx]!, idx);
    }
  });
  await Promise.all(workers);
  return ret;
}
```

Don't fire 10,000 concurrent requests — bound it.

## Streams

```ts
async function* toLines(res: Response): AsyncGenerator<string> {
  const reader = res.body!.pipeThrough(new TextDecoderStream()).getReader();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += value;
    const lines = buf.split("\n");
    buf = lines.pop() ?? "";
    for (const line of lines) yield line;
  }
  if (buf) yield buf;
}

for await (const line of toLines(res)) {
  console.log(line);
}
```

## Async iterators

```ts
async function* paginate<T>(url: string): AsyncGenerator<T[]> {
  let next: string | null = url;
  while (next) {
    const page = await fetchJson<{ items: T[]; next: string | null }>(next);
    yield page.items;
    next = page.next;
  }
}

for await (const batch of paginate<User>("/api/users")) {
  for (const u of batch) console.log(u.name);
}
```

## Anti-patterns

```ts
// WRONG — fire-and-forget, errors swallowed
function handler(req: Request) {
  processAsync(req);  // floating promise
}

// RIGHT
async function handler(req: Request): Promise<void> {
  await processAsync(req);
}

// WRONG — sequential when independent
const a = await fetchA();
const b = await fetchB();

// RIGHT — concurrent
const [a, b] = await Promise.all([fetchA(), fetchB()]);

// WRONG — `async` with no `await`
async function double(x: number) { return x * 2; }

// RIGHT
function double(x: number): number { return x * 2; }
```

## Testing async

```ts
import { describe, it, expect } from "vitest";

describe("fetchUser", () => {
  it("returns the user", async () => {
    const u = await fetchUser("1");
    expect(u.id).toBe("1");
  });

  it("rejects on 404", async () => {
    await expect(fetchUser("missing")).rejects.toThrow(HttpError);
  });
});
```

For time-based tests, use fake timers (`vi.useFakeTimers()`) — never `setTimeout` in tests.
