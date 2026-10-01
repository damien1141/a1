# gRPC + Protobuf Contracts

gRPC for service-to-service RPC: binary protobuf payloads, schema-strict, bidi-streaming, polyglot codegen. Use for internal mesh; pair with Connect-RPC or grpc-web for browser/mobile reach. Lint with `buf lint`; generate with `buf generate`.

## When gRPC

| Need | Pick |
|---|---|
| Internal service-to-service, schema-strict | **gRPC** |
| Bidi-streaming (chat, sync, telemetry) | **gRPC** (or WebSocket for browser) |
| Polyglot services (Go + Java + Python) | **gRPC** — codegen for every language |
| Browser/mobile client | **Connect-RPC** (HTTP/JSON + protobuf) or grpc-web |
| Public API, third-party integrations | **REST/OpenAPI** (gRPC is too rigid for clients you don't control) |

gRPC defaults: HTTP/2, protobuf, unary + server-stream + client-stream + bidi-stream, pluggable auth/interceptors/load balancing.

## proto3 service definition

```proto
// order/v1/order.proto
syntax = "proto3";

package order.v1;

import "google/protobuf/timestamp.proto";
import "google/protobuf/field_mask.proto";

// Service — one service per bounded context
service OrderService {
  // Unary — request/response RPC
  rpc CreateOrder(CreateOrderRequest) returns (Order);
  // Server-stream — server pushes updates for one request
  rpc StreamUpdates(OrderId) returns (stream OrderUpdate);
  // Client-stream — client streams, server responds once
  rpc ImportOrders(stream Order) returns (ImportSummary);
  // Bidi-stream — both sides stream
  rpc SyncInventory(stream Sku) returns (stream InventoryDelta);
}

message Order {
  string id = 1;
  string customer_id = 2;
  repeated LineItem items = 3;
  google.protobuf.Timestamp created_at = 4;
  OrderStatus status = 5;
  // EVOLUTION: add fields, never reuse numbers, never change types.
  // Deprecated field — keep the number, mark deprecated, do not delete.
  string legacy_coupon = 6 [deprecated = true];
}

message LineItem {
  string sku = 1;
  int32 quantity = 2;
  // proto3 scalar fields are implicit `optional` since v3.15; use `optional`
  // keyword for explicit presence tracking on scalars.
  optional string note = 3;
}

enum OrderStatus {
  ORDER_STATUS_UNSPECIFIED = 0;   // always have a zero value (proto3 default)
  ORDER_STATUS_PENDING = 1;
  ORDER_STATUS_PAID = 2;
  ORDER_STATUS_SHIPPED = 3;
  ORDER_STATUS_CANCELLED = 4;
}

message CreateOrderRequest {
  string customer_id = 1;
  repeated LineItem items = 2;
}

message OrderId { string id = 1; }
message OrderUpdate { string order_id = 1; OrderStatus status = 2; }
message ImportSummary { int32 inserted = 1; int32 failed = 2; }
message Sku { string id = 1; int32 quantity = 2; }
message InventoryDelta { string sku = 1; int32 delta = 2; }
```

## Backward compatibility rules (proto3)

**Safe (non-breaking):**
- Add a new field with a new number — old clients ignore it; new clients see default.
- Add a new enum value (after the existing ones).
- Mark a field `deprecated = true` (cosmetic).
- Add a new RPC method.
- Change a field from `optional` to plain (rarely matters).

**Breaking — forbidden in prod:**
- Reuse a field number (data corruption — old binaries write the old type into your new field).
- Change a field type (`string` → `int32`).
- Renumber fields.
- Delete a field (use `reserved N` instead — prevents accidental reuse).
- Change `optional` → `required` (proto3 has no `required` keyword anyway; do not add it back via proto2).
- Rename a service or RPC (client codegen breaks).
- Remove an enum value (old clients may send it; new server rejects).

```proto
// Correct deprecation — reserve the number, never reuse
message Order {
  reserved 6;                    // was legacy_coupon
  reserved "legacy_coupon";      // also reserve the name
  string id = 1;
  // ...
}
```

## buf workflow

`buf.yaml` (module + lint config):
```yaml
version: v1
name: buf.build/acme/order
lint:
  use: [DEFAULT]
  except: [RPC_REQUEST_RESPONSE_UNIQUE]
  enum_zero_value_suffix: _UNSPECIFIED
breaking:
  use: [FILE]
```

`buf.gen.yaml` (codegen):
```yaml
version: v1
plugins:
  - plugin: buf.build/protocolbuffers/go
    out: gen/go
    opt: paths=source_relative
  - plugin: buf.build/grpc/go
    out: gen/go
    opt: paths=source_relative,require_unimplemented_servers=false
  - plugin: buf.build/connectrpc/go
    out: gen/go
```

Commands:
```bash
buf lint                    # lint all .proto files
buf breaking --against .git#branch=main   # CI gate: no breaking changes since main
buf generate                # codegen for all configured languages
buf build                   # produce a BSR image for pushing
```

Run `buf breaking` in CI on every PR; block merge if it fails.

## Server (Go example)

```go
type orderServer struct {
    pb.UnimplementedOrderServiceServer   // forces forward-compat: new RPCs return UNIMPLEMENTED
    repo OrderRepo
}

func (s *orderServer) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.Order, error) {
    if req.CustomerId == "" {
        return nil, status.Error(codes.InvalidArgument, "customer_id required")
    }
    order, err := s.repo.Create(ctx, req)
    if err != nil {
        log.Error(err)
        return nil, status.Error(codes.Internal, "create failed")
    }
    return order, nil
}

func (s *orderServer) StreamUpdates(req *pb.OrderId, stream pb.OrderService_StreamUpdatesServer) error {
    ch := s.repo.Subscribe(req.Id)
    for {
        select {
        case <-stream.Context().Done():
            return nil
        case upd := <-ch:
            if err := stream.Send(&pb.OrderUpdate{OrderId: req.Id, Status: upd.Status}); err != nil {
                return err
            }
        }
    }
}
```

## Interceptors — auth, logging, metrics, retry

```go
// Auth interceptor — verify JWT in metadata
func authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok { return nil, status.Error(codes.Unauthenticated, "no metadata") }
    tokens := md.Get("authorization")
    if len(tokens) == 0 { return nil, status.Error(codes.Unauthenticated, "no token") }
    user, err := verifyJwt(strings.TrimPrefix(tokens[0], "Bearer "))
    if err != nil { return nil, status.Error(codes.Unauthenticated, "bad token") }
    return handler(context.WithValue(ctx, userKey, user), req)
}

// Metrics interceptor — Prometheus
func metricsInterceptor(...) (interface{}, error) {
    start := time.Now()
    resp, err := handler(ctx, req)
    grpcRequestDuration.WithLabelValues(info.FullMethod, status.Code(err).String()).Observe(time.Since(start).Seconds())
    return resp, err
}

// Chain them
srv := grpc.NewServer(
    grpc.ChainUnaryInterceptor(authInterceptor, metricsInterceptor, loggingInterceptor),
)
```

## Status codes (gRPC ↔ HTTP)

| gRPC code | HTTP/2 | When |
|---|---|---|
| OK | 200 | Success |
| InvalidArgument | 400 | Bad input |
| Unauthenticated | 401 | Missing/bad auth |
| PermissionDenied | 403 | Authenticated but not allowed |
| NotFound | 404 | Resource doesn't exist |
| AlreadyExists | 409 | Duplicate |
| FailedPrecondition | 400 | State mismatch (e.g. cancel shipped order) |
| ResourceExhausted | 429 | Rate limited / quota |
| Unimplemented | 501 | Method not implemented |
| Internal | 500 | Server bug |
| Unavailable | 503 | Down/overloaded |
| DeadlineExceeded | 504 | Client timeout |

Always map to canonical codes; clients switch on codes, not strings.

## Deadlines, cancellation, context

```go
// Client — always set a deadline
ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
order, err := client.CreateOrder(ctx, req)

// Server — respect ctx.Done()
func (s *orderServer) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.Order, error) {
    select {
    case <-ctx.Done():
        return nil, status.FromContextError(ctx.Err()).Err()
    default:
    }
    // ...
}
```

Never start a long operation without a context deadline. Propagate `ctx` into every downstream call (DB, other RPCs, file I/O).

## Load balancing

| Mode | How |
|---|---|
| Client-side LB (default) | gRPC client picks a backend per RPC via DNS resolver round-robin |
| Proxy LB (Envoy, Linkerd) | L7 proxy balances; client talks to proxy |
| xDS | Control plane pushes backend list to client (advanced; service mesh territory) |

For K8s: use a headless Service (returns pod IPs) + client-side LB. Don't use a normal Service with ClusterIP — gRPC uses HTTP/2 keep-alive, so connections stick to one pod forever, defeating round-robin.

## Connect-RPC (browser/mobile friendly)

Connect-RPC speaks protobuf over HTTP/JSON or HTTP/2 binary. Same `.proto`, same codegen, browser-friendly.

```go
// Server
mux := http.NewServeMux()
mux.Handle(orderv1connect.NewOrderServiceHandler(&orderServer{}))
http.ListenAndServe(":8080", mux)

// Client (browser)
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
const transport = createConnectTransport({ baseUrl: "https://api.example.com" });
const client = createClient(OrderService, transport);
const order = await client.createOrder({ customerId: "u1", items: [...] });
```

## Verification gates

- `buf lint` — style + correctness.
- `buf breaking --against .git#branch=main` — no breaking changes.
- `buf generate` — codegen succeeds for all target languages.
- Integration tests: each RPC returns expected shape under each status code.
- `grpcurl` — manual probing: `grpcurl -plaintext -d '{"customer_id":"u1"}' localhost:8080 order.v1.OrderService/CreateOrder`.
- Load test: `ghz` for unary, `grpcannon` for streaming; verify p99 latency, no connection leaks.
- Health check: register `grpc_health_v1.Health` service; K8s liveness probe uses it.
