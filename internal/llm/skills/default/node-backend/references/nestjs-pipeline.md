# NestJS Pipeline — Guards, Pipes, Interceptors, Filters, DTOs

NestJS request lifecycle: middleware → guards → interceptors (pre) → pipes → controller handler → interceptors (post) → exception filter (if thrown). Each layer has a single responsibility; do not mix concerns.

## Lifecycle order

```
Request
  → Middleware (CORS, helmet, body parser)
  → Guard (authn/authz — can it proceed?)
  → Interceptor (pre — logging, transform request, start timer)
  → Pipe (validate/transform input — DTOs, params, query)
  → Controller handler
  → Interceptor (post — transform response, log timer)
  → Exception filter (if anything threw)
Response
```

- **Guard** — decides whether the request proceeds. Throws `ForbiddenException` if not.
- **Pipe** — transforms or validates input. Throws `BadRequestException` if invalid.
- **Interceptor** — wraps the handler; can transform request, response, or short-circuit (cache).
- **Filter** — catches exceptions, formats the response.

## DTOs with class-validator

```typescript
import { IsEmail, IsEnum, IsInt, IsOptional, IsString, Min, MinLength } from 'class-validator';
import { ApiProperty, ApiPropertyOptional } from '@nestjs/swagger';
import { Type } from 'class-transformer';

export enum UserRole { Admin = 'admin', User = 'user' }

export class CreateUserDto {
  @ApiProperty({ example: 'a@b.com' })
  @IsEmail() email: string;

  @ApiProperty({ example: 'password123', minLength: 8 })
  @IsString() @MinLength(8) password: string;

  @ApiPropertyOptional({ enum: UserRole, default: UserRole.User })
  @IsOptional() @IsEnum(UserRole) role: UserRole = UserRole.User;

  @ApiPropertyOptional({ example: 30, minimum: 18 })
  @IsOptional() @Type(() => Number) @IsInt() @Min(18) age?: number;
}

export class ListUsersQuery {
  @ApiPropertyOptional({ default: 20, maximum: 100 })
  @IsOptional() @Type(() => Number) @IsInt() @Min(1) limit: number = 20;

  @ApiPropertyOptional() @IsOptional() @IsString() cursor?: string;
}
```

Rules:
- One DTO per input shape — `CreateUserDto`, `UpdateUserDto` (with all fields optional), `ListUsersQuery`.
- `@ApiProperty` for Swagger doc; required by default. `@ApiPropertyOptional` for optional fields.
- `@Type(() => Number)` from `class-transformer` — converts query string to number before validation.
- Validation groups: `@ValidateIf((o) => o.role === UserRole.Admin)` for conditional rules.
- Never reuse a create DTO for update — `PartialType(CreateUserDto)` makes all fields optional.

```typescript
import { PartialType, OmitType } from '@nestjs/swagger';

export class UpdateUserDto extends PartialType(OmitType(CreateUserDto, ['password'] as const)) {}
```

## Global ValidationPipe

```typescript
// main.ts
app.useGlobalPipes(new ValidationPipe({
  whitelist: true,             // strip unknown properties
  forbidNonWhitelisted: true,  // 400 if unknown properties present
  transform: true,             // auto-convert to DTO instances
  transformOptions: { enableImplicitConversion: true },
}));
```

Without `whitelist + forbidNonWhitelisted`, a malicious client can pass `isAdmin: true` and your DTO will accept it (mass assignment vulnerability).

## Custom pipe

```typescript
@Injectable()
export class ParseCsvPipe implements PipeTransform<string, string[]> {
  transform(value: string | undefined): string[] {
    if (!value) return [];
    return value.split(',').map((s) => s.trim()).filter(Boolean);
  }
}

// usage
@Get()
findAll(@Query('tags', new ParseCsvPipe()) tags: string[]) { ... }
```

## Guard — authn/authz

```typescript
@Injectable()
export class JwtAuthGuard implements CanActivate {
  constructor(private readonly jwt: JwtService) {}

  async canActivate(ctx: ExecutionContext): Promise<boolean> {
    const req = ctx.switchToHttp().getRequest();
    const token = req.headers.authorization?.replace('Bearer ', '');
    if (!token) throw new UnauthorizedException('missing token');
    try {
      req.user = await this.jwt.verifyAsync(token);
      return true;
    } catch {
      throw new UnauthorizedException('invalid token');
    }
  }
}

// role-based
@Injectable()
export class RolesGuard implements CanActivate {
  constructor(private readonly reflector: Reflector) {}
  canActivate(ctx: ExecutionContext): boolean {
    const required = this.reflector.get<UserRole[]>('roles', ctx.getHandler()) ?? [];
    const req = ctx.switchToHttp().getRequest();
    if (!required.length) return true;
    return required.includes(req.user?.role);
  }
}

// decorator
export const Roles = (...roles: UserRole[]) => SetMetadata('roles', roles);

// usage
@UseGuards(JwtAuthGuard, RolesGuard)
@Roles(UserRole.Admin)
@Delete(':id')
remove(@Param('id') id: string) { ... }
```

