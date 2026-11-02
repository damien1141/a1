BINARY   ?= a1
MAIN_SRC  = ./cmd

GOBIN    ?= $(shell go env GOBIN)
GOPATH   ?= $(shell go env GOPATH)
ifeq ($(GOBIN),)
GOBIN     = $(GOPATH)/bin
endif

GO       ?= go
GOFLAGS  ?= -ldflags="-s -w"
CGO      ?= 0

# Web frontend build. The Vite app under web/ builds into
# internal/webserver/dist/, which the Go server embeds via //go:embed
# (see internal/webserver/render.go). The binary ships whatever dist/
# contains at build time — so `make build` must produce a fresh dist
# or the GUI will be stale / empty.
#
# Bun is preferred (the repo ships web/bun.lock); npm is the fallback
# for environments without Bun. The PM variable picks whichever is on
# PATH first. Override with `make build PM=npm` to force npm.
PM       ?= $(shell command -v bun 2>/dev/null)
ifeq ($(PM),)
PM       := $(shell command -v npm 2>/dev/null)
endif
WEB_DIR  := web
WEB_DIST := internal/webserver/dist

# Pin digest so CI / local make use the same markdownlint image.
MARKDOWN_LINT_IMAGE ?= avtodev/markdown-lint:v1@sha256:6aeedc2f49138ce7a1cd0adffc1b1c0321b841dc2102408967d9301c031949ee

# BetterWright browser runtime (optional): the persistent browser daemon
# behind the browser tools and the captcha-resistant search fallback.
# Keep this pin in sync with internal/toolmanager (BunTools["betterwright"].Version)
# — the two must move together when the CLI contract bumps.
BETTERWRIGHT_VERSION ?= 2.9.0

.PHONY: all build install run clean test fmt fmt-check lint lint-markdown deadcode check help web-build web-dev web-clean web-install web-reinstall eval-golden betterwright-install

all: build

# build depends on web-build so the embedded dist is fresh. web-build
# is a no-op when no JS toolchain is installed (the existing dist, if
# any, is left in place — better stale than nothing).
build: web-build
	CGO_ENABLED=$(CGO) $(GO) build $(GOFLAGS) -o $(BINARY) $(MAIN_SRC)

install: build
	@mkdir -p $(GOBIN)
	mv $(BINARY) $(GOBIN)/$(BINARY)
	@echo "installed $(BINARY) -> $(GOBIN)/$(BINARY)"

run: build
	./$(BINARY)

clean: web-clean
	rm -f $(BINARY)
	$(GO) clean

test:
	$(GO) test ./...
	$(GO) test -C ext/go ./...

# Rust extension SDK (ext/rust): build + test.
test-rust:
	cd ext/rust && cargo test --all-targets

# Golden-set eval (Bet 07): run the pinned ~20-task suite against the
# configured model and record results in the vault. Exits 0 with a clear
# notice when no model is configured, so CI without secrets stays green.
eval-golden:
	$(GO) run . eval golden

# Apply gofumpt / goimports / golines via .golangci.yml formatters.
fmt:
	golangci-lint fmt ./...
	cd ext/go && golangci-lint fmt ./...

# Fail if formatting would change files (used by CI).
fmt-check:
	golangci-lint fmt --diff ./...
	cd ext/go && golangci-lint fmt --diff ./...

lint:
	golangci-lint run ./...
	cd ext/go && golangci-lint run ./...

# Markdown structure lint (Docker). Needs a local Docker daemon.
lint-markdown:
	docker run --rm -v "$(CURDIR):/work" -w /work $(MARKDOWN_LINT_IMAGE) -c /work/.markdownlint.yaml $$(find . -name '*.md' -type f | sort)

deadcode:
	./scripts/deadcode-check.sh

check: fmt-check lint deadcode

# Rust extension SDK checks: format + lint (CI mirrors this).
check-rust:
	cd ext/rust && cargo fmt --check && cargo clippy --all-targets -- -D warnings

# === Web frontend ===

# web-install installs/syncs the JS deps under web/node_modules. It
# ALWAYS runs the package-manager install: a satisfied lockfile makes the
# no-op sync near-instant (bun finishes in well under a second), while a
# skip-if-node_modules-exists check silently breaks web-build with
# "Rollup failed to resolve import" whenever the tree was refreshed from
# elsewhere and package.json gained a dependency the local node_modules
# predates. Picks the package manager from PM (bun by default, npm
# fallback). Errors out if neither is installed.
web-install:
ifeq ($(PM),)
	@echo "web-install: neither bun nor npm found in PATH — install one and re-run" >&2
	@exit 1
else
	@echo "web-install: syncing JS deps via $(notdir $(PM))"
	cd $(WEB_DIR) && $(PM) install
endif

# web-reinstall forces a clean reinstall of JS deps. Use this when
# `make web-build` fails with package-resolution errors (e.g.
# "Failed to resolve entry for package X" or "ENOENT: no such file" for
# a path that should exist inside node_modules). Some bun versions
# occasionally leave node_modules half-populated; wiping + reinstalling
# is the reliable fix.
web-reinstall:
ifeq ($(PM),)
	@echo "web-reinstall: neither bun nor npm found in PATH — install one and re-run" >&2
	@exit 1
