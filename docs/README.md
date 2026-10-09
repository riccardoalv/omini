# Omini documentation

Omini is a self-hosted network map. It finds the devices on your network,
reads what your routers, switches, access points and servers know about it,
and draws a live map of what is plugged into what, with traffic, health and
alerts. It only reads: it never changes the configuration of a device.

## Start here

| Guide | What it covers |
|---|---|
| [Installation](installation.md) | Docker, building from source, first run, upgrading, backup, uninstalling |
| [Configuration](configuration.md) | Environment variables, the Settings screen, collection rounds |
| [Discovery](discovery.md) | The network scan, SNMP, nmap, device identification, traffic flows |
| [The map](map.md) | Layout, nodes and links, panels, folding, areas, filters, export |
| [Alerts and notifications](alerts.md) | Every alert rule, the Alerts screen, Telegram, e-mail, webhooks |
| [Integrations](integrations.md) | Built-in integrations, each plugin and how to give it a read-only account |
| [Troubleshooting](troubleshooting.md) | Common problems and how to fix them |

## For developers

| Guide | What it covers |
|---|---|
| [Writing a plugin](plugins.md) | Repository layout, `plugin.yaml`, the Python SDK, the protocol, testing, publishing |
| [HTTP API](api.md) | Authentication and every route |
| [Architecture](architecture.md) | Components, data model, how the topology is built |
| [Decisions](decisions.md) | What was decided, and why, area by area |
| [Development](development.md) | Make targets, tests, CI, the simulated network, releases |
| [SNMP profiles](snmp-profiles.md) | YAML files that teach Omini a vendor's SNMP data |
| [Plugin review](plugin-review.md) | How plugins get a trust level in the store |

## In one minute

```bash
docker run -d --name omini --network host --restart unless-stopped \
  -v omini-data:/data riccardoalv/omini:latest
```

Open `http://<your-server>:8080`, create the admin account, and the map fills
in by itself within a minute or two. Read [Installation](installation.md) for
the details, and [Discovery](discovery.md) to understand what Omini can see.
