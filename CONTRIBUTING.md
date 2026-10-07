# Contributing to Omini

Thanks for helping! The most valuable contributions are **device support** (SNMP profiles, plugins) and **real device data** (anonymized SNMP walks). See [`CLAUDE.md`](CLAUDE.md) for architecture, decisions and conventions.

Everything in the project is in **English**: code, comments, docs, commits, issues and PRs.

## Development setup

Requirements: Go (version in `go.mod`), [uv](https://docs.astral.sh/uv/), `make`, [golangci-lint](https://golangci-lint.run) v2 and [lefthook](https://lefthook.dev).
Nix users can get all of them with `nix develop`.

```bash
make hooks      # install git hooks (format, lint, commit message check)
make generate   # regenerate Go types and Python models from schema/
make lint       # golangci-lint + ruff
make fmt        # gofumpt/goimports + ruff format
make test       # Go tests + Python SDK tests
make ci         # everything CI runs, locally — run it before pushing
```

## Code style

| Language | Formatter | Linter | Config |
|---|---|---|---|
| Go | gofumpt + goimports | golangci-lint v2 | `.golangci.yml` |
| Python (SDK, plugins) | ruff format | ruff | `sdk/python/pyproject.toml` |

Generated files (`internal/model/model_gen.go`, `sdk/python/src/omini_sdk/models.py`) are never edited by hand: change `schema/omini.schema.json` and run `make generate`. CI fails if they are out of date.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org). They drive the changelog and version numbers (via [release-please](https://github.com/googleapis/release-please)), so please follow them.

```
<type>(<optional scope>): <description>

[optional body]

[optional footer, e.g. BREAKING CHANGE: ..., Closes #12]
```

**Types**

| Type | Use for | Shows in changelog |
|---|---|---|
| `feat` | A new feature | ✅ |
| `fix` | A bug fix | ✅ |
| `perf` | A performance improvement | ✅ |
| `refactor` | Code change that neither fixes a bug nor adds a feature | |
| `docs` | Documentation only | |
| `test` | Adding or fixing tests | |
| `build` | Build system, dependencies, Docker | |
| `ci` | CI configuration | |
| `chore` | Anything else (tooling, housekeeping) | |
| `style` | Formatting only | |
| `revert` | Reverts a previous commit | |

**Scopes** (optional): `schema`, `snmp`, `collector`, `topology`, `insights`, `store`, `api`, `auth`, `plugins`, `web`, `sdk`, `demo`, `deps`, `docs`.

Add `!` after the type/scope (`feat(schema)!: ...`) or a `BREAKING CHANGE:` footer for breaking changes.

**Examples**

```
feat(snmp): read LLDP neighbors from lldpRemTable
fix(topology): ignore uplink ports when placing wired clients
docs: explain the plugin manifest
build(deps): bump gosnmp to v1.45.0
```

The `commit-msg` hook checks this locally; CI checks every commit and the PR title.

## Pull requests

1. Fork and create a branch from `main`.
2. Keep PRs focused: one feature or fix per PR.
3. The **PR title** must follow Conventional Commits — PRs are **squash-merged**, so the title becomes the commit on `main`.
4. Fill in the PR template; describe how you tested (real hardware, simulator, unit tests).
5. CI must be green.

## Device data and privacy

Never commit credentials, SNMP communities, public IP addresses, serial numbers or real MAC addresses of your network. Anonymize fixtures before adding them to `testdata/`.
