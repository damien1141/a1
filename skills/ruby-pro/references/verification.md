# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - bundle exec rubocop
      42 files inspected, no offenses
  - bundle exec rspec
      156 examples, 0 failures, 3 pending
      Coverage: 87.3% (SimpleCov)
  - bundle exec brakeman
      No warnings found
ASSUMED:
  - Postgres query plan with real data (only SQLite in CI)
  - Sidekiq throughput under load (no production broker)
  - Turbo Stream broadcasts over Action Cable in production
Lingering risk:
  - # rubocop:disable Rails/SkipsModelValidations on app/services/import.rb:42 (reason: bulk import)
```

## VERIFIED — what counts

Only what you ran and saw:

- `rubocop` clean (no offenses)
- `rspec` green with pass/fail count + coverage
- `brakeman` no warnings
- A migration SQL you reviewed (`rails db:migrate:status`, `--dry-run` on PG)

## ASSUMED — what to flag

- Production DB query plans (only SQLite/dev DB in CI)
- Sidekiq throughput under real load
- Action Cable broadcasting over real WebSocket connections
- Performance under concurrency (Puma cluster mode)
- Third-party gem behavior not exercised

## The verification loop

```bash
bundle exec rubocop -A       # auto-correct safe offenses
bundle exec rubocop          # confirm clean
bundle exec rspec            # all specs
bundle exec brakeman         # security
# For migrations (PG only):
rails db:migrate --dry-run
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- `# rubocop:disable` without a reason
- `pending`/`xit` to silence failures
- Delete a failing spec
- Disable a cop project-wide to silence

## When you cannot run a gate

If Sidekiq isn't installed or the DB isn't reachable:

```
ASSUMED:
  - Brakeman not run (gem not installed); security patterns hand-reviewed
  - Integration specs skipped (no Postgres); unit specs green
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: bundle install --jobs 4 --retry 3
- run: bundle exec rubocop --parallel
- run: bundle exec rspec --format RSpecJunitFormatter --out rspec.xml
- run: bundle exec brakeman --exit-on-warn
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: coverage/ }
```

For matrix testing across Ruby versions:
```yaml
strategy:
  matrix:
    ruby: ['3.3', '3.4']
```

## Rubocop config (`.rubocop.yml`)

```yaml
require:
  - rubocop-rails
  - rubocop-rspec
  - rubocop-performance

AllCops:
  NewCops: enable
  TargetRubyVersion: 3.3
  TargetRailsVersion: 7.1
  Exclude:
    - "db/schema.rb"
    - "bin/*"
    - "vendor/**/*"

Style/Documentation:
  Enabled: false
Metrics/BlockLength:
  Exclude: ["spec/**/*", "config/**/*"]
Rails/SkipsModelValidations:
  Exclude: ["app/services/import.rb"]
```

Every cop disable should be a deliberate choice with `Exclude` for specific files, not project-wide `Enabled: false`.

## `# rubocop:disable` audit

```bash
grep -rn "# rubocop:disable" app/ lib/
```

Every `# rubocop:disable` should have a `# reason:` comment. Run `rubocop --auto-gen-config` to see what's disabled; audit periodically.

## N+1 detection

In `config/environments/development.rb`:
```ruby
config.active_record.verbose_query_logs = true
```

Or use the `bullet` gem:
```ruby
# Gemfile
gem "bullet", group: :development

# config/environments/development.rb
config.after_initialize do
  Bullet.enable = true
  Bullet.bullet_logger = true
  Bullet.rails_logger = true
  Bullet.raise = true  # raise in tests
end
```

Bullet raises on N+1 queries in tests. CI catches them before production.

## Brakeman — security scan

```bash
bundle exec brakeman --exit-on-warn
```

Catches SQL injection, mass-assignment, XSS, CSRF, dangerous `send`, etc. Run on every PR.

For deeper SAST: add `bundler-audit` (CVE check) and `strong_migrations` (migration safety).

## The exit checklist

Before writing the final report:

- [ ] `rubocop` clean
- [ ] `rspec` green, coverage ≥ 85%
- [ ] `brakeman` no warnings
- [ ] No new `# rubocop:disable` without reason
- [ ] No `pending`/`xit` to silence failures
- [ ] N+1 checked (Bullet in tests or query log inspected)
- [ ] Migration SQL reviewed (if schema changed)
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
