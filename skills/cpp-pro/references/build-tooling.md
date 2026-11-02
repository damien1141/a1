# Build Tooling — CMake, Sanitizers, clang-tidy

## Minimal `CMakeLists.txt`

```cmake
cmake_minimum_required(VERSION 3.24)
project(myapp CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)        # use -std=c++23, not -std=gnu++23

# Warnings as errors
add_compile_options(-Wall -Wextra -Wpedantic -Wconversion -Wshadow -Werror)

add_executable(myapp src/main.cpp)
target_include_directories(myapp PRIVATE include)

enable_testing()
add_subdirectory(tests)
```

## Modern target-based usage

```cmake
add_library(mylib src/parser.cpp)
target_include_directories(mylib PUBLIC include)
target_compile_features(mylib PUBLIC cxx_std_20)

find_package(fmt CONFIG REQUIRED)
target_link_libraries(mylib PRIVATE fmt::fmt)
```

- `PUBLIC` — both this target and consumers see it
- `PRIVATE` — only this target sees it
- `INTERFACE` — only consumers see it (header-only libraries)

Avoid `include_directories()` (global) — use `target_include_directories()` (per-target).

## Sanitizers

```cmake
option(ENABLE_ASAN "AddressSanitizer" OFF)
option(ENABLE_UBSAN "UndefinedBehaviorSanitizer" OFF)
option(ENABLE_TSAN "ThreadSanitizer" OFF)

if(ENABLE_ASAN)
    add_compile_options(-fsanitize=address -fno-omit-frame-pointer -g)
    add_link_options(-fsanitize=address)
endif()
if(ENABLE_UBSAN)
    add_compile_options(-fsanitize=undefined -fno-omit-frame-pointer -g)
    add_link_options(-fsanitize=undefined)
endif()
if(ENABLE_TSAN)
    add_compile_options(-fsanitize=thread -g)
    add_link_options(-fsanitize=thread)
endif()
```

**Never combine ASan and TSan** — they're mutually exclusive.

Run with options:
```bash
ASAN_OPTIONS=detect_leaks=1:abort_on_error=1 ./myapp
UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1 ./myapp
TSAN_OPTIONS=second_deadlock_stack=1 ./myapp
```

## clang-tidy

```yaml
# .clang-tidy
Checks: >
  -*,
  bugprone-*,
  cert-*,
  cppcoreguidelines-*,
  modernize-*,
  performance-*,
  readability-*,
  -modernize-use-trailing-return-type,
  -readability-identifier-length
WarningsAsErrors: ''
HeaderFilterRegex: '.'
FormatStyle: file
```

Run:
```bash
cmake -B build -DCMAKE_EXPORT_COMPILE_COMMANDS=ON
clang-tidy -p build src/*.cpp
```

Use `NOLINT(category)` with a reason for justified suppressions:

```cpp
auto p = reinterpret_cast<uintptr_t>(ptr);  // NOLINT(cppcoreguidelines-pro-type-reinterpret-cast) required for pointer hashing
```

## CTest

```cmake
enable_testing()
add_executable(mylib_test tests/test_parser.cpp)
target_link_libraries(mylib_test PRIVATE mylib Catch2::Catch2WithMain)
add_test(NAME parser_test COMMAND mylib_test)
```

```bash
ctest --test-dir build --output-on-failure
ctest --test-dir build -j$(nproc)        # parallel
```

## Package managers

| Manager | Style |
|---|---|
| Conan | Center repo, per-project profile |
| vcpkg | Microsoft, integrated with CMake via toolchain file |
| FetchContent (built-in) | CMake downloads git repos at configure time |

```cmake
# FetchContent example
include(FetchContent)
FetchContent_Declare(
    fmt
    GIT_REPOSITORY https://github.com/fmtlib/fmt.git
    GIT_TAG        10.2.1
)
FetchContent_MakeAvailable(fmt)
target_link_libraries(mylib PRIVATE fmt::fmt)
```

```cmake
# vcpkg
# Pass -DCMAKE_TOOLCHAIN_FILE=<vcpkg>/scripts/buildsystems/vcpkg.cmake
find_package(fmt CONFIG REQUIRED)
```

```cmake
# Conan
# conan install . --output-folder=build --build=missing
# cmake -B build -DCMAKE_TOOLCHAIN_FILE=build/build/Release/generators/conan_toolchain.cmake
```

## Presets — `CMakePresets.json`

```json
{
  "version": 3,
  "configurePresets": [
    {
      "name": "asan",
      "binaryDir": "${sourceDir}/build-asan",
      "cacheVariables": { "ENABLE_ASAN": "ON" }
    },
    {
      "name": "release",
      "binaryDir": "${sourceDir}/build-release",
      "cacheVariables": { "CMAKE_BUILD_TYPE": "Release" }
    }
  ],
  "testPresets": [
    { "name": "asan", "configurePreset": "asan", "output": { "outputOnFailure": true } }
  ]
}
```

```bash
cmake --preset asan
cmake --build --preset asan
ctest --preset asan
```

## CI sketch

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        sanitizer: [asan, ubsan, tsan]
    steps:
      - uses: actions/checkout@v4
      - run: cmake --preset ${{ matrix.sanitizer }}
      - run: cmake --build --preset ${{ matrix.sanitizer }} --parallel
      - run: ctest --preset ${{ matrix.sanitizer }}
      - run: clang-tidy -p build-${{ matrix.sanitizer }} src/*.cpp
```

## Common pitfalls

- `add_compile_options` globally — use `target_compile_options` for per-target
- Forgetting `CMAKE_EXPORT_COMPILE_COMMANDS=ON` — clang-tidy won't work without `compile_commands.json`
- `CMAKE_CXX_EXTENSIONS OFF` not set — gets `-std=gnu++23` with non-standard extensions
- Building in-source — always use a separate build dir (`cmake -B build`)
- Linking `PRIVATE` deps as `PUBLIC` — leaks into consumers' link lines
- Not setting `CMAKE_BUILD_TYPE` for single-config generators — defaults to empty (no optimization, no asserts)
