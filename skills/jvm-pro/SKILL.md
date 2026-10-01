---
name: jvm-pro
description: "Use when writing Java 21 or Kotlin 2.x on the JVM that must pass mvn/gradle build with -Werror and tests. Generates Spring Boot 3 REST APIs, JPA repositories with N+1 prevention, Spring Security 6 JWT chains, Kotlin coroutines + Flow, sealed classes, records, and verifies with mvn verify + JaCoCo before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "java,kotlin,jvm,spring boot,jpa,hibernate,spring security,coroutines,flow,records,sealed classes,junit"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "dotnet-pro,golang-pro,api-design,system-architecture"
---

# JVM Pro

Java 21 + Kotlin 2.x + Spring Boot 3 specialist. Constructor-injected, JPA-disciplined, security-aware, with a verification gate of `./mvnw verify` (or `./gradlew check`) + JaCoCo coverage. Honest exit separates **VERIFIED** (ran a build/test command, saw green) from **ASSUMED** (could not run).

## When to Use

- Building Spring Boot 3.x REST APIs (MVC or WebFlux) with Spring Data JPA
- Java 21: records, sealed classes, pattern matching, virtual threads
- Kotlin 2.x: coroutines/Flow, sealed classes, null-safety, KMP
- Spring Security 6 with OAuth2/JWT, method security
- JPA query optimization: N+1 prevention, projections, fetch plans
- Reactive: WebFlux + Project Reactor, or Kotlin coroutines
- Migrating Spring Boot 2.x → 3.x (drop `WebSecurityConfigurerAdapter`)

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "blocking or reactive stack?"). State language (Java 21 / Kotlin 2.x), Spring Boot 3.x, build tool (Maven/Gradle).
2. **Recon** — Read `pom.xml`/`build.gradle.kts`, `application.yml`, profiles, test layout. Confirm Java/Kotlin versions, Spring Boot version.
3. **Design layers** — Domain entities → repositories → services → controllers/DTOs. Records for DTOs. Constructor injection everywhere.
4. **Implement** — `@Valid` on mutating endpoints; `@Transactional` on writes, `@Transactional(readOnly = true)` on reads; never `block()` inside reactive chains.
5. **Verify (gate)** — In order, until clean:
   - `./mvnw verify` (or `./gradlew check`) — compiles + runs all tests
   - JaCoCo coverage ≥ 85% (configured in build)
   - `./mvnw spotless:check` (or `ktlintCheck`) — formatting
   - For JPA: enable SQL logging in a test, verify no N+1
   - For Spring Security: confirm `@WithMockUser` tests pass; verify filter chain order
   - If any step fails: fix the cause, do not add `@SuppressWarnings` without reason. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each build/test command + summary. **ASSUMED**: list what you believe but did not run. Flag lingering risk.

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| JVM fundamentals | `references/jvm-fundamentals.md` | Java 21 records/sealed/pattern matching, virtual threads, Kotlin 2.x idioms |
| Spring Boot 3 web | `references/spring-boot.md` | Controllers, REST, validation, exception handlers, config |
| Spring Data JPA | `references/spring-data-jpa.md` | Repositories, N+1 prevention, projections, transactions, Hibernate tuning |
| Spring Security 6 | `references/spring-security.md` | OAuth2, JWT, method security, filter chain, CORS |
| Kotlin coroutines & Flow | `references/kotlin-coroutines.md` | Structured concurrency, `Flow`/`StateFlow`, cancellation, Ktor |
| Testing | `references/testing.md` | JUnit 5, MockMvc, Testcontainers, MockK, Kotest, Turbine |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, JaCoCo |

## Constraints

