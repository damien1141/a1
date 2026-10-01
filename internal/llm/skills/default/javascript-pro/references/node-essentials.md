# Node.js Essentials

Baseline: Node 20 LTS+ (Node 22 current). ESM-first. Use `node:` prefix for built-in imports.

## File system — `fs/promises`

```js
import { readFile, writeFile, appendFile, mkdir, rm, readdir, stat, cp } from 'node:fs/promises';
import { access } from 'node:fs/promises';

const text = await readFile('./file.txt', 'utf-8');
await writeFile('./out.txt', text);
await appendFile('./log.txt', `${new Date().toISOString()} event\n`);
await mkdir('./nested/path', { recursive: true });
await rm('./temp', { recursive: true, force: true });
await cp('./src', './dest', { recursive: true });

// List with file types
const entries = await readdir('./src', { withFileTypes: true });
const files = entries.filter((e) => e.isFile()).map((e) => e.name);

// Stats
const s = await stat('./file.txt');
s.size; s.mtime; s.isFile(); s.isDirectory();

// Existence check — do NOT use existsSync in async code
try { await access('./path'); /* exists */ }
catch { /* does not exist */ }
```
Use `fs/promises` over `fs` callbacks. Reserve sync APIs (`readFileSync`) for CLI startup before the event loop matters.

## Paths and `import.meta`

```js
import { join, resolve, dirname, basename, extname, parse } from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname  = dirname(__filename);
// Node 20.11+ also exposes:
import.meta.dirname;  // __dirname equivalent
import.meta.filename; // __filename equivalent

join(__dirname, 'data', 'config.json');  // platform-correct
resolve('./relative');                    // absolute
basename('/a/b.txt');                     // 'b.txt'
extname('/a/b.txt');                      // '.txt'
parse('/a/b.txt');                        // { root, dir, base, ext, name }
```

## Streams — `stream/promises`

```js
import { createReadStream, createWriteStream } from 'node:fs';
import { pipeline } from 'node:stream/promises';
import { Transform } from 'node:stream';

// Async-iterate a file
for await (const chunk of createReadStream('./big.txt', { encoding: 'utf-8' })) {
  process(chunk);
}

// Pipe with backpressure handling + error propagation
const upper = new Transform({
  transform(chunk, _enc, cb) { cb(null, chunk.toString().toUpperCase()); },
  // object mode: { objectMode: true }
});

await pipeline(
  createReadStream('./in.txt'),
  upper,
  createWriteStream('./out.txt'),
);
```
Rules:
- Use `pipeline` not `.pipe()` — `pipeline` propagates errors and destroys streams.
- `for await...of` over a stream is convenient but does NOT propagate errors mid-stream — wrap in try/catch.
- Use `Readable.from(iterable)` / `Readable.fromWeb(webStream)` for interop with Web Streams.

## `EventEmitter`

```js
import { EventEmitter } from 'node:events';

class Processor extends EventEmitter {
  async run(items) {
    this.emit('start', { total: items.length });
    for (const item of items) {
      try {
        const r = await this.#handle(item);
        this.emit('item', r);
      } catch (err) {
        this.emit('error', err); // 'error' is special — throws if no listener
      }
    }
    this.emit('done');
  }
  async #handle(item) { /* ... */ }
}

const p = new Processor();
p.on('start', ({ total }) => console.log(`start ${total}`));
p.once('done', () => console.log('done'));
p.on('error', (err) => console.error(err));
// Remove a single listener:
const onItem = (r) => console.log(r);
p.on('item', onItem);
p.off('item', onItem);
```
- An `error` event with no listener throws and crashes the process. Always wire one.
- `EventEmitter.captureRejections = true` makes `async` listeners forward rejections to `error`.
- Cap listeners: `p.setMaxListeners(20)` if you genuinely need more than the default 10.

## `worker_threads`

```js
// main.mjs
import { Worker } from 'node:worker_threads';
import { fileURLToPath } from 'node:url';

const worker = new Worker(new URL('./worker.mjs', import.meta.url));
worker.postMessage({ task: 'hash', bytes: largeBuffer });
worker.on('message', (hash) => console.log(hash));
worker.on('error', (err) => console.error(err));
worker.on('exit', (code) => { if (code !== 0) console.error(`exit ${code}`); });
```

