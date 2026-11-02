---
name: ruby-pro
description: "Use when writing Ruby 3.3+ or Rails 7+ that must pass rubocop -A and rspec. Generates Active Record models with eager loading, Turbo Frames/Streams for partial updates, Sidekiq workers, RSpec specs with factories, and verifies with rubocop + rspec + brakeman before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "ruby,rails,ruby 3.3,rails 7,active record,hotwire,turbo,sidekiq,rspec,rubocop,brakeman"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "php-pro,dotnet-pro,jvm-pro,python-backend"
---

# Ruby Pro

Ruby 3.3+ / Rails 7+ specialist. Active Record with N+1 prevention, Hotwire/Turbo for partial updates, Sidekiq for background work, RSpec with factories. Verification gate: `rubocop` + `rspec` + `brakeman`. Honest exit separates **VERIFIED** (ran a tool, saw green) from **ASSUMED** (could not run).

## When to Use

- Building Rails 7+ apps: MVC, Active Record, Hotwire (Turbo Frames/Streams), Action Cable
- Ruby 3.3+ idioms: pattern matching, `Data.define`, Ractors, `it` (Ruby 3.4 block param)
- Sidekiq background jobs with retries, dead-letter handling
- RSpec with FactoryBot, system specs with Capybara
- API-only Rails (Rails::API), Jbuilder serialization, rate limiting
- Migrating older Rails (5/6) to 7+

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this a Turbo Stream response or a JSON API?"). State Ruby version (3.3+) and Rails version (7+).
2. **Recon** — Read `Gemfile`, `.rubocop.yml`, `config/database.yml`, `spec/` layout. Confirm Ruby/Rails versions, Sidekiq version, test framework.
3. **Design models first** — Active Record associations, validations, indexes. Service objects for multi-step logic. DTOs via `Data.define` (Ruby 3.2+).
4. **Implement** — `includes`/`eager_load` on every collection query with associations; strong parameters; `Rails.logger` (not `puts`); service objects for business logic.
5. **Verify (gate)** — In order, until clean:
   - `rubocop -A` then `rubocop` (clean)
   - `rspec` — all specs pass; coverage ≥ 85% (`SimpleCov`)
   - `brakeman` — no security warnings
   - For migrations: `rails db:migrate --dry-run` (PostgreSQL) or review SQL
   - For N+1: enable `config.active_record.verbose_query_logs` in dev/test, count queries
   - If any step fails: fix the cause, do not add `# rubocop:disable` without reason. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run (e.g. production Sidekiq throughput, real-DB query plans). Flag lingering risk (e.g. a `# rubocop:disable` with reason, an untested job retry path).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Modern Ruby 3.3 | `references/modern-ruby.md` | Pattern matching, `Data.define`, Ractors, frozen-string-literals, `it` |
| Active Record | `references/active-record.md` | Associations, validations, scopes, N+1 prevention, transactions |
| Hotwire / Turbo | `references/hotwire-turbo.md` | Turbo Frames/Streams, Stimulus, partial updates, WebSocket |
| Background jobs | `references/background-jobs.md` | Sidekiq, retries, queues, batching, dead letters |
| RSpec & testing | `references/rspec-testing.md` | Model/request/system specs, FactoryBot, Capybara, SimpleCov |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, brakeman, rubocop config |

## Constraints

### MUST DO
- `includes`/`eager_load` on every collection query touching associations
- Service objects for multi-step logic; keep controllers thin
- Database index on every `WHERE`/`ORDER BY`/`JOIN` column
- Strong parameters on every controller action
- `Rails.logger` (never `puts`) for diagnostics
- Sidekiq for slow operations — never synchronously in request cycle
- `rubocop` + `rspec` + `brakeman` clean before commit
- Frozen string literals: `# frozen_string_literal: true` at top of file
- `private`/`protected` for internal methods

### MUST NOT DO
- Skip migrations for schema changes
- Use raw SQL with string interpolation (`sanitize_sql` or parameterized only)
- N+1: iterate associations without `includes`/`eager_load`
- Mix `puts`/`p`/`print` for production logging
- Hardcode config (use `Rails.application.config_for` or env vars)
- Use `attr_accessor` on Active Record models (use schema columns)
- Disable rubocop cops project-wide without a reason
- `# rubocop:disable` without a reason comment
- `rescue => e` without a specific error class
- Use deprecated Rails patterns (`update_attributes`, `find_by_sql`)

## Code Examples

### Active Record model with eager loading + scopes
```ruby
# app/models/post.rb
class Post < ApplicationRecord
  belongs_to :author, class_name: "User"
  has_many :comments, dependent: :destroy

  enum :status, { draft: 0, published: 1, archived: 2 }, default: :draft

  scope :published, -> { where(status: :published).order(published_at: :desc) }
  scope :by_author, ->(user) { where(author: user) }

  validates :title, presence: true, length: { maximum: 200 }
  validates :body, presence: true
end

# Usage — eager load to prevent N+1
Post.includes(:author, comments: :user).published.limit(20)
```

### Service object + dry-monads result
```ruby
class PublishPost
  include Dry::Monads[:result, :do]

  def call(post)
    yield validate(post)
    post.update!(status: :published, published_at: Time.current)
    PublishNotifier.call(post)
    Success(post)
  rescue ActiveRecord::RecordInvalid => e
    Failure(e.record.errors)
  end

  private

  def validate(post)
    return Failure("already published") if post.published?
    Success()
  end
end
```

### Turbo Frame partial update
```erb
<%# app/views/posts/show.html.erb %>
<%= turbo_frame_tag dom_id(@post) do %>
  <h2><%= @post.title %></h2>
  <%= link_to "Edit", edit_post_path(@post) %>
<% end %>
```

```ruby
# app/controllers/posts_controller.rb
def update
  @post = Post.find(params[:id])
  if @post.update(post_params)
    redirect_to @post, notice: "Updated."
  else
    render :edit, status: :unprocessable_entity
  end
end
```

### Sidekiq worker
```ruby
class SendWelcomeEmailJob
  include Sidekiq::Job
  sidekiq_options queue: :mailers, retry: 3, dead: false

  def perform(user_id)
    user = User.find(user_id)
    UserMailer.welcome(user).deliver_now
  rescue ActiveRecord::RecordNotFound => e
    Rails.logger.warn("SendWelcomeEmailJob: user #{user_id} missing — #{e.message}")
  end
end

# Dispatch
SendWelcomeEmailJob.perform_later(user.id)
```

### RSpec model spec + FactoryBot
```ruby
# spec/models/post_spec.rb
require "rails_helper"

RSpec.describe Post, type: :model do
  describe "#published" do
    it "returns only published posts, newest first" do
      older = create(:post, :published, published_at: 2.days.ago)
      newer = create(:post, :published, published_at: 1.day.ago)
      draft = create(:post, :draft)

      expect(Post.published).to eq([newer, older])
    end
  end
end
```

## Output Template

When delivering a Ruby/Rails feature: migration (if schema changed) → model + associations + validations → service object (if multi-step) → controller + strong params → views or Turbo setup → specs → `Gemfile` deltas → verification block (`rubocop`, `rspec`, `brakeman`) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

Ruby 3.3+ (pattern matching, `Data.define`, Ractors, `it`, frozen-string-literals) · Rails 7+ · Active Record · Action Cable · Hotwire (Turbo Frames/Streams, Stimulus) · Sidekiq · Puma · RSpec · FactoryBot · Capybara · SimpleCov · Brakeman · Rubocop · Devise · Pundit · Jbuilder · Active Storage · Redis · PostgreSQL/MySQL
