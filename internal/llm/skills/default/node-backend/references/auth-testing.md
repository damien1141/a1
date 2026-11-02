# Auth, Testing, Observability — Node Backend

JWT and Passport auth patterns, Jest + Supertest testing, structured logging with pino, and OpenTelemetry tracing. Applies to both NestJS and Hono.

## JWT auth — NestJS

```bash
npm i @nestjs/jwt @nestjs/passport passport passport-jwt
```

```typescript
// auth.module.ts
import { JwtModule } from '@nestjs/jwt';
import { PassportModule } from '@nestjs/passport';
import { JwtStrategy } from './jwt.strategy';

@Module({
  imports: [
    PassportModule.register({ defaultStrategy: 'jwt' }),
    JwtModule.registerAsync({
      inject: [ConfigService],
      useFactory: (cfg: ConfigService) => ({
        secret: cfg.getOrThrow('JWT_SECRET'),
        signOptions: { expiresIn: cfg.get('JWT_EXPIRES_IN', '15m') },
      }),
    }),
  ],
  providers: [JwtStrategy, AuthService],
  exports: [AuthService],
})
export class AuthModule {}

// jwt.strategy.ts
@Injectable()
export class JwtStrategy extends PassportStrategy(Strategy) {
  constructor(cfg: ConfigService) {
    super({
      jwtFromRequest: ExtractJwt.fromAuthHeaderAsBearerToken(),
      ignoreExpiration: false,
      secretOrKey: cfg.getOrThrow('JWT_SECRET'),
    });
  }
  async validate(payload: { sub: string; email: string; role: string }) {
    return { id: payload.sub, email: payload.email, role: payload.role };
  }
}

// auth.service.ts
@Injectable()
export class AuthService {
  constructor(private readonly jwt: JwtService, private readonly users: UsersService) {}

  async login(email: string, password: string) {
    const user = await this.users.findByEmail(email);
    if (!user || !(await bcrypt.compare(password, user.passwordHash)))
      throw new UnauthorizedException('invalid credentials');
    const access = await this.jwt.signAsync({ sub: user.id, email: user.email, role: user.role });
    const refresh = await this.jwt.signAsync({ sub: user.id }, { expiresIn: '7d' });
    return { accessToken: access, refreshToken: refresh };
  }
}

// controller
@UseGuards(AuthGuard('jwt'))
@Controller('me')
export class MeController {
  @Get() me(@CurrentUser() user: User) { return user; }
}
```

Rules:
- Short-lived access token (5-15min), long-lived refresh token (7d).
- Refresh token in `HttpOnly` cookie; access token in JS memory (or short-lived cookie).
- `ignoreExpiration: false` — never accept expired tokens.
- Rotate `JWT_SECRET` on schedule; support `kid` (key ID) header for smooth rotation.

## JWT auth — Hono

```typescript
import { jwt, sign, verify } from 'hono/jwt';

const secret = process.env.JWT_SECRET!;

// Middleware verifies JWT; sets c.set('jwtPayload', payload)
app.use('/api/*', jwt({ secret }));

app.get('/api/me', (c) => {
  const payload = c.get('jwtPayload') as { sub: string; email: string };
  return c.json(payload);
});

// Login route — mint tokens
app.post('/auth/login', async (c) => {
  const { email, password } = await c.req.json();
  const user = await users.findByEmail(email);
  if (!user || !(await verifyPassword(password, user.hash)))
    return c.json({ type: '.../unauthorized', title: 'Invalid credentials', status: 401 }, 401);
  const access = await sign({ sub: user.id, email, exp: Math.floor(Date.now()/1000) + 900 }, secret);
  return c.json({ accessToken: access });
});
```

## OAuth2 / social — Passport strategies

