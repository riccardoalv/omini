# Installation

Omini is one program: a Go binary with the web UI built in and an embedded
SQLite database. Plugins run as Python scripts next to it. You can run it with
Docker (recommended) or build it yourself.

- [Requirements](#requirements)
- [Docker](#docker)
- [Docker Compose](#docker-compose)
- [From source](#from-source)
- [First run](#first-run)
- [Upgrading](#upgrading)
- [Backup and restore](#backup-and-restore)
- [Uninstalling](#uninstalling)

## Requirements

- A Linux machine on the network you want to map: a mini PC, a VM, a NAS or a
  Raspberry Pi 4/5 (64-bit) are all fine. Omini uses little CPU and memory.
- **Host networking.** The network scan reads the ARP table and listens for
  mDNS and SSDP announcements, so Omini must sit on your LAN, not behind
  Docker's NAT. See [What the network scan needs](discovery.md#what-each-method-needs).
- A browser to open the web UI (port 8080 by default).

## Docker

The image is published for **amd64** and **arm64** on two registries:

| Registry | Image |
|---|---|
| Docker Hub | `riccardoalv/omini` |
| GitHub Container Registry | `ghcr.io/riccardoalv/omini` |

Tags: `latest`, the exact version (`1.0.0`) and the minor version (`1.0`).
Pin a version if you want upgrades to happen only when you decide.

```bash
docker run -d \
  --name omini \
  --network host \
  --restart unless-stopped \
  -v omini-data:/data \
  -e TZ=Europe/Lisbon \
  riccardoalv/omini:latest
```

Then open `http://<your-server>:8080`.

What the image contains:

- The Omini binary, with the UI and the app icon bundle built in.
- Python 3 and [uv](https://docs.astral.sh/uv/), so every plugin works out of the box.
- Everything Omini writes goes to the `/data` volume.
- It runs as root, which lets the network scan ping and lets nmap detect
  operating systems. Omini itself only reads from your network.

### nmap

Omini can use [nmap](https://nmap.org) for deeper scans, but does not ship it
(nmap's license is not MIT). Set `OMINI_NMAP=install` and the container
installs it from Debian's packages on start (once per container):

```bash
docker run -d --name omini --network host --restart unless-stopped \
  -v omini-data:/data -e OMINI_NMAP=install riccardoalv/omini:latest
```

Then add the **nmap** integration in **Integrations → Add integration**. See
[nmap](discovery.md#nmap).

### Ports

With host networking nothing needs to be published. Omini uses:

| Port | Protocol | Used for |
|---|---|---|
| 8080 | TCP | Web UI and API (`OMINI_ADDR` changes it) |
| 5353 | UDP, inbound | mDNS announcements (names, models) |
| 1900 | UDP, inbound | SSDP / UPnP answers (models) |
| 2055 | UDP, inbound | NetFlow / IPFIX, only with the Traffic flows integration |
| 6343 | UDP, inbound | sFlow, only with the Traffic flows integration |

If the host has a firewall, allow the inbound UDP ports you use.

## Docker Compose

Save this as `docker-compose.yml`:

```yaml
services:
  omini:
    image: riccardoalv/omini:latest   # or ghcr.io/riccardoalv/omini:latest
    container_name: omini
    network_mode: host                # the network scan must see your LAN
    restart: unless-stopped
    volumes:
      - ./data:/data
    environment:
      TZ: UTC
      # OMINI_NMAP: install           # install nmap on start, for the nmap integration
```

```bash
docker compose up -d
docker compose logs -f omini          # watch it start
```

`network_mode: host` is what makes discovery work. Without it, Omini only sees
Docker's own bridge network, and the **Network discovery is limited** alert
tells you so.

Other settings are environment variables: see [Configuration](configuration.md).

## From source

Requirements: Go (the version in `go.mod`), Node.js 24 and `make`. Plugins also
need [uv](https://docs.astral.sh/uv/) on the `PATH` (it downloads a Python if
the system has none).

```bash
git clone https://github.com/riccardoalv/omini.git
cd omini
make web                                  # build the UI into web/dist
go run ./internal/appicons/gen -bundle    # optional: bundle the app icons
go build -trimpath -ldflags "-X main.version=1.0.0" -o omini ./cmd/omini
./omini
```

- The UI is embedded in the binary, so `make web` must run before `go build`.
  Without it the server answers with a page saying the UI was not built.
- Without the icon bundle, app icons are downloaded from the Dashboard Icons
  CDN the first time they are shown and cached in `<data dir>/icons`.
- Data goes to `./data` unless you set `OMINI_DATA_DIR`.
- `make run` does the build and starts Omini on `:8080` in one step.

Running as a normal user works, with some limits: ping falls back to TCP
unless the system allows unprivileged ping sockets, and nmap cannot detect
operating systems without raw-socket permission. The fixes are in
[What each method needs](discovery.md#what-each-method-needs).

### As a systemd service

```ini
# /etc/systemd/system/omini.service
[Unit]
Description=Omini network map
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/omini
Environment=OMINI_DATA_DIR=/var/lib/omini
StateDirectory=omini
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now omini
```

This runs Omini as root. To run it as another user, add `User=` and allow
ping sockets (`sysctl net.ipv4.ping_group_range="0 2147483647"`).

## First run

1. Open `http://<your-server>:8080`.
2. Pick your language and **create the admin account** (password of at least
   8 characters). There is one account per Omini; the first visit creates it.
3. The **Network scan** integration is created automatically
   (`OMINI_AUTOSCAN`) and the first collection round starts at once. Devices
   appear within a minute or two; names, ports and models fill in over the
   next rounds.
4. Open **Integrations** and look at **Network scan → On this server**: it
   says whether host networking, multicast and ping work where Omini runs.
5. Add more integrations from **Integrations → Add integration**: SNMP is
   already part of the network scan; nmap, traffic flows and the plugins for
   firewalls, switches, access points and hypervisors are in the store. See
   [Integrations](integrations.md).

## Upgrading

Back up the data directory first (see below). Then:

```bash
# Docker
docker pull riccardoalv/omini:latest
docker rm -f omini
docker run -d ...                 # the same command as before

# Docker Compose
docker compose pull
docker compose up -d
```

From source, pull the new code, rebuild and restart.

Database changes are applied automatically on start. Downgrading is not
supported: an older Omini refuses to start on a database a newer one has
changed ("the database was created by a newer version of Omini"), so nothing is
lost. To go back to an older version, restore the backup you made before the
upgrade. Installed plugins are kept; update them from the store
(**Integrations → Add integration → Plugin store**, "Update").

## Backup and restore

Everything Omini keeps is in its data directory (`/data` in Docker,
`OMINI_DATA_DIR` otherwise):

| Path | Contents |
|---|---|
| `omini.db`, `omini.db-wal`, `omini.db-shm` | The SQLite database: integrations, inventory, layout, areas, alerts, history, notification channels |
| `secret.key` | The key that encrypts device passwords, API keys and tokens in the database |
| `plugins/` | Installed plugins, their Python environments and their state (sessions) |
| `profiles/` | Your own [SNMP profiles](snmp-profiles.md), if any |
| `icons/` | Downloaded app icons (a cache) |

**Back up `omini.db` and `secret.key` together.** Credentials in the database
cannot be read without the key. If you set `OMINI_SECRET_KEY` instead of using
the file, keep that value with the backup.

The simplest consistent backup is to stop Omini and copy the directory:

```bash
docker stop omini
tar czf omini-backup-$(date +%F).tar.gz -C /var/lib/docker/volumes/omini-data/_data .
docker start omini
```

(With Compose and `./data:/data`, archive `./data` instead.)

To back up while it runs, use SQLite's online backup, then copy the key:

```bash
sqlite3 /path/to/data/omini.db ".backup '/backups/omini.db'"
cp /path/to/data/secret.key /backups/
```

To restore, stop Omini, put the files back in an empty data directory and
start it. `plugins/` can be left out: reinstall the plugins from the store
(their integrations stay in the database).

## Uninstalling

```bash
docker rm -f omini
docker volume rm omini-data      # or delete ./data with Compose: this erases everything
docker rmi riccardoalv/omini
```

For a binary install, stop the service, delete the binary and the data
directory. Omini changes nothing on your devices, so there is nothing to undo
there. You may want to remove the read-only accounts or API tokens you created
for its integrations.
