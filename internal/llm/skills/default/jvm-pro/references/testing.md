# Testing on the JVM

## JUnit 5

```java
class CalculatorTest {

    @Test
    void adds_two_numbers() {
        var calc = new Calculator();
        assertEquals(5, calc.add(2, 3));
    }

    @ParameterizedTest
    @CsvSource({ "1,2,3", "10,20,30", "-1,-2,-3" })
    void adds_correctly(int a, int b, int expected) {
        assertEquals(expected, new Calculator().add(a, b));
    }

    @Nested
    class EdgeCases {
        @Test void zero() { assertEquals(0, new Calculator().add(0, 0)); }
        @Test void overflow() { assertThrows(ArithmeticException.class, () -> new Calculator().add(Integer.MAX_VALUE, 1)); }
    }
}
```

`@Nested` for grouping; `@ParameterizedTest` with `@CsvSource`/`@MethodSource`/`@EnumSource`.

## Lifecycle

```java
class DbTest {
    @BeforeAll static void setupDb() { /* once before all tests */ }
    @BeforeEach void setup() { /* before each test */ }
    @AfterEach void teardown() { /* after each */ }
    @AfterAll static void stopDb() { /* once after all */ }
}
```

Prefer constructor injection (JUnit 5 creates a new instance per test):

```java
class RepoTest {
    private final Repo repo;
    RepoTest() { this.repo = new Repo(testDb()); }
}
```

## Mockito

```java
@ExtendWith(MockitoExtension.class)
class ProductServiceTest {
    @Mock ProductRepository repo;
    @Mock CategoryRepository categories;
    @InjectMocks ProductService service;

    @Test
    void create_throws_when_category_missing() {
        when(categories.existsById(99L)).thenReturn(false);
        var req = new CreateProductRequest("X", BigDecimal.TEN, 99L);
        assertThrows(EntityNotFoundException.class, () -> service.create(req));
        verify(repo, never()).save(any());
    }
}
```

Mock interfaces, not concrete classes. `MockitoExtension` validates `@Mock` usage.

## MockK (Kotlin)

```kotlin
class UserServiceTest {
    private val api: UserApi = mockk()
    private val service = UserService(api)

    @Test
    fun `fetches user`() = runTest {
        coEvery { api.fetch("1") } returns User("1", "Alice")
        val user = service.find("1")
        assertEquals("Alice", user.name)
        coVerify { api.fetch("1") }
    }
}
```

`coEvery`/`coVerify` for suspend functions. `every`/`verify` for regular.

## MockMvc — slice tests

```java
@WebMvcTest(ProductController.class)
class ProductControllerTest {
    @Autowired MockMvc mvc;
    @MockBean ProductService service;

    @Test
    void get_returns_200() throws Exception {
        when(service.find(1L)).thenReturn(new ProductDto(1, "Widget", BigDecimal.TEN));

        mvc.perform(get("/api/v1/products/1"))
           .andExpect(status().isOk())
           .andExpect(jsonPath("$.name").value("Widget"));
    }
}
```

`@WebMvcTest` loads only the web layer + a mock service. Fast and isolated.

## `@SpringBootTest` — full integration

```java
@SpringBootTest
@AutoConfigureMockMvc
class FullStackTest {
    @Autowired MockMvc mvc;
    @Autowired AppDbContext db;

    @Test
    void end_to_end() throws Exception {
        mvc.perform(post("/api/v1/products")
            .contentType(MediaType.APPLICATION_JSON)
            .content("""
                {"name":"Widget","price":10.0,"categoryId":1}
                """))
           .andExpect(status().isCreated());

        assertThat(db.count()).isEqualTo(1);
    }
}
```

`@SpringBootTest` boots the whole context. Slower but realistic.

## Testcontainers — real dependencies

```java
@Testcontainers
@SpringBootTest
class RepoIT {
    @Container
    static PostgreSQLContainer<?> pg = new PostgreSQLContainer<>("postgres:16-alpine");

    @DynamicPropertySource
    static void props(DynamicPropertyRegistry r) {
        r.add("spring.datasource.url", pg::getJdbcUrl);
        r.add("spring.datasource.username", pg::getUsername);
        r.add("spring.datasource.password", pg::getPassword);
    }

    @Test
    void persists_product() { /* ... */ }
}
```

Testcontainers spins up a real Postgres in Docker per test class. Use `static` to share across tests.

## Turbine — Flow testing (Kotlin)

```kotlin
@Test
fun `flow emits loading then success`() = runTest {
    repo.userFlow("1").test {
        assertEquals(UiState.Loading, awaitItem())
        assertEquals(UiState.Success(user), awaitItem())
        awaitComplete()
    }
}
```

## Kotest (alternative)

```kotlin
class StringSpecExample : StringSpec({
    "addition works" {
        2 + 2 shouldBe 4
    }

    "list contains" {
        listOf(1, 2, 3) shouldContain 2
    }
})
```

Property-based testing with Kotest's `forAll`:

```kotlin
"string length is non-negative" {
    forAll<String> { s -> s.length >= 0 }
}
```

## AssertJ — fluent assertions

```java
assertThat(users)
    .hasSize(3)
    .extracting(User::getName)
    .containsExactly("Alice", "Bob", "Carol");
```

More readable than JUnit's `assertEquals`. Use AssertJ or AssertK (Kotlin).

## Coverage with JaCoCo

```xml
<!-- pom.xml -->
<plugin>
    <groupId>org.jacoco</groupId>
    <artifactId>jacoco-maven-plugin</artifactId>
    <version>0.8.11</version>
    <executions>
        <execution>
            <goals><goal>prepare-agent</goal></goals>
        </execution>
        <execution>
            <id>report</id>
            <phase>test</phase>
            <goals><goal>report</goal></goals>
        </execution>
        <execution>
            <id>check</id>
            <goals><goal>check</goal></goals>
            <configuration>
                <rules><rule>
                    <element>BUNDLE</element>
                    <limits><limit>
                        <counter>LINE</counter>
                        <minimum>0.85</minimum>
                    </limit></limits>
                </rule></rules>
            </configuration>
        </execution>
    </executions>
</plugin>
```

`mvn verify` runs tests + JaCoCo. Report at `target/site/jacoco/index.html`. Build fails if coverage < 85%.

## Common pitfalls

- `Thread.sleep` in tests — use Awaitility (`await().atMost(5, SECONDS).until(...)`)
- Mocking concrete classes — Mockito needs `mockito-inline` or interfaces; prefer interfaces
- `@SpringBootTest` for everything — slow; use slice tests (`@WebMvcTest`, `@DataJpaTest`) where possible
- Shared mutable state across tests — JUnit creates a new instance per test; use `@TestInstance(Lifecycle.PER_CLASS)` carefully
- `assertEquals` chains — use AssertJ for readability
- Testcontainers without `static` field — container starts/stops per test, 10x slower
- Forgetting `@Transactional` in `@DataJpaTest` — tests pollute each other
