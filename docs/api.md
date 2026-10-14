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
| `POST` | `/api/collection/run` | Start a round now in which every integration skips its caches (deep scan, nmap scan): the Integrations screen's "Run a round now" (202) |
| `PUT` | `/api/layout` | Body `{positions: {"<node id>": {x, y}}}`. Saves dragged positions; top-down positions use ids prefixed `DOWN:` |
| `DELETE` | `/api/layout` | Forget every saved position ("Reset layout", which also folds the clients in the UI) |
| `POST` | `/api/areas` | Create an area: `{name, color, direction, x, y, width, height, members}`. `color` is a preset (`gray`, `blue`, `green`, `yellow`, `red`, `purple`) or `#rrggbb`; `direction` is `RIGHT` or `DOWN` |
| `PATCH` | `/api/areas/{id}` | Change any of `name`, `color`, `x`, `y`, `width`, `height`, `members`, `hidden` |
| `DELETE` | `/api/areas/{id}` | Delete an area (its devices stay) |
| `PUT` | `/api/nodes/{id}/ports/{port}` | Body `{label}`: name a device's port; `""` restores the device's own description |
| `POST` | `/api/nodes/{id}/changes` | Body `{kind, port, value, confirm}`: apply a [configuration change](decisions.md#configuration-changes) (`kind` one of the node's `changes`, e.g. `{"kind":"port.enabled","port":"Port 5","value":false}`). Returns the change as kept in the history. 409 `{error:"destructive", reason, node, allowed}` for a destructive one: blocked (`allowed` false) or to be confirmed again with `confirm: true`; 400 for an unknown port or a wrong value; 404 when the device cannot apply it; 502 `{error, change}` when the device refused it |
| `GET` | `/api/nodes/{id}/changes` | The latest changes made to the node, newest first: `[{id, kind, port, before, after, error, at}]` (kept a year) |
| `GET`, `PUT` | `/api/change-settings` | `{allow_destructive, dns_domain}`: whether destructive changes are allowed (asked twice) or blocked, and the domain of local DNS names (`''`: the one the DNS server or router uses, else `home.arpa`); `PUT` changes the fields it has |
| `GET` | `/api/nodes/{id}/web` | Web interfaces found on the node's IP: `[{url, port, title}]`. Only nodes on the map are probed; cached 10 minutes |
| `POST` | `/api/nodes/{id}/scan` | Scan the node with the nmap integration (up to 6 minutes) and rebuild the map. Returns the host found. 409 without an enabled nmap integration, 404 when the node has no address |

**Nodes** have `id`, `kind` (`device`, `unmanaged`, `segment`, `client`,
`app`, `wan`), `label`, `role`, `online`, `ip`, `mac`, `hostname`, `vendor`,
`model`, `parent_id` and `port` (where it is attached), Wi-Fi fields (`ssid`,
`band`, `signal_dbm`, `link_mbps`, `flow`), `traffic` (current rate per
interface: `rx_bps`, `tx_bps`), the identification (`type`, `os`, `brand`,
`product`, `reasons`), scan results (`open_ports`, `services`, `titles`,
`web`, `banners`), user fields (`hidden`, `icon`, `port_labels`), for devices whose
integration applies changes `changes` (their kinds) and `protected` (port →
`{reason, node}`: `omini` or `internet`, the ports whose disabling is
destructive),
`last_seen` for offline nodes, and for managed devices `device`: everything
the integration reported, in the [plugin data model](plugins.md#what-to-return).

**Edges** have `id`, `source` (the upstream end), `target`, `source_port`,
`target_port`, `kind` (`lldp`, `fdb`, `wifi`, `inferred`, `vpn`) and
`speed_mbps`.

## VLANs

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/vlans` | The network's VLANs (imported from the devices the first time, with the networks the router serves): `[{id, name, color, subnet, network: {gateway, dhcp: {enabled, from, to}, access}, devices: [{node_id, name, untagged, tagged}]}]` |
| `POST` | `/api/vlans` | Body `{id, name, color, subnet, network, confirm}`: add a VLAN to the catalog (409 when it exists); with a `network`, the router serves it first (`network.set`), and a destructive one answers 409 `destructive` until `confirm` |
| `PATCH` | `/api/vlans/{id}` | Change `name`, `color`, `subnet` or `network` (`confirm` as above); the network goes to the router first, a new name to the devices that have the VLAN. Returns `{vlan, changes}` |
| `PUT` | `/api/nodes/{id}/reservation` | Body `{ip}`: fix the device's address on the router whose network holds it (`reservation.set`) |
| `DELETE` | `/api/nodes/{id}/reservation` | Release it (`reservation.delete`) |
| `GET` | `/api/fix-ip` | Devices on DHCP without a reservation, on a router that can make one: `node_id`, `label`, `type`, `mac`, `ip`, `router`, `infra` and `proposed` (the address used instead, outside the DHCP range, when the router refuses one inside) |
| `POST` | `/api/fix-ip` | Body `{items: [{node_id, ip}]}`: one reservation each; answers each device's `{node_id, change}` or `{node_id, error}` |
| `GET` | `/api/nodes/{id}/dns-name` | The device's local DNS name: `name` (full, `''` when none), `label`, `custom`, `off`, `server` (the node that answers it), `missing` (`no_fixed_ip`, `no_server`, `no_name`) and `others` (every name the DNS servers have for its address) |
| `PUT` | `/api/nodes/{id}/dns-name` | Body `{name}`: a label, `''` for the device's own, `-` for none; applied at once (`dns.set`/`dns.delete`) |
| `GET` | `/api/nodes/{id}/actions` | What its panel can do: `wake` (`via`: `omini` or `router`, and the `router`) and `can_pause` (with `pause_router`) |
| `POST` | `/api/nodes/{id}/wake` | Send it a Wake-on-LAN packet (from Omini on its own networks, else `wol.send` on the router); kept in its changes |
| `PUT`, `DELETE` | `/api/nodes/{id}/pause` | Pause its internet on its router (`pause.set`), or give it back (`pause.delete`) |
| `GET` | `/api/nodes/{id}/backups` | Its configuration versions, newest first: `id`, `taken_at`, `format`, `size`, `by_omini` |
| `GET` | `/api/backups/{id}` | One version with its `content` (it may hold secrets the device would not leave out) |
| `POST` | `/api/integrations/{id}/backup` | Back up the integration's devices now (204) |
| `PUT` | `/api/wifi` | Body `{ssid, previous, vlan, password, enabled}`: create or change a Wi-Fi network on every controller and access point Omini manages (`wifi.set`); an empty password keeps the current one. The password is passed on, never kept nor logged |
| `DELETE` | `/api/wifi/{ssid}` | Remove it from every device that broadcasts it |
| `DELETE` | `/api/vlans/{id}` | Remove its network from the router, delete it from every device, then from the catalog; 409 when a device cannot (it names it), or `destructive` (Omini's network) until `?confirm=1` |
| `POST` | `/api/nodes/{id}/vlan-plan` | Body `{port, value}` (`value`: `{untagged, tagged}`): what putting the port in those VLANs changes: `steps` (`node_id`, `kind`, `port`, `value`, `protected` when destructive) and `notes` (`node_id`, `reason`: `router`, `unmanaged` or `unknown_port`, `vlans`) |
| `POST` | `/api/nodes/{id}/vlans` | Body `{port, value, confirm}`: apply that plan step by step; 409 like a change for a destructive step, 502 with the `changes` made when a device refuses |

## DNS and DHCP

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/dns-dhcp` | The **DNS & DHCP** tab: `dhcp` (`checks`: per interface, the `offers` with `known` and the server's `node_id`; `pools` and `leases` the routers report, with the `node_id` of the server and the `client` on the map) and `dns` (`servers`: each with its `external` and `local` test queries and its `node_id`), and `generated_at` |

## Inventory

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/inventory` | Every device ever seen: `id`, `kind`, `label`, `mac`, `ip`, `hostname`, `vendor`, `parent_id`, `port`, `alias`, `hidden`, `device_type`, `icon`, `first_seen`, `last_seen`, plus `online` and the current identification |
| `PATCH` | `/api/inventory/{id}` | Change user fields: `alias` (name), `hidden`, `device_type`, `icon` (`""` = automatic) |
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
| `GET` | `/api/internet` | Who used the internet over `range` (`15m`, `1h`, `today`, `7d`, `30d`; default `15m`), busiest first: `devices` (`node`, `label`, `on_map`, `rx`, `tx` in bytes; `node` is `ip:<address>` for a device not on the map), `total`, `since`, `seconds`, `listening` and `exporters` |
| `GET` | `/api/nodes/{id}/destinations` | Where a device went on the internet over the last `hours` (1 to 24, default 24), busiest first: `destinations` (`name`: a domain or the address, `rx`, `tx`, `ports: [{proto, port, bytes}]`) |

## Notifications

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/notifier-types` | Channel types (`webhook`, `telegram`, `email`) with their form fields |
| `GET` | `/api/notifiers` | Channels: `id`, `type`, `config` (secrets masked), `min_severity`, `notify_resolved`, `enabled`, `last_sent_at`, `last_error`, `created_at` |
| `POST` | `/api/notifiers` | Body `{type, config, min_severity, notify_resolved, enabled}`. `min_severity` is `critical`, `warning` (default) or `info` |
| `PUT` | `/api/notifiers/{id}` | Change any of those fields |
| `DELETE` | `/api/notifiers/{id}` | Delete a channel |
| `POST` | `/api/notifiers/test` | Body `{type, config}` or `{id}`: send a test message. Answers `{ok}` or `{ok: false, error}` |
| `GET` | `/api/notifier-settings` | `{public_url}`: Omini's address for the links in messages ("" when unknown) |
| `PUT` | `/api/notifier-settings` | Body `{public_url}` (`http://` or `https://`, "" to remove). Adding or testing a channel sets it from the browser's `Origin` when empty |

Channel `config` keys: webhook `url`, `format` (`json`, `slack`, `discord`,
`ntfy`), `secret`; Telegram `token`, `chat_id`; e-mail `host`, `port`,
`security` (`starttls`, `tls`, `none`), `username`, `password`, `from`, `to`.

## Other

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/api/diagnostics` | yes | The anonymized diagnostics file (Settings → About): integrations and their status, open alerts, the last round and the map with what each integration reported. MACs keep their vendor part; names, SSIDs, serials, public IPv4 and IPv6 addresses become placeholders; private IPv4 addresses stay; secrets, integration settings and backups are left out |
| `GET` | `/api/icons` | no | Names of the app icons Omini has |
| `GET` | `/api/icons/{name}` | no | One app icon (SVG), cached a day |

Any path outside `/api/` serves the web UI.
