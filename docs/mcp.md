# Use Omini from an AI assistant (MCP)

Omini has a built-in [Model Context Protocol](https://modelcontextprotocol.io)
server, so an AI assistant (Claude Code, Claude Desktop, Cursor, VS Code and
others) can answer questions about your network: "which devices are offline?",
"where is the printer plugged in?", "what is on VLAN 30?", "who used the most
internet today?".

The assistant **only reads**. No tool changes anything: a configuration change
is still made, and confirmed, by you in Omini's UI.

## 1. Create a token

1. Open **Settings → API tokens**.
2. Give the token a name that says where it will be used (for example
   "Claude on my laptop") and click **Create token**.
3. Copy the token (`omini_…`) right away: Omini keeps only its hash and shows
   it once. Lost it? Delete it and create another.

The list shows when each token was last used. **Delete** a token to cut
whatever uses it off at once.

A token opens the [HTTP API](api.md#api-tokens)'s `GET` routes too, for
scripts that read the map. Any other request with a token is refused.

## 2. Connect the assistant

The MCP server is at `<Omini's address>/mcp`, for example
`http://omini.lan:8080/mcp` (streamable HTTP). The Settings page shows the
address and the snippets below with your token filled in.

**Claude Code:**

```bash
claude mcp add --transport http omini http://omini.lan:8080/mcp \
  --header "Authorization: Bearer omini_…"
```

**Clients configured with JSON** take an entry like this (the exact keys
vary by client; see its documentation for remote HTTP servers):

```json
{
  "mcpServers": {
    "omini": {
      "type": "http",
      "url": "http://omini.lan:8080/mcp",
      "headers": { "Authorization": "Bearer omini_…" }
    }
  }
}
```

The token travels in a header: over plain HTTP it can be read on the
network. If the assistant reaches Omini from outside your LAN, put Omini
behind HTTPS (a reverse proxy).

## What the assistant can read

| Tool | What it answers |
|---|---|
| `get_map_summary` | How many devices, of which kinds, types and roles; online and offline; each integration's status; open alerts by severity |
| `search_devices` | Devices by name, alias, IP, MAC, vendor, hostname or [note](map.md#the-device-panel) |
| `get_device` | One device: identity, your note, IPs and MACs, where it is connected, its ports, health and open alerts |
| `list_alerts` | The open alerts, worst first, with their text |
| `list_networks` | VLANs and subnets, the router serving each one (gateway, DHCP range), the devices carrying it, its Wi-Fi networks |
| `get_dns_dhcp` | DNS servers and their checks, DHCP checks, pools and leases |
| `get_internet_usage` | Who used the internet over 15 minutes, an hour, today, 7 or 30 days (needs [traffic flows](discovery.md#traffic-flows)) |

Each tool's arguments and results are in the [API reference](api.md#mcp-server).
