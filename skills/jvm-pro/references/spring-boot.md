# Spring Boot 3 Web

## Project layout

```
src/main/java/com/example/
├── MyAppApplication.java
├── config/
├── domain/         # entities, value objects
├── application/    # services, use cases
├── infrastructure/ # repos, external clients
└── api/            # controllers, DTOs, advice
src/main/resources/
├── application.yml
└── db/migration/   # Flyway
```

## Application entry point

```java
@SpringBootApplication
public class MyAppApplication {
    public static void main(String[] args) {
        SpringApplication.run(MyAppApplication.class, args);
    }
}
```

Or with Java 21 + Spring Boot 3.2+ main DSL (Kotlin-style):

```java
public class MyAppApplication {
    public static void main(String[] args) {
        new SpringApplicationBuilder(MyAppApplication.class)
            .properties(Map.of("spring.threads.virtual.enabled", "true"))
            .run(args);
    }
}
```

## REST controller

```java
@RestController
@RequestMapping("/api/v1/products")
@Validated
@RequiredArgsConstructor
public class ProductController {
    private final ProductService service;

    @GetMapping("/{id}")
    public ProductDto get(@PathVariable long id) {
        return service.find(id);
    }

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public ProductDto create(@Valid @RequestBody CreateProductRequest req) {
        return service.create(req);
    }

    @GetMapping
    public Page<ProductDto> list(
        @RequestParam(defaultValue = "") String q,
        @PageableDefault(size = 20) Pageable page) {
        return service.search(q, page);
    }
}
```

## DTOs as records

```java
public record ProductDto(long id, String name, BigDecimal price) {}
public record CreateProductRequest(
    @NotBlank @Size(max = 200) String name,
    @DecimalMin("0.0") BigDecimal price,
    @NotNull Long categoryId) {}
```

## Global exception handler

```java
@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(MethodArgumentNotValidException.class)
    @ResponseStatus(HttpStatus.BAD_REQUEST)
    public ProblemDetail handleValidation(MethodArgumentNotValidException ex) {
        ProblemDetail pd = ProblemDetail.forStatus(HttpStatus.BAD_REQUEST);
        pd.setTitle("Validation failed");
        pd.setProperty("errors", ex.getBindingResult().getFieldErrors().stream()
            .map(f -> Map.of("field", f.getField(), "message", f.getDefaultMessage()))
            .toList());
        return pd;
    }

    @ExceptionHandler(EntityNotFoundException.class)
    public ProblemDetail handleNotFound(EntityNotFoundException ex) {
        return ProblemDetail.forStatusAndDetail(HttpStatus.NOT_FOUND, ex.getMessage());
    }

    @ExceptionHandler(Exception.class)
    public ProblemDetail handleOther(Exception ex) {
        log.error("unhandled", ex);
        return ProblemDetail.forStatusAndDetail(HttpStatus.INTERNAL_SERVER_ERROR, "internal error");
    }
}
```

`ProblemDetail` (RFC 9457) is the Spring Boot 3 standard for error responses.

## Service layer

```java
@Service
@RequiredArgsConstructor
public class ProductService {
    private final ProductRepository repo;
    private final CategoryRepository categories;

    @Transactional(readOnly = true)
    public ProductDto find(long id) {
        return repo.findByIdWithCategory(id)
            .map(ProductDto::from)
            .orElseThrow(() -> new EntityNotFoundException("product " + id));
    }

    @Transactional
    public ProductDto create(CreateProductRequest req) {
        if (!categories.existsById(req.categoryId())) {
            throw new EntityNotFoundException("category " + req.categoryId());
        }
        var p = new Product(req.name(), req.price(), req.categoryId());
        return ProductDto.from(repo.save(p));
    }
}
```

`@RequiredArgsConstructor` (Lombok) generates the constructor — Spring injects via constructor (no `@Autowired` needed on the field).

## Configuration properties

```yaml
# application.yml
app:
  port: 8080
  rate-limit:
    rps: 100
    burst: 200
  allowed-origins: [https://example.com, https://app.example.com]
```

```java
@ConfigurationProperties(prefix = "app")
public record AppProps(
    int port,
    RateLimit rateLimit,
    List<String> allowedOrigins) {

    public record RateLimit(int rps, int burst) {}
}

// Enable in main
@SpringBootApplication
@ConfigurationPropertiesScan
public class MyAppApplication { ... }
```

## Profiles

```yaml
# application.yml
spring:
  profiles:
    active: ${SPRING_PROFILES_ACTIVE:dev}

# application-prod.yml
server:
  port: ${PORT:8080}
```

Activate with `--spring.profiles.active=prod` or `SPRING_PROFILES_ACTIVE=prod`.

## Actuator + observability

```yaml
management:
  endpoints:
    web:
      exposure:
        include: health,info,metrics,prometheus
  endpoint:
    health:
      show-details: when-authorized
  metrics:
    tags:
      application: ${spring.application.name}
  tracing:
    sampling:
      probability: 1.0
```

Spring Boot 3 ships Micrometer + OpenTelemetry. Add `micrometer-tracing-bridge-otel` and `opentelemetry-exporter-otlp` deps.

## Common pitfalls

- Field injection — use constructor injection (Lombok `@RequiredArgsConstructor`)
- `@Transactional` on private methods — doesn't work (Spring AOP is proxy-based)
- `@Transactional` called from within the same class — self-invocation bypasses the proxy; split into two beans
- `repo.save()` returning the entity but caller mutating the original — `save` returns the persisted instance; use that
- `@RequestMapping` on the class with no method mapping — common bug; check `@GetMapping` etc. on methods
- Catching `Exception` in controllers — let `@RestControllerAdvice` handle it
- Returning entities instead of DTOs — leaks schema; couples API to DB
