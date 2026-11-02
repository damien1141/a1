# Observability — Metrics, Logs, Traces

## Three Pillars

| Pillar | Tool | What it answers |
|---|---|---|
| **Metrics** | Prometheus | Aggregated numeric data over time. "Is it slow? For whom?" |
| **Logs** | Loki / ELK / Datadog | Discrete events with context. "What happened in this request?" |
| **Traces** | Tempo / Jaeger / Honeycomb | Causal chain across services. "Where did the time go?" |

All three must share a **correlation ID** (trace_id) so you can pivot between them.

## Metrics — Prometheus

### Metric types

| Type | Use | Example |
|---|---|---|
| `Counter` | Cumulative, only increases | `http_requests_total` |
| `Gauge` | Point-in-time, up/down | `active_connections` |
| `Histogram` | Distribution in buckets | `http_request_duration_seconds` |
| `Summary` | Client-computed quantiles | (prefer Histogram for aggregatability) |

### Node.js instrumentation

```typescript
import { Counter, Histogram, Gauge, collectDefaultMetrics, register } from 'prom-client';

collectDefaultMetrics();  // Node.js runtime metrics

const httpRequests = new Counter({
  name: 'http_requests_total',
  help: 'Total HTTP requests',
  labelNames: ['method', 'route', 'status'],
});

const httpDuration = new Histogram({
  name: 'http_request_duration_seconds',
  help: 'HTTP request duration',
  labelNames: ['method', 'route'],
  buckets: [0.01, 0.05, 0.1, 0.3, 0.5, 1, 2, 5],  // seconds
});

const inFlight = new Gauge({
  name: 'http_requests_in_flight',
  help: 'In-flight HTTP requests',
  labelNames: ['method'],
});

// Middleware
app.use((req, res, next) => {
  inFlight.inc({ method: req.method });
  const end = httpDuration.startTimer({ method: req.method, route: req.route?.path ?? req.path });

  res.on('finish', () => {
    httpRequests.inc({ method: req.method, route: req.route?.path ?? req.path, status: res.statusCode });
    end();
    inFlight.dec({ method: req.method });
  });

  next();
});

app.get('/metrics', async (_req, res) => {
  res.set('Content-Type', register.contentType);
  res.end(await register.metrics());
});
```

### Python instrumentation

```python
from prometheus_client import Counter, Histogram, Gauge, generate_latest, CONTENT_TYPE_LATEST
from fastapi import FastAPI, Request, Response
import time

app = FastAPI()

http_requests = Counter('http_requests_total', 'Total HTTP requests', ['method', 'route', 'status'])
http_duration = Histogram('http_request_duration_seconds', 'HTTP latency', ['method', 'route'],
                          buckets=[0.01, 0.05, 0.1, 0.3, 0.5, 1, 2, 5])
in_flight = Gauge('http_requests_in_flight', 'In-flight requests')

@app.middleware("http")
async def instrument(request: Request, call_next):
    in_flight.inc()
    start = time.perf_counter()
    try:
        response = await call_next(request)
        return response
    finally:
        elapsed = time.perf_counter() - start
        route = request.url.path
        http_duration.labels(method=request.method, route=route).observe(elapsed)
        http_requests.labels(method=request.method, route=route, status=response.status_code).inc()
        in_flight.dec()

@app.get("/metrics")
def metrics():
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)
```

### Naming conventions

- Units in suffix: `_seconds`, `_bytes`, `_total` (for counters)
- Base unit: seconds (not ms), bytes (not KB)
- Prefix with service: `orders_api_...`
- Use `_total` suffix on counters (Prometheus convention)
- Avoid `application_*` — too generic

### Cardinality rules

| OK (low cardinality) | DANGER (high cardinality) |
|---|---|
| `method`, `route`, `status` | `user_id`, `session_id`, `request_id`, `ip` |
| `service`, `instance`, `env` | `url` (contains IDs), `email` |

Rule of thumb: total active time series < few million. Each label that can take many values multiplies cardinality.

### PromQL cheatsheet

```promql
# Rate of requests per second
sum(rate(http_requests_total[5m])) by (service)

# p99 latency
histogram_quantile(0.99,
  sum(rate(http_request_duration_seconds_bucket[5m])) by (le, route)
)

# Error ratio
sum(rate(http_requests_total{status=~"5.."}[5m]))
  /
sum(rate(http_requests_total[5m]))

# Top 5 routes by error rate
topk(5,
  sum(rate(http_requests_total{status=~"5.."}[5m])) by (route)
    /
  sum(rate(http_requests_total[5m])) by (route)
)

# Saturation: CPU throttling ratio
sum(rate(container_cpu_cfs_throttled_seconds_total[5m])) by (pod)
  /
sum(rate(container_cpu_cfs_periods_total[5m])) by (pod)

# SLO: remaining error budget (99.9% SLO, 30d window)
1 - (
  (1 - (
    sum(rate(http_requests_total{status=~"2..|4.."}[30d]))
    /
    sum(rate(http_requests_total[30d]))
  )) / 0.001
)
```

