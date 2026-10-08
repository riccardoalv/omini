"""Sets up a devnet instance of Omini through its HTTP API (run by the lab, see README.md).

Waits for the server, creates the admin user on first run (or signs in), then
adds the integrations Acme needs, each pointed at an emulated device on its
real address: the built-in network scan (with Acme's SNMP communities and v3
user) and one integration per emulated API (two OPNsense firewalls, Proxmox,
UniFi, two MikroTik, Omada, OpenWrt, pfSense, Horaco, Mercusys). The admin's
credentials are generated test values kept in <data>/devnet-admin.json and the
devices' in <data>/devnet-secrets.json (the data folder is gitignored); they
are never printed.
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

from emulator import acme, configs


class API:
    def __init__(self, base: str) -> None:
        self.base = base.rstrip("/")
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, method: str, path: str, body: object | None = None) -> tuple[int, object]:
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        req.add_header("X-Omini-Request", "1")  # the API's CSRF guard
        try:
            with self.opener.open(req, timeout=30) as resp:
                return resp.status, json.loads(resp.read() or b"null")
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read() or b"null")


def credentials(data: Path) -> dict[str, str]:
    path = data / "devnet-admin.json"
    if path.exists():
        return json.loads(path.read_text())
    creds = {"username": "devnet", "password": secrets.token_urlsafe(18)}
    data.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(creds, f)
    return creds


def wait_up(api: API, seconds: float = 120) -> None:
    deadline = time.monotonic() + seconds
    while True:
        try:
            if api.call("GET", "/api/health")[0] == 200:
                return
        except OSError:
            pass
        if time.monotonic() > deadline:
            sys.exit(f"devnet: Omini did not start at {api.base}")
        time.sleep(0.5)


def lab_urls() -> dict[str, str]:
    """Each emulated API on its real address (inside the lab's network namespace)."""
    out = {}
    for name, spec in acme.APIS.items():
        if spec["kind"] == "snmp":
            continue
        scheme = "https" if spec["tls"] else "http"
        default = 443 if spec["tls"] else 80
        port = "" if spec["port"] == default else f":{spec['port']}"
        out[name] = f"{scheme}://{spec['ip']}{port}"
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--url", default="http://127.0.0.1:8093")
    ap.add_argument("--data", default="data-devnet", help="Omini's data folder")
    ap.add_argument("--lab", action="store_true", help="also add the network scan (only inside the lab)")
    args = ap.parse_args()
    data = Path(args.data)
    api = API(args.url)
    wait_up(api)

    creds = credentials(data)
    _, body = api.call("GET", "/api/auth/status")
    if isinstance(body, dict) and body.get("setup_required"):
        status, body = api.call("POST", "/api/auth/setup", creds)
    else:
        status, body = api.call("POST", "/api/auth/login", creds)
    if status != 200:
        sys.exit(f"devnet: could not sign in ({status}); delete {data} to start over with new credentials")

    sec = configs.load(data / "devnet-secrets.json")
    want = [(p, c) for _, p, c in configs.integrations(lab_urls(), sec)]
    if args.lab:
        want.insert(0, ("network", configs.scan_config(sec)))
    _, existing = api.call("GET", "/api/integrations")
    have = set()
    for i in existing if isinstance(existing, list) else []:
        cfg = i.get("config") or {}
        have.add((i["type"], cfg.get("url") or cfg.get("host") or ""))
    added = 0
    for plugin, cfg in want:
        key = (plugin, cfg.get("url") or cfg.get("host") or "")
        if key in have:
            continue
        status, body = api.call("POST", "/api/integrations", {"type": plugin, "config": cfg})
        if status != 201:
            sys.exit(f"devnet: could not add {plugin} {key[1]} ({status}: {body})")
        added += 1
    print(
        f"devnet: {len(want)} integrations ({added} added now). "
        f"Sign in with the credentials in {data / 'devnet-admin.json'}.",
        flush=True,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
