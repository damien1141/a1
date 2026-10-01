# Background Jobs with Sidekiq

## Basic job

```ruby
class SendWelcomeEmailJob
  include Sidekiq::Job
  sidekiq_options queue: :mailers, retry: 3, dead: false

  def perform(user_id)
    user = User.find(user_id)
    UserMailer.welcome(user).deliver_now
  rescue ActiveRecord::RecordNotFound => e
    Rails.logger.warn("SendWelcomeEmailJob: user #{user_id} missing — #{e.message}")
    # Don't re-raise; record gone, no point retrying
  end
end

# Dispatch
SendWelcomeEmailJob.perform_later(user.id)
SendWelcomeEmailJob.perform_in(1.hour, user.id)
SendWelcomeEmailJob.set(queue: :urgent).perform_later(user.id)
```

`ApplicationJob` (Rails) is the base; `include Sidekiq::Job` (Sidekiq 7+) replaces `include Sidekiq::Worker`. Both `perform_later`/`perform_async` work.

## Retries and backoff

```ruby
class ProcessPaymentJob
  include Sidekiq::Job
  sidekiq_options retry: 5, queue: :critical

  sidekiq_retry_in do |count, exception|
    case exception
    when RateLimitError
      60 * (count + 1)  # 60s, 120s, 180s, ...
    when NetworkError
      5 * (count + 1) ** 2  # 5s, 20s, 45s, 80s, ...
    else
      nil  # default backoff
    end
  end

  sidekiq_retries_exhausted do |msg, exception|
    Rails.logger.error("Exhausted: #{msg['args']} — #{exception.message}")
    NotifyFailure.call(msg, exception)
  end
end
```

Default backoff: exponential (15s + count**4 + 15). Custom `sidekiq_retry_in` returns seconds to wait.

## Queues

```ruby
sidekiq_options queue: :default

# config/sidekiq.yml
:queues:
  - [critical, 4]
  - [default, 2]
  - [mailers, 1]
  - [low, 1]
```

Run workers per queue:
```bash
bundle exec sidekiq -q critical -q default -q mailers -q low
```

Higher-priority queues polled more often via weighted round-robin.

## Batches (Sidekiq Pro / Enterprise)

```ruby
batch = Sidekiq::Batch.new
batch.jobs do
  100.times { |i| ProcessItemJob.perform_later(i) }
end
batch.on(:success, BatchCallback)  # fires when all jobs succeed
```

Batches track a group of jobs and fire callbacks when they all complete. Without Pro, simulate with a counter record.

## Active Job vs Sidekiq direct

```ruby
# Active Job (framework-agnostic)
class MyJob < ApplicationJob
  queue_as :default
  def perform(...); end
end

# Sidekiq direct (more features)
class MyJob
  include Sidekiq::Job
  def perform(...); end
end
```

Active Job is portable across backends (Sidekiq, Resque, DelayedJob). Use it when you might switch. Use Sidekiq direct for advanced features (batch, middleware, `sidekiq_retry_in`).

## Idempotency — retries are real

```ruby
class ChargeCardJob
  include Sidekiq::Job

  def perform(payment_id)
    payment = Payment.find(payment_id)
    return if payment.charged?  # idempotent guard

    Stripe::Charge.create(amount: payment.amount, source: payment.source)
    payment.update!(status: :charged)
  end
end
```

Sidekiq retries on any exception — your job MUST be safe to run twice. Check state at the start; use unique constraints in the DB for create-once semantics.

## Dead letters

After `retry:` attempts, jobs go to the dead set (configurable). Inspect:
```bash
bundle exec sidekiqmon
# Or via web UI: /sidekiq/morgue
```

Replay:
```ruby
Sidekiq::DeadSet.new.each { |job| job.retry if job.klass == "CriticalJob" }
```

## Middleware

```ruby
class TracingMiddleware
  include Sidekiq::ServerMiddleware  # or ClientMiddleware

  def call(worker, job, queue)
    span = tracer.start_span(worker.class.name)
    yield
  rescue => e
    span.record_exception(e)
    raise
  ensure
    span.finish
  end
end

Sidekiq.configure_server do |config|
  config.server_middleware do |chain|
    chain.add TracingMiddleware
  end
end
```

Server middleware wraps job execution. Client middleware wraps `perform_async` dispatch.

## Cron jobs (Sidekiq-Cron)

```yaml
# config/sidekiq_cron.yml
cleanup_orphaned_files:
  cron: "0 3 * * *"
  class: "CleanupOrphanedFilesJob"
  queue: low
```

```ruby
# config/initializers/sidekiq_cron.rb
Sidekiq::Cron::Job.load_from_hash YAML.load_file("config/sidekiq_cron.yml")
```

## Common pitfalls

- Sidekiq on Heroku without `worker` dyno — jobs enqueue but never run
- Non-idempotent jobs retried — double-charges, double-emails; guard with state checks
- `perform_later(record)` (passing the record) — works via GlobalID but slower; pass IDs
- `User.all` in a job — loads millions of records; batch with `find_each`
- Long-running jobs (>30s) — bump `Sidekiq::Job` timeout; or split into chunks
- Storing state in class variables — workers are separate processes; use Redis or DB
- `retry: 0` on flaky external API — let Sidekiq retry with backoff; set `retry: 5` instead
- Redis with no maxmemory policy — OOM kills Sidekiq; set `maxmemory-policy: noeviction`
