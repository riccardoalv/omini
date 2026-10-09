# CLAUDE.md

Guide for agents (and humans) working on Omini: a self-hosted tool that reads
network devices and software of many vendors, normalizes what they know into
one model, and shows a live map of the network (topology, traffic, clients,
health and alerts) in a web UI.

Read the [README](README.md) for the product, [docs/decisions.md](docs/decisions.md)
for what was decided and why, and [docs/architecture.md](docs/architecture.md)
for how it is built.

## How we work

- **Decisions are made by consensus with the maintainer.** Raise questions
  and trade-offs instead of deciding alone; record what was agreed in
  [docs/decisions.md](docs/decisions.md) (and the README when users see it).
- **Every change ships with tests** that run in CI; run `make ci` before
  saying something is done.
- **Never push** to GitHub unless the maintainer asks.
- **Docs follow the code**: a change that users or contributors notice
  updates the guide in `docs/` it touches.

## Principles

1. **Read-only.** No integration may send commands that change a device's
   configuration.
2. **Lightweight.** One binary, SQLite, no external services. It must run on
   a Raspberry Pi.
3. **Easy to contribute.** New device support is a YAML SNMP profile or a
   plugin, not core code.
4. **Vendor logic stays in integrations** (plugins and SNMP profiles). The
   topology, insights and UI know no vendor.
5. **One device failing never breaks the rest.** An integration error is an
   "offline" status and an alert; the round goes on with its last good data.
6. **Small first.** When in doubt, leave it out and put it on the roadmap.

## Where things are

| Path | What |
|---|---|
| `cmd/omini` | Entry point: configuration, wiring, `omini reset-password` |
| `internal/collector` | Collection rounds (edge to center), rates, history, presence, alerts, memories (MAC ports, attachments) |
| `internal/topology` | Pure function: devices → nodes and edges (where everything hangs) |
| `internal/classify` | Type, OS, brand and product from evidence |
| `internal/insights` | Alert rules (pure functions of the topology) |
| `internal/netscan`, `nmapscan`, `flows`, `snmp` | Built-in discovery: the network scan (ARP, ping, ports, names, mDNS, SSDP, DHCP fingerprints, SNMP), nmap, NetFlow/IPFIX/sFlow |
| `internal/plugins`, `sdk/python` | Plugin runtime and store; the Python SDK (embedded in the binary) |
| `internal/notify` | Notification channels and message cards |
| `internal/store` | SQLite and its migrations (forward only) |
| `internal/api`, `internal/auth` | HTTP API and the admin login |
| `schema/` | JSON Schema: the data contract (Go and Python models are generated from it) |
| `web/` | Vue 3 UI (map in `views/MapView.vue`, layout in `lib/layout.ts`) |
| `testdata/devnet` | Acme, an emulated company network for the real plugins (`make devnet`) |
| `docs/` | User and developer guides |

## Rules that are easy to break

The full list, with the reasons, is in [docs/decisions.md](docs/decisions.md).

- **MACs** are always `aa:bb:cc:dd:ee:ff`; **ports** are named (`ifName`),
  never by index; unknown fields stay empty, never invented.
- **One machine, one node**: whatever reports it, a device is merged by MAC
  or address into the node that already exists.
- **A client stays where it was last seen for sure** (an access point's list,
  alone on a switch port, behind a desk phone) until it is seen elsewhere for
  sure; shared ports, uplinks and ARP never move it.
- **Collection runs in rounds**, edge to center, one interval for all; the
  map is built once per round. No per-integration "Run now".
- **Nothing on the map may overlap**: nodes, link pills, areas they are not
  in. Areas are strict (members and what hangs below them, nothing else).
- **Alert texts are translated in the UI** (`insights.rules.<rule>`) and in
  `internal/notify` for messages; rules return keys and parameters only.
- **No emoji in notifications**; the color says the severity.
- **The device list is a drawer on the map, the store a modal**; Settings has
  no plugin list.
- **No third-party lookups**: identification uses embedded lists only (no
  Fingerbank, no cloud APIs); Omini never ships nmap.

## Commands

```bash
make run         # build the UI and run on :8080 (scans your network)
make dev         # backend on :8080 + Vite with hot reload on :5173
make devnet      # run on :8093 against Acme's emulated network
make test        # Go, Python SDK, devnet emulator and web tests
make ci          # everything CI runs (run before pushing)
make lint        # golangci-lint, ruff, oxlint/eslint, prettier, vue-tsc
make fmt         # format Go, Python and web
make generate    # Go types and Python models from schema/
make icons       # refresh the app icon catalog
make models      # refresh the device model names
make oui         # refresh the MAC vendor registry
make image       # build the Docker image (omini:dev)
make hooks       # install the git hooks (lefthook)
```

## Conventions

- **Everything in English**: code, comments, docs, commits, issues. UI texts
  go through i18n (`en`, `pt-BR`).
- **Conventional Commits** for commits and PR titles; scopes in
  [CONTRIBUTING.md](CONTRIBUTING.md). release-please makes the releases.
- **Formatting**: gofumpt + goimports + golangci-lint v2; ruff; Prettier +
  ESLint + oxlint + vue-tsc. The git hooks run them.
- **Never edit generated files** (`internal/model/model_gen.go`,
  `sdk/python/src/omini_sdk/models.py`): change the schema and run
  `make generate`.
- **Structured logging** (`slog`), no prints. Credentials never reach logs or
  API responses.
- **Fixtures**: anonymized real data in `testdata/`; the devnet for anything
  that needs the real plugins.

## Out of scope (for now)

Write actions to devices, multi-tenancy.
