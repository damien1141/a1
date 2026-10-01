# Active Record

## Associations

```ruby
class Post < ApplicationRecord
  belongs_to :author, class_name: "User", counter_cache: true
  has_many :comments, dependent: :destroy
  has_many :taggings, dependent: :destroy
  has_many :tags, through: :taggings
  has_one :cover_image, class_name: "Image", as: :imageable
end

class Comment < ApplicationRecord
  belongs_to :post, touch: true
  belongs_to :author, class_name: "User"
end
```

- `belongs_to` requires the foreign key on this table
- `has_many`/`has_one` looks up by FK on the other table
- `counter_cache: true` caches the count, avoiding `COUNT(*)` queries
- `touch: true` updates the parent's `updated_at` when the child changes
- `dependent: :destroy` cascades deletes (and runs callbacks); `:delete_all` skips callbacks

## N+1 prevention

```ruby
# BAD — N+1: query per post.author access
Post.all.each { |p| puts p.author.name }

# GOOD — eager load
Post.includes(:author).each { |p| puts p.author.name }

# GOOD — eager_load forces JOIN (useful when filtering on association)
Post.eager_load(:author).where(users: { verified: true })

# GOOD — preload (default; separate queries, no JOIN)
Post.preload(:author).where(created_at: 1.day.ago..)
```

| Method | Behavior |
|---|---|
| `includes` | Smart: JOIN if you filter on the association, else separate IN-query |
| `preload` | Always separate IN-query; can't filter on association |
| `eager_load` | Always JOIN; useful when filtering/sorting by association |

## Validations

```ruby
class Post < ApplicationRecord
  validates :title, presence: true, length: { maximum: 200 }
  validates :body, presence: true
  validates :slug, uniqueness: true, format: { with: /\A[a-z0-9-]+\z/ }
  validate :not_in_future

  private

  def not_in_future
    errors.add(:published_at, "can't be in the future") if published_at&.future?
  end
end
```

Use `validates` for common checks; `validate` for custom. Run with `valid?`/`invalid?` or `save!`/`update!` (raise on invalid).

## Callbacks — sparingly

```ruby
class Comment < ApplicationRecord
  after_create_commit :notify_author

  private

  def notify_author
    CommentNotificationJob.perform_later(id)
  end
end
```

- Prefer `_commit` callbacks (`after_create_commit`) over `after_create` — they fire after the transaction commits
- Avoid callbacks for cross-cutting concerns (use service objects)
- Test callbacks fire — they're easy to forget

## Scopes

```ruby
class Post < ApplicationRecord
  scope :published, -> { where(status: :published).order(published_at: :desc) }
  scope :by_author, ->(user) { where(author: user) }
  scope :recent, ->(n = 10) { order(created_at: :desc).limit(n) }
end

# Usage
Post.published.by_author(user).recent(5)
```

Scopes chain. Use class methods (`def self.published`) for complex logic.

## Indexes — every `WHERE`/`ORDER BY`/`JOIN` column

```ruby
class AddIndexes < ActiveRecord::Migration[7.1]
  def change
    add_index :posts, :slug, unique: true
    add_index :posts, :status
    add_index :posts, [:author_id, :created_at]  # composite
    add_index :posts, :title, opclass: :gin_trgm_ops, using: :gin  # trigram search (PG)
  end
end
```

- Foreign keys: Rails auto-creates for `references`/`belongs_to` since 5.1
- Composite indexes for multi-column WHERE
- Partial indexes: `where: "status = 1"`

## Transactions

```ruby
ActiveRecord::Base.transaction do
  account.update!(balance: account.balance - 100)
  target.update!(balance: target.balance + 100)
  Transfer.create!(from: account, to: target, amount: 100)
end
```

`update!` raises on failure, rolling back the transaction. Never swallow exceptions inside a transaction — `ActiveRecord::Rollback` is the only one that triggers silent rollback.

## Counter cache

```ruby
class Comment < ApplicationRecord
  belongs_to :post, counter_cache: true
end

post.comments.size   # uses cached count column, no COUNT(*) query
```

Add `comments_count` integer column to `posts`. Rails maintains it automatically.

## Single-table inheritance (STI)

```ruby
class User < ApplicationRecord; end
class Admin < User; end
class Guest < User; end

# Migration: add `type` string column to users
```

STI shares one table with a `type` column. Simple but doesn't scale to truly different fields per subclass — consider delegated types or separate tables.

## Delegated types (Rails 6.1+) — alternative to STI

```ruby
class Entry < ApplicationRecord
  delegated_type :entryable, types: %w[Message Comment]
  delegate :title, :body, to: :entryable
end

class Message < ApplicationRecord
  has_one :entry, as: :entryable
end
```

Each subclass has its own table — no sparse columns, no STI quirks.

## Common pitfalls

- `Post.all.each` on a million rows — OOM; use `find_each(batch_size: 1000)`
- `Post.where(...).count` then `Post.where(...).to_a` — two queries; cache the relation
- `post.comments.length` (loads all) vs `post.comments.size` (uses counter cache) vs `post.comments.count` (always SQL)
- Callbacks that enqueue jobs but the record rolls back — use `_commit` callbacks
- `validates_uniqueness_of` without a DB unique index — race condition; always add the index
- `dependent: :destroy` on `has_many :through` — deletes the join records, not the through records
- N+1 in serializers (Jbuilder, ActiveModel::Serializers) — preload associations explicitly
