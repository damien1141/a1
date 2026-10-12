# Security Policy

## Supported versions

Security fixes land on the latest release of [a1](https://github.com/damien1141/a1).
Please upgrade before reporting issues that are already fixed on `main`.

## Reporting a vulnerability

Please **do not** open a public issue with exploit detail.

Prefer one of:

1. [GitHub private security advisories](https://github.com/damien1141/a1/security/advisories/new)
2. A minimal private report to the maintainers via GitHub

Include: a1 version / commit, OS, steps to reproduce, and impact.

We aim to acknowledge reports quickly and coordinate disclosure after a fix
is available.

## Scope

In scope examples:

- Permission gate bypasses that allow unexpected destructive tool use
- Path traversal or workspace escape around `workspace_only_writes`
- Supply-chain issues in release artifacts or install scripts

Out of scope examples:

- Issues that require a malicious model provider already trusted by the user
- Vulnerabilities only present in third-party MCP servers or extensions you installed
- Social engineering of API keys stored in the user’s own config

Product overview of the local-first design: [README.md](README.md) (same repo).
