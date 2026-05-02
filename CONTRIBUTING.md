# Contributing to Aura

First of all, thank you for your interest in contributing to Aura. This project is still evolving, and contributions are very welcome, from small bug fixes to major features.

## Code of Conduct

By participating in this project, you agree to treat all other contributors with respect and professionalism. Be constructive in code reviews, avoid personal attacks, and assume good faith.

If you experience or witness unacceptable behavior, please open an issue or contact the maintainer privately.

## How to Get Started

- Fork this repository on GitHub.
- Create a new branch for your change:  
  `git checkout -b feature/my-awesome-change`
- Make your changes in small, focused commits with clear messages.
- Ensure the project builds and tests pass before opening a pull request.

## Project Overview

Aura is a Go-based Telegram assistant with LLM integrations, local wiki storage, search, budget tracking, health endpoints, logging, and optional tracing.[page:1] The codebase is mainly Go with a TypeScript-based web dashboard.[page:1]

Before working on a new feature, please check existing issues and the project documentation (`README.md`, `INSTALL.md`, `AGENTS.md`, `wiki/`, `docs/`).[page:1] If you plan a larger change, consider opening an issue first to discuss the design.

## Development Setup

### Requirements

- Go (version matching `go.mod`, currently 1.25.5 or newer)[page:1]
- Node 20+ for the web dashboard[page:1]
- A Telegram bot token
- At least one allowlisted Telegram user ID
- Optional: OpenAI-compatible LLM and embedding API credentials

### Basic Commands

Run tests:

```bash
go test ./...
```

Build the project:

```bash
go build ./...
```

Run the main service:

```bash
go run ./cmd/aura
```

Run the LLM debug tool:

```bash
go run ./cmd/debug_llm
```

You can also use the Makefile shortcuts:

```bash
make test
make build
make run
make debug-llm
```

These commands should run without errors before you submit a pull request.[page:1]

## Coding Guidelines

- Follow existing Go formatting and style (`gofmt`, idiomatic Go).
- Keep functions small and focused, prefer clear code over clever tricks.
- For TypeScript and frontend code, follow the patterns already used in the `web` folder.[page:1]
- Add or update tests when you change behavior or add new features.
- Keep documentation in sync (e.g., `README.md`, `INSTALL.md`, `AGENTS.md`, `docs/`, `wiki/`).[page:1]

## Commit Messages

- Use clear, descriptive commit messages (e.g., `feat: add new health check endpoint`).
- Group related changes into a single commit where reasonable.
- Avoid committing generated binaries, local databases (`aura.db`), `.env` files, or other runtime data, which are ignored by the repo.[page:1]

## Pull Request Process

- Ensure your branch is up to date with `master` before opening a PR.
- Describe the motivation and the approach of your change in the PR description.
- Reference related issues if applicable (e.g. `Fixes #42`).
- Include screenshots or logs if your change affects the web UI or user-facing behavior.
- Be responsive to review comments and ready to update your PR accordingly.

Once your PR is approved and tests pass, it may be merged by the maintainer.

## Reporting Issues

If you find a bug or have a feature request:

- Check existing issues to see if it is already reported.
- Open a new issue with:
  - A clear title
  - Steps to reproduce (for bugs)
  - Expected vs actual behavior
  - Logs, stack traces, or configuration snippets if relevant
  - Any context about your environment (OS, Go version, etc.)

## Security

If you discover a security vulnerability, please do not open a public issue. Instead, contact the maintainer directly so we can investigate and fix the issue responsibly.

## License

By contributing to Aura, you agree that your contributions will be licensed under the same license as the project (see `LICENSE.md`).
