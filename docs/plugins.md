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
- [What to return](#what-to-return)
- [The protocol](#the-protocol)
- [State between runs](#state-between-runs)
- [Testing](#testing)
- [Development mode](#development-mode)
- [Publishing](#publishing)

## Rules

1. **Read-only.** Never send a request that changes the device's
   configuration, reboots it or starts a check. A `POST` is acceptable only for
   a login or a read that needs it, and must be documented in the README.
2. **Optional data never fails the collection.** Try optional endpoints and
   skip them on error. Fail only when the essential data cannot be read.
3. **Short timeouts** on every request (about 5 to 10 seconds).
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

import httpx

from omini_sdk import ArpEntry, Device, Interface, PluginError, log, plugin


def client(cfg) -> httpx.Client:
    return httpx.Client(
        base_url=cfg.str("url"),
        auth=(cfg.str("username"), cfg.str("password")),
        verify=cfg.bool("verify_tls", False),
        timeout=10,
    )


@plugin.collect
def collect(cfg) -> list[Device]:
    with client(cfg) as c:
        try:
            system = c.get("/api/system").raise_for_status().json()
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 401:
                raise PluginError("wrong username or password") from e
            raise PluginError(f"the router answered {e.response.status_code}") from e
        except httpx.HTTPError as e:
            raise PluginError(f"cannot reach {cfg.str('url')}: {e}") from e

        arp = []
        try:  # optional: skip it if this firmware lacks it
            arp = [ArpEntry(ip=a["ip"], mac=a["mac"].lower(), interface=a["port"])
                   for a in c.get("/api/arp").raise_for_status().json()]
        except httpx.HTTPError as e:
            log.info("no ARP table: %s", e)

    return [
        Device(
            key=system["mac"].lower(),
            name=system["hostname"],
            role="router",
            vendor="Example",
            model=system.get("model"),
            interfaces=[
                Interface(name=p["name"], up=p["link"], speed_mbps=p.get("speed"),
                          rx_bytes=p["rx_bytes"], tx_bytes=p["tx_bytes"])
                for p in system["ports"]
            ],
            arp=arp,
        )
    ]


@plugin.test
def test(cfg) -> str:
    with client(cfg) as c:
        try:
            info = c.get("/api/system").raise_for_status().json()
        except httpx.HTTPError as e:
            raise PluginError(f"cannot read the router: {e}") from e
    return f"Connected to {info['hostname']}"


if __name__ == "__main__":
    plugin.run()
```

| Name | What it is |
|---|---|
| `plugin.collect` | Decorator for the function that returns the devices (a list of `Device`, or dicts with the same shape) |
| `plugin.test` | Decorator for the connection test; returns a short message for the user |
| `plugin.run()` | Reads the request, runs the action, prints the answer and exits |
| `PluginError("...")` | An error the user should read: wrong key, host unreachable, missing permission. Any other exception becomes "unexpected error", with the traceback in Omini's log |
| `Config` | The `cfg` argument: a dict of the form values, with `cfg.str(key, default)`, `cfg.bool(key, default)`, `cfg.int(key, default)` and `cfg.state_dir` |
| `log` | A `logging` logger that writes to stderr (shown in Omini's log at debug level) |
| Models | `Device`, `Interface`, `Neighbor`, `FdbEntry`, `ArpEntry`, `DhcpLease`, `WirelessClient`, `Gateway`, `Firmware`, `Storage`, `Temperature`, `Transceiver`, `PortVlans`, `Vlan`, `Service`, `VpnPeer`, `DhcpPool`, `FirewallStates`, `Host` |

The models are pydantic classes generated from Omini's JSON Schema
(`schema/omini.schema.json`), so invalid data is caught before it reaches
Omini: the SDK answers "the plugin returned invalid data" and logs the details.

Make the test useful: when the account lacks a permission, say which one.

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
| `interfaces` | Ports: `name`, `description`, `mac`, `up`, `speed_mbps`, `duplex`, `media`, `connector` (`rj45`, `sfp`, `qsfp`), `type`, `rx_bytes`/`tx_bytes` counters, `rx_errors`/`tx_errors`, `ips`, `wan`, `members` (bridges, LAGs), `parent`, `vlan`, `vlans` (untagged and tagged), `transceiver` |
| `neighbors` | LLDP/CDP/MNDP neighbors: `local_port`, `remote_name`, `remote_port`, `remote_mac`, `remote_ip`, `protocol` |
| `fdb` | MAC table: `mac`, `port`, `vlan` |
| `arp`, `dhcp_leases` | IP ↔ MAC (and hostnames) |
| `wireless_clients` | `mac`, `interface`, `ssid`, `band`, `signal_dbm`, link rates, `rx_bps`/`tx_bps` |
| `gateways` | WAN gateways: `status` (`up`, `degraded`, `down`, `unknown`), `rtt_ms`, `loss_pct` |
| `vlans`, `services`, `vpn_peers`, `dhcp_pools`, `firewall_states` | Extras shown in the device panel |
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

   `action` is `collect` or `test`.
3. The plugin writes one JSON response on stdout, exactly one of:

   ```json
   {"devices": [{"key": "...", "name": "..."}]}
   {"message": "Connected to router1"}
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
not log in on every collection:

```python
import json
from pathlib import Path

def load_session(cfg) -> dict:
    path = Path(cfg.state_dir) / "session.json"
    try:
        return json.loads(path.read_text())
    except (OSError, ValueError):
        return {}
```

Write files readable only by the owner, never store a password in clear, and
tie a cached session to a hash of the address and credentials so a change in
the form takes effect. A connection test runs with its own state folder.

Many devices lock an account after repeated failed logins: when a login is
refused, wait before trying again (the Mercusys and UniFi plugins wait 10 to
15 minutes).

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
the current code: edit, then press **Run now** in the integration. Omini
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
