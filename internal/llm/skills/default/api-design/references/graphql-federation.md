# GraphQL — Schema, Federation, DataLoader, Complexity

Schema-first design, Apollo Federation 2.5+ subgraphs, N+1 prevention with DataLoader, and abuse defenses (depth limit, complexity limit, persisted queries). Validate composition with `rover supergraph compose --config supergraph.yaml` before deploying.

## Schema-first rules

1. **camelCase fields, PascalCase types** — GraphQL convention; do not fight it.
2. **Nullability is a contract** — non-null (`!`) when absence is a bug; nullable when absence is normal. Prefer nullable for new fields; never widen a non-null to nullable without a version bump.
3. **One mutation = one input type** — `CreateUserInput`, `UpdateUserInput`. Never reuse inputs across mutations.
4. **Connections for lists** — `UserConnection { edges: [UserEdge], pageInfo: PageInfo }` (Relay spec) for paginated lists; plain `[User!]!` only for bounded lookups (`tags: [String!]!`).
5. **Enums for closed sets** — `enum OrderStatus { PENDING PAID SHIPPED }`. String for open sets.
6. **Interfaces for shared shape** — `interface Node { id: ID! }`. Unions for "one of these types" without shared shape.
7. **Custom scalars for domain types** — `scalar DateTime`, `scalar UUID`, `scalar Email`. Never `String` for typed data.

## Federation 2.5+ subgraph patterns

Each subgraph owns a slice of the graph. Entities (`@key`) are shared across subgraphs; only one subgraph *resolves* each field, others *extend*.

```graphql
# products subgraph — owns Product
type Product @key(fields: "id") {
  id: ID!
  name: String!
  price: Float!
  inStock: Boolean!
}

# reviews subgraph — extends Product with reviews
type Product @key(fields: "id") {
  id: ID! @external               # owned by products subgraph
  reviews: [Review!]!             # resolved by reviews subgraph
}

type Review @key(fields: "id") {
  id: ID!
  rating: Int!
  body: String
  author: User!                   # User is an entity owned by users subgraph
}

# users subgraph — owns User
type User @key(fields: "id") {
  id: ID!
  username: String!
  email: Email!
}
```

Directives cheat sheet:
- `@key(fields: "id")` — declares an entity; resolvable across subgraphs.
- `@external` — field owned by another subgraph.
- `@shareable` — field resolvable by multiple subgraphs (use sparingly).
- `@provides(fields: "username")` — this subgraph can populate `username` for `User` returned here.
- `@requires(fields: "price")` — this subgraph's resolver needs `price` (from owning subgraph) to compute.
- `@override(from: "products")` — newer subgraph takes over resolution (one-shot migration tool).

### Composition checks

```bash
rover supergraph compose --config supergraph.yaml > supergraph.graphql
# Fails on:
#   - @key field not resolvable in any subgraph
#   - @shareable on a field only one subgraph resolves
#   - @external on a field no subgraph owns
#   - type defined in two subgraphs without @shareable or @key
```

Fix composition errors before deploying. A broken subgraph breaks the whole gateway.

## Resolvers

```js
const resolvers = {
  Query: {
    user: (_root, { id }, { dataLoaders }) => dataLoaders.user.load(id),
    products: async (_root, { first, after }, { dataLoaders }) => {
      const { items, nextCursor } = await productsRepo.list({ first, after });
      return {
        edges: items.map((node) => ({ node, cursor: node.id })),
        pageInfo: { hasNextPage: !!nextCursor, endCursor: nextCursor },
      };
    },
  },
  Mutation: {
    createReview: async (_root, { input }, { user, dataLoaders }) => {
      if (!user) throw new ForbiddenError("auth required");
      const review = await reviewsRepo.create({ ...input, authorId: user.id });
      return { review };
    },
  },
  Product: {
    reviews: (product, _args, { dataLoaders }) =>
      dataLoaders.reviewsByProduct.load(product.id),
  },
  Review: {
    author: (review, _args, { dataLoaders }) => dataLoaders.user.load(review.authorId),
  },
};
```

## DataLoader — N+1 prevention

The N+1 problem: `Query { users { reviews { author { username } } } }` issues one query for users, then N for each user's reviews, then N for each review's author. DataLoader batches all `author` loads in a single tick into one `WHERE id IN (...)` query.

```js
import DataLoader from 'dataloader';

function buildLoaders({ db }) {
  return {
    user: new DataLoader(async (userIds) => {
      const users = await db.users.findMany({ where: { id: { in: userIds } } });
      const byId = new Map(users.map((u) => [u.id, u]));
      // CRITICAL: return results in same order as input keys; null for missing
      return userIds.map((id) => byId.get(id) ?? null);
    }),
    reviewsByProduct: new DataLoader(async (productIds) => {
      const reviews = await db.reviews.findMany({ where: { productId: { in: productIds } } });
      // Group by product, then map each input key to its array
      const byProduct = new Map();
      for (const r of reviews) {
        if (!byProduct.has(r.productId)) byProduct.set(r.productId, []);
        byProduct.get(r.productId).push(r);
      }
      return productIds.map((id) => byProduct.get(id) ?? []);
    }),
  };
}

const server = new ApolloServer({
  schema,
  // Fresh loaders per request — caches are request-scoped, never shared
  context: ({ req }) => ({ user: req.user, dataLoaders: buildLoaders({ db }) }),
});
```

