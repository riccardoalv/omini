"""Sets up a devnet instance of Omini through its HTTP API (run by `make devnet`).

Waits for the server, creates the admin user on first run (or signs in), then
adds one integration per devnet plugin that is not added yet. The admin's
credentials are generated test values kept in <data>/devnet-admin.json (the
data folder is gitignored); they are never printed.
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

HERE = Path(__file__).resolve().parent


class API:
    def __init__(self, base: str) -> None:
        self.base = base.rstrip("/")
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
        )

    def call(self, method: str, path: str, body: object | None = None) -> tuple[int, object]:
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        req.add_header("X-Omini-Request", "1")  # the API's CSRF guard
        try:
            with self.opener.open(req, timeout=10) as resp:
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


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--url", default="http://127.0.0.1:8093")
    ap.add_argument("--data", default="data-devnet", help="Omini's data folder")
    args = ap.parse_args()
    data = Path(args.data)
    api = API(args.url)
    wait_up(api)

    creds = credentials(data)
    status, body = api.call("GET", "/api/auth/status")
    if isinstance(body, dict) and body.get("setup_required"):
        status, body = api.call("POST", "/api/auth/setup", creds)
    else:
        status, body = api.call("POST", "/api/auth/login", creds)
    if status != 200:
        sys.exit(
            f"devnet: could not sign in ({status}: {body}); "
            f"delete {data} to start over with new credentials"
        )

    plugins = sorted(p.name for p in HERE.iterdir() if (p / "plugin.yaml").is_file())
    _, existing = api.call("GET", "/api/integrations")
    have = {i["type"] for i in existing} if isinstance(existing, list) else set()
    added = 0
    for plugin in plugins:
        if plugin in have:
            continue
        status, body = api.call("POST", "/api/integrations", {"type": plugin, "config": {}})
        if status != 201:
            sys.exit(f"devnet: could not add {plugin} ({status}: {body})")
        added += 1
    print(
        f"devnet: {args.url} is up with {len(plugins)} simulated integrations "
        f"({added} added now). Sign in with the credentials in {data / 'devnet-admin.json'}."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
