"""Command line: `python -m emulator <command>` (run from testdata/devnet with `uv run`).

lab      run Omini and Acme's network in a network namespace (`make devnet`)
serve    serve every emulated API on 127.0.0.1 random ports (tests); writes the endpoints file
prepare  build the plugins' Python environments (fills uv's cache for the offline lab)
dump     print what the network looks like at an instant
clock    read or move the lab's simulated clock
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import os
import signal
import socket
import sys
import time
from pathlib import Path

from . import acme, configs
from .control import describe, parse_at, serve_control
from .server import Context, Hub, tls_context
from .vendors import service_class
from .world import Clock, World


def cmd_serve(args: argparse.Namespace) -> int:
    world = World()
    at = parse_at(args.at) if args.at is not None else time.time()
    # Speed 0 freezes the clock (counters still read at the real-time scale: speed 1).
    clock = Clock(start=at, speed=args.speed or 1.0, real_start=time.time(), frozen=at if args.speed == 0 else None)
    secrets = configs.load(Path(args.secrets)) if args.secrets else configs.generate()
    ctx = Context(world, clock, secrets)

    async def main() -> None:
        hub = Hub(ctx, tls_context(["localhost"], ["127.0.0.1"]))
        urls, snmp = {}, {}
        for name, spec in acme.APIS.items():
            try:
                svc = service_class(spec["kind"])(ctx, name, spec)
            except ModuleNotFoundError as e:
                print(f"devnet: {name} not emulated ({e})", file=sys.stderr)
                continue
            if spec["kind"] == "snmp":
                ip, port = await hub.udp(svc, "127.0.0.1", 0)
                snmp[name] = {
                    "host": ip,
                    "port": port,
                    **configs.snmp_credential(name, secrets),
                    "node": spec["node"],
                    "design_ip": spec["ip"],
                }
            else:
                ip, port = await hub.http(svc, "127.0.0.1", 0, spec["tls"])
                urls[name] = f"{'https' if spec['tls'] else 'http'}://{ip}:{port}"
        ctl = await serve_control(ctx, "127.0.0.1", 0)
        out = {
            "control": f"http://127.0.0.1:{ctl.sockets[0].getsockname()[1]}",
            "urls": urls,
            "snmp": snmp,
            "integrations": [
                {"api": n, "plugin": p, "config": c, "design_ip": acme.APIS[n]["ip"]}
                for n, p, c in configs.integrations(urls, secrets)
            ],
        }
        path = Path(args.out)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as f:
            json.dump(out, f)
        print("ready", flush=True)
        stop = asyncio.Event()
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, stop.set)
        if args.until_stdin_closes:
            loop.add_reader(sys.stdin.fileno(), lambda: stop.set() if not sys.stdin.buffer.read1(4096) else None)
        await stop.wait()
        hub.close()

    asyncio.run(main())
    return 0


def cmd_dump(args: argparse.Namespace) -> int:
    world = World()
    snap = world.at(parse_at(args.at) if args.at else time.time())
    if args.node:
        n = world.nodes[args.node]
        print(
            json.dumps(
                {
                    "node": vars(n) | {"ports": list(n.ports)},
                    "location": snap.location(n.name),
                    "ports": {p: vars(snap.port(n.name, p)) for p in n.ports},
                },
                default=str,
                indent=1,
            )
        )
        return 0
    kinds: dict[str, int] = {}
    for n in world.nodes.values():
        kinds[n.kind] = kinds.get(n.kind, 0) + 1
    present = sum(1 for n in world.nodes.values() if snap.present(n.name))
    print(
        json.dumps(
            {
                **describe(snap),
                "nodes": len(world.nodes),
                "present": present,
                "kinds": kinds,
                "links": len(world.d.links),
                "duplicate_ip": world.duplicate_ip,
            },
            indent=1,
        )
    )
    return 0


def cmd_clock(args: argparse.Namespace) -> int:
    sock = Path(args.data) / "devnet-control.sock"
    body = None
    if args.at or args.speed is not None:
        body = {k: v for k, v in (("at", args.at), ("speed", args.speed)) if v is not None}
    payload = json.dumps(body).encode() if body else b""
    req = (
        f"{'PUT' if body else 'GET'} /clock HTTP/1.1\r\nHost: devnet\r\nConnection: close\r\n"
        f"Content-Type: application/json\r\nContent-Length: {len(payload)}\r\n\r\n"
    ).encode() + payload
    with socket.socket(socket.AF_UNIX) as s:
        s.connect(str(sock))
        s.sendall(req)
        data = b""
        while chunk := s.recv(65536):
            data += chunk
    print(data.split(b"\r\n\r\n", 1)[1].decode())
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(prog="python -m emulator", description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("serve", help="serve the emulated APIs on 127.0.0.1 (tests)")
    s.add_argument("--at", help="simulated instant: epoch or 'tue 14:05' (this week)")
    s.add_argument("--speed", type=float, default=0, help="0 = frozen at --at (default), else the clock's speed-up")
    s.add_argument("--out", required=True, help="endpoints file to write (0600)")
    s.add_argument("--secrets", help="credentials file (default: new random ones)")
    s.add_argument("--until-stdin-closes", action="store_true")
    for name in ("lab", "inner"):
        p = sub.add_parser(name, help="run the lab (make devnet)" if name == "lab" else argparse.SUPPRESS)
        p.add_argument("--data", default="data-devnet")
        p.add_argument("--port", type=int, default=int(os.environ.get("DEVNET_PORT", "8093")))
        p.add_argument("--bind", default=os.environ.get("DEVNET_BIND", "127.0.0.1"))
        p.add_argument("--omini", default="bin/omini")
        p.add_argument("--speed", type=float)
        p.add_argument("--start", help="where the clock starts: epoch or 'mon 07:00'")
        p.add_argument("--plugins", default="")
    sub.add_parser("prepare", help="build the plugins' environments")
    d = sub.add_parser("dump", help="print the network at an instant")
    d.add_argument("--at")
    d.add_argument("--node")
    c = sub.add_parser("clock", help="read or move the lab's clock")
    c.add_argument("--data", default="data-devnet")
    c.add_argument("--at", help="epoch or 'tue 14:05'")
    c.add_argument("--speed", type=float)
    args = ap.parse_args()
    if args.cmd == "serve":
        return cmd_serve(args)
    if args.cmd in ("lab", "inner"):
        from . import lab

        return lab.outer(args) if args.cmd == "lab" else lab.inner(args)
    if args.cmd == "prepare":
        from .lab import prepare

        print("\n".join(map(str, prepare())))
        return 0
    if args.cmd == "dump":
        return cmd_dump(args)
    if args.cmd == "clock":
        return cmd_clock(args)
    return 2


if __name__ == "__main__":
    with contextlib.suppress(KeyboardInterrupt):
        sys.exit(main())