else
	@echo "web-reinstall: wiping $(WEB_DIR)/node_modules and reinstalling via $(notdir $(PM))"
	rm -rf $(WEB_DIR)/node_modules
	cd $(WEB_DIR) && $(PM) install
endif

# web-build runs the Vite production build, writing to
# internal/webserver/dist (where the Go server embeds it). Depends on
# web-install so a fresh checkout works without a manual install step.
# Skips with a warning (not an error) when no JS toolchain is installed
# — `make build` then falls back to whatever dist/ is already checked in.
#
# Sanity-checks that lucide-preact actually has its dist files after
# install — half-installed bun state can leave the package with only a
# package.json and no actual code, which then fails at vite build time
# with a confusing "Failed to resolve entry" error. Suggests
# `make web-reinstall` when the check fails.
web-build: web-install
ifeq ($(PM),)
	@echo "web-build: WARNING — no JS toolchain (bun/npm), leaving $(WEB_DIST) as-is" >&2
else
	@if [ ! -d $(WEB_DIR)/node_modules/lucide-preact/dist ]; then \
	        echo "web-build: lucide-preact is missing its dist/ (broken install). Run \`make web-reinstall\` and re-run." >&2; \
	        exit 1; \
	fi
	@echo "web-build: building frontend via $(notdir $(PM)) -> $(WEB_DIST)"
	cd $(WEB_DIR) && $(PM) run build
endif

# web-dev runs the Vite dev server with hot-module reload. Useful while
# iterating on the GUI; not part of `make build`. The dev server proxies
# /api to the Go server at 127.0.0.1:7331 (see web/vite.config.ts).
web-dev:
ifeq ($(PM),)
	@echo "web-dev: neither bun nor npm found in PATH — install one and re-run" >&2
	@exit 1
else
	cd $(WEB_DIR) && $(PM) run dev
endif

# web-clean removes the built dist and node_modules. Use before a
# clean rebuild of the frontend, or to reclaim disk space.
web-clean:
	rm -rf $(WEB_DIST) $(WEB_DIR)/node_modules

# === Optional runtimes ===

# betterwright-install installs the BetterWright CLI at the pinned
# version and runs its explicit setup step (the pinned BetterChromium
# download, ~150 MiB — which is why this is a standalone target and never
# a side effect of `make install`). Enables the browser tools and the
# captcha-resistant search fallback; a1 degrades gracefully without it.
betterwright-install:
ifeq ($(shell command -v bun 2>/dev/null),)
	@echo "betterwright-install: bun not found in PATH — install it from https://bun.com and re-run" >&2
	@exit 1
else
	@echo "betterwright-install: installing betterwright@$(BETTERWRIGHT_VERSION) via bun"
	bun install -g betterwright@$(BETTERWRIGHT_VERSION)
	@if command -v betterwright >/dev/null 2>&1; then \
	        echo "betterwright-install: downloading the pinned browser (one-time, ~150 MiB)"; \
	        betterwright setup; \
	else \
	        echo "betterwright-install: CLI installed but not on PATH yet — open a new shell and run 'betterwright setup'" >&2; \
	fi
endif

help:
	@echo "Usage:"
	@echo "  make             - build binary ($(BINARY)) with fresh web frontend"
	@echo "  make install     - build & install to \$$GOBIN ($(GOBIN))"
	@echo "  make run         - build & run"
	@echo "  make clean       - remove binary, cache, web dist + node_modules"
	@echo "  make test        - run all tests (root + nested ext module)"
	@echo "  make fmt         - format Go sources (gofumpt/goimports/golines)"
	@echo "  make fmt-check   - check formatting without writing (CI)"
	@echo "  make lint        - run golangci-lint"
	@echo "  make lint-markdown - lint Markdown (Docker; needs daemon)"
	@echo "  make deadcode    - unreachable func check (deadcode -test vs baseline)"
	@echo "  make check       - fmt-check + lint + deadcode (CI)"
	@echo "  make test-rust   - test Rust extension SDK (ext/rust)"
	@echo "  make check-rust  - format + lint Rust extension SDK (CI)"
	@echo ""
	@echo "Web frontend (Vite + Preact, embedded in the Go binary):"
	@echo "  make web-build     - build web/ -> internal/webserver/dist (used by \`make build\`)"
	@echo "  make web-install   - install JS deps under web/node_modules (bun preferred, npm fallback)"
	@echo "  make web-reinstall - wipe + reinstall JS deps (fixes broken/half-installed state)"
	@echo "  make web-dev       - run the Vite dev server (hot reload, proxies /api to :7331)"
	@echo "  make web-clean     - remove web dist + node_modules"
	@echo ""
	@echo "Optional runtimes:"
	@echo "  make betterwright-install - install the pinned BetterWright CLI + browser (browser tools, search fallback)"
	@echo ""
	@echo "Override PM=bun|npm to force a package manager (default: bun if on PATH, else npm)."