### Recording rules (precompute heavy queries)

```yaml
groups:
  - name: orders-api-recording
    rules:
      - record: orders_api:request_rate:5m
        expr: sum(rate(http_requests_total[5m]))

      - record: orders_api:error_rate:5m
        expr: |
          sum(rate(http_requests_total{status=~"5.."}[5m]))
            /
          sum(rate(http_requests_total[5m]))

      - record: orders_api:p99_latency:5m
        expr: |
          histogram_quantile(0.99,
            sum(rate(http_request_duration_seconds_bucket[5m])) by (le, route)
          )
```

Validate:

```bash
promtool check rules recording.yml
promtool check config prometheus.yml
```

## Structured Logging

### Principles

1. **JSON output** — parseable by Loki/ELK
2. **Always include correlation IDs** — `request_id`, `trace_id`, `span_id`
3. **Use fields, not interpolation** — `{"user_id": 42}` not `"user 42"`
4. **Log to stderr** — keep stdout for app data
5. **Levels** — DEBUG, INFO, WARN, ERROR (never use ERROR for expected behaviors)
6. **Never log secrets** — passwords, tokens, PII

### Node.js (Pino)

```javascript
import pino from 'pino';
import { trace, context } from '@opentelemetry/api';

const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info',
  formatters: {
    log(obj) {
      const span = trace.getSpan(context.active());
      if (span) {
        const ctx = span.spanContext();
        return { ...obj, trace_id: ctx.traceId, span_id: ctx.spanId };
      }
      return obj;
    },
  },
});

// Good
logger.info({ orderId: 123, userId: 42, durationMs: 120 }, 'order.created');

// Bad — interpolation loses structure
console.log(`Order 123 created for user 42 in 120ms`);
```

### Python (structlog)

```python
import structlog
import logging
import sys

structlog.configure(
    processors=[
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.JSONRenderer(),
    ],
    wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
    logger_factory=structlog.PrintLoggerFactory(file=sys.stdout),
)

log = structlog.get_logger()

# Bind request context
log = log.bind(request_id=req.id, trace_id=trace_id)
log.info("order.created", order_id=123, user_id=42, duration_ms=120)
```

### Go (slog)

```go
package main

import (
    "log/slog"
    "os"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))
    slog.SetDefault(logger)

    slog.Info("order.created",
        "order_id", 123,
        "user_id", 42,
        "duration_ms", 120,
        "trace_id", traceID,
    )
}
```

## Distributed Tracing — OpenTelemetry

### Concepts

| Term | Definition |
|---|---|
| **Span** | Single operation with start/end + attributes |
| **Trace** | Tree of spans forming a request flow |
| **Context** | Propagated across service boundaries (W3C Trace Context) |
| **Attributes** | Key/value metadata on a span |
| **Events** | Timestamped logs within a span |
| **Span kind** | SERVER, CLIENT, INTERNAL, PRODUCER, CONSUMER |

### Setup (Python + FastAPI)

```python
from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.requests import RequestsInstrumentor
from opentelemetry.instrumentation.sqlalchemy import SQLAlchemyInstrumentor

resource = Resource.create({
    "service.name": "orders-api",
    "service.version": "1.4.2",
    "deployment.environment": "production",
})

provider = TracerProvider(resource=resource)
provider.add_span_processor(BatchSpanProcessor(
    OTLPSpanExporter(endpoint="http://otel-collector:4318/v1/traces")
))
trace.set_tracer_provider(provider)

app = FastAPI()
FastAPIInstrumentor.instrument_app(app)
RequestsInstrumentor().instrument()
SQLAlchemyInstrumentor().instrument(engine=db.engine)

# Manual spans for business logic
tracer = trace.get_tracer(__name__)

@app.post("/orders")
async def create_order(req: OrderRequest):
    with tracer.start_as_current_span("create_order") as span:
        span.set_attribute("order.user_id", req.user_id)
        span.set_attribute("order.amount", req.amount)

        with tracer.start_as_current_span("validate_inventory"):
            inventory.check(req.items)

        with tracer.start_as_current_span("charge_payment") as ps:
            try:
                payment.charge(req.user_id, req.amount)
                ps.set_attribute("payment.status", "ok")
            except PaymentError as e:
                ps.set_status(trace.Status(trace.StatusCode.ERROR))
                ps.record_exception(e)
                raise
```

### Context propagation

OTel propagates `traceparent` (W3C) header automatically via instrumented HTTP clients (requests, httpx, fetch, axios). For non-instrumented clients, inject manually:

