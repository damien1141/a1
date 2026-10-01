# Type Guards & Narrowing

## `typeof` and `instanceof`

```ts
function format(v: string | number): string {
  if (typeof v === "string") return v.toUpperCase();
  return v.toFixed(2);
}

function isDate(v: unknown): v is Date {
  return v instanceof Date;
}
```

## Type predicates (`x is T`)

```ts
type User = { id: string; name: string };
type UserOrError = User | { error: string };

function isUser(v: UserOrError): v is User {
  return "id" in v && "name" in v;
}

const results: UserOrError[] = [];
const users: User[] = results.filter(isUser);
```

## `in` operator narrowing

```ts
type Cat = { kind: "cat"; meow: () => void };
type Dog = { kind: "dog"; bark: () => void };
type Pet = Cat | Dog;

function speak(p: Pet): void {
  if ("meow" in p) p.meow();
  else p.bark();
}
```

## Discriminated unions (preferred)

Tag every variant with a literal so the compiler narrows on the discriminant:

```ts
type Shape =
  | { kind: "circle"; radius: number }
  | { kind: "square"; size: number }
  | { kind: "rect"; w: number; h: number };

function area(s: Shape): number {
  switch (s.kind) {
    case "circle": return Math.PI * s.radius ** 2;
    case "square": return s.size ** 2;
    case "rect":   return s.w * s.h;
    default: {
      const _: never = s;  // exhaustiveness check
      throw new Error(`unhandled: ${_}`);
    }
  }
}
```

The `never` default catches missing cases at compile time when you add a new variant.

## Assertion functions

```ts
function assertNonNull<T>(v: T | null | undefined, msg = "non-null"): asserts v {
  if (v == null) throw new Error(msg);
}

const u: User | null = getUser();
assertNonNull(u);
u.name;  // narrowed to User
```

Assertion functions narrow in the caller's scope — useful at the start of a function for invariants.

```ts
function assertUser(v: unknown): asserts v is User {
  if (typeof v !== "object" || v === null || !("id" in v) || !("name" in v)) {
    throw new Error("not a User");
  }
}
```

## `Array.isArray` and `Array.prototype.filter`

```ts
const xs: (string | null)[] = ["a", null, "b"];
const strs: string[] = xs.filter((x): x is string => x !== null);
```

Without the predicate, `.filter(x => x !== null)` returns the same type as the input — a known TS limitation. Always supply the predicate.

## `unknown` is safer than `any`

```ts
function handle(v: unknown): void {
  if (typeof v === "string") {
    console.log(v.toUpperCase());  // OK
  }
}
```

`unknown` forces narrowing; `any` opts out of type-checking entirely.

## Narrowing with `?.` and `??`

```ts
const name = user?.profile?.name ?? "Anonymous";
// name is string (not string | undefined) because of ??
```

## `noUncheckedIndexedAccess`

With this flag on, `arr[i]` returns `T | undefined`. Narrow before use:

```ts
const xs: number[] = [1, 2, 3];
const first = xs[0];  // number | undefined
if (first !== undefined) {
  first.toFixed(2);  // OK
}
```

## User-defined guards checklist

- Return type must be `x is T` (a type predicate), not just `boolean`
- The function must actually check the shape; TS does not verify the check matches the type
- For complex shapes, use a runtime validator (`zod`, `valibot`, `arktype`) and derive the type:

```ts
import { z } from "zod";

const UserSchema = z.object({ id: z.string(), name: z.string() });
type User = z.infer<typeof UserSchema>;

function isUser(v: unknown): v is User {
  return UserSchema.safeParse(v).success;
}
```

## Exhaustiveness with `never`

```ts
type Status = "pending" | "active" | "done";

function label(s: Status): string {
  switch (s) {
    case "pending": return "Pending";
    case "active":  return "Active";
    case "done":    return "Done";
    default: {
      const _: never = s;
      throw new Error(`unhandled: ${String(_)}`);
    }
  }
}
```

If a new status is added, the `never` assignment fails to type-check until you handle it.

## Common pitfalls

- `if (x)` narrows out falsy values like `""`, `0`, `NaN` — be intentional
- `Array.isArray` is the only built-in array guard; do not roll your own
- `typeof null === "object"` — always check `=== null` separately
- `instanceof` across realms (iframes, workers) can fail; use structural checks for cross-realm data
