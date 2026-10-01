# NestJS 10+ Core — Modules, DI, Controllers, Services, Swagger

NestJS is an opinionated Node.js framework built on Express (or Fastify). It brings Angular-style DI, modules, decorators, and a pipeline of guards/pipes/interceptors/filters. Use it for enterprise backends where structure matters more than cold-start.

## Project layout

```
src/
  main.ts                    bootstrap, global pipes, Swagger setup
  app.module.ts              root module, imports feature modules
  common/                    shared filters, interceptors, guards
  config/                    ConfigModule setup, env validation
  users/
    users.module.ts
    users.controller.ts
    users.service.ts
    dto/
      create-user.dto.ts
      update-user.dto.ts
    entities/user.entity.ts
    users.service.spec.ts
    users.controller.e2e-spec.ts
```

One folder per feature (bounded context). Each module is self-contained: controller, service, DTOs, entity, tests. Export the service only if other modules need it.

## Module

```typescript
import { Module } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { ConfigModule } from '@nestjs/config';
import { UsersModule } from './users/users.module';

@Module({
  imports: [
    ConfigModule.forRoot({ isGlobal: true }),
    TypeOrmModule.forRootAsync({
      inject: [ConfigService],
      useFactory: (cfg: ConfigService) => ({
        type: 'postgres',
        url: cfg.getOrThrow('DATABASE_URL'),
        autoLoadEntities: true,
        synchronize: cfg.get('NODE_ENV') !== 'production',  // dev only
      }),
    }),
    UsersModule,
  ],
})
export class AppModule {}
```

- `imports` — other modules whose exports this module needs.
- `providers` — services/repositories this module owns.
- `controllers` — route handlers.
- `exports` — providers visible to importing modules. **Default: export nothing.** Add exports only when other modules genuinely need the service.

## Dependency injection

Constructor injection is the only pattern. NestJS resolves providers by token (class, string, or symbol).

```typescript
@Injectable()
export class OrdersService {
  constructor(
    private readonly users: UsersService,                    // class token
    @InjectRepository(Order) private readonly orders: Repository<Order>,
    @Inject('PAYMENT_GATEWAY') private readonly payments: PaymentGateway,  // string token
    @Optional() private readonly cache?: Cache,              // optional dep
  ) {}
}
```

For interface-based injection (no class token), use a string or symbol token:

```typescript
const PAYMENT_GATEWAY = Symbol('PAYMENT_GATEWAY');

@Module({
  providers: [
    { provide: PAYMENT_GATEWAY, useClass: StripeGateway },   // swap easily
    // { provide: PAYMENT_GATEWAY, useValue: mockGateway },  // for tests
    // { provide: PAYMENT_GATEWAY, useFactory: (cfg) => cfg.get('STRIPE_KEY') ? new StripeGateway(...) : new MockGateway(), inject: [ConfigService] },
  ],
})
```

**Rules:**
- Never `new Service()` in a controller — let the DI container resolve it.
- `@Injectable()` marks a class as injectable.
- Providers are singletons by default within a module scope.
- Use `Scope.REQUEST` for per-request providers (rare; performance cost).
- Circular deps: `forwardRef(() => OtherModule)` — but treat as a smell; usually a missing abstraction.

## Controller

```typescript
import { Body, Controller, Delete, Get, Param, Patch, Post, Query } from '@nestjs/common';
import { ApiCreatedResponse, ApiOkResponse, ApiTags } from '@nestjs/swagger';

@ApiTags('users')
@Controller('users')
export class UsersController {
  constructor(private readonly users: UsersService) {}

  @Post()
  @HttpCode(HttpStatus.CREATED)
  @ApiCreatedResponse({ type: UserResponse })
  create(@Body() dto: CreateUserDto) { return this.users.create(dto); }

  @Get()
  @ApiOkResponse({ type: [UserResponse] })
  findAll(@Query() query: ListUsersQuery) { return this.users.findAll(query); }

  @Get(':id')
  @ApiOkResponse({ type: UserResponse })
  findOne(@Param('id', ParseUUIDPipe) id: string) { return this.users.findOne(id); }

  @Patch(':id')
  @ApiOkResponse({ type: UserResponse })
  update(@Param('id', ParseUUIDPipe) id: string, @Body() dto: UpdateUserDto) {
    return this.users.update(id, dto);
  }

  @Delete(':id')
  @HttpCode(HttpStatus.NO_CONTENT)
  remove(@Param('id', ParseUUIDPipe) id: string) { return this.users.remove(id); }
}
```

- `@Controller('users')` — base path. NestJS auto-adds `/` if missing.
- `@Param('id', ParseUUIDPipe)` — built-in pipe validates and transforms the param.
- `@Query()` — binds all query params to a DTO (with validation).
- `@Body()` — binds request body to a DTO (with ValidationPipe).
- Swagger decorators (`@ApiTags`, `@ApiOperation`, `@ApiResponse`, `@ApiProperty`) drive OpenAPI generation.

## Service

```typescript
@Injectable()
export class UsersService {
  constructor(@InjectRepository(User) private readonly repo: Repository<User>) {}

  async create(dto: CreateUserDto): Promise<User> {
    if (await this.repo.findOneBy({ email: dto.email }))
      throw new ConflictException('Email already registered');
    return this.repo.save(this.repo.create(dto));
  }

  async findOne(id: string): Promise<User> {
    const user = await this.repo.findOneBy({ id });
    if (!user) throw new NotFoundException(`User ${id} not found`);
    return user;
  }

  async update(id: string, dto: UpdateUserDto): Promise<User> {
    const user = await this.findOne(id);
    Object.assign(user, dto);
    return this.repo.save(user);
  }
}
```

