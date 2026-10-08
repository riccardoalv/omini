# Configuration

Omini needs no configuration file. A few environment variables set how it
starts; everything else is changed in the web UI and stored in the database.

- [Environment variables](#environment-variables)
- [Collection rounds](#collection-rounds)
- [The Settings screen](#the-settings-screen)
- [Integration settings](#integration-settings)
- [Preferences kept in the browser](#preferences-kept-in-the-browser)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `OMINI_ADDR` | `:8080` | Address and port the web server listens on, e.g. `127.0.0.1:8080` or `:9000` |
| `OMINI_DATA_DIR` | `./data` (`/data` in Docker) | Where the database, the secret key, plugins and caches live |
| `OMINI_POLL_INTERVAL` | `60` | Default time between [collection rounds](#collection-rounds): seconds (`90`) or a duration (`1m30s`). Minimum 10 s. The interval chosen on the Integrations screen takes precedence |
| `OMINI_SECRET_KEY` | (generated) | Base64 of a 32-byte key used to encrypt credentials in the database. When unset, Omini creates `<data dir>/secret.key` on first start. Back it up with the database |
| `OMINI_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. `debug` also shows what plugins write to stderr |
| `OMINI_AUTOSCAN` | `true` | Create the Network scan integration on first start (when there are no integrations yet) |
| `OMINI_NMAP` | (unset) | Docker image only: `install` installs nmap when the container starts |
| `OMINI_NMAP_PRIVILEGED` | `false` | `true` when the nmap binary has raw-socket capabilities while Omini does not run as root, so nmap may detect operating systems (see [nmap without root](discovery.md#nmap-without-root)) |
| `OMINI_PLUGIN_DIRS` | (unset) | Comma-separated folders of plugins loaded in place, without installing them (plugin development, see [Writing a plugin](plugins.md#development-mode)) |
| `OMINI_PLUGIN_INDEX` | this repository's `internal/plugins/catalog.json` on GitHub | URL of the plugin store's index, fetched once a day. `off` uses only the list shipped in the binary |
| `OMINI_UV` | `uv` | Path of the [uv](https://docs.astral.sh/uv/) binary used to build plugin environments |

The Docker image also sets `UV_PYTHON_DOWNLOADS=never` (plugins use the
image's Python) and `UV_CACHE_DIR=/data/.cache/uv`.

Generate a key for `OMINI_SECRET_KEY` with:

```bash
head -c 32 /dev/urandom | base64
```

Changing or losing the key makes the stored credentials unreadable: the
integrations then fail until you enter their passwords again.

### Example

```yaml
services:
  omini:
    image: riccardoalv/omini:latest
    network_mode: host
    restart: unless-stopped
    volumes:
      - ./data:/data
    environment:
      TZ: America/Sao_Paulo
      OMINI_ADDR: ":8090"
      OMINI_POLL_INTERVAL: "2m"
      OMINI_LOG_LEVEL: debug
      OMINI_NMAP: install
```

## Collection rounds

Omini reads the network in **rounds**. A round runs every enabled integration
**one at a time**, from the edge of the network to its center:

1. access points and servers,
2. then switches,
3. then routers and firewalls,
4. then the built-in discovery (network scan, nmap, traffic flows), which sees
   the whole network from the center.

The order comes from where each integration's devices sat on the last map
(before there is a map, from their roles). The map is built **once, at the end
of the round**, from that single picture. This keeps a client from jumping
between the access point that saw it a moment ago and the switch that sees it
now.

- **Interval.** The **Collection in rounds** card at the top of the
  Integrations screen sets the time between rounds: 15 seconds to 24 hours.
  "default" is `OMINI_POLL_INTERVAL` (1 minute unless set). The card also shows
  the order of the last round and how long it took.
- **Failures.** An integration that fails keeps its last data, shown as
  offline, and raises an alert. The others are not affected.
- **Time limit.** Each integration gets 45 seconds per round; plugins get the
  limit declared in their manifest (60 seconds by default, up to 300).
- **Run now.** "Run now" in an integration's details collects only that
  integration, at once, skipping its caches. "Refresh now" on the map starts a
  whole round. Saving an integration's settings also starts a round.
- **Traffic** is the average between two collections of the same interface,
  so a shorter interval gives finer traffic numbers and history.

Some work happens on its own schedule inside a round:

| Work | When |
|---|---|
| Network scan: ARP, ping, SNMP of known SNMP devices | every round |
| Network scan: ports, names, banners, web titles, SNMP probe of new hosts | once per new device, then every 6 hours (`Re-check ports and names every`) |
| nmap: scan of every subnet | in the background, every 24 hours by default (`Scan again every`) |
| Plugin store index | once a day |

## The Settings screen

**Settings** has a menu of sections on the left. A link can open one directly
with `?section=` (for example `/settings?section=notifications`).

| Section | What you set |
|---|---|
| **General** | Theme (follow the system, dark or light) and language (English or Brazilian Portuguese). The language is saved for your user and applied when you sign in |
| **Map** | "Group clients when there are more than" N: above this many clients, an access point, switch port or segment folds them into a bubble (default 8). See [Folding](map.md#folding-children-into-bubbles) |
| **Notifications** | Where alerts are sent: Telegram, e-mail, Slack, Discord, ntfy or a webhook. See [Notifications](alerts.md#notifications) |
| **Account** | Who is signed in, and **Sign out** |
| **About** | The running version and a link to the source code |

There is one user, the admin created on first run. Its password cannot be
changed from the UI yet; see
[Resetting the admin password](troubleshooting.md#i-forgot-the-admin-password).

## Integration settings

Click an integration on the **Integrations** screen to expand its settings
and status inline: last collection, duration, devices found, what found them,
**Test connection**, **Run now**, the enable switch and delete.

- Forms are generated from each integration's fields, so plugins need no UI
  code. Secret fields (passwords, keys, communities) are encrypted in the
  database, never shown again ("Saved — leave blank to keep") and never logged.
- New settings apply on the next collection, which saving starts.
- An integration is named after its type ("Network scan", "OPNsense"); names
  are not editable.
- The **Network scan**, **nmap** and **Traffic flows** integrations can be
  added only once. The store marks them "Already added".

What each integration offers is in [Discovery](discovery.md) and
[Integrations](integrations.md).

## Preferences kept in the browser

These are remembered per browser, not per user:

- the map orientation (left to right or top down),
- which groups and areas are folded or unfolded,
- "Hide offline" on the map,
- the sidebar (compact or expanded),
- the theme and the client grouping threshold,
- alert popups you closed.

The layout itself (where you dragged nodes), areas, port names, device names,
hidden and pinned devices are stored on the server and shared by every browser.
