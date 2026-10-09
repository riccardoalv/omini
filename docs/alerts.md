# Alerts and notifications

After every collection round, Omini checks the map against a set of rules.
What a rule finds becomes an **alert**: it opens when the problem appears and
resolves by itself when the problem is gone. Alerts are shown in Omini and can
be sent to Telegram, e-mail, Slack, Discord, ntfy or any webhook.

- [Alert rules](#alert-rules)
- [How an alert lives](#how-an-alert-lives)
- [The Alerts screen](#the-alerts-screen)
- [Popups and marks](#popups-and-marks)
- [Notifications](#notifications)
- [Webhook payload](#webhook-payload)

## Alert rules

Severities: **critical** (something is down), **warning** (something needs
attention) and **info** (worth knowing).

### Availability

| Rule | Severity | Opens when |
|---|---|---|
| `device_offline` | critical | A device read by an integration is offline: its integration's last collection failed. The integration's error is shown |
| `integration_failed` | critical | An integration fails and none of its devices is on the map yet |
| `wan_down` | critical | A WAN gateway is down |
| `wan_degraded` | warning | A WAN gateway is degraded (loss or latency, as the router reports it) |
| `device_rebooted` | warning | A device's uptime went back: it restarted. Stays open for 1 hour |
| `discovery_limited` | warning | The network scan is set up but cannot do everything where Omini runs (no host network, no multicast, no ping). See [What each method needs](discovery.md#what-each-method-needs) |

### Health

| Rule | Severity | Opens when |
|---|---|---|
| `high_cpu` | warning above 80 %, critical from 95 % | CPU use of an online device |
| `high_memory` | warning from 90 % | Memory use of an online device |
| `hot_cpu` | warning from 70 °C, critical from 85 °C | The hottest CPU temperature sensor |
| `disk_full` | warning from 80 %, critical from 90 % | Use of a disk or filesystem |
| `update_pending` | warning | The device knows of a pending update (from its own last check; Omini never starts one) |
| `dhcp_pool_full` | warning from 90 % | Addresses in use in a DHCP pool |
| `firewall_states_full` | warning from 90 % | Entries in the firewall's state table against its limit |

### Links and ports

| Rule | Severity | Opens when |
|---|---|---|
| `slow_uplink` | warning | A wired link between two network devices (switches, access points, routers, segments) runs below 1 Gbps |
| `fast_ethernet` | warning | A wired link to an end device runs at 100 Mbps or less (a Fast Ethernet port, a cable with a broken pair, a forced speed). Not for devices made with a 100 Mbps port: cameras, UPSes, air conditioning, badge readers and other smart home gear, appliances, solar inverters, desk phones |
| `saturated_link` | warning | A physical port uses more than 80 % of its speed, in either direction, averaged over the last collection |
| `interface_errors` | warning | A port's receive or transmit errors grew since the last collection |
| `link_flapping` | warning | A port went up or down 3 or more times in the last hour |
| `half_duplex` | warning | A port that is up negotiated half duplex |
| `sfp_low_rx` | warning | An SFP module receives less than its low alarm threshold, or less than -20 dBm when it has none |
| `weak_wifi` | warning | A Wi-Fi client's signal is below -75 dBm |

### Network and security

| Rule | Severity | Opens when |
|---|---|---|
| `duplicate_ip` | warning | Two online devices with different MAC addresses use the same IP |
| `insecure_service` | warning | Telnet (23) or FTP (21) is open on a device, or a network device's admin page is served only over plain HTTP (no 443 or 8443) |
| `new_devices_burst` | warning | 5 or more devices never seen before appeared within 10 minutes |
| `new_device` | info | A device is seen for the first time. Shown for 24 hours. Devices found in the first 15 minutes after Omini started tracking are the existing network, not new |
| `integration_available` | warning | Omini identified a device (by product, brand or OS) that an integration in the store can read in full, and no integration of that type is set up |
| `unmanaged_switch` | info | Three or more devices sit behind one switch port where nothing is integrated: probably a small switch, an access point or a hypervisor |
| `unknown_neighbor` | info | A device announces itself by LLDP but no integration reads it |

Rules only look at what the integrations report: a rule about temperatures,
for example, needs an integration that reads them (an SNMP profile, the
OPNsense plugin...).

## How an alert lives

Each alert has a stable **key**: the rule plus its subject (a device, a port,
a gateway). On every round:

- a new key **opens** an alert,
- a known key **refreshes** it (its severity and values may change),
- an open alert whose key is gone is **resolved**.

Resolved alerts are kept for 90 days. Alert texts are translated in the UI, so
switching language changes them too.

**Dismissing** an alert hides it until it resolves. If it comes back later,
it is a new alert and shows again. "Show again" undoes a dismissal.

## The Alerts screen

**Alerts** in the sidebar has two tabs.

**Alerts**

- open alerts, most severe first, each with what happened, a tip on what to
  do, since when, and **Show on the map** (opens the device's panel and
  centers it),
- a severity filter (all, critical, warning, info),
- **Show dismissed**,
- **Resolved in the last 7 days**.

**Timeline**

- every device that joined or left the network, grouped by day,
- **New devices only**: first sightings,
- **Load more** for older events (kept a year).

A device that disappears gets its "left" event (at the time it was last seen)
only after it has been missing for 5 minutes or three collection rounds
(the interval set in Settings), whichever is longer, so a phone that skips one round does not flap.

## Popups and marks

- **Popups.** Open critical and warning alerts pop up in a corner of every
  screen except Alerts: up to three at a time, then "N more alerts". Each has
  the tip, **Show on the map** and a close button. A closed popup stays closed
  (per browser) until its alert resolves and opens again.
- **Menu badge.** The Alerts entry in the sidebar counts open critical and
  warning alerts that are not dismissed.
- **On the map.** The "N alerts" chip in the top bar, and a red or yellow mark
  on each device with an open alert. The device panel lists its alerts.

## Notifications

Set up channels in **Settings → Notifications**. Each way of being told has
step-by-step instructions in the UI; add as many channels as you like, of any
kind.

| Channel | You need |
|---|---|
| **Telegram** | A bot token (talk to `@BotFather`, send `/newbot`) and the chat ID: send your bot a message, open `https://api.telegram.org/bot<token>/getUpdates` and copy the `id` inside `chat` (groups start with `-`) |
| **E-mail** | An SMTP server: host, port (587 by default), security (STARTTLS, TLS or none), optional username and password, From and To (several addresses separated by commas). Gmail and Outlook need an app password |
| **Slack** | An incoming webhook URL (`https://hooks.slack.com/services/...`) |
| **Discord** | A channel webhook URL (channel settings → Integrations → Webhooks) |
| **ntfy** | A topic URL, `https://ntfy.sh/<hard-to-guess-topic>` or your own server's |
| **Webhook** | Any URL that accepts a JSON `POST`: Home Assistant, n8n, Node-RED, your own script |

Slack, Discord and ntfy are webhooks with their own format. A Slack, Discord
or ntfy.sh address saved with the default `json` format gets its service's
format automatically.

Each channel has:

- **Send alerts from**: critical only, warning and critical (the default), or
  everything.
- **Also tell when an alert is resolved** (on by default).
- An on/off switch, **Send a test**, and when it last sent (or its last error).

Secrets (bot tokens, SMTP passwords, signing secrets) are encrypted in the
database and never shown again.

### What is sent

- **One message per channel per round.** All the alerts a round opened and
  resolved go together, most severe first.
- **A card per device.** Each card names the device and lists what
  happened to it, one line per alert ("Memory almost full: Memory at 98.3%").
  Under the alerts come the device's details as Omini knows them: type, make
  and model, IP and MAC address, where it is connected (switch and port, or
  access point and Wi-Fi network), its system, and since when it is offline.
  The card ends with **What to do** (the same tip as on the Alerts screen) and
  an **Open in Omini** link to the device on the map. The new devices go
  together on one card (name, make, address, where), and so do the resolved
  alerts. The subject sums it up: `Omini: 1 critical, 3 warnings, 2 new
  devices`, or `Omini: <the alert>` when there is only one.
- **Formats.** Each service shows the cards its own way, colored by severity,
  without emoji:
  - **Discord**: an embed per card (at most 10), with the details as fields,
    "Offline since" in the reader's time zone, and the title linking to the
    device;
  - **Slack**: an attachment per card, with the details as fields;
  - **Telegram**: formatted text (bold names, addresses in monospace, a link
    per device);
  - **E-mail**: an HTML message with a card per device and a button to open
    it, plus a plain-text version;
  - **ntfy**: Markdown, a priority and a tag by the worst severity; tapping
    the notification opens the device (or the map when there are several).
- **Links.** The links go to **Omini's address** (Settings → Notifications).
  Omini takes it from your browser the first time you add or test a channel.
  Set it yourself when you open Omini by another name (a domain, a reverse
  proxy). Without it, messages have no links.
- **Send a test** sends an example: a device with two alerts and a new
  device, so you see what the alerts will look like. It is marked as a test
  and carries no events.
- **Language.** Messages are written in the admin's language.
- **Cooldown.** An alert that opens again within 30 minutes of its last
  notification is not sent again, nor is its resolution: a flapping link sends
  one message, not dozens. The resolution of a dismissed alert is not sent.

Delivery runs in the background with a 15-second timeout per channel; a
failed delivery is logged and shown on the channel.

## Webhook payload

With the `json` format, Omini sends a `POST` with
`Content-Type: application/json` and `User-Agent: Omini`:

```json
{
  "subject": "Omini: 1 warning, 1 new device",
  "text": "WARNING · nas-01\n  - Memory almost full: Memory at 94.2%\n  Type: NAS / storage\n  ...",
  "events": [
    {
      "opened": true,
      "alert": {
        "id": 412,
        "key": "high_memory|dev:00:11:32:4a:5b:6c",
        "rule": "high_memory",
        "severity": "warning",
        "node_id": "dev:00:11:32:4a:5b:6c",
        "params": { "node": "nas-01", "pct": 94.2 },
        "opened_at": "2026-10-08T14:02:11Z",
        "updated_at": "2026-10-08T14:02:11Z",
        "dismissed": false
      },
      "title": "Memory almost full on nas-01",
      "detail": "Memory at 94.2%."
    },
    {
      "opened": true,
      "alert": {
        "id": 413,
        "key": "new_device|mac:aa:bb:cc:dd:ee:ff",
        "rule": "new_device",
        "severity": "info",
        "node_id": "mac:aa:bb:cc:dd:ee:ff",
        "params": {
          "node": "Galaxy-S23",
          "first_seen": "2026-10-08T14:01:40Z",
          "mac": "aa:bb:cc:dd:ee:ff",
          "ip": "192.168.1.57",
          "vendor": "Samsung"
        },
        "opened_at": "2026-10-08T14:02:11Z",
        "updated_at": "2026-10-08T14:02:11Z",
        "dismissed": false
      },
      "title": "New device: Galaxy-S23",
      "detail": "Samsung aa:bb:cc:dd:ee:ff 192.168.1.57"
    }
  ],
  "groups": [
    {
      "severity": "warning",
      "title": "nas-01",
      "lines": ["Memory almost full: Memory at 94.2%"],
      "items": [{ "severity": "warning", "label": "Memory almost full", "detail": "Memory at 94.2%." }],
      "device": {
        "id": "dev:00:11:32:4a:5b:6c",
        "name": "nas-01",
        "type": "NAS / storage",
        "vendor": "Synology",
        "model": "DS920+",
        "ip": "192.168.1.20",
        "mac": "00:11:32:4a:5b:6c",
        "connected_to": "core-sw1 · Port 4",
        "online": true,
        "url": "http://omini.lan:8080/?node=dev%3A00%3A11%3A32%3A4a%3A5b%3A6c"
      },
      "tip": "See what uses the memory; a cache that frees itself is not a problem.",
      "url": "http://omini.lan:8080/?node=dev%3A00%3A11%3A32%3A4a%3A5b%3A6c"
    },
    {
      "severity": "info",
      "title": "1 new device",
      "lines": ["Galaxy-S23 · Samsung · 192.168.1.57 · ap-lobby · Home 5 GHz"],
      "tip": "If you do not know it, look at its vendor and where it is connected."
    }
  ],
  "url": "http://omini.lan:8080",
  "at": "2026-10-08T14:02:11Z",
  "locale": "en"
}
```

| Field | Meaning |
|---|---|
| `subject`, `text` | The message as plain text, in the admin's language |
| `events[]` | One per alert opened (`opened: true`) or resolved (`opened: false`) |
| `events[].alert.rule` | The rule (see [Alert rules](#alert-rules)); `params` holds its values (`node` is the device's name) |
| `events[].alert.key` | Stable across rounds: the same problem keeps the same key |
| `events[].alert.resolved_at` | Set on resolved alerts |
| `groups[]` | The message as cards; `severity` is `critical`, `warning`, `info` or `resolved` |
| `groups[].items[]` | The card's alerts: what happened (`label`) and the numbers (`detail`) |
| `groups[].device` | The device as Omini knows it (type, make, model, addresses, `connected_to`, `last_seen` when offline); absent when the map does not have it |
| `groups[].tip`, `groups[].url` | What to do, and the device on Omini's map (with Omini's address set) |
| `url`, `at` | Omini's map, and when the round ended |
| `test` | `true` on the example sent by "Send a test" (which has no `events`) |

Node ids (`node_id`) are Omini's internal ids; use them with
`/?node=<id>` to link to the device on the map.

### Checking the signature

With a **signing secret** set, each request carries
`X-Omini-Signature: sha256=<hex>`: the HMAC-SHA256 of the raw body with the
secret as key. Verify it before trusting the payload:

```python
import hashlib, hmac

def valid(body: bytes, header: str, secret: str) -> bool:
    expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, header)
```
