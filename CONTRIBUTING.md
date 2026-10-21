# Contributing

Thank you for your interest in contributing to CDN Network.

---

## Getting Started

1. Fork the repository and clone your fork.
2. Follow the [Getting Started guide](docs/getting-started.md) to set up a local development environment.
3. Create a feature branch from `main`: `git checkout -b feature/my-change`.

---

## Development Workflow

### Before making changes

```bash
go mod download
go build ./...
go test ./...
```

All three commands should succeed without errors before you start.

### Making changes

- Follow the code organisation and naming conventions in [docs/development.md](docs/development.md).
- Respect the dependency rules between packages (domain → use case → handler; no reverse imports).
- Add or update tests for the code you change.
- Update documentation if you add new configuration variables, endpoints, or behaviour.

### Before submitting

```bash
gofmt -l .           # should produce no output
go vet ./...         # should produce no output
go test -race ./...  # should pass
go build ./...       # should succeed
```

---

## Pull Requests

- One logical change per PR.
- Write a clear description explaining **what** the change does and **why**.
- Reference any related issues.
- Do not commit `.env`, secrets, credentials, or generated binaries.

---

## Reporting Bugs

Open a GitHub issue with:
- What you expected to happen.
- What actually happened.
- Steps to reproduce.
- Relevant log output.
- Go version and OS.

---

## Security Vulnerabilities

Do not open public issues for security vulnerabilities. See [SECURITY.md](SECURITY.md) for the responsible disclosure process.

---

## Code Style

This project follows standard Go conventions:
- `gofmt` formatting.
- Exported identifiers have doc comments.
- Errors are handled explicitly — do not discard errors silently except where explicitly documented (e.g. cache invalidation).

There is no linter configuration file. `go vet` is the minimum automated check.
