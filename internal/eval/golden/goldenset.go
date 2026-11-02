// Package golden pins the golden-set CI tasks (Bet 07): ~20 small,
// hermetic tasks across go / rust / python whose done-criteria are
// machine-checked contract ids. The nightly workflow runs `a1 eval golden`
// against them; regressions in rounds-to-green or token spend show up as
// diffs against the eval_runs history in the vault.
//
// Tasks are deliberately tiny. The golden set protects the lean core
// (tool loop, evidence ledger, contract gate), not model ambition: a task
// a human finishes in one edit isolates exactly one capability per row.
package golden

import (
	"fmt"
	"sort"

	"github.com/damien1141/a1/internal/evidence"
)

// Task is one golden-set entry.
type Task struct {
	// ID is the stable task identifier (also the eval_runs.task value).
	ID string
	// Language is go | rust | python (reporting + CI selection).
	Language string
	// Prompt is the instruction sent to the model.
	Prompt string
	// Fixture is the workspace file set written before the run
	// (path → content, forward slashes, relative to the workspace root).
	Fixture map[string]string
	// Checks are the expected contract check ids (the check-id grammar).
	Checks []string
}

const (
	goModPath   = "example.com/golden"
	cargoTomlFd = `[package]
name = "golden"
version = "0.1.0"
edition = "2021"

[lib]
path = "src/lib.rs"
`
)

// goMod renders a minimal go.mod for a golden module.
func goMod() string { return "module " + goModPath + "\n\ngo 1.24\n" }