```typescript
import { PassportStrategy } from '@nestjs/passport';
import { Strategy as GoogleStrategy } from 'passport-google-oauth20';

@Injectable()
export class GoogleStrategy extends PassportStrategy(GoogleStrategy, 'google') {
  constructor(cfg: ConfigService) {
    super({
      clientID: cfg.getOrThrow('GOOGLE_CLIENT_ID'),
      clientSecret: cfg.getOrThrow('GOOGLE_CLIENT_SECRET'),
      callbackURL: '/auth/google/callback',
      scope: ['email', 'profile'],
    });
  }
  async validate(_accessToken: string, _refreshToken: string, profile: any) {
    return { id: profile.id, email: profile.emails[0].value, name: profile.displayName };
  }
}

// controller
@Get('google')          @UseGuards(AuthGuard('google'))        googleAuth() {}
@Get('google/callback') @UseGuards(AuthGuard('google'))        googleCallback(@Req() req, @Res() res) {
  const tokens = this.auth.login(req.user);
  res.cookie('refresh', tokens.refreshToken, { httpOnly: true, secure: true, sameSite: 'strict' });
  res.redirect(`/?access=${tokens.accessToken}`);
}
```

## Password hashing

```typescript
import bcrypt from 'bcrypt';

const SALT_ROUNDS = 12;                              // ~250ms — adjust to hardware

export async function hashPassword(plain: string): Promise<string> {
  return bcrypt.hash(plain, SALT_ROUNDS);
}
export async function verifyPassword(plain: string, hash: string): Promise<boolean> {
  return bcrypt.compare(plain, hash);
}
```

For > 10K users, consider argon2 (memory-hard, GPU-resistant). For > 100K, use a separate auth service or a managed provider (Auth0, Cognito, Clerk).

## Rate limiting

NestJS (`@nestjs/throttler`):
```typescript
import { ThrottlerModule, ThrottlerGuard } from '@nestjs/throttler';

@Module({
  imports: [ThrottlerModule.forRoot([{ ttl: 60_000, limit: 100 }])],
  providers: [{ provide: APP_GUARD, useClass: ThrottlerGuard }],
})
app.get('/login', /* skip with @SkipThrottle() on internal routes */);
```

Hono:
```typescript
import { rateLimit } from 'hono-rate-limiter';

app.use('/api/*', rateLimit({ windowMs: 60_000, limit: 100,
  standardHeaders: 'draft-6', keyGenerator: (c) => c.req.header('x-api-key') ?? c.req.header('cf-connecting-ip') ?? 'anon' }));
```

## Unit tests — Jest (NestJS)

```typescript
// users.service.spec.ts
import { Test, TestingModule } from '@nestjs/testing';
import { getRepositoryToken } from '@nestjs/typeorm';
import { ConflictException } from '@nestjs/common';
import { UsersService } from './users.service';
import { User } from './entities/user.entity';

const mockRepo = {
  findOneBy: jest.fn(),
  create: jest.fn(),
  save: jest.fn(),
};

describe('UsersService', () => {
  let service: UsersService;
  beforeEach(async () => {
    const module: TestingModule = await Test.createTestingModule({
      providers: [UsersService, { provide: getRepositoryToken(User), useValue: mockRepo }],
    }).compile();
    service = module.get(UsersService);
    jest.clearAllMocks();
  });

  it('throws ConflictException on duplicate email', async () => {
    mockRepo.findOneBy.mockResolvedValue({ id: '1', email: 'a@b.com' });
    await expect(service.create({ email: 'a@b.com', password: 'password123' }))
      .rejects.toThrow(ConflictException);
  });

  it('creates user when email is unique', async () => {
    mockRepo.findOneBy.mockResolvedValue(null);
    mockRepo.create.mockImplementation((dto) => ({ id: '1', ...dto }));
    mockRepo.save.mockResolvedValue({ id: '1', email: 'a@b.com' });
    const user = await service.create({ email: 'a@b.com', password: 'password123' });
    expect(user.id).toBe('1');
    expect(mockRepo.save).toHaveBeenCalledTimes(1);
  });
});
```

## E2E tests — Supertest (NestJS)

```typescript
// users.e2e-spec.ts
import { Test } from '@nestjs/testing';
import { INestApplication, ValidationPipe } from '@nestjs/common';
import * as request from 'supertest';
import { AppModule } from '../src/app.module';

describe('UsersController (e2e)', () => {
  let app: INestApplication;
  beforeAll(async () => {
    const module = await Test.createTestingModule({ imports: [AppModule] }).compile();
    app = module.createNestApplication();
    app.useGlobalPipes(new ValidationPipe({ whitelist: true, forbidNonWhitelisted: true, transform: true }));
    await app.init();
  });
  afterAll(() => app.close());

  it('POST /users with invalid email → 400', () => {
    return request(app.getHttpServer())
      .post('/users')
      .send({ email: 'not-an-email', password: 'short' })
      .expect(400);
  });

  it('POST /users with valid body → 201 + Location', async () => {
    const res = await request(app.getHttpServer())
      .post('/users')
      .send({ email: 'a@b.com', password: 'password123' })
      .expect(201);
    expect(res.body.id).toBeDefined();
  });
});
```