Rules:
- Guards return `boolean` or throw — never return a response.
- Apply globally (`app.useGlobalGuards(...)`) for default auth, per-controller for overrides.
- Passport integration: `@nestjs/passport` + `passport-jwt` is the standard pattern.

## Interceptor

```typescript
@Injectable()
export class LoggingInterceptor implements NestInterceptor {
  intercept(ctx: ExecutionContext, next: CallHandler): Observable<any> {
    const now = Date.now();
    const req = ctx.switchToHttp().getRequest();
    return next.handle().pipe(
      tap(() => console.log(`${req.method} ${req.url} ${Date.now() - now}ms`)),
    );
  }
}

// Response transform — wrap all responses in {data, meta}
@Injectable()
export class TransformInterceptor<T> implements NestInterceptor<T, { data: T }> {
  intercept(ctx: ExecutionContext, next: CallHandler<T>): Observable<{ data: T }> {
    return next.handle().pipe(map((data) => ({ data })));
  }
}

// Cache
@Injectable()
export class CacheInterceptor implements NestInterceptor {
  constructor(private readonly cache: CacheService) {}
  async intercept(ctx: ExecutionContext, next: CallHandler) {
    const req = ctx.switchToHttp().getRequest();
    const cached = await this.cache.get(req.url);
    if (cached) return of(cached);                       // short-circuit
    const result = await firstValueFrom(next.handle());
    await this.cache.set(req.url, result, 60);
    return result;
  }
}
```

Rules:
- Interceptors wrap the handler — use RxJS `Observable` operators.
- `next.handle()` returns the stream; do not await it directly, use `tap`/`map`/`catchError`.
- Cache interceptor short-circuits via `of(value)` — handler never runs.

## Exception filter

```typescript
@Catch()
export class AllExceptionsFilter implements ExceptionFilter {
  catch(ex: unknown, host: ArgumentsHost) {
    const res = host.switchToHttp().getResponse();

    if (ex instanceof HttpException) {
      const status = ex.getStatus();
      const r = ex.getResponse() as any;
      return res.status(status).json({
        type: `https://api.example.com/errors/${status}`,
        title: ex.name,
        status,
        detail: typeof r === 'string' ? r : r.message,
        errors: r.message,                                // class-validator array
      });
    }

    // Unexpected — log full, return generic 500
    console.error(ex);
    res.status(500).json({
      type: 'https://api.example.com/errors/internal',
      title: 'Internal Server Error',
      status: 500,
    });
  }
}

// main.ts
app.useGlobalFilters(new AllExceptionsFilter());
```

Output is RFC 7807 `Problem` shape — matches the API design skill.

## Built-in exception → status mapping

| Exception | Status |
|---|---|
| `BadRequestException` | 400 |
| `UnauthorizedException` | 401 |
| `ForbiddenException` | 403 |
| `NotFoundException` | 404 |
| `ConflictException` | 409 |
| `GoneException` | 410 |
| `UnprocessableEntityException` | 422 |
| `PayloadTooLargeException` | 413 |
| `TooManyRequestsException` | 429 |
| `InternalServerErrorException` | 500 |
| `NotImplementedException` | 501 |
| `ServiceUnavailableException` | 503 |

Throw the right exception; let the filter format it.

## Class-validator patterns

```typescript
// Cross-field validation
@ValidatorConstraint({ name: 'IsPasswordsMatch', async: false })
export class IsPasswordsMatch implements ValidatorConstraintInterface {
  validate(_: unknown, args: ValidationArguments) {
    return args.object['password'] === args.object['passwordConfirm'];
  }
  defaultMessage() { return 'passwords do not match'; }
}

export class ResetPasswordDto {
  @IsString() @MinLength(8) password: string;
  @IsString() @MinLength(8) passwordConfirm: string;
  @Validate(IsPasswordsMatch) _match: string;             // virtual field
}

// Async validator (check DB)
@Injectable()
export class UniqueEmailConstraint implements ValidatorConstraintInterface {
  constructor(private readonly users: UsersService) {}
  async validate(email: string): Promise<boolean> {
    return !(await this.users.findByEmail(email));
  }
  defaultMessage() { return 'email $value already exists'; }
}

@ValidatorConstraint({ name: 'UniqueEmail', async: true })
@Injectable()
export class ResetDto {
  @IsEmail() @Validate(UniqueEmailConstraint) email: string;
}
```

## Custom decorators

```typescript
// Current user decorator — extracts req.user
export const CurrentUser = createParamDecorator(
  (data: keyof User | undefined, ctx: ExecutionContext) => {
    const req = ctx.switchToHttp().getRequest();
    return data ? req.user?.[data] : req.user;
  },
);

@Get('me')
me(@CurrentUser() user: User) { return user; }
@Get('me/id')
myId(@CurrentUser('id') id: string) { return { id }; }
```

## Verification gates

- `tsc --noEmit` — type errors block.
- Unit test each guard/pipe/interceptor in isolation with `Test.createTestingModule`.
- E2E test the full pipeline: send invalid DTO → 422 with `errors[]`; missing token → 401; forbidden role → 403.
- Confirm filter output matches RFC 7807 shape (assert `type`, `title`, `status`, `detail`).
- Verify Swagger UI shows validation constraints (`minLength`, `maximum`, `enum`).
