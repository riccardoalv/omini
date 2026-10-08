# Development

How to build, run and test Omini from a checkout, and how changes reach a
release. Conventions in detail are in [CONTRIBUTING.md](../CONTRIBUTING.md).

- [Requirements](#requirements)
- [Make targets](#make-targets)
- [Running it](#running-it)
- [The simulated network](#the-simulated-network)
- [Tests](#tests)
- [Generated code](#generated-code)
- [Style and hooks](#style-and-hooks)
- [CI](#ci)
- [Commits and releases](#commits-and-releases)

## Requirements

- Go (the version in `go.mod`)
- Node.js 24
- [uv](https://docs.astral.sh/uv/) (Python SDK, plugins, tools)
- `make`
- [golangci-lint](https://golangci-lint.run) v2 and [lefthook](https://lefthook.dev)
  for linting and git hooks

With Nix, `nix develop` gives you all of them (plus `net-snmp` for poking at
SNMP devices).

## Make targets

| Target | What it does |
|---|---|
| `make run` | Builds the UI and runs Omini on `http://localhost:8080`. It scans your network |
| `make dev` | Backend on `:8080` and the Vite dev server with hot reload on `http://localhost:5173` |
| `make devnet` | Runs Omini on `http://localhost:8093` against a large [simulated network](#the-simulated-network) |
| `make web` | Builds the UI into `web/dist` (embedded into the binary) |
| `make test` | Go, Python SDK and web tests |
| `make cover` | Tests with a coverage report per package |
| `make lint` | golangci-lint, ruff, ESLint + oxlint, Prettier check, vue-tsc |
| `make fmt` | Formats Go, Python and web code |
| `make ci` | Everything CI runs, locally. Run it before pushing |
| `make generate` | Regenerates Go types and Python models from `schema/` |
| `make check-generated` | Fails if generated code does not match the schema |
| `make hooks` | Installs the git hooks (lefthook) |
| `make image` | Builds the Docker image locally as `omini:dev` |
| `make icons` | Refreshes the app icon catalog and downloads the icon bundle |
| `make oui` | Refreshes the embedded MAC vendor database from the IEEE registry |
| `make models` | Refreshes the device model names (Apple identifiers, Google Play devices) |

## Running it

```bash
make dev
```

Open `http://localhost:5173`. Vite proxies the API to the Go server on
`:8080`. The first visit asks you to create the admin user. Data goes to
`./data` (delete it to start over).

Useful environment variables while developing:

```bash
OMINI_LOG_LEVEL=debug                      # verbose, with plugin stderr
OMINI_PLUGIN_DIRS=../omini-plugin-opnsense # load a plugin from its checkout
OMINI_AUTOSCAN=false                       # do not create the network scan
OMINI_PLUGIN_INDEX=off                     # do not fetch the store index
```

## The simulated network

`make devnet` runs Omini against a large, deliberately messy network served
by ten fake plugins in `testdata/devnet/`: a firewall with two WANs and eight
VLANs, an MLAG core pair, access switches, UniFi access points with about 60
Wi-Fi clients, three Proxmox hosts with guests, a branch office behind a
tunnel, storage, and about 130 scanned hosts. Devices reboot, roam, fill up
and fail on a schedule, so alerts, history and the presence timeline move.

It uses its own data folder (`data-devnet/`), no network scan, no remote
index and a 30-second round. On the first run, `bootstrap.py` creates the
admin user with generated credentials (saved in `data-devnet/devnet-admin.json`)
and adds one integration per fake plugin. It only reads simulated data and
never touches a real device. Details and the schedule of events are in
[testdata/devnet/README.md](../testdata/devnet/README.md).

## Tests

Every change ships with tests that run in CI.

| Suite | Where | Run |
|---|---|---|
| Go | `*_test.go` next to the code | `go test ./...` (`-race` in CI) |
| Python SDK | `sdk/python/tests` | `cd sdk/python && uv run pytest` |
| Web | `web/src/**/__tests__` (Vitest) | `cd web && npm test` |

- Topology and insights tests use JSON fixtures of anonymized real networks in
  `testdata/`; `internal/demo` is a fictional network used as a fixture.
- `internal/collector/devnet_test.go` builds the map from the simulated
  network and checks where things hang and which alerts fire. It needs `uv`
  and is skipped without it or with `-short`.
- `sdk/python/tests/test_devnet.py` runs every devnet plugin the way Omini
  does and validates their output.
- SNMP code is tested against an in-memory agent (`internal/snmp/snmptest`).

Never commit credentials, communities, public IPs, serial numbers or real MAC
addresses: anonymize fixtures first.

## Generated code

`schema/omini.schema.json` is the single source of truth for the data that
crosses the plugin boundary. From it, `make generate` writes:

- `internal/model/model_gen.go` (Go types),
- `sdk/python/src/omini_sdk/models.py` (pydantic models).

Never edit those files by hand. Change the schema, run `make generate` and
commit both. CI fails when they are stale.

## Style and hooks

| Language | Formatter | Linter |
|---|---|---|
| Go | gofumpt + goimports | golangci-lint v2 (`.golangci.yml`) |
| Python | ruff format | ruff |
| TypeScript / Vue | Prettier | ESLint + oxlint, vue-tsc |

`make hooks` installs lefthook: on commit it formats and lints the staged
files and checks the commit message.

Everything is in English: code, comments, docs, commits, issues. UI strings
go through vue-i18n (`web/src/i18n/en.ts` and `pt-BR.ts`). Alert texts sent by
notifications live in `internal/notify/messages.go` and are kept in step with
the UI's.

## CI

GitHub Actions run on every push and pull request:

| Job | Checks |
|---|---|
| Go | build (also for arm64 and armv7), tests with `-race` and coverage, golangci-lint |
| Generated code | `make check-generated` |
| Python SDK | ruff, pytest |
| Web UI | lint, type-check, Vitest with coverage, build |
| Conventional Commits | every commit message and the PR title |

`make ci` runs the same checks locally.

## Commits and releases

Commits and PR titles follow [Conventional Commits](https://www.conventionalcommits.org):

```
feat(map): fold a host's VMs like clients
fix(topology): ignore uplink ports when placing wired clients
docs: explain the plugin manifest
```

PRs are squash-merged, so the PR title becomes the commit on `main`. Scopes
and types are listed in [CONTRIBUTING.md](../CONTRIBUTING.md).

Releases are made by [release-please](https://github.com/googleapis/release-please):

1. Every push to `main` updates a release PR with the next version and the
   changelog, from the commits since the last release (`feat` → minor, `fix`
   → patch, `!` or `BREAKING CHANGE` → major).
2. Merging that PR tags the release (`vX.Y.Z`) and creates the GitHub release.
3. The same workflow builds the Docker image for amd64 and arm64 and pushes
   it to `ghcr.io/riccardoalv/omini` and Docker Hub with the tags `X.Y.Z`,
   `X.Y` and `latest`. The version is stamped into the binary
   (`-X main.version`).

Plugins are released from their own repositories; see
[Publishing](plugins.md#publishing).
