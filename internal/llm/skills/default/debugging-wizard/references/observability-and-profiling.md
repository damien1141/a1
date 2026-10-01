# Observability & Profiling

When the bug isn't a crash but a slowdown, a memory growth, or a "weird" behaviour in production, you need observability: structured logs, metrics, and distributed traces. When the bug is local but slow, you need a profiler.

## The Three Pillars of Observability

| Pillar | What it answers | Tooling |
|---|---|---|
| **Logs** | What happened, in what order, with what context? | structured logging, centralised aggregation |
| **Metrics** | How is the system behaving in aggregate? | Prometheus, Grafana, CloudWatch, Datadog |
| **Traces** | Where did this request spend its time, across services? | OpenTelemetry, Jaeger, Zipkin, Tempo |

You need all three. Logs explain individual events; metrics show trends; traces connect requests across service boundaries.

## Structured Logging

### Principles
- **Structured, not stringly-typed** — emit JSON or key-value pairs, not free text
- **Include request ID / correlation ID** — so you can follow one request across services
- **Include timestamps in UTC with timezone** — ISO 8601
- **Levels mean what they say** — DEBUG (dev only), INFO (lifecycle), WARN (recoverable), ERROR (investigate), FATAL (page someone)
- **Never log secrets** — passwords, tokens, PII beyond what's needed
- **Log at boundaries** — service entry, external calls, decision points
- **Make logs searchable** — ship to a central store (ELK, Loki, CloudWatch, Datadog)

### Python (structlog)
```python
import structlog
logger = structlog.get_logger()

logger.info("user_login", user_id=user.id, ip=request.remote_ip)
logger.warning("rate_limit_hit", endpoint="/api/login", ip=request.remote_ip)
logger.error("payment_failed", order_id=order.id, reason=str(exc), exc_info=True)
```

### Node (pino)
```typescript
import pino from 'pino';
const logger = pino({ level: process.env.LOG_LEVEL || 'info' });

logger.info({ userId: user.id, ip: req.ip }, 'user_login');
logger.warn({ endpoint: '/api/login', ip: req.ip }, 'rate_limit_hit');
logger.error({ orderId: order.id, err }, 'payment_failed');
```

### Go (slog, stdlib 1.21+)
```go
import "log/slog"

slog.Info("user_login", "user_id", user.ID, "ip", r.RemoteAddr)
slog.Warn("rate_limit_hit", "endpoint", "/api/login", "ip", r.RemoteAddr)
slog.Error("payment_failed", "order_id", order.ID, "err", err)
```

### Log levels
| Level | When to use | Where it ships |
|---|---|---|
| DEBUG | Detailed flow; only useful during development | Dev only |
| INFO | Lifecycle events (login, order placed, job started) | All envs |
| WARN | Recoverable anomaly (rate limit, fallback used, retry succeeded) | All envs |
| ERROR | Operation failed; investigate | All envs + alerting |
| FATAL | Process cannot continue; crash | All envs + paging |

### What NOT to log
- Passwords (even hashed)
- Full session tokens / JWTs
- Credit card numbers (PCI DSS forbids)
- PII beyond what's needed for forensics
- Raw request bodies (may contain secrets)
- Stack traces in customer-facing responses (log server-side only; return generic message)

### Redaction
```typescript
// pino redaction
const logger = pino({
  redact: ['password', 'token', '*.password', 'req.headers.authorization'],
});
```

```python
# structlog processor
def redact(_, __, event_dict):
    for key in ("password", "token", "ssn"):
        if key in event_dict:
            event_dict[key] = "[REDACTED]"
    return event_dict
```

## Correlation IDs

Every request gets a unique ID, propagated across services. Without this, multi-service debugging is impossible.

```typescript
// Express middleware
app.use((req, res, next) => {
  req.id = req.headers['x-request-id'] || crypto.randomUUID();
  res.setHeader('x-request-id', req.id);
  req.log = logger.child({ requestId: req.id });
  next();
});

// In a handler
app.post('/api/orders', async (req, res) => {
  req.log.info({ userId: req.user.id }, 'order_started');
  // ...
});
```

Propagate via headers: `x-request-id` (or `traceparent` per W3C Trace Context).

## Metrics

### RED metrics (for services)
- **R**ate — requests per second
- **E**rrors — error rate
- **D**uration — latency distribution (p50, p95, p99)

### USE metrics (for resources)
- **U**tilization — busy time (CPU, disk, network)
- **S**aturation — queue length
- **E**rrors — error count

### Prometheus
```python
from prometheus_client import Counter, Histogram

requests = Counter('http_requests_total', 'Total requests', ['method', 'endpoint', 'status'])
latency = Histogram('http_request_duration_seconds', 'Request latency', ['endpoint'])

@app.route('/api/users')
def get_users():
    start = time.time()
    # ...
    requests.labels(method='GET', endpoint='/api/users', status='200').inc()
    latency.labels(endpoint='/api/users').observe(time.time() - start)
    return users
```

### What to alert on
- Error rate > threshold for N minutes
- p95 latency > SLO for N minutes
- Saturation (CPU > 80%, disk > 90%, queue depth > N)
- Uptime checks failing
- Log-based: spike in ERROR-level logs

## Distributed Tracing

