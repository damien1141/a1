# Modern Ruby 3.3+

## Pattern matching (Ruby 3.0+, expanded in 3.1+)

```ruby
case user
in { role: "admin", name: String => name }
  puts "Admin: #{name}"
in { role: "guest" }
  puts "Guest"
else
  puts "Other"
end

# Array pattern
case [1, 2, 3]
in [first, *rest]
  puts "first: #{first}, rest: #{rest}"
end

# Hash pattern with binding
case config
in { host: String => host, port: Integer => port }
  connect(host, port)
end

# `in` (no-raise on mismatch) vs `case/in` (raises NoMatchingPatternError)
user => { name: String => name, email: String => email }  # one-line destructure
```

Pattern matching is exhaustive via `else`; without it, `NoMatchingPatternError` raises on miss.

## `Data.define` (Ruby 3.2+)

```ruby
class Point < Data.define(:x, :y)
  def distance(other) = Math.sqrt((other.x - x)**2 + (other.y - y)**2)
end

p = Point.new(x: 1, y: 2)
p.x          # 1
p.x = 5      # NoMethodError — immutable
p.with(x: 5) # new Point with x=5, y=2
```

`Data.define` is Ruby's immutable value type — like `Struct` but immutable and with `with` for non-destructive updates. Use for DTOs and value objects.

## `Struct` (mutable) vs `Data` (immutable)

```ruby
# Mutable
Point = Struct.new(:x, :y) do
  def magnitude = Math.sqrt(x**2 + y**2)
end
p = Point.new(1, 2)
p.x = 5  # OK

# Immutable — use Data
class Point < Data.define(:x, :y)
  def magnitude = Math.sqrt(x**2 + y**2)
end
p = Point.new(x: 1, y: 2)
p.x = 5  # NoMethodError
```

## Endless methods (Ruby 3.0+)

```ruby
def greet(name) = "Hello, #{name}"

def magnitude = Math.sqrt(x**2 + y**2)
```

One-line method definitions. Cleaner than `def f; ...; end` for trivial bodies.

## Hash shorthand (Ruby 3.1+)

```ruby
def send_email(to:, subject:, body:)
  # ...
end

to = "a@b.c"
subject = "Hi"
body = "Body"
send_email(to:, subject:, body:)  # shorthand for to: to, subject: subject, body: body
```

## `it` (Ruby 3.4+ preview, refinement in 3.3)

```ruby
[1, 2, 3].map { _1 * 2 }       # _1, _2, ... are numbered block params (3.0+)
# Ruby 3.4: `it` as the implicit first param
[1, 2, 3].map { it * 2 }       # cleaner
```

`_1`/`_2` are the numbered-block-param syntax (Ruby 3.0+). Ruby 3.4+ adds `it` as a friendlier alias.

## Ractors (Ruby 3.0+) — true parallelism

```ruby
ractor = Ractor.new do
  value = receive
  Ractor.yield(value * 2)
end

ractor.send(21)
puts ractor.take  # 42
```

Ractors are Ruby's parallel-without-GIL primitive. Each Ractor has its own heap; data crosses via copy or move. Limited library support; use for CPU-bound isolated work.

## Frozen string literals

```ruby
# frozen_string_literal: true

name = "Alice"  # frozen — name << "x" raises FrozenError
greeting = "Hello, #{name}"  # interpolation creates a new (mutable) String
```

Add the magic comment to every file. Saves memory (string deduplication) and prevents accidental mutation.

## `private`/`protected` with method symbols

```ruby
class Service
  def call = do_work

  private

  def do_work = "working"
end
```

`private` with no args applies to subsequent methods. Per-method: `private :do_work`.

## Keyword arguments

```ruby
def send_email(to:, subject:, body:, cc: nil) # kwargs
  # ...
end

send_email(to: "a@b.c", subject: "Hi", body: "Body")
```

Kwargs are explicit (`to:`) — no positional/keyword ambiguity. `**opts` for splat:

```ruby
def send_email(to:, **opts)
  # opts is a hash of the rest
end
```

## Refinements — scoped monkey-patching

```ruby
module TitleCase
  refine String do
    def titlecase = split.map(&:capitalize).join(" ")
  end
end

class Formatter
  using TitleCase

  def format(s) = s.titlecase  # available only here
end
```

Refinements scope a monkey-patch to a lexical scope (file, class, module). Safer than global `String#titlecase = ...`.

## Type-checking with RBS + Steep

```ruby
# sig/post.rbs
class Post
  attr_reader title: String
  attr_reader body: String
  def initialize: (title: String, body: String) -> void
  def word_count: () -> Integer
end
```

```bash
bundle exec steep check   # type-check against RBS signatures
```

RBS is the signature language; Steep is the type checker. Optional but catches bugs in larger codebases.

## Common pitfalls

- `attr_accessor` on Active Record models — overrides the column accessor; use schema columns
- `String + String` mutating frozen strings — `FrozenError`; use `<<` carefully or interpolation
- `class Foo; def bar; end` without endless method — endless is cleaner for one-liners
- Refinements in too many files — defeats the safety point
- Pattern matching with `case`/`when` (older syntax) instead of `case`/`in` — pattern matching is the modern idiom
- `Struct` for immutable data — use `Data.define`
