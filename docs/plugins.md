# Writing a plugin

A plugin teaches Omini to read a device or a piece of software it does not
know: a firewall's API, a switch's web interface, a Wi-Fi controller. It is a
small Python program in its own Git repository. Omini runs it once per
collection, gives it its settings as JSON on stdin and reads the devices it
prints on stdout. The `omini-sdk` package does that plumbing, so you write two
functions.

Before writing a plugin, check whether an [SNMP profile](snmp-profiles.md) is
enough: if the device speaks SNMP, a YAML file adds its vendor data with no
code.

- [Rules](#rules)
- [Repository layout](#repository-layout)
- [plugin.yaml](#pluginyaml)
- [The SDK](#the-sdk)
- [The toolkit](#the-toolkit)
- [Conventions](#conventions)
- [What to return](#what-to-return)
- [The protocol](#the-protocol)
- [State between runs](#state-between-runs)
- [Testing](#testing)
- [Development mode](#development-mode)
- [Publishing](#publishing)

## Rules

1. **Changes only through `apply`.** `collect` and `test` never change the
   device, reboot it or start a check (a `POST` only for a login or a read
   that needs it, documented in the README). A plugin changes a device only
   in `apply`, for the changes its manifest declares.
2. **Optional data never fails the collection.** Read optional endpoints
   with `optional(...)`, which skips them on error. Fail only when the
   essential data cannot be read.
3. **Short timeouts** on every request: 15 seconds by default (the
   `timeout_s` field), with the retries of `omini_sdk.http.Client`.
4. **Secrets** only in `secret` fields, never logged, never written in clear to
   the state folder.
5. **Unknown is empty.** Leave a field out when the device does not say it;
   never guess a value.
6. **No telemetry**, no calls to third parties.

The same rules are the checklist of the [plugin review](plugin-review.md).

## Repository layout

Name the repository `omini-plugin-<id>`:

```
omini-plugin-example/
├── plugin.yaml          # manifest: id, version, entrypoint, form fields
├── requirements.txt     # dependencies, installed into the plugin's environment
├── main.py              # entrypoint
├── omini_example/       # your code (optional package)
├── tests/
│   ├── fixtures/        # recorded, anonymized answers of real devices
│   └── test_collect.py
├── pyproject.toml       # development only (uv, ruff, pytest)
├── README.md            # what it reads, the least-privilege account, limitations
└── LICENSE              # MIT or compatible
```

Do not list `omini-sdk` in `requirements.txt`: Omini installs its own copy of
the SDK in every plugin environment, so the protocol always matches the core
that runs the plugin.

For development, point `pyproject.toml` at the SDK in an Omini checkout next
to your repository:

```toml
# Development only: Omini installs the plugin from requirements.txt and adds
# its own copy of omini-sdk to the plugin's environment.
[project]
name = "omini-plugin-example"
version = "0.1.0"
requires-python = ">=3.10"
dependencies = ["httpx>=0.27,<1", "omini-sdk"]

[dependency-groups]
dev = ["pytest>=8", "ruff"]

[tool.uv]
package = false

[tool.uv.sources]
omini-sdk = { path = "../omini/sdk/python" }

[tool.pytest.ini_options]
testpaths = ["tests"]
pythonpath = ["."]
```

## plugin.yaml

```yaml
id: example                 # lowercase letters, digits and dashes; unique
name: Example router        # shown in the store and as the integration's name
version: 0.1.0              # semantic version
protocol: 1                 # plugin protocol version (Omini supports 1)
entrypoint: main.py         # Python file run by Omini, inside the repository
timeout_s: 60               # time limit per run, 5 to 300 (default 60)
description: >-
  Example routers through their REST API: ports, ARP, DHCP leases. Read-only.
author: Your Name
homepage: https://github.com/you/omini-plugin-example
changes:                    # optional: the changes `apply` supports
  - port.enabled            # enable or disable a port
  - port.vlans              # a port's VLANs: {untagged, tagged}
  - vlan.set                # create or rename a VLAN: {id, name}
  - vlan.delete             # delete a VLAN: {id}
  - network.set             # a router's VLAN network: {vlan, name, parent, gateway, dhcp, access, mdns}
  - network.delete          # remove it: {vlan}
  - reservation.set         # a fixed address: {mac, ip, name}
  - reservation.delete      # {mac}
  - wifi.set                # a Wi-Fi network: {ssid, previous, vlan, password, enabled}
  - wifi.delete             # {ssid}
  - dns.set                 # a local DNS name: {name, ip} (name fully qualified)
  - dns.delete              # {name}
  - wol.send                # wake a device: {mac, ip}, sent on the network holding ip
  - pause.set               # cut a device's internet: {mac, ips, name}
  - pause.delete            # give it back: {mac}
backup: true                # optional: the plugin answers the backup action
fields:                     # the "add integration" form
  - key: url
    type: url
    label: Address
    required: true
    help: The router's web interface, e.g. https://192.168.1.1
  - key: username
    type: string
    label: Username
    default: omini
    required: true
  - key: password
    type: secret
    label: Password
    required: true
  - key: verify_tls
    type: bool
    label: Verify the TLS certificate
    default: false
    group: Advanced
  - key: timeout_s
    type: int
    label: Timeout (seconds)
    default: 15
    group: Advanced
    help: How long to wait for each answer of the router.
```

Omini refuses a manifest with an unknown key, a missing required key, an `id`
that does not match `^[a-z][a-z0-9-]*$`, an entrypoint outside the plugin, or
a protocol it does not support.

### Form fields

| Key | Required | Meaning |
|---|---|---|
| `key` | yes | Name of the value in the config (`^[a-z][a-z0-9_]*$`) |
| `type` | yes | `string`, `secret`, `host`, `url`, `int`, `bool` or `select` |
| `label` | | Label in the form |
| `help` | | Help text under the field |
| `required` | | The form cannot be saved without it |
| `default` | | Default value, of the field's type |
| `options` | | Choices of a `select` field |
| `group` | | Section the field is shown under ("Advanced"); fields without a group come first |

`secret` fields are encrypted in Omini's database, masked in the UI and the
API, and decrypted only when passed to your plugin. The UI renders the form
from these fields: a plugin needs no frontend code.

## The SDK

```python
"""main.py: Omini plugin for Example routers."""

from omini_sdk import ArpEntry, Device, Interface, PluginError, optional, plugin
from omini_sdk.http import Client, settings


def client(cfg) -> Client:
    if not cfg.str("url") or not cfg.str("password"):
        raise PluginError("the address, username and password are required")
    return Client(
        cfg.str("url"),
        name="the router",
        auth=(cfg.str("username"), cfg.str("password")),
        unauthorized="the router rejected the username or password",
        user_agent="omini-plugin-example",
        **settings(cfg),  # verify_tls and timeout_s from the form
    )


def arp(c: Client) -> list[ArpEntry]:
    return [ArpEntry(ip=a["ip"], mac=a["mac"].lower(), interface=a["port"])
            for a in c.get("/api/arp")]


@plugin.collect
def collect(cfg) -> list[Device]:
    with client(cfg) as c:
        system = c.get("/api/system")  # essential: its errors fail the run
        # Optional: skipped (and logged) when this firmware lacks it.
        neighbours = optional("the ARP table", lambda: arp(c), [])

    return [
        Device(
            key=system["lan_mac"].lower(),
            name=system["hostname"],
            host=c.host,
            role="router",
            vendor="Example",
            model=system.get("model"),
            interfaces=[
                Interface(name=p["name"], up=p["link"], speed_mbps=p.get("speed"),
                          rx_bytes=p["rx_bytes"], tx_bytes=p["tx_bytes"])
                for p in system["ports"]
            ],
            arp=neighbours or None,
        )
    ]


@plugin.test
def test(cfg) -> str:
    with client(cfg) as c:
        info = c.get("/api/system")
    return f"Connected to {info['hostname']}"


if __name__ == "__main__":
    plugin.run()
```

| Name | What it is |
|---|---|
| `plugin.collect` | Decorator for the function that returns the devices (a list of `Device`, or dicts with the same shape) |
| `plugin.test` | Decorator for the connection test; returns a short message for the user |
| `plugin.apply` | Decorator for the function that applies a change declared under `changes` in the manifest: `apply(cfg, change)` with `change.device`, `change.kind`, `change.port` and `change.value`. Apply it, read the device again to check it took effect, save it to the startup configuration only then, and return the device as read after the change |
| `plugin.backup` | Decorator for the function that returns each device's configuration (`backup: true` in the manifest): `backup(cfg)` returns a list of `ConfigBackup(device=<key>, format="text"|"json"|"xml", content=...)`, deterministic and without secrets where the device allows |
| `plugin.run()` | Reads the request, runs the action, prints the answer and exits |
| `PluginError("...")` | An error the user should read: wrong key, host unreachable, missing permission. Any other exception becomes "unexpected error", with the traceback in Omini's log |
| `Unauthorized`, `Forbidden`, `NotFound`, `Unavailable`, `HttpError` | `PluginError`s for what a device refuses or lacks (see [the toolkit](#the-toolkit)) |
| `optional(name, fn, default)` | Runs an optional read: skipped, and logged once, when the device lacks it |
| `omini_sdk.http` | `Client` (HTTP with timeouts, retries and those errors), `Session`, `LoginBackoff`, `settings(cfg)` |
| `omini_sdk.changes` | `apply_verify(apply, verify, undo)`: apply, check, undo when it does not take |
| `Config` | The `cfg` argument: a dict of the form values, with `cfg.str(key, default)`, `cfg.bool(key, default)`, `cfg.int(key, default)` and `cfg.state_dir` |
| `log` | A `logging` logger that writes to stderr (shown in Omini's log at debug level) |
| Models | `Change`, `ConfigBackup`, `Device`, `Interface`, `Neighbor`, `FdbEntry`, `ArpEntry`, `DhcpLease`, `DhcpReservation`, `DnsRecord`, `WirelessClient`, `WifiNetwork`, `Gateway`, `Firmware`, `Storage`, `Temperature`, `Transceiver`, `PortStp`, `PortVlans`, `Vlan`, `Network`, `NetworkDhcp`, `PausedDevice`, `Service`, `VpnPeer`, `DhcpPool`, `FirewallStates`, `Host` |

The models are pydantic classes generated from Omini's JSON Schema
(`schema/omini.schema.json`), so invalid data is caught before it reaches
Omini: the SDK answers "the plugin returned invalid data" and logs the details.

Make the test useful: when the account lacks a permission, say which one
(`optional(..., missing=missing)` collects them).

## The toolkit

Every plugin solves the same problems: timeouts, a device that is busy for a
moment, a login that must not be repeated every minute, an account without a
privilege, a change that does not take. The SDK solves them once, so every
plugin behaves alike for the user. Use it instead of your own.

### HTTP: `omini_sdk.http`

```python
from omini_sdk.http import Client, LoginBackoff, Session, settings
```

`Client(base_url, *, name, api="", scheme="https", port=None, strip=(),
verify_tls=False, timeout=15, retries=2, backoff=1.0, auth=None,
headers=None, user_agent="omini-plugin", unauthorized=None,
forbidden_hint=None, follow_redirects=False, transport=None)` wraps
`httpx.Client` (`c.http` when you need it, for a stream):

- The address gets `scheme` when it has none, `port` (one, or one per
  scheme) when it has none, and loses a pasted API suffix in `strip`
  (`normalize_url` does it alone). `api` is a prefix every path goes under.
  `c.base` is the address, `c.host` its host.
- `c.get(path, params)`, `c.post(path, json)`, `c.put`, `c.patch`,
  `c.delete` return the JSON answer (None when it is empty); `c.text(path)`
  a file as is; `c.request(method, path, **httpx_kwargs)` any of them;
  `c.send(...)` the raw response, with the retries but no status check.
- **Retries**: a refused connection and a busy answer (429, 503) are sent
  again `retries` times after a pause (`backoff`, doubled each time, or the
  device's Retry-After up to 10 s); a timeout or a cut answer once, and only
  for GET, HEAD, OPTIONS, PUT and DELETE (a POST that timed out may have been
  done).
- **Errors**, all `PluginError`, so one that reaches the SDK is shown as it
  is: `Unauthorized` (401, with the `unauthorized` message), `Forbidden`
  (403; `privilege` from `forbidden_hint`, a function of `(method, path)` or
  a mapping of path prefixes to the device's name for the privilege),
  `NotFound` (404, 410; `resource` is the path), `Unavailable` (connection
  refused, timeout, 429, 5xx), and `HttpError`, their base, for another 4xx
  (with the device's words for a refused change). Each has `status`,
  `method` and `resource`.
- A device that answers errors its own way (an error code in a 200, 400 for
  a missing menu): subclass `Client` and override `check(response, method,
  path)`, which raises the error a status means, or `detail(response)`, the
  device's words for an error.
- `settings(cfg)` gives `verify_tls` and `timeout` from the form's
  `verify_tls` and `timeout_s` fields: `Client(url, **settings(cfg))`.
- Tests pass `transport=httpx.MockTransport(handler)` (and `sleep=` to skip
  the pauses).

Raise the same errors from other transports (SNMP, SSH, web pages) when they
fit, so `optional` and `apply_verify` treat them alike.

**Logins.** `Session(cfg.state_dir, url, username, password, name="session")`
keeps a login in `<state_dir>/<name>.json`, readable by the owner only and
tied to a hash of the address and credentials: `load()` (an empty dict for
other credentials), `save(data)`, `clear()`. Keep a session id, cookies or a
token, never the password. `LoginBackoff(cfg.state_dir, url, username,
password, minutes=10)` waits after the device refuses the credentials:
`check()` raises while it waits, `refused(message)` after a refused password
(only then: a device out of reach is asked again next run), `clear()` after a
good login. Without a state folder neither keeps anything on disk.

### Optional reads: `omini_sdk.optional`

```python
from omini_sdk import optional

missing: list[str] = []
leases = optional("the DHCP leases", lambda: read_leases(c), [], missing=missing)
```

`optional(name, fn, default, *, missing=None)` runs a read the collection can
do without. `Forbidden`, `NotFound`, `Unavailable` and timeouts are logged once
at info and give `default`; a `Forbidden` also adds its privilege (else
`name`) to `missing`, for the connection test to list. `Unauthorized` is
raised. Anything else (a parser bug) is logged as a warning with its
traceback and gives `default`.

### Changes: `omini_sdk.changes`

```python
from omini_sdk.changes import Failed, Pending, apply_verify

apply_verify(
    lambda: c.put(f"/api/port/{port}", {"enabled": False}),
    lambda: read_port(c, port)["enabled"] is False,
    undo=lambda: c.put(f"/api/port/{port}", {"enabled": True}),
    message=f"could not disable {port}",
)
```

`apply_verify(apply, verify, undo=None, *, timeout=30, interval=2,
message="the change did not take effect", check_in="on the device",
undo_failed_apply=True)` runs
`apply`, then `verify` every `interval` seconds until it returns True or
`timeout` seconds pass (at least once), and returns what `apply` returned.
`verify` returns False (not yet), raises `Pending(why)` (not yet, and why),
`Failed(why)` (it will not take: stop now) or another `PluginError` (a read
that failed while the device applies it: read again). When `apply` raises or
the change is not seen, `undo` puts things back and the error says
`<message>: <why> (undone)` or `<message>: <why> (could not be undone: check
it on the device)` (`check_in="in UniFi Network"` names where). Give an
`undo` whenever the API allows it, safe to call when `apply` did only part;
when `apply` is one request that changes all or nothing, pass
`undo_failed_apply=False` so a refused one is not "undone".
Wait longer than 30 s only when the device needs it (a radio on a DFS channel
listens 60 s before it sends).

## Conventions

What Omini expects of every plugin, so devices look alike whatever reads them:

- **Device key**: stable, and never text the user can edit (a name, a
  description). The MAC of the LAN-side interface; else the lowest MAC of a
  physical port; else the host from the form. A controller uses each
  device's base MAC (or serial).
- **Role**: what the device is (`firewall`, `router`, `switch`, `ap`,
  `server`), from its kind or model. A default route alone does not make a
  device a `router` (servers and NAS have one too).
- **Vendor**: always set, as the brand is written (`OPNsense`, `Ubiquiti`,
  `MikroTik`).
- **Form fields**: `url` (type `url`) with the scheme in the help example
  (`https://192.168.1.1`); credentials `secret` (an API key ID that is not a
  secret may be `string`); `verify_tls` (default false) and `timeout_s`
  (default 15) under `group: Advanced`. Labels in sentence case without a
  colon ("API secret"); help says where to find or create the value, in one
  or two sentences.
- **IPv6** is first-class: addresses, neighbors (NDP), leases and DNS
  records are reported like IPv4 ones, never dropped.
- **Unknown values stay empty**: leave a field out when the device does not
  say it; `0`, `""` or `"unknown"` are not "unknown".
- **HTTP** goes through `omini_sdk.http.Client`, optional reads through
  `optional`, changes through `apply_verify`.
- **Backups** return `ConfigBackup(device=<the key collect reports>, ...)`.

## What to return

A list of **devices**. A controller returns every device it manages; a router
returns itself. The main fields:

| Field | Meaning |
|---|---|
| `key` (required) | Stable id within your integration: a base MAC or a serial |
| `name` (required) | The device's name |
| `host` | The address Omini reaches it at |
| `role` | `router`, `switch`, `ap`, `firewall`, `server` or `unknown` |
| `vendor`, `model`, `serial`, `os_version`, `uptime_s` | Identity |
| `macs`, `ips` | Its own addresses |
| `cpu_pct`, `cpu_count`, `mem_pct`, `mem_used_bytes`, `mem_total_bytes`, `swap_pct`, `load_avg` | Health |
| `temperatures`, `storage`, `firmware` | Sensors (`kind`: cpu, disk, board, other), disks, pending updates (what the device knows from its own last check) |
| `interfaces` | Ports: `name`, `description`, `mac`, `up`, `speed_mbps`, `duplex`, `media`, `connector` (`rj45`, `sfp`, `qsfp`), `type`, `rx_bytes`/`tx_bytes` counters, `rx_errors`/`tx_errors`, `ips`, `wan`, `members` (bridges, LAGs), `lag` (the LAG a port belongs to), `stp` (spanning tree `state` and `role`), `parent`, `vlan`, `vlans` (untagged and tagged), `transceiver` |
| `neighbors` | LLDP/CDP/MNDP neighbors: `local_port`, `remote_name`, `remote_port`, `remote_mac`, `remote_ip`, `protocol` |
| `fdb` | MAC table: `mac`, `port`, `vlan` |
| `arp`, `dhcp_leases` | IP ↔ MAC (and hostnames) |
| `wireless_clients` | `mac`, `interface`, `ssid`, `band`, `signal_dbm`, link rates, `rx_bps`/`tx_bps` |
| `gateways` | WAN gateways: `status` (`up`, `degraded`, `down`, `unknown`), `rtt_ms`, `loss_pct` |
| `vlans`, `services`, `vpn_peers`, `dhcp_pools`, `firewall_states` | Extras shown in the device panel |
| `networks`, `dhcp_reservations`, `wifi_networks` | A router's VLAN networks and fixed addresses, an access point's Wi-Fi networks (what `network.*`, `reservation.*` and `wifi.*` change) |
| `dns_records`, `dns_domain` | Local DNS names a DNS server or router answers, and its domain (what `dns.*` changes) |
| `paused` | Devices whose internet a router cuts: `mac`, `ips`, `name` (what `pause.*` changes) |
| `stp_root` | The spanning tree root bridge the device sees: `<priority>.<mac>` |
| `hosts` | End devices your integration observed with extra details (scanners, controllers) |

Rules of the data model:

- **MACs** are lowercase and colon-separated: `aa:bb:cc:dd:ee:ff`.
- **Ports** are referenced by their readable name (`ifName`, `igc0`, `Port 3`)
  everywhere: in `fdb`, `arp`, `neighbors`, `wireless_clients`. Translating an
  index to a name is your job.
- **Counters**, not rates: report byte counters as they are. Omini computes
  rates between two collections and handles wraps and resets.
- An interface flagged `wan: true` with `gateways` becomes a WAN node.
- **Declaring a link** you know but no protocol announces (a VM on its host,
  a mesh satellite): add a neighbor with `protocol: other`. It places only the
  side that reports it:

  ```json
  {"local_port": "net0", "protocol": "other", "remote_name": "pve1",
   "remote_port": "vmbr0", "remote_ip": "192.168.1.10"}
  ```

The full schema, with every field and its description, is
`schema/omini.schema.json` in the Omini repository.

## The protocol

You do not need this when you use the SDK, but this is what happens on each
run:

1. Omini starts `<plugin>/.venv/bin/python <entrypoint>` in the plugin's
   folder, with a minimal environment.
2. It writes one JSON request on stdin:

   ```json
   {"protocol": 1, "action": "collect",
    "config": {"url": "https://192.168.1.1", "username": "omini", "password": "..."},
    "state_dir": "/data/plugins/example/state/7"}
   ```

   `action` is `collect`, `test`, `apply` or `backup`. An `apply` request also carries
   the change, one of those the manifest declares under `changes`:

   ```json
   {"protocol": 1, "action": "apply", "config": {"...": "..."},
    "change": {"device": "1c:2a:a3:2f:57:f7", "kind": "port.enabled",
               "port": "Port 5", "value": false}}
   ```

   The plugin applies it, reads the device again to check that it took
   effect, saves it to the startup configuration only then, and answers with
   the device as read after the change (`{"devices": [...]}`), or an error.
   A `backup` request (only with `backup: true`) asks for each device's
   configuration as readable text: the device's own text export where it has
   one, else its settings as sorted JSON. It must be deterministic (no
   timestamps, uptimes, counters or client lists), so two backups differ only
   when the configuration changed, and leave out secrets where the device
   lets it. Omini keeps the versions that changed and shows their
   differences; it never sends a configuration back.
3. The plugin writes one JSON response on stdout, exactly one of:

   ```json
   {"devices": [{"key": "...", "name": "..."}]}
   {"message": "Connected to router1"}
   {"backups": [{"device": "<key as collect reports it>", "format": "xml", "content": "..."}]}
   {"error": "wrong username or password"}
   ```

4. Logs go to stderr. Omini shows them at debug level.

Limits: the manifest's `timeout_s` (60 seconds by default, at most 300) and
32 MB of output. A plugin that runs past its time is stopped and the
collection fails with "did not answer within ...".

## State between runs

`cfg.state_dir` is a folder private to one configured integration,
`<data dir>/plugins/<id>/state/<integration id>`, kept between runs and
removed with the plugin. Use it for sessions and tokens, so the plugin does
not log in on every collection, through `omini_sdk.http.Session`:

```python
from omini_sdk.http import Session

session = Session(cfg.state_dir, url, username, password)
sid = session.load().get("sid") or login()   # login() calls session.save({"sid": ...})
```

`Session` writes files readable only by the owner, never the password, and
ties a saved session to a hash of the address and credentials, so a change
in the form takes effect. A connection test runs with its own state folder.

Many devices lock an account after repeated failed logins: when a login is
refused, wait before trying again with `LoginBackoff` (10 minutes by
default; the Mercusys plugin waits 15).

## Testing

Test against **recorded answers of real devices**, anonymized (replace MACs,
serials, names and public IPs), kept in `tests/fixtures/`. Mock the HTTP
client and check the devices you return.

```python
import json

from omini_sdk.plugin import Plugin


def run(plugin: Plugin, config: dict, action: str = "collect") -> dict:
    request = json.dumps({"protocol": 1, "action": action, "config": config,
                          "state_dir": "/tmp/state"})
    response, code = plugin.handle(request)
    return response
```

`plugin.handle()` runs the whole request/response cycle without stdin and
stdout, including model validation, so a test catches data Omini would
refuse.

```bash
uv run pytest            # tests
uv run ruff check .      # lint
uv run ruff format .     # format
```

Run the same checks in CI (GitHub Actions): check out your repository and
`riccardoalv/omini` side by side, so `../omini/sdk/python` resolves.

## Development mode

Load your plugin in place, without installing it:

```bash
# in the Omini repository
OMINI_PLUGIN_DIRS=../omini-plugin-example make run
```

`OMINI_PLUGIN_DIRS` takes a comma-separated list of folders. Each is loaded
as a plugin (marked "development" in the store), and every collection runs
the current code: edit, then press **Run a round now** on the Integrations
screen. Omini
still builds an environment for it with uv, rebuilt when `requirements.txt`
changes. Set `OMINI_LOG_LEVEL=debug` to see what the plugin writes to stderr.

A development plugin cannot be removed or installed from the store while it is
loaded from a folder.

## Publishing

1. Push the repository to GitHub (public).
2. Create a **release** with a tag matching `version` in `plugin.yaml`
   (`v0.1.0`). Omini installs the latest release, or the newest commit of the
   default branch when there is none.
3. Anyone can now install it from **Integrations → Add integration → Plugin
   store → +** with the repository's address. Plugins installed that way are
   **Community / Unverified**.
4. To be listed in the store, open a **Plugin review** issue in the Omini
   repository with the repository, the release, the trust level asked for, what
   it was tested on and its known issues. The process and the criteria are in
   [Plugin review](plugin-review.md).

A store entry in `internal/plugins/catalog.json` looks like this:

```json
{
  "id": "example",
  "detect": {"brands": ["example"]},
  "name": "Example router",
  "description": "Example routers through their REST API: ports, ARP, DHCP leases. Read-only.",
  "url": "https://github.com/you/omini-plugin-example",
  "icon": "example",
  "publisher": "community",
  "trust": "experimental",
  "categories": ["router"],
  "reviewed_version": "0.1.0",
  "reviewed_at": "2026-10-01",
  "known_issues": "https://github.com/you/omini-plugin-example#known-issues"
}
```

`detect` lists the products, brands or operating systems Omini's
identification gives the devices your plugin reads: when one is found and the
plugin is not set up, the **Integration available** alert suggests it.
`icon` is a [Simple Icons](https://simpleicons.org) or Dashboard Icons slug,
or a device type (`switch`) when the brand has no logo.
