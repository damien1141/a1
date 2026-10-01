# Advanced TypeScript Types

## Generics with constraints

```ts
function pickFirst<T extends { length: number }>(xs: T): T[number] | undefined {
  return xs[0];
}

// Generic over both data and error
async function wrap<T, E extends Error>(
  fn: () => Promise<T>,
  makeErr: (cause: unknown) => E,
): Promise<[T, null] | [null, E]> {
  try {
    return [await fn(), null];
  } catch (e) {
    return [null, makeErr(e)];
  }
}
```

## Conditional types

```ts
type IsString<T> = T extends string ? true : false;
type Unwrap<T> = T extends Promise<infer U> ? U : T;
type ElementOf<T> = T extends (infer E)[] ? E : never;

type A = Unwrap<Promise<number>>;   // number
type B = ElementOf<string[]>;        // string
```

## Mapped types

```ts
type Mutable<T> = { -readonly [K in keyof T]: T[K] };
type Getter<T> = { [K in keyof T as `get${Capitalize<string & K>}`]: () => T[K] };
type Optional<T, K extends keyof T> = Omit<T, K> & Partial<Pick<T, K>>;

interface User { id: number; name: string; email: string; }
type UserGetters = Getter<User>;
// { getId: () => number; getName: () => string; getEmail: () => string; }
```

## Template literal types

```ts
type Route = `/${string}`;
type EventName = `on${Capitalize<string>}`;
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE" | "PATCH";

type ApiRoute<M extends HttpMethod, P extends string> = `${M} ${P}`;
type R = ApiRoute<"GET", "/users">;  // "GET /users"
```

## `satisfies` — validate without widening

```ts
const config = {
  port: 8080,
  host: "localhost",
} satisfies { port: number; host: string };
// type of config is { port: number; host: string } (the literal type, not widened)
```

Use `satisfies` when you want to check a value conforms to a type **without** widening it (so the literal types are preserved for downstream inference).

## Branded / opaque types

```ts
type Brand<T, B extends string> = T & { readonly __brand: B };

type UserId = Brand<string, "UserId">;
type Email = Brand<string, "Email">;

const UserId = (s: string): UserId => {
  if (!s.startsWith("u_")) throw new Error("bad id");
  return s as UserId;
};

const Email = (s: string): Email => {
  if (!s.includes("@")) throw new Error("bad email");
  return s as Email;
};

function send(to: Email, body: string): void { /* ... */ }
send(UserId("u_1"), "hi"); // type error — UserId is not Email
```

## `infer` in conditional positions

```ts
type ReturnType<T> = T extends (...args: never[]) => infer R ? R : never;
type Awaited<T> = T extends Promise<infer U> ? U : T;
type Params<T> = T extends (...args: infer P) => unknown ? P : never;
```

## Recursive types and JSON

```ts
type Json = string | number | boolean | null | Json[] | { [k: string]: Json };

function isJson(v: unknown): v is Json {
  if (v === null || typeof v === "string" || typeof v === "number" || typeof v === "boolean") return true;
  if (Array.isArray(v)) return v.every(isJson);
  if (typeof v === "object" && v !== null) return Object.values(v).every(isJson);
  return false;
}
```

## `const` type parameters (5.0+)

```ts
function values<const T extends readonly string[]>(xs: T): T {
  return xs;
}
const r = values(["a", "b", "c"]);  // type: readonly ["a", "b", "c"]
```

## Decorators (5.0 stage 3)

```ts
function log<This, Args extends unknown[], R>(
  target: (this: This, ...args: Args) => R,
  ctx: ClassMethodDecoratorContext,
) {
  return function (this: This, ...args: Args): R {
    console.log(`calling ${String(ctx.name)}`);
    return target.call(this, ...args);
  };
}

class Service {
  @log
  fetch(id: string): Promise<User> { /* ... */ }
}
```

## Anti-patterns

- `any` — use `unknown` + narrow, or define a `type`
- `Record<string, any>` — use `Record<string, unknown>` or a `Map`
- `as T` to silence — fix the source; if forced, document why
- `enum` — use `as const` objects or union literals
- Over-narrowing with `!` (non-null assertion) — narrow with `if (x)`
- Generic `<T = any>` — pick a meaningful default or omit

## Pitfalls

- `keyof any` is `string | number | symbol`; use `keyof T` for object keys
- `Partial<T>` makes all props optional including nested — use `DeepPartial<T>` only when needed (and define it explicitly)
- `readonly` is shallow — `Readonly<T[]>` still allows `.push()`; use `readonly T[]`
- `exactOptionalPropertyTypes: true` makes `x?: T` and `x: T | undefined` distinct — be deliberate