Rules:
- **One DataLoader instance per request.** Sharing across requests leaks cache and breaks batching.
- **Preserve input order** in the batch function. DataLoader matches results to keys by index.
- **Return null for missing keys**, not undefined. Same length as input.
- **Batch keys** via `WHERE id IN (...)` (single SQL query), not N queries.
- **Cache by key within the request** — repeated `load(42)` in the same query hits the cache once.

## Security: depth, complexity, persisted queries

Without limits, a malicious query like `{ users { reviews { author { reviews { author { reviews { ... } } } } } } }` will exhaust the DB.

```js
import { createComplexityRule, simpleEstimator, fieldExtensionsEstimator } from 'graphql-query-complexity';
import depthLimit from 'graphql-depth-limit';

const server = new ApolloServer({
  schema,
  validationRules: [
    depthLimit(7),                                    // max nesting = 7
    createComplexityRule({
      maximumComplexity: 1000,
      estimators: [
        fieldExtensionsEstimator(),                   // per-field @cost(value: 5) directive
        simpleEstimator({ defaultComplexity: 1 }),    // fallback: 1 per field
      ],
      onCost: (cost) => { if (cost > 1000) throw new Error(`Query too complex: ${cost}`); },
    }),
  ],
});
```

- **Depth limit:** 7-10 levels is usually enough for any real client.
- **Complexity limit:** assign costs via `@cost(value: N)` directive — DB-touching fields cost more. Sum > limit → reject.
- **Rate limit** per IP/user at the gateway (Apollo Server middleware or proxy layer).
- **Persisted queries** in production: client sends a query hash, server looks up the stored query. Disables arbitrary ad-hoc queries entirely.

```js
import { ApolloServer } from '@apollo/server';
import responseCachePlugin from '@apollo/server-plugin-response-cache';

const server = new ApolloServer({
  schema,
  // Persisted queries via APQ or external store
  persistedQueryCache: redisCache,
  // Response cache for idempotent queries
  plugins: [responseCachePlugin()],
});
```

## Subscriptions (real-time over WebSocket)

```graphql
type Subscription {
  reviewAdded(productId: ID!): Review!
}
```

```js
import { makeExecutableSchema } from '@graphql-tools/schema';
import { WebSocketServer } from 'ws';
import { useServer } from 'graphql-ws/lib/use/ws';

const schema = makeExecutableSchema({ typeDefs, resolvers });
const wsServer = new WebSocketServer({ port: 4000, path: '/graphql' });
useServer({ schema, context: async (ctx) => {
  const token = ctx.connectionParams?.authToken;
  const user = verifyJwt(token);
  return { user, pubsub };
} }, wsServer);

// In resolver
const resolvers = {
  Subscription: {
    reviewAdded: { subscribe: (_root, { productId }, { pubsub }) => pubsub.asyncIterator(`review:${productId}`) },
  },
  Mutation: {
    createReview: async (_root, { input }, { pubsub }) => {
      const review = await reviewsRepo.create(input);
      pubsub.publish(`review:${review.productId}`, { reviewAdded: review });
      return review;
    },
  },
};
```

For horizontal scaling, back `pubsub` with Redis (`graphql-redis-subscriptions`) so events fan out across instances.

## Verification gates

- `rover supergraph compose` — composition succeeds.
- `rover subgraph check` — schema diff against prod (CI gate).
- `graphql-depth-limit` + `graphql-query-complexity` enforced in `validationRules`.
- Persisted-query enforcement: production rejects un-persisted queries.
- Integration tests: each resolver returns expected shape; DataLoader batches confirmed via query-count assertion.
- Load test (`k6`): p99 latency under expected concurrency; DB query count grows O(1) per depth level, not O(N^depth).

## Anti-patterns

- **Passing raw `req.body` to resolvers** — bypasses the schema; defeats the type system.
- **Business logic in resolvers** — resolvers should delegate to services. Resolvers translate between graph and domain.
- **Exposing DB column names** — `user.last_login_at` becomes `user.lastSeenAt`. The schema is a public API, not a DB projection.
- **N+1 without DataLoader** — profile with DB query logging in dev; count should grow slowly with depth.
- **Non-null on new fields** — start nullable, widen later. Bumping from non-null to nullable is a breaking change.
- **Mutations returning scalar** — return the mutated entity or a payload type (`{ user: User!, errors: [Error!] }`).