// Tasks is the pinned golden set. Order is stable (alphabetical by ID) so
// report rows line up across runs.
var Tasks = []Task{
	{
		ID:       "go-counter",
		Language: "go",
		Prompt: `Implement the Counter type in counter/counter.go so the tests in counter/counter_test.go pass.
The file has a stub with the method signatures — fill in the bodies. Do not modify the test file.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"counter/counter.go": `package counter

// Counter counts events, starting at zero.
type Counter struct {
	n int
}

// Value returns the current count.
func (c *Counter) Value() int {
	return 0 // TODO
}

// Inc adds one to the count.
func (c *Counter) Inc() {
	// TODO
}

// Reset returns the count to zero.
func (c *Counter) Reset() {
	// TODO
}
`,
			"counter/counter_test.go": `package counter

import "testing"

func TestCounterInc(t *testing.T) {
	var c Counter
	c.Inc()
	c.Inc()
	c.Inc()
	if got := c.Value(); got != 3 {
		t.Fatalf("Value() = %d, want 3", got)
	}
}

func TestCounterReset(t *testing.T) {
	var c Counter
	c.Inc()
	c.Reset()
	if got := c.Value(); got != 0 {
		t.Fatalf("Value() after Reset() = %d, want 0", got)
	}
}
`,
		},
		Checks: []string{"go-build", "go-test:" + goModPath + "/counter"},
	},
	{
		ID:       "go-errs-wrap",
		Language: "go",
		Prompt: `Implement ErrNotFound handling in errs/store.go so the tests pass:
Get returns the wrapped ErrNotFound (errors.Is must find it) with the key in the message.
Do not modify the test file.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"errs/store.go": `package errs

import "errors"

// ErrNotFound is returned by Get for missing keys.
var ErrNotFound = errors.New("not found")

// Store is a tiny in-memory string store.
type Store struct {
	data map[string]string
}

func New() *Store { return &Store{data: map[string]string{}} }

// Put stores a value.
func (s *Store) Put(k, v string) { s.data[k] = v }

// Get returns the value for k, or an error wrapping ErrNotFound.
func (s *Store) Get(k string) (string, error) {
	// TODO: return the stored value, or ErrNotFound wrapped with the key.
	return "", nil
}
`,
			"errs/store_test.go": `package errs

import (
	"errors"
	"strings"
	"testing"
)

func TestGetFound(t *testing.T) {
	s := New()
	s.Put("a", "1")
	got, err := s.Get("a")
	if err != nil || got != "1" {
		t.Fatalf("Get(a) = %q, %v", got, err)
	}
}

func TestGetNotFoundWraps(t *testing.T) {
	s := New()
	_, err := s.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error %v does not wrap ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error %q does not mention the key", err)
	}
}
`,
		},
		Checks: []string{"go-test:" + goModPath + "/errs"},
	},
	{
		ID:       "go-fib",
		Language: "go",
		Prompt: `Implement Fib in fib/fib.go: Fib(0)=0, Fib(1)=1, Fib(n)=Fib(n-1)+Fib(n-2).
Make the tests in fib/fib_test.go pass. Do not modify the test file.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"fib/fib.go": `package fib

// Fib returns the n-th Fibonacci number (Fib(0)=0, Fib(1)=1).
func Fib(n int) int {
	// TODO
	return 0
}
`,
			"fib/fib_test.go": `package fib

import "testing"

func TestFib(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0}, {1, 1}, {2, 1}, {5, 5}, {10, 55}, {20, 6765},
	}
	for _, c := range cases {
		if got := Fib(c.n); got != c.want {
			t.Fatalf("Fib(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}
`,
		},
		Checks: []string{"go-build", "go-test:" + goModPath + "/fib"},
	},
	{
		ID:       "go-fix-math",
		Language: "go",
		Prompt: `The test in mathx/mathx_test.go is failing: Add returns the wrong result.
Find the bug in mathx/mathx.go and fix it. Do not modify the test file.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"mathx/mathx.go": `package mathx

// Add returns a + b.
func Add(a, b int) int {
	// BUG: returns the difference.
	return a - b
}

// Mul returns a * b.
func Mul(a, b int) int {
	return a * b
}
`,
			"mathx/mathx_test.go": `package mathx

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatalf("Add(2,3) = %d, want 5", Add(2, 3))
	}
	if Add(-1, 1) != 0 {
		t.Fatalf("Add(-1,1) = %d, want 0", Add(-1, 1))
	}
}

func TestMul(t *testing.T) {
	if Mul(3, 4) != 12 {
		t.Fatalf("Mul(3,4) = %d, want 12", Mul(3, 4))
	}
}
`,
		},
		Checks: []string{"go-test:" + goModPath + "/mathx"},
	},
	{
		ID:       "go-fix-vet",
		Language: "go",
		Prompt: `vetcheck/vetcheck.go fails go vet. Fix the code so go vet ./... is clean.
Do not delete the function or the test; make vet pass.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"vetcheck/vetcheck.go": `package vetcheck

import "fmt"

// Greet builds a greeting.
func Greet(name string) string {
	// BUG: unused local and a bad Printf verb.
	greeting := fmt.Sprintf("hello %s", name)
	unused := "never read"
	_ = unused
	return fmt.Sprintf("hello %d", name)
}
`,
			"vetcheck/vetcheck_test.go": `package vetcheck

import "testing"

func TestGreet(t *testing.T) {
	if Greet("a1") != "hello a1" {
		t.Fatalf("Greet(a1) = %q", Greet("a1"))
	}
}
`,
		},
		Checks: []string{"go-vet", "go-test:" + goModPath + "/vetcheck"},
	},
	{
		ID:       "go-greeter",
		Language: "go",
		Prompt: `Implement Hello in greeter/greeter.go so the tests in greeter/greeter_test.go pass:
Hello("a1") returns "Hello, a1!". Hello("") returns "Hello, world!".
Do not modify the test file.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"greeter/greeter.go": `package greeter

// Hello greets name; an empty name greets the world.
func Hello(name string) string {
	// TODO
	return ""
}
`,
			"greeter/greeter_test.go": `package greeter

import "testing"

func TestHello(t *testing.T) {
	if got := Hello("a1"); got != "Hello, a1!" {
		t.Fatalf("Hello(a1) = %q, want %q", got, "Hello, a1!")
	}
	if got := Hello(""); got != "Hello, world!" {
		t.Fatalf("Hello(\"\") = %q, want %q", got, "Hello, world!")
	}
}
`,
		},
		Checks: []string{"go-build", "go-test:" + goModPath + "/greeter"},
	},
	{
		ID:       "go-reverse",
		Language: "go",
		Prompt: `Implement Reverse in stringutil/reverse.go: it reverses a UTF-8 string
(rune-wise, not byte-wise). Make the tests pass; do not modify them.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"stringutil/reverse.go": `package stringutil

// Reverse returns s reversed (rune-wise).
func Reverse(s string) string {
	// TODO
	return s
}
`,
			"stringutil/reverse_test.go": `package stringutil

import "testing"

func TestReverse(t *testing.T) {
	if got := Reverse("abc"); got != "cba" {
		t.Fatalf("Reverse(abc) = %q", got)
	}
	if got := Reverse("héllo"); got != "olléh" {
		t.Fatalf("Reverse(héllo) = %q (must be rune-wise)", got)
	}
	if got := Reverse(""); got != "" {
		t.Fatalf("Reverse(\"\") = %q", got)
	}
}
`,
		},
		Checks: []string{"go-test:" + goModPath + "/stringutil"},
	},
	{
		ID:       "go-wordcount",
		Language: "go",
		Prompt: `Implement WordCount in wordcount/wordcount.go: it splits the input on
whitespace and returns a map of word → occurrence count. Make the tests
pass; do not modify them.`,
		Fixture: map[string]string{
			"go.mod": goMod(),
			"wordcount/wordcount.go": `package wordcount

// WordCount counts whitespace-separated words.
func WordCount(s string) map[string]int {
	// TODO
	return nil
}
`,
			"wordcount/wordcount_test.go": `package wordcount

import "testing"

func TestWordCount(t *testing.T) {
	got := WordCount("the cat the hat the")
	want := map[string]int{"the": 3, "cat": 1, "hat": 1}
	if len(got) != len(want) {
		t.Fatalf("WordCount = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("WordCount[%q] = %d, want %d", k, got[k], v)
		}
	}
}
`,
		},
		Checks: []string{"go-test:" + goModPath + "/wordcount"},
	},
	{
		ID:       "py-add",
		Language: "python",
		Prompt: `Implement add and sub in calc.py so the tests in tests/test_calc.py pass.
Do not modify the test file.`,
		Fixture: map[string]string{
			"calc.py": `def add(a: int, b: int) -> int:
    """Return a + b."""
    # TODO
    return 0


def sub(a: int, b: int) -> int:
    """Return a - b."""
    # TODO
    return 0
`,
			"tests/test_calc.py": `from calc import add, sub


def test_add():
    assert add(2, 3) == 5
    assert add(-1, 1) == 0


def test_add_negative():
    assert add(-5, -7) == -12


def test_sub():
    assert sub(10, 4) == 6
    assert sub(0, 5) == -5
`,
		},
		Checks: []string{
			"pytest:tests/test_calc.py::test_add",
			"pytest:tests/test_calc.py::test_add_negative",
			"pytest:tests/test_calc.py::test_sub",
		},
	},
	{
		ID:       "py-class-stack",
		Language: "python",
		Prompt: `Implement the Stack class in stack.py so the tests in tests/test_stack.py pass.
Do not modify the test file.`,
		Fixture: map[string]string{
			"stack.py": `class Stack:
    """A simple LIFO stack of integers."""

    def __init__(self) -> None:
        # TODO

    def push(self, value: int) -> None:
        # TODO

    def pop(self) -> int:
        # TODO
        return 0

    def size(self) -> int:
        # TODO
        return 0
`,
			"tests/test_stack.py": `import pytest

from stack import Stack


def test_push_pop():
    s = Stack()
    s.push(1)
    s.push(2)
    assert s.pop() == 2
    assert s.pop() == 1


def test_size():
    s = Stack()
    assert s.size() == 0
    s.push(9)
    assert s.size() == 1


def test_pop_empty_raises():
    s = Stack()
    with pytest.raises(IndexError):
        s.pop()
`,
		},
		Checks: []string{
			"pytest:tests/test_stack.py::test_push_pop",
			"pytest:tests/test_stack.py::test_size",
			"pytest:tests/test_stack.py::test_pop_empty_raises",
		},
	},
	{
		ID:       "py-fix-slice",
		Language: "python",
		Prompt: `The test in tests/test_last.py is failing: last() returns the wrong element.
Find the bug in last.py and fix it. Do not modify the test file.`,
		Fixture: map[string]string{
			"last.py": `def last(items: list[int]) -> int:
    """Return the last element of a non-empty list."""
    # BUG: returns the first element.
    return items[0]
`,
			"tests/test_last.py": `from last import last


def test_last():
    assert last([1, 2, 3]) == 3
    assert last([7]) == 7
`,
		},
		Checks: []string{"pytest:tests/test_last.py::test_last"},
	},
	{
		ID:       "py-mypy-clean",
		Language: "python",
		Prompt: `typed.py has type errors mypy rejects (mypy typed.py). Fix them so
` + "`mypy typed.py`" + ` exits 0. Keep the annotations; correct the code under them.`,
		Fixture: map[string]string{
			"typed.py": `def double(x: int) -> int:
    """Return 2 * x."""
    # BUG: wrong types all over.
    x = str(x)
    return x + x


def greet(name: str) -> str:
    # BUG: returns None.
    return None


def total(values: list[int]) -> int:
    out: int = "0"
    for v in values:
        out = out + v
    return out
`,
		},
		Checks: []string{"exit:0:mypy typed.py"},
	},
	{
		ID:       "py-parse-int",
		Language: "python",
		Prompt: `Implement parse_int in parseint.py: it returns the int value of s, or
None when s is not a valid integer (optionally signed). Make the tests
pass; do not modify them.`,
		Fixture: map[string]string{
			"parseint.py": `def parse_int(s: str) -> int | None:
    """Return int(s), or None when s is not a valid integer."""
    # TODO
    return None
`,
			"tests/test_parseint.py": `from parseint import parse_int


def test_valid():
    assert parse_int("42") == 42
    assert parse_int("-7") == -7
    assert parse_int("+3") == 3
    assert parse_int("0") == 0


def test_invalid():
    assert parse_int("4.2") is None
    assert parse_int("abc") is None
    assert parse_int("") is None
    assert parse_int(" 1") is None
`,
		},
		Checks: []string{
			"pytest:tests/test_parseint.py::test_valid",
			"pytest:tests/test_parseint.py::test_invalid",
		},
	},
	{
		ID:       "py-ruff-clean",
		Language: "python",
		Prompt: `messy.py fails ` + "`ruff check .`" + ` (unused imports, unused variables).
Clean it up so ` + "`ruff check .`" + ` exits 0. Keep the public function working the same.`,
		Fixture: map[string]string{
			"messy.py": `import os
import sys
import json


def slugify(text: str) -> str:
    """Return a lowercase, dash-separated slug."""
    unused = "not needed"
    parts = text.lower().split()
    return "-".join(parts)
`,
		},
		Checks: []string{"exit:0:ruff check ."},
	},
	{
		ID:       "rs-add",
		Language: "rust",
		Prompt: `Implement add in src/lib.rs so the test add_works passes.
Do not modify the tests.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns a + b.
pub fn add(a: i64, b: i64) -> i64 {
    // TODO
    0
}

#[cfg(test)]
mod tests {
    use super::add;

    #[test]
    fn add_works() {
        assert_eq!(add(2, 3), 5);
        assert_eq!(add(-1, 1), 0);
        assert_eq!(add(i64::MAX - 1, 1), i64::MAX);
    }
}
`,
		},
		Checks: []string{"cargo-build", "cargo-test:tests::add_works"},
	},
	{
		ID:       "rs-dedup",
		Language: "rust",
		Prompt: `Implement dedup in src/lib.rs: preserve first-occurrence order, drop
duplicates. Make the tests pass; do not modify them.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns items with duplicates removed, keeping first occurrences.
pub fn dedup(items: &[i32]) -> Vec<i32> {
    // TODO
    items.to_vec()
}

#[cfg(test)]
mod tests {
    use super::dedup;

    #[test]
    fn dedup_works() {
        assert_eq!(dedup(&[1, 2, 2, 3, 1]), vec![1, 2, 3]);
        assert_eq!(dedup(&[]), Vec::<i32>::new());
        assert_eq!(dedup(&[5, 5, 5]), vec![5]);
    }
}
`,
		},
		Checks: []string{"cargo-test:tests::dedup_works"},
	},
	{
		ID:       "rs-fix-compile",
		Language: "rust",
		Prompt: `src/lib.rs does not compile. Fix the code so cargo build succeeds and
the test passes. Do not modify the test.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns the n-th triangle number (1+2+...+n, 0 for n=0).
pub fn triangle(n: u64) -> u64 {
    // BUG: wrong types and a bad loop.
    let mut total = "0";
    for i in 1..=n {
        total += i;
    }
    return total
}

#[cfg(test)]
mod tests {
    use super::triangle;

    #[test]
    fn triangle_works() {
        assert_eq!(triangle(0), 0);
        assert_eq!(triangle(4), 10);
    }
}
`,
		},
		Checks: []string{"cargo-build", "cargo-test:tests::triangle_works"},
	},
	{
		ID:       "rs-fix-test",
		Language: "rust",
		Prompt: `The test max_works in src/lib.rs is failing: my_max returns the wrong
value for the empty-with-default case ordering. Fix my_max. Do not modify
the test.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns the maximum of a and b.
pub fn my_max(a: i64, b: i64) -> i64 {
    // BUG: returns the minimum.
    if a < b {
        a
    } else {
        b
    }
}

#[cfg(test)]
mod tests {
    use super::my_max;

    #[test]
    fn max_works() {
        assert_eq!(my_max(1, 2), 2);
        assert_eq!(my_max(5, -5), 5);
        assert_eq!(my_max(3, 3), 3);
    }
}
`,
		},
		Checks: []string{"cargo-test:tests::max_works"},
	},
	{
		ID:       "rs-hello",
		Language: "rust",
		Prompt: `Implement hello in src/lib.rs so the test hello_works passes.
Do not modify the tests.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns "Hello, <name>!".
pub fn hello(name: &str) -> String {
    // TODO
    String::new()
}

#[cfg(test)]
mod tests {
    use super::hello;

    #[test]
    fn hello_works() {
        assert_eq!(hello("a1"), "Hello, a1!");
        assert_eq!(hello(""), "Hello, world!");
    }
}
`,
		},
		Checks: []string{"cargo-build", "cargo-test:tests::hello_works"},
	},
	{
		ID:       "rs-clippy",
		Language: "rust",
		Prompt: `src/lib.rs draws a cargo clippy warning (a comparison that should be
a method call). Fix it so ` + "`cargo clippy -- -D warnings`" + ` is clean and the
test still passes.`,
		Fixture: map[string]string{
			"Cargo.toml": cargoTomlFd,
			"src/lib.rs": `/// Returns true when flag is set.
pub fn is_on(flag: bool) -> bool {
    // CLIPPY: comparing to a bool literal; the value itself is the answer.
    if flag == true {
        true
    } else {
        false
    }
}

#[cfg(test)]
mod tests {
    use super::is_on;

    #[test]
    fn is_on_works() {
        assert!(is_on(true));
        assert!(!is_on(false));
    }
}
`,
		},
		Checks: []string{"cargo-clippy", "cargo-test:tests::is_on_works"},
	},
}

// ByID returns the task with the given id.
func ByID(id string) (Task, bool) {
	for _, t := range Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// Validate checks one task's integrity: a prompt, a fixture, and at least
// one well-formed check id from the ledger's grammar. It returns the first
// problem found (nil when the task is well-formed).
func (t Task) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("golden: task with no id")
	}
	if t.Language == "" {
		return fmt.Errorf("golden: %s: language is required", t.ID)
	}
	if t.Prompt == "" {
		return fmt.Errorf("golden: %s: prompt is required", t.ID)
	}
	if len(t.Fixture) == 0 {
		return fmt.Errorf("golden: %s: fixture is required", t.ID)
	}
	if len(t.Checks) == 0 {
		return fmt.Errorf("golden: %s: at least one check id is required", t.ID)
	}
	seen := map[string]bool{}
	for _, id := range t.Checks {
		if !evidence.ValidCheckID(id) {
			return fmt.Errorf("golden: %s: check id %q is not in the check-id grammar", t.ID, id)
		}
		if seen[id] {
			return fmt.Errorf("golden: %s: duplicate check id %q", t.ID, id)
		}
		seen[id] = true
	}
	return nil
}

// IDs returns the sorted task ids.
func IDs() []string {
	out := make([]string, 0, len(Tasks))
	for _, t := range Tasks {
		out = append(out, t.ID)
	}
	sort.Strings(out)
	return out
}