**Rules:**
- Throw typed HTTP exceptions (`NotFoundException`, `ConflictException`, `BadRequestException`, `UnauthorizedException`, `ForbiddenException`, `UnprocessableEntityException`). The framework maps them to status codes.
- Services should not know about HTTP — but NestJS blurs this; the convention is to throw HTTP exceptions in services that back controllers. For pure domain services, throw domain errors and map them in a filter.
- Always return promises; never use callbacks.

## main.ts bootstrap

```typescript
import { NestFactory } from '@nestjs/core';
import { ValidationPipe, Logger } from '@nestjs/common';
import { DocumentBuilder, SwaggerModule } from '@nestjs/swagger';
import { AppModule } from './app.module';
import helmet from 'helmet';

async function bootstrap() {
  const app = await NestFactory.create(AppModule, { bufferLogs: true });

  app.use(helmet());
  app.enableCors({ origin: process.env.ALLOWED_ORIGINS?.split(',') ?? false, credentials: true });
  app.useLogger(app.get(Logger));

  app.useGlobalPipes(new ValidationPipe({
    whitelist: true,           // strip unknown properties
    forbidNonWhitelisted: true, // 400 if unknown properties sent
    transform: true,            // auto-transform payloads to DTO instances
    transformOptions: { enableImplicitConversion: true },
  }));

  const config = new DocumentBuilder()
    .setTitle('Example API')
    .setVersion('1.0.0')
    .addBearerAuth()
    .build();
  SwaggerModule.setup('docs', app, SwaggerModule.createDocument(app, config));

  await app.listen(process.env.PORT ?? 3000);
}
bootstrap();
```

**Critical:** `whitelist: true` + `forbidNonWhitelisted: true` on `ValidationPipe` — without this, clients can inject arbitrary fields into your DTOs.

## TypeORM entity

```typescript
import { Column, CreateDateColumn, Entity, Index, PrimaryGeneratedColumn, UpdateDateColumn } from 'typeorm';

@Entity('users')
@Index('idx_users_email', ['email'], { unique: true })
export class User {
  @PrimaryGeneratedColumn('uuid') id: string;
  @Column({ unique: true }) email: string;
  @Column({ select: false }) passwordHash: string;        // never select by default
  @Column({ nullable: true }) name: string | null;
  @CreateDateColumn() createdAt: Date;
  @UpdateDateColumn() updatedAt: Date;
}
```

- `select: false` on sensitive fields — must explicitly `addSelect('user.passwordHash')` to fetch.
- Always index unique constraints and frequently queried columns.
- Use `uuid` primary keys (not auto-increment integer) for security and sharding.

## Prisma alternative

```typescript
// prisma.service.ts
@Injectable()
export class PrismaService extends PrismaClient implements OnModuleInit {
  async onModuleInit() { await this.$connect(); }
}

// users.service.ts
@Injectable()
export class UsersService {
  constructor(private readonly prisma: PrismaService) {}

  create(dto: CreateUserDto) {
    return this.prisma.user.create({ data: dto });
  }
}
```

## TypeORM vs Prisma

| Criterion | TypeORM | Prisma |
|---|---|---|
| Schema | Entity classes (decorators) | `schema.prisma` file |
| Migrations | Auto-generated | Auto-generated |
| Type safety | Partial (queries are typed) | Strong (queries fully typed from schema) |
| Relations | Decorator-driven (`@ManyToOne`) | Schema-driven |
| Raw SQL | Yes | Yes |
| Performance | Mature | Newer, very good |

Either is fine. Prisma has stronger DX; TypeORM is closer to Java/Hibernate mental model.

## Configuration

```typescript
// config/env.validation.ts
import { plainToInstance } from 'class-transformer';
import { IsEnum, IsNumber, IsString, validateSync } from 'class-validator';

enum NodeEnv { Development = 'development', Production = 'production', Test = 'test' }

class EnvVariables {
  @IsEnum(NodeEnv) NODE_ENV: NodeEnv = NodeEnv.Development;
  @IsString() DATABASE_URL: string;
  @IsString() JWT_SECRET: string;
  @IsNumber() PORT: number = 3000;
}

export function validate(config: Record<string, unknown>) {
  const validated = plainToInstance(EnvVariables, config, { enableImplicitConversion: true });
  const errors = validateSync(validated, { skipMissingProperties: false });
  if (errors.length) throw new Error(errors.toString());
  return validated;
}

// app.module.ts
ConfigModule.forRoot({ validate, isGlobal: true });
```

Always validate env at startup. `getOrThrow('DATABASE_URL')` in services — fail fast if missing.

## Swagger / OpenAPI

```typescript
// users.dto.ts
export class CreateUserDto {
  @ApiProperty({ example: 'a@b.com', format: 'email' })
  @IsEmail() email: string;

  @ApiProperty({ example: 'password123', minLength: 8 })
  @IsString() @MinLength(8) password: string;
}
```

- `@ApiProperty` on every DTO field — drives the OpenAPI schema.
- `@ApiTags('users')` on controllers — groups endpoints in the docs UI.
- `@ApiResponse({ status: 422, description: 'Validation failed' })` — document non-2xx responses.
- Visit `/docs` in dev; gate it behind auth in production.

## Verification gates

- `tsc --noEmit` — type errors block.
- `eslint 'src/**/*.ts'` — lint passes.
- `npm test` — unit tests for services (Jest + `Test.createTestingModule`).
- `npm run test:e2e` — Supertest against full app instance.
- `npm run build` — production build succeeds.
- `/docs` (Swagger UI) loads; all endpoints documented.
