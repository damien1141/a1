# Spring Security 6

## Minimal stateless JWT config

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
                .requestMatchers("/actuator/health", "/auth/login").permitAll()
                .requestMatchers("/admin/**").hasRole("ADMIN")
                .anyRequest().authenticated())
            .oauth2ResourceServer(o -> o.jwt(Customizer.withDefaults()))
            .build();
    }
}
```

Key points:
- `csrf` disabled for stateless APIs (CSRF protection is for cookie-based auth)
- `STATELESS` session — no HTTP session, every request carries JWT
- `oauth2ResourceServer` — validates JWT from `Authorization: Bearer <token>`
- `@EnableMethodSecurity` enables `@PreAuthorize` / `@PostAuthorize`

## Method security

```java
@Service
public class OrderService {
    @PreAuthorize("hasRole('ADMIN')")
    public void delete(long id) { /* ... */ }

    @PreAuthorize("hasRole('USER') and #order.ownerId == authentication.principal.id")
    public void cancel(Order order) { /* ... */ }

    @PostAuthorize("returnObject.ownerId == authentication.principal.id")
    public Order find(long id) { /* ... */ }
}
```

Method security uses SpEL. `#order` refers to a method parameter; `returnObject` to the return value.

## Issuing a JWT

```java
@Service
public class TokenService {

    private final RSAPrivateKey privateKey;
    private final String issuer = "myapp";

    public TokenService(@Value("${jwt.private-key}") RSAPrivateKey k) {
        this.privateKey = k;
    }

    public String issue(User user, List<String> roles) {
        var claims = Map.<String, Object>of(
            "sub", user.getId().toString(),
            "email", user.getEmail(),
            "roles", roles
        );
        return Jwts.builder()
            .issuer(issuer)
            .claims(claims)
            .expiration(Date.from(Instant.now().plus(Duration.ofHours(1))))
            .signWith(privateKey, Jwts.SIG.RS256)
            .compact();
    }
}
```

Use RS256/ES256 (asymmetric) so the resource server can verify with the public key only.

## Configuring JWKS (asymmetric verification)

```yaml
spring:
  security:
    oauth2:
      resourceserver:
        jwt:
          jwk-set-uri: https://idp.example.com/.well-known/jwks.json
```

Spring fetches the JWKS, caches it, verifies tokens with the public key.

## Symmetric key (HMAC)

```java
@Bean
public JwtDecoder jwtDecoder(@Value("${jwt.secret}") String secret) {
    byte[] key = Base64.getDecoder().decode(secret);
    return NimbusJwtDecoder.withSecretKey(new SecretKeySpec(key, "HmacSHA256")).build();
}
```

For symmetric: both issuer and verifier share the secret. Simpler but less secure for distributed systems.

## CORS

```java
@Bean
public CorsConfigurationSource corsSource() {
    var cfg = new CorsConfiguration();
    cfg.setAllowedOrigins(List.of("https://app.example.com"));
    cfg.setAllowedMethods(List.of("GET", "POST", "PUT", "DELETE"));
    cfg.setAllowedHeaders(List.of("*"));
    cfg.setAllowCredentials(true);
    var source = new UrlBasedCorsConfigurationSource();
    source.registerCorsConfiguration("/**", cfg);
    return source;
}

// In filter chain:
http.cors(c -> c.configurationSource(corsSource()));
```

## Filter chain order

```
1. SecurityContextPersistenceFilter  (skip in stateless)
2. HeaderWriterFilter
3. CorsFilter
4. CsrfFilter
5. LogoutFilter
6. UsernamePasswordAuthenticationFilter  (form login)
7. ... custom filters ...
8. BearerTokenAuthenticationFilter  (JWT)
9. AuthorizationFilter
```

Custom filters go between specific existing filters with `addFilterBefore` / `addFilterAfter`:

```java
http.addFilterBefore(new MyFilter(), UsernamePasswordAuthenticationFilter.class);
```

## OAuth2 login (social)

```java
http
    .authorizeHttpRequests(a -> a
        .requestMatchers("/", "/login**").permitAll()
        .anyRequest().authenticated())
    .oauth2Login(Customizer.withDefaults());
```

Configure in `application.yml`:
```yaml
spring:
  security:
    oauth2:
      client:
        registration:
          google:
            client-id: ${GOOGLE_CLIENT_ID}
            client-secret: ${GOOGLE_CLIENT_SECRET}
            scope: profile, email
```

## Testing security

```java
@SpringBootTest
@AutoConfigureMockMvc
class OrderControllerTest {
    @Autowired MockMvc mvc;

    @Test
    @WithMockUser(roles = "ADMIN")
    void admin_can_delete() throws Exception {
        mvc.perform(delete("/orders/1"))
           .andExpect(status().isNoContent());
    }

    @Test
    @WithMockUser(roles = "USER")
    void user_cannot_delete() throws Exception {
        mvc.perform(delete("/orders/1"))
           .andExpect(status().isForbidden());
    }

    @Test
    void anonymous_is_unauthorized() throws Exception {
        mvc.perform(delete("/orders/1"))
           .andExpect(status().isUnauthorized());
    }
}
```

For custom JWT-based test auth, write a `@WithSecurityContext` annotation.

## Common pitfalls

- `WebSecurityConfigurerAdapter` (Spring Security 5) — removed in 6; use `SecurityFilterChain` beans
- CSRF enabled for stateless APIs — breaks JWT-only flows
- Storing JWT signing key in `application.yml` — use env var or secret manager
- `permitAll()` on `/admin/**` accidentally — order matters; specific rules before `anyRequest()`
- Mixing session + JWT — pick one; STATELESS + JWT is the modern default for APIs
- Filter ordering — custom filters must be in the right slot; `addFilterBefore`/`After`
- Forgetting `@EnableMethodSecurity` — `@PreAuthorize` silently does nothing without it
- Returning stack traces from `Exception` — `@RestControllerAdvice` should sanitize
