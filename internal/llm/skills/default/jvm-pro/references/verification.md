# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - ./mvnw verify
      [INFO] BUILD SUCCESS
      Tests run: 42, Failures: 0, Errors: 0, Skipped: 0
  - JaCoCo: 87.3% line coverage (threshold 85%)
  - ./mvnw spotless:check → BUILD SUCCESS
  - SQL logging enabled in OrderIT: 3 queries for 50 orders (no N+1)
ASSUMED:
  - Postgres production performance (Testcontainers in CI only)
  - GC behavior under load (no JVM tuning profiled)
  - Reactive backpressure on Kafka consumer (not stress-tested)
Lingering risk:
  - @SuppressWarnings("unchecked") on src/main/java/.../Cache.java:42 (reason: Spring Cache raw type)
```

## VERIFIED — what counts

Only what you ran and saw:

- `./mvnw verify` (or `./gradlew check`) — BUILD SUCCESS with test counts
- JaCoCo coverage ≥ threshold
- `spotless:check` / `ktlintCheck` — clean
- A SQL log inspection verifying no N+1
- A security test verifying filter chain order

## ASSUMED — what to flag

- Production DB behavior with real data volumes
- GC tuning, heap sizing
- Reactive backpressure under load
- Cross-JDK behavior (Temurin vs Oracle vs GraalVM)
- Third-party library behavior not exercised
- Security filter chain against real OAuth2 IdP (Testcontainers Keycloak only)

## The verification loop

```bash
./mvnw spotless:check        # or: ./gradlew ktlintCheck
./mvnw verify                 # compiles + runs unit + integration + JaCoCo
# For JPA changes — verify no N+1:
SPRING_JPA_SHOW_SQL=true ./mvnw test -Dtest=OrderIT
# For Spring Security:
./mvnw test -Dtest=SecurityIT
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- `@SuppressWarnings` without a reason comment
- `@Disabled` / `@Ignore` to silence a failing test
- Delete a failing test
- Weaken JaCoCo threshold

## When you cannot run a gate

If Testcontainers isn't available or the IdP isn't reachable:

```
ASSUMED:
  - Integration tests skipped (no Docker for Testcontainers); unit tests green
  - OAuth2 not tested against real IdP (Keycloak not available)
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: ./mvnw --batch-mode --no-transfer-progress verify
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: target/site/jacoco/ }
- run: ./mvnw dependency:analyze  # warn on unused declared deps
```

For multi-module builds, run with `-pl <module> -am` to verify a single module + dependencies.

## `@SuppressWarnings` audit

```bash
grep -rn "@SuppressWarnings" src/main/java | grep -v "// .*reason"
```

Every suppression should have a reason. SonarQube / SpotBugs can flag stale suppressions.

## N+1 detection

Enable Hibernate SQL logging in a test:

```yaml
# application-test.yml
spring:
  jpa:
    show-sql: true
    properties:
      hibernate:
        format_sql: true
logging:
  level:
    org.hibernate.SQL: DEBUG
    org.hibernate.orm.jdbc.bind: TRACE
```

Run the test, count queries. A `findBy*` returning N entities with a lazy association should issue 1-2 queries, not N+1.

Tools:
- **Hibernate Statistics** — `spring.jpa.properties.hibernate.generate_statistics=true`
- **p6spy** — logs SQL with timing; catches N+1 in CI
- ** datasource-proxy** — programmatic query counting in tests

## JaCoCo thresholds

Per-package thresholds are more useful than global:

```xml
<rule>
    <element>PACKAGE</element>
    <limits>
        <limit><counter>LINE</counter><minimum>0.80</minimum></limit>
        <limit><counter>BRANCH</counter><minimum>0.70</minimum></limit>
    </limits>
    <excludes>
        <exclude>com.example.api.dto.*</exclude>
        <exclude>com.example.config.*</exclude>
    </excludes>
</rule>
```

DTOs and config classes often have low coverage — exclude them.

## Security filter chain verification

```java
@Test
void anonymous_cannot_access_protected() throws Exception {
    mvc.perform(get("/api/v1/products"))
       .andExpect(status().isUnauthorized());
}

@Test
@WithMockUser(roles = "USER")
void user_can_list() throws Exception {
    mvc.perform(get("/api/v1/products"))
       .andExpect(status().isOk());
}

@Test
@WithMockUser(roles = "USER")
void user_cannot_delete() throws Exception {
    mvc.perform(delete("/api/v1/products/1"))
       .andExpect(status().isForbidden());
}
```

Run these after every `SecurityFilterChain` change.

## The exit checklist

Before writing the final report:

- [ ] `./mvnw verify` (or `./gradlew check`) BUILD SUCCESS
- [ ] JaCoCo ≥ threshold (85% or per-package config)
- [ ] `spotless:check` / `ktlintCheck` clean
- [ ] No new `@SuppressWarnings` without reason
- [ ] No `@Disabled`/`@Ignore` added to silence failures
- [ ] JPA changes verified for N+1 (SQL log inspected)
- [ ] Security changes verified with `@WithMockUser` tests
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