Traces follow a single request across service boundaries. Each service adds a span; spans nest into a trace.

### OpenTelemetry
```typescript
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('my-app');

app.post('/api/orders', async (req, res) => {
  const span = tracer.startSpan('create_order');
  try {
    await span.setAttribute('user_id', req.user.id);
    // ... business logic, with child spans for DB / API calls
    span.setStatus({ code: 1 }); // OK
  } catch (e) {
    span.recordException(e);
    span.setStatus({ code: 2, message: e.message }); // ERROR
    throw e;
  } finally {
    span.end();
  }
});
```

### Reading a trace
- The **span waterfall** shows where time was spent
- Look for the **widest span** — that's the slowest hop
- Look for **gaps** between spans — that's where the request was idle (queue, lock wait, GC)
- Compare a slow trace to a fast trace — the difference is your regression

## Profiling

When something is slow locally, profile it. Don't guess — measure.

### CPU Profiling

#### Python (cProfile + snakeviz)
```bash
python -m cProfile -o profile.prof script.py
snakeviz profile.prof
```

#### Python (py-spy, sampling, no code changes)
```bash
pip install py-spy
py-spy record -o profile.svg --pid <pid>
py-spy top --pid <pid>                          # live top-like view
```

#### Node (CPU profiler)
```bash
node --prof app.js                              # writes v8.log
node --prof-process isolate-*.log > profile.txt

# Or with inspector
node --inspect app.js
# DevTools → Performance → Record → Stop → Flame chart
```

#### Go (pprof)
```go
import _ "net/http/pprof"

go func() {
    http.ListenAndServe("localhost:6060", nil)
}()
```
```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
# (pprof) top
# (pprof) list MyFunction
# (pprof) web                                    # SVG flame graph (needs graphviz)
```

#### Rust (perf + flamegraph)
```bash
cargo install flamegraph
flamegraph -o flamegraph.svg -- ./target/release/app
# Or perf record + perf script + FlameGraph repo
```

### Reading a flame graph
- **Width** = time spent in that function (including children)
- **Stack** = call stack, top of stack at the bottom
- **Look for wide plates** — those are the functions to optimise
- **Compare before/after** — make a flame graph before the fix and after, side by side

### Memory Profiling

#### Python (tracemalloc)
```python
import tracemalloc
tracemalloc.start()
# ... run the suspect code ...
snapshot = tracemalloc.take_snapshot()
top = snapshot.statistics('lineno')
for stat in top[:10]:
    print(stat)
```

#### Python (memray)
```bash
pip install memray
python -m memray run -o output.bin script.py
memray flamegraph output.bin
```

#### Node (heap snapshots)
```bash
node --inspect app.js
# DevTools → Memory → Heap snapshot
# Take baseline, trigger leak, take another, compare
```

#### Go
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
# (pprof) top
# (pprof) web
```

### Lock Contention Profiling

#### Go (block profile)
```go
runtime.SetBlockProfileRate(1)
```
```bash
go tool pprof http://localhost:6060/debug/pprof/block
```

#### Go (mutex profile)
```go
runtime.SetMutexProfileFraction(1)
```
```bash
go tool pprof http://localhost:6060/debug/pprof/mutex
```

## Logging Strategies for Debugging

### Add logs at boundaries
```typescript
// Service entry
logger.info({ userId, action: 'create_order' }, 'service_enter');

// External call
logger.debug({ url, method }, 'http_call_start');
const response = await fetch(url);
logger.debug({ url, status: response.status, durationMs }, 'http_call_end');

// Decision point
logger.debug({ userId, hasPermission, requiredRole }, 'authz_decision');

// Service exit
logger.info({ orderId, durationMs }, 'service_exit');
```

### Log the unexpected
```typescript
if (user.role !== 'user' && user.role !== 'admin') {
  logger.warn({ userId, role: user.role }, 'unexpected_role');
}
```

### Don't log inside hot loops
```typescript
// ❌ Generates millions of log lines
for (const item of items) {
  logger.info({ item }, 'processing');
  process(item);
}

// ✅ Log progress at intervals
for (const [i, item] of items.entries()) {
  if (i % 1000 === 0) logger.info({ i, total: items.length }, 'progress');
  process(item);
}
```

## Quick Reference

| Need | Tool |
|---|---|
| Structured logs (Python) | structlog |
| Structured logs (Node) | pino |
| Structured logs (Go) | log/slog (1.21+) |
| Metrics | Prometheus + Grafana |
| Traces | OpenTelemetry → Jaeger / Tempo / Zipkin |
| CPU profile (Python) | cProfile, py-spy |
| CPU profile (Node) | `--prof`, DevTools Performance |
| CPU profile (Go) | pprof CPU |
| CPU profile (Rust) | perf, flamegraph crate |
| Memory (Python) | tracemalloc, memray |
| Memory (Node) | DevTools heap snapshot |
| Memory (Go) | pprof heap |
| Lock contention (Go) | pprof block, pprof mutex |
| Flame graph | `py-spy record -o profile.svg`, `go tool pprof -web`, `flamegraph` (Rust) |
| Correlation ID | generate at edge, propagate via `x-request-id` / `traceparent` |
| Redaction | pino `redact`, structlog processor |
