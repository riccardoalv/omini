# HTTP API

The web UI uses a JSON API that you can call yourself: to read the map from a
script, add integrations in bulk, or connect Omini to other tools. This page
lists every route of the current release. The API is not versioned yet; field
names can change between minor releases, and the release notes say so.

- [Conventions](#conventions)
- [Authentication](#authentication)
- [Map](#map)
- [Inventory](#inventory)
- [Integrations and collection](#integrations-and-collection)
- [Plugins](#plugins)
- [Alerts, presence and history](#alerts-presence-and-history)
- [Flows](#flows)
- [Notifications](#notifications)
- [Other](#other)

## Conventions

- Base URL: `http://<server>:8080/api`.
- Bodies are JSON (`Content-Type: application/json`), at most 1 MB. Unknown
  fields are refused with 400.
- Errors are `{"error": "message"}` with a 4xx or 5xx status. Internal errors
  answer only `"internal error"`; the details go to Omini's log.
- Times are RFC 3339 (`2026-10-08T14:02:11Z`). Rates are bits per second,
  counters bytes.
- Secret fields of integrations and notification channels are never returned:
  they come back masked. Send the masked value back unchanged to keep the
  stored one.

## Authentication

Omini has a single admin user. A session is a cookie, `omini_session`
(HttpOnly, SameSite=Lax, Secure over HTTPS), valid for 30 days.

**CSRF protection.** Every request that is not `GET`, `HEAD` or `OPTIONS`
must carry the header `X-Omini-Request` (any value, e.g. `1`). Browsers do not
let other sites set custom headers without CORS, which Omini never allows.
Requests without it get 403.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/api/health` | no | `{"status": "ok", "version": "1.0.0"}` |
| `GET` | `/api/auth/status` | no | `setup_required`, `authenticated`, and when signed in `username` and `locale` |
| `POST` | `/api/auth/setup` | no | First run only: create the admin. Body `{username, password, locale}` (password of 8 characters or more). Signs in. 409 when an admin exists |
| `POST` | `/api/auth/login` | no | Body `{username, password}`. Sets the session cookie. 401 on wrong credentials |
| `POST` | `/api/auth/logout` | no | Ends the session |
| `PATCH` | `/api/me` | yes | Body `{locale}`: `en`, `pt-BR` or `""` (the browser's) |
| `POST` | `/api/me/password` | yes | Body `{current, new}` (8+ characters). `204`; `403` when the current password is wrong; other sessions are signed out |

Every other `/api/` route requires a session and answers 401 without one.

```bash
# sign in and keep the cookie
curl -c omini.cookie -H 'X-Omini-Request: 1' -H 'Content-Type: application/json' \
  -d '{"username": "admin", "password": "your-password"}' \
  http://omini.lan:8080/api/auth/login

# read the map
curl -b omini.cookie http://omini.lan:8080/api/topology

# start a collection round
curl -b omini.cookie -X POST -H 'X-Omini-Request: 1' http://omini.lan:8080/api/refresh
```

## Map

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/topology` | The map: `topology` (`nodes`, `edges`), `statuses` (last collection of each integration), `alerts` (open), `generated_at`, `round` (last collection round), `layout` (saved positions) and `areas` |
| `POST` | `/api/refresh` | Start a collection round as soon as possible (202) |
| `PUT` | `/api/layout` | Body `{positions: {"<node id>": {x, y}}}`. Saves dragged positions; top-down positions use ids prefixed `DOWN:` |
| `DELETE` | `/api/layout` | Forget every saved position ("Reset layout") |
| `POST` | `/api/areas` | Create an area: `{name, color, direction, x, y, width, height, members}`. `color` is a preset (`gray`, `blue`, `green`, `yellow`, `red`, `purple`) or `#rrggbb`; `direction` is `RIGHT` or `DOWN` |
| `PATCH` | `/api/areas/{id}` | Change any of `name`, `color`, `x`, `y`, `width`, `height`, `members`, `hidden` |
| `DELETE` | `/api/areas/{id}` | Delete an area (its devices stay) |
| `PUT` | `/api/nodes/{id}/ports/{port}` | Body `{label}`: name a device's port; `""` restores the device's own description |
| `GET` | `/api/nodes/{id}/web` | Web interfaces found on the node's IP: `[{url, port, title}]`. Only nodes on the map are probed; cached 10 minutes |
| `POST` | `/api/nodes/{id}/scan` | Scan the node with the nmap integration (up to 6 minutes) and rebuild the map. Returns the host found. 409 without an enabled nmap integration, 404 when the node has no address |

**Nodes** have `id`, `kind` (`device`, `unmanaged`, `segment`, `client`,
`app`, `wan`), `label`, `role`, `online`, `ip`, `mac`, `hostname`, `vendor`,
`model`, `parent_id` and `port` (where it is attached), Wi-Fi fields (`ssid`,
`band`, `signal_dbm`, `link_mbps`, `flow`), `traffic` (current rate per
interface: `rx_bps`, `tx_bps`), the identification (`type`, `os`, `brand`,
`product`, `reasons`), scan results (`open_ports`, `services`, `titles`,
`web`, `banners`), user fields (`pinned`, `hidden`, `icon`, `port_labels`),
`last_seen` for offline nodes, and for managed devices `device`: everything
the integration reported, in the [plugin data model](plugins.md#what-to-return).

**Edges** have `id`, `source` (the upstream end), `target`, `source_port`,
`target_port`, `kind` (`lldp`, `fdb`, `wifi`, `inferred`, `vpn`) and
`speed_mbps`.

## Inventory

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/inventory` | Every device ever seen: `id`, `kind`, `label`, `mac`, `ip`, `hostname`, `vendor`, `parent_id`, `port`, `alias`, `pinned`, `hidden`, `device_type`, `icon`, `first_seen`, `last_seen`, plus `online` and the current identification |
| `PATCH` | `/api/inventory/{id}` | Change user fields: `alias` (name), `pinned`, `hidden`, `device_type`, `icon` (`""` = automatic) |
| `POST` | `/api/inventory/delete` | Body `{ids: [...]}`: delete entries and their history (they come back if seen again) |

## Integrations and collection

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/integration-types` | Available types: built-in and installed plugins, with `type`, `name`, `description`, `kind` (`core` or `plugin`), `single` and the form `fields` |
| `GET` | `/api/integrations` | Configured integrations: `id`, `name`, `type`, `config` (secrets masked), `enabled`, `created_at`, `updated_at`, and `status` (last collection: `ok`, `error`, `collected_at`, `duration_ms`, `devices`) |
| `POST` | `/api/integrations` | Body `{type, config, enabled}`. Validates and encrypts the config, then starts a round. 409 for a second instance of a single type |
| `PUT` | `/api/integrations/{id}` | Body `{config, enabled}`. The type cannot change |
| `DELETE` | `/api/integrations/{id}` | Delete an integration (its devices leave the map on the next rebuild) |
| `POST` | `/api/integrations/test` | Body `{type, config}`, or `{id, config}` to test a saved one with edited values. Answers `{ok: true, message}` or `{ok: false, error}` (20 s limit) |
| `POST` | `/api/integrations/{id}/run` | Collect this integration now, skipping its caches; returns its new status (2 minute limit). 409 when it is disabled |
| `GET` | `/api/collection` | The round settings: `interval_s`, `default_s` (`OMINI_POLL_INTERVAL`) and the last `round` (`started_at`, `duration_ms`, `order`, `interval_s`) |
| `PUT` | `/api/collection` | Body `{interval_s}`: 15 to 86400 seconds, or 0 for the default |
| `GET` | `/api/capabilities` | What the network scan can do here: `capabilities` (`container`, `host_network`, `unprivileged_ping`, `raw_sockets`, `multicast`) and `limited` (what is missing) |

## Plugins

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/plugins` | Installed plugins: manifest, source (URL, version, commit, date), `dev`, `publisher`, `trust` |
| `GET` | `/api/plugins/catalog` | The store's catalog, each entry with `installed` and the installed `version` |
| `GET` | `/api/plugins/index` | Where the catalog came from and when it was fetched |
| `POST` | `/api/plugins/index/refresh` | Fetch the remote index now. 409 when `OMINI_PLUGIN_INDEX=off` |
| `POST` | `/api/plugins` | Body `{url, version}`: install or update a plugin from a GitHub repository. `version` is a tag, branch or commit; empty for the latest release (else the newest commit). Can take minutes |
| `DELETE` | `/api/plugins/{id}` | Remove a plugin. 409 while integrations use it, or for a development plugin |

## Alerts, presence and history

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/alerts` | Open alerts; `?resolved_hours=N` (1 to 2160) adds those resolved in the last N hours. Each has `id`, `key`, `rule`, `severity`, `node_id`, `params`, `opened_at`, `updated_at`, `resolved_at`, `dismissed` |
| `POST` | `/api/alerts/{id}/dismiss` | Body `{dismissed: true}` hides the alert until it resolves; `false` shows it again |
| `GET` | `/api/presence` | The presence timeline, newest first: `id`, `node_id`, `kind` (`join`, `leave`), `at`, `first`, `label`, `mac`, `ip`. Query: `node` (one device), `first=1` (first sightings only), `before` (an event id, for the next page), `limit` (1 to 500) |
| `GET` | `/api/history` | One interface's traffic: `?node=<id>&iface=<name>&hours=N` (1 to 8760, default 24). Per minute up to a day, per hour beyond: `[{at, rx_bps, tx_bps, rx_max_bps, tx_max_bps}]`. An empty `iface` is a Wi-Fi client's own traffic |

## Flows

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/flows` | Conversations over the last `minutes` (1 to 1440, default 60), at most `limit` (1 to 1000, default 200); `node` or `ip` limits them to one device. Answers `conversations` (`a`, `b`, `a_node`, `b_node`, `bytes_ab`, `bytes_ba`, `packets`, `ports: [{proto, port, bytes}]`), `listening` and `exporters` |

## Notifications

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/notifier-types` | Channel types (`webhook`, `telegram`, `email`) with their form fields |
| `GET` | `/api/notifiers` | Channels: `id`, `type`, `config` (secrets masked), `min_severity`, `notify_resolved`, `enabled`, `last_sent_at`, `last_error`, `created_at` |
| `POST` | `/api/notifiers` | Body `{type, config, min_severity, notify_resolved, enabled}`. `min_severity` is `critical`, `warning` (default) or `info` |
| `PUT` | `/api/notifiers/{id}` | Change any of those fields |
| `DELETE` | `/api/notifiers/{id}` | Delete a channel |
| `POST` | `/api/notifiers/test` | Body `{type, config}` or `{id}`: send a test message. Answers `{ok}` or `{ok: false, error}` |

Channel `config` keys: webhook `url`, `format` (`json`, `slack`, `discord`,
`ntfy`), `secret`; Telegram `token`, `chat_id`; e-mail `host`, `port`,
`security` (`starttls`, `tls`, `none`), `username`, `password`, `from`, `to`.

## Other

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/api/icons` | no | Names of the app icons Omini has |
| `GET` | `/api/icons/{name}` | no | One app icon (SVG), cached a day |

Any path outside `/api/` serves the web UI.