## Integration tests — Hono `app.request()`

```typescript
import { describe, it, expect, beforeEach } from 'vitest';
import app from '../src/index';
import { resetDb } from './helpers';

describe('users API', () => {
  beforeEach(resetDb);

  it('POST /users validates body', async () => {
    const res = await app.request('/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'not-an-email', password: 'short' }),
    });
    expect(res.status).toBe(422);
    const body = await res.json();
    expect(body.errors).toBeInstanceOf(Array);
  });

  it('GET /users/:id returns 404 for missing', async () => {
    const res = await app.request('/users/nonexistent');
    expect(res.status).toBe(404);
  });
});
```

## Structured logging — pino

```typescript
// logger.ts
import pino from 'pino';
export const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info',
  redact: ['req.headers.authorization', 'password', '*.password', '*.token'],
  serializers: { req: (req) => ({ method: req.method, url: req.url, id: req.id }) },
  transport: process.env.NODE_ENV !== 'production' ? { target: 'pino-pretty' } : undefined,
});

// NestJS — register as custom logger
app.useLogger(new Logger({ pino: logger }));
// or
import { LoggerService } from '@nestjs/common';
class PinoLogger implements LoggerService {
  log(msg: string, ctx?: string) { logger.info({ ctx }, msg); }
  error(msg: string, trace?: string) { logger.error({ trace }, msg); }
  // ...
}
```

Hono:
```typescript
import { pinoLogger } from 'hono-pino';
app.use('*', pinoLogger({ pino: logger }));
```

Always redact auth headers and passwords. Always include `requestId` (correlation ID) so logs from one request can be filtered.

## OpenTelemetry tracing

```typescript
// tracing.ts
import { NodeSDK } from '@opentelemetry/sdk-node';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { HttpInstrumentation } from '@opentelemetry/instrumentation-http';
import { ExpressInstrumentation } from '@opentelemetry/instrumentation-express';
import { PgInstrumentation } from '@opentelemetry/instrumentation-pg';

const sdk = new NodeSDK({
  traceExporter: new OTLPTraceExporter({ url: process.env.OTLP_ENDPOINT }),
  instrumentations: [new HttpInstrumentation(), new ExpressInstrumentation(), new PgInstrumentation()],
});
sdk.start();
```

In NestJS, register tracing BEFORE `NestFactory.create` — instrumentation must hook the runtime at startup.

```typescript
// main.ts
import './tracing';
import { NestFactory } from '@nestjs/core';
// ...
```

Hono on Workers: use `@hono/otel` middleware; Workers tracing comes via Logpush + OpenTelemetry collector.

## Health check

```typescript
// NestJS
@Controller('health')
export class HealthController {
  constructor(private readonly db: DataSource) {}

  @Get('live')  live() { return { status: 'ok' }; }            // process alive
  @Get('ready') async ready() {                                // can serve traffic
    await this.db.query('SELECT 1');
    return { status: 'ok' };
  }
}

// Hono
app.get('/health/live', (c) => c.json({ status: 'ok' }));
app.get('/health/ready', async (c) => {
  try { await c.env.DB.prepare('SELECT 1').first(); return c.json({ status: 'ok' }); }
  catch { return c.json({ status: 'degraded' }, 503); }
});
```

K8s liveness → `/health/live`; readiness → `/health/ready`. Only `/ready` should depend on downstream services.

## Verification gates

- `npm run test` — unit tests pass.
- `npm run test:e2e` — e2e/integration pass.
- `npm run build` — production build succeeds.
- `tsc --noEmit` — type errors block.
- `eslint` — lint passes.
- Auth flow tested: invalid token → 401; expired token → 401; correct role → 200; wrong role → 403.
- Logs structured; sensitive fields redacted.
- Traces exported; one trace per request spans DB + external calls.