```js
// worker.mjs
import { parentPort } from 'node:worker_threads';
import { webcrypto } from 'node:crypto';

parentPort.on('message', async ({ task, bytes }) => {
  if (task === 'hash') {
    const digest = new Uint8Array(await webcrypto.subtle.digest('SHA-256', bytes));
    parentPort.postMessage(digest);
  }
});
```
- Pass `transferList` for `ArrayBuffer`s to avoid copy: `postMessage(buf, [buf.buffer])`.
- For long-running pools, use `piscina` (battle-tested) rather than rolling your own.

## `node:test` — built-in test runner

```js
// src/math.test.mjs
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { add } from './math.js';

describe('math', () => {
  test('adds', () => {
    assert.equal(add(1, 2), 3);
  });

  test('async with t.context', async (t) => {
    const result = await addAsync(1, 2);
    assert.equal(result, 3);
  });

  test('snapshot-like', () => {
    assert.deepEqual({ a: 1 }, { a: 1 });
  });
});
```
Run: `node --test` (auto-discovers `*.test.{js,mjs}`).
- `node --test --watch` — watch mode.
- `node --test --experimental-test-coverage` — coverage.
- Prefer `node:assert/strict` (uses `===`).

## `process` — env, args, signals

```js
const PORT = Number(process.env.PORT ?? 3000);
const isProd = process.env.NODE_ENV === 'production';
const args = process.argv.slice(2);

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
process.on('uncaughtException', (err) => {
  console.error('uncaught', err);
  process.exit(1);
});
process.on('unhandledRejection', (reason) => {
  console.error('unhandledRejection', reason);
  process.exit(1);
});

async function shutdown() {
  console.log('shutting down…');
  await server.close();
  process.exit(0);
}
```

## `node:crypto`

```js
import { randomBytes, randomUUID, createHash, webcrypto } from 'node:crypto';

randomUUID();                                  // v4 UUID
randomBytes(16).toString('hex');               // 32 hex chars
createHash('sha256').update('x').digest('hex');

// Use webcrypto for sync-style async ops matching the browser
const digest = await webcrypto.subtle.digest('SHA-256', encoded);
```
For password hashing use `scrypt` (sync or promise form):
```js
import { scrypt as scryptCb, randomBytes, timingSafeEqual } from 'node:crypto';
import { promisify } from 'node:util';
const scrypt = promisify(scryptCb);

async function hash(password) {
  const salt = randomBytes(16);
  const derived = await scrypt(password, salt, 64);
  return `${salt.toString('hex')}.${derived.toString('hex')}`;
}

async function verify(password, stored) {
  const [saltHex, expectedHex] = stored.split('.');
  const salt = Buffer.from(saltHex, 'hex');
  const expected = Buffer.from(expectedHex, 'hex');
  const derived = await scrypt(password, salt, expected.length);
  return timingSafeEqual(derived, expected);
}
```

## CJS interop (in ESM)

```js
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const legacy = require('./legacy.cjs'); // synchronous — sparingly

// ESM importing CJS — default export is module.exports
import pkg from './legacy.cjs';
```

## `--watch` mode (Node 18.11+, stable in 22)

```bash
node --watch src/index.js        # restart on file change
node --watch-path=./src --watch src/index.js
node --test --watch              # re-run tests on change
```
Combine with `--watch` for dev loops without `nodemon`.

## HTTP server (when you need it without a framework)

```js
import { createServer } from 'node:http';

const server = createServer(async (req, res) => {
  try {
    if (req.method === 'GET' && req.url === '/health') {
      res.writeHead(200, { 'content-type': 'application/json' });
      return res.end(JSON.stringify({ ok: true }));
    }
    res.writeHead(404);
    res.end();
  } catch (err) {
    res.writeHead(500);
    res.end();
  }
});

server.listen(PORT, () => console.log(`listening on :${PORT}`));
```
For anything beyond toy servers, use Hono, Fastify, or Express — see `node-backend` skill.

## Cluster / multi-core (use a process manager instead)

Prefer a process manager (`pm2`, systemd, Docker replicas, Kubernetes) over the `cluster` module. `cluster` is fine for a single-instance deployment but doesn't handle rolling deploys or zero-downtime restarts on its own.

## Quick reference

| Need | Module |
|---|---|
| Async FS | `node:fs/promises` |
| Path manipulation | `node:path` |
| Streams | `node:stream/promises` (`pipeline`) |
| Events | `node:events` |
| Threads | `node:worker_threads` |
| Tests | `node:test` + `node:assert/strict` |
| Process / env / signals | `node:process` |
| Crypto | `node:crypto` / `node:crypto/webcrypto` |
| CJS interop | `node:module` (`createRequire`) |
| `__dirname` ESM | `import.meta.dirname` (Node 20.11+) |
| Dev loop | `node --watch` |
