"""The emulator's control API (127.0.0.1 only): read or move the simulated clock.

GET  /clock          → {"t", "speed", "frozen", "local": "Tue 14:05", "events": [...]}
PUT  /clock          ← {"at": epoch | "tue 14:05", "speed": x, "freeze": bool}
GET  /state?node=x   → what a node looks like now (debugging)
"""

from __future__ import annotations

import asyncio
import time
from typing import Any

from .server import Context, HttpService, Request, Response, _http_handler
from .world import Snapshot, at_local, week_index

DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]


def describe(snap: Snapshot) -> dict[str, Any]:
    day, hh, mm = snap.local()
    return {
        "t": snap.t,
        "local": f"{DAYS[day]} {hh:02d}:{mm:02d}",
        "events": sorted(snap.events),
        "master": snap.master(),
        "reboots": sorted(n for n in snap.w.down if snap.w.is_down(n, snap.t)),
    }


def parse_at(v: Any) -> float:
    """An instant: epoch seconds, "tue 14:05" (this week, local time) or "next tue 14:05"."""
    if isinstance(v, (int, float)):
        return float(v)
    text = str(v).strip().lower()
    if text.replace(".", "").isdigit():
        return float(text)
    # "tue 14:05" is this week; "next tue 14:05" the next one (plugins compare lease expiry with the real clock,
    # so tests ask for instants ahead of it).
    week = week_index(time.time())
    if text.startswith("next "):
        week, text = week + 1, text[5:]
    day, _, hm = text.partition(" ")
    hh, _, mm = hm.partition(":")
    return at_local([d.lower() for d in DAYS].index(day[:3]), int(hh), int(mm or 0), week=week)


def placements(snap: Snapshot) -> dict[str, Any]:
    """Every node now: where it plugs in (the truth the map is checked against)."""
    out = {}
    for n in snap.nodes.values():
        loc = snap.location(n.name) if snap.up(n.name) else None
        out[n.name] = {
            "kind": n.kind,
            "site": n.site,
            "mac": n.mac,
            "ip": n.ip,
            "present": snap.up(n.name),
            "at": loc[0] if loc else None,
            "port": loc[1] if loc else None,
            "private_mac": bool(n.extra.get("private_mac")),
            "unmanaged": bool(n.extra.get("unmanaged")),
        }
    return out


class Control(HttpService):
    def __init__(self, ctx: Context) -> None:
        super().__init__(ctx, "control", {"node": "omini"})

    def reachable(self, snap: Snapshot) -> bool:
        return True

    def handle(self, req: Request, snap: Snapshot) -> Response:
        clock = self.ctx.clock
        if req.path == "/clock" and req.method == "GET":
            return Response.json({**describe(snap), "speed": clock.speed, "frozen": clock.frozen is not None})
        if req.path == "/clock" and req.method == "PUT":
            body = req.json() or {}
            now = time.time()
            at = parse_at(body["at"]) if "at" in body else clock.now(now)
            speed = float(body.get("speed", clock.speed))
            clock.start, clock.real_start, clock.speed = at, now, speed
            clock.frozen = at if body.get("freeze", clock.frozen is not None) else None
            return Response.json({**describe(self.ctx.snapshot()), "speed": clock.speed})
        if req.path == "/placements":
            return Response.json(placements(snap))
        if req.path == "/state":
            name = req.arg("node") or ""
            if name not in snap.nodes:
                return Response.json({"error": "unknown node"}, 404)
            n = snap.node(name)
            return Response.json(
                {
                    "name": name,
                    "up": snap.up(name),
                    "present": snap.present(name),
                    "uptime": snap.uptime(name),
                    "ip": n.ip,
                    "mac": n.mac,
                    "location": snap.location(name),
                    "cpu": snap.cpu(name),
                    "ports": {p: vars(snap.port(name, p)) for p in n.ports},
                }
            )
        return Response.json({"error": "not found"}, 404)


async def serve_control(ctx: Context, host: str, port: int) -> asyncio.base_events.Server:
    return await asyncio.start_server(_http_handler(Control(ctx), False), host, port)