### MUST DO
- Constructor injection — no field `@Autowired`
- `@Valid @RequestBody` on every mutating endpoint
- `@Transactional` on multi-step writes; `@Transactional(readOnly = true)` on reads
- Records for DTOs (Java) / data classes (Kotlin)
- Type-safe config: `@ConfigurationProperties` bound to a record
- `@RestControllerAdvice` for global exception handling — never return stack traces
- Externalize all config — env vars or Spring Cloud Config
- Database migrations: Flyway or Liquibase
- Run `./mvnw verify` (or `./gradlew check`) before commit; JaCoCo ≥ 85%

### MUST NOT DO
- Field injection (`@Autowired` on fields)
- Skip input validation on mutating endpoints
- Use `@Component` when `@Service`/`@Repository`/`@Controller` applies
- Mix blocking and reactive code (no `.block()` inside WebFlux chains)
- Store secrets in `application.yml` — use env vars or Cloud Config
- Use deprecated Spring Boot 2.x patterns (`WebSecurityConfigurerAdapter`)
- Use `repo.findAll()` and iterate associations without `JOIN FETCH` (N+1)
- Hardcode URLs, credentials, or environment-specific values
- Suppress warnings with `@SuppressWarnings` without a reason comment

## Code Examples

### Spring Boot 3 REST controller (Java 21 records)
```java
@RestController
@RequestMapping("/api/v1/products")
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
}

public record ProductDto(long id, String name, BigDecimal price) {}
public record CreateProductRequest(
    @NotBlank String name,
    @DecimalMin("0.0") BigDecimal price) {}
```

### JPA repository with N+1 prevention
```java
public interface ProductRepository extends JpaRepository<Product, Long> {

    @Query("SELECT p FROM Product p JOIN FETCH p.category WHERE p.id = :id")
    Optional<Product> findByIdWithCategory(@Param("id") Long id);

    @Query("""
        SELECT new com.example.ProductSummary(p.id, p.name, p.price)
        FROM Product p
        WHERE p.category.id = :catId
        """)
    Page<ProductSummary> findSummaries(@Param("catId") Long catId, Pageable page);
}
```

### Kotlin sealed state + Flow
```kotlin
sealed class UiState<out T> {
    data object Loading : UiState<Nothing>()
    data class Success<T>(val data: T) : UiState<T>()
    data class Error(val msg: String) : UiState<Nothing>()
}

class UserRepo(private val api: UserApi) {
    fun user(id: String): Flow<UiState<User>> = flow {
        emit(UiState.Loading)
        try { emit(UiState.Success(api.fetch(id))) }
        catch (e: IOException) { emit(UiState.Error(e.message ?: "network")) }
    }.flowOn(Dispatchers.IO)
}
```

### Spring Security 6 JWT config
```java
@Configuration
@EnableMethodSecurity
public class SecurityConfig {
    @Bean
    public SecurityFilterChain filterChain(HttpSecurity http) throws Exception {
        return http
            .csrf(AbstractHttpConfigurer::disable)
            .sessionManagement(s -> s.sessionCreationPolicy(SessionCreationPolicy.STATELESS))
            .authorizeHttpRequests(a -> a
                .requestMatchers("/actuator/health").permitAll()
                .anyRequest().authenticated())
            .oauth2ResourceServer(o -> o.jwt(Customizer.withDefaults()))
            .build();
    }
}
```

## Output Template

When delivering a JVM feature: domain entities + DTOs (records/data classes) → repositories → services → controllers → tests → build file deltas (`pom.xml` / `build.gradle.kts`) → verification block (`./mvnw verify`, JaCoCo) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

Java 21 (records, sealed classes, pattern matching, virtual threads, `HttpClient`) · Kotlin 2.x (coroutines, Flow, StateFlow, sealed, data classes) · Spring Boot 3.x · Spring WebFlux/Reactor · Spring Data JPA · Spring Security 6 · OAuth2/JWT · Hibernate · R2DBC · Flyway/Liquibase · Spring Cloud · Resilience4j · Micrometer · JUnit 5 · Mockito/MockK · Turbine · Testcontainers · Maven/Gradle · `@ConfigurationProperties`