```python
from opentelemetry import propagate, context

# Inject on outgoing request
headers = {}
propagate.inject(headers)
resp = httpx.post("http://downstream/api", headers=headers)

# Extract on incoming (auto with FastAPIInstrumentor, but for custom servers):
ctx = propagate.extract(request.headers)
with trace.use_context(ctx):
    ...
```

### Sampling

Production sampling: 1-10% to control cost. Always keep errors.

```python
from opentelemetry.sdk.trace.sampling import TraceIdRatioBased, ParentBased, ALWAYS_ON

# 5% sampling, but always sample if parent was sampled
sampler = ParentBased(root=TraceIdRatioBased(0.05))
provider = TracerProvider(resource=resource, sampler=sampler)
```

For "always sample errors": use a custom sampler or tail-based sampling in the OTel Collector:

```yaml
# otel-collector-config.yaml
processors:
  tail_sampling:
    decision_wait: 10s
    policies:
      - { name: errors, type: status_code, status_code: { status_codes: [ERROR] } }
      - { name: slow, type: latency, latency: { threshold_ms: 1000 } }
      - { name: baseline, type: probabilistic, probabilistic: { sampling_percentage: 5 } }
```

## OTel Collector

The collector sits between your app and backends (Tempo, Jaeger, Honeycomb, Datadog). Decouples SDK from backend choice.

```yaml
receivers:
  otlp:
    protocols:
      grpc: { endpoint: 0.0.0.0:4317 }
      http: { endpoint: 0.0.0.0:4318 }

processors:
  batch:
    timeout: 5s
    send_batch_size: 1000
  memory_limiter:
    check_interval: 1s
    limit_percentage: 80
    spike_limit_percentage: 25
  attributes:
    actions:
      - { key: environment, action: upsert, value: production }
  tail_sampling:
    decision_wait: 10s
    policies:
      - { name: errors, type: status_code, status_code: { status_codes: [ERROR] } }
      - { name: baseline, type: probabilistic, probabilistic: { sampling_percentage: 5 } }

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls: { insecure: true }
  prometheusremotewrite:
    endpoint: http://mimir:9009/api/v1/push
  loki:
    endpoint: http://loki:3100/loki/api/v1/push

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, tail_sampling, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [loki]
```

Validate:

```bash
otelcol validate --config otel-collector-config.yaml
otelcol --dry-run --config otel-collector-config.yaml
```

## Grafana Dashboards

Use the **RED method** for services (Rate, Errors, Duration) and **USE method** for resources (Utilization, Saturation, Errors).

### RED dashboard panels (per service)

1. **R** — Request rate: `sum(rate(http_requests_total{job="$service"}[5m])) by (route)`
2. **E** — Error rate: `sum(rate(http_requests_total{job="$service",status=~"5.."}[5m])) by (route) / sum(rate(http_requests_total{job="$service"}[5m])) by (route)`
3. **D** — Duration p50/p95/p99: `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{job="$service"}[5m])) by (le, route))`
4. Saturation — CPU/memory/throttling
5. SLO budget remaining (single-stat panel)

### USE dashboard panels (per resource)

1. **U** — Utilization: CPU%, memory%, disk%
2. **S** — Saturation: queue depth, throttling, connection pool used/total
3. **E** — Errors: disk errors, OOM kills, NIC drops

## Health endpoints

Every service should expose:

- `/live` — process is alive (returns 200 if app responds; used for liveness probe)
- `/ready` — ready to serve traffic (returns 200 if dependencies OK; used for readiness probe)
- `/metrics` — Prometheus scrape
- `/health` — combined: includes dependency checks for humans

```python
@app.get("/ready")
async def ready():
    checks = {
        "db": await check_db(),
        "redis": await check_redis(),
        "kafka": await check_kafka(),
    }
    healthy = all(checks.values())
    return Response(
        status_code=200 if healthy else 503,
        content=json.dumps(checks),
        media_type="application/json",
    )
```

## Common Pitfalls

1. **Logging without trace_id** — can't correlate logs to traces. Always include.
2. **Histogram buckets poorly chosen** — too coarse (`[1, 10]`) misses p99 around 500ms. Use `[0.01, 0.05, 0.1, 0.3, 0.5, 1, 2, 5]`.
3. **100% trace sampling** — costs explode. Use 1-10% baseline + 100% errors.
4. **Alerting on CPU > 80%** — utilization is not saturation. Check queue depth, throttling, latency instead.
5. **Mixing stdout + stderr** — logs go to stdout, errors to stderr is OK; mixing them in JSON pipelines breaks parsing.
6. **Log rotation missing** — disk fills. Use journald or container logging driver.
7. **Cardinality explosion** — `http_requests_total{user_id="42"}` will OOM Prometheus. Never put user/request IDs as labels.
8. **No `/metrics` auth** — exposes internal structure. Put behind auth or restrict to scrape CIDR.
