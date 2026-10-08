"""The lab: Omini and every emulated device in a rootless network namespace.

`python -m emulator lab` (what `make devnet` runs) starts a forwarder on the
host (127.0.0.1:8093 → a Unix socket) and re-runs itself as `inner` under
`unshare --user --map-root-user --net`. Inside, it builds Acme's addressing
for real: a dummy `eth0` with Omini's address in the MGMT VLAN
(10.10.10.250/24, default route via the firewalls' CARP address 10.10.10.1),
every emulated host's address as a /32 on `lo` (only while the host is on the
network), permanent ARP entries for the MGMT VLAN (Omini's own segment), and
`unreachable` routes for everything else. The emulated APIs, SNMP agents and
the services the network scan probes listen on their real addresses and
ports; then Omini starts with the real plugins and the built-in network scan.

Nothing leaves the namespace: plugins' Python environments are built offline
from uv's cache, which `prepare` fills beforehand.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import logging
import os
import signal
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

from . import acme, configs, testing
from .server import Context, Hub, tls_context
from .vendors import service_class
from .world import STEP, Clock, World, at_local, week_index

log = logging.getLogger("devnet")

OMINI_IP = "10.10.10.250"
GATEWAY_VIP = "10.10.10.1"
MGMT = "10.10.10."
UNREACHABLE = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "198.51.100.0/24", "203.0.113.0/24"]


# ------------------------------------------------------------------ the clock


def load_clock(data: Path, speed: float | None, start: str | None) -> Clock:
    """The simulated clock, persisted so a restart continues where it stopped."""
    path = data / "devnet-clock.json"
    now = time.time()
    if speed is None:
        speed = float(os.environ.get("DEVNET_SPEED", "60"))
    if start is None:
        start = os.environ.get("DEVNET_START")
    saved = json.loads(path.read_text()) if path.exists() else None
    if start:
        from .control import parse_at

        sim = parse_at(start)
    elif saved:
        sim = saved["sim_start"] + (now - saved["real_start"]) * saved["speed"]
    else:
        # Next Monday 07:00 (people arriving): ahead of the real clock, which some plugins compare with
        # (OPNsense drops DHCP leases that expired before the real now).
        sim = at_local(0, 7, 0, week=week_index(now) + 1)
    clock = Clock(start=sim, speed=speed, real_start=now)
    path.write_text(json.dumps({"sim_start": sim, "real_start": now, "speed": speed}))
    return clock


# -------------------------------------------------------------- forwarding


async def _pipe(r: asyncio.StreamReader, w: asyncio.StreamWriter) -> None:
    try:
        while data := await r.read(65536):
            w.write(data)
            await w.drain()
    except (ConnectionError, OSError):
        pass
    finally:
        with contextlib.suppress(Exception):
            w.close()


async def forward_tcp_to_unix(host: str, port: int, sock: Path) -> asyncio.base_events.Server:
    async def handle(r: asyncio.StreamReader, w: asyncio.StreamWriter) -> None:
        try:
            ur, uw = await asyncio.open_unix_connection(str(sock))
        except OSError:
            w.close()
            return
        await asyncio.gather(_pipe(r, uw), _pipe(ur, w))

    return await asyncio.start_server(handle, host, port, reuse_address=True)


async def forward_unix_to_tcp(sock: Path, host: str, port: int) -> asyncio.base_events.Server:
    with contextlib.suppress(FileNotFoundError):
        sock.unlink()

    async def handle(r: asyncio.StreamReader, w: asyncio.StreamWriter) -> None:
        try:
            tr, tw = await asyncio.open_connection(host, port)
        except OSError:
            w.close()
            return
        await asyncio.gather(_pipe(r, tw), _pipe(tr, w))

    return await asyncio.start_unix_server(handle, str(sock))


# ------------------------------------------------------------------ outside


def prepare() -> list[Path]:
    """Build each plugin's environment once outside the namespace, so uv's cache has every package."""
    dirs = []
    for plugin in sorted(set(configs.PLUGIN_OF.values())):
        d = testing.plugin_dir(plugin)
        if d is None:
            print(
                f"devnet: omini-plugin-{plugin} not found next to this repository (set OMINI_DEVNET_PLUGINS)",
                file=sys.stderr,
            )
            continue
        testing.plugin_python(plugin)
        dirs.append(d)
    return dirs


def outer(args: Any) -> int:
    data = Path(args.data).resolve()
    data.mkdir(parents=True, exist_ok=True)
    plugin_dirs = prepare()
    configs.load(data / "devnet-secrets.json")
    ui_sock, ctl_sock = data / "devnet-ui.sock", data / "devnet-control.sock"
    cmd = [
        "unshare",
        "--user",
        "--map-root-user",
        "--net",
        "--",
        sys.executable,
        "-m",
        "emulator",
        "inner",
        "--data",
        str(data),
        "--port",
        str(args.port),
        "--omini",
        str(Path(args.omini).resolve()),
        "--plugins",
        ",".join(map(str, plugin_dirs)),
    ]
    if args.speed is not None:
        cmd += ["--speed", str(args.speed)]
    if args.start:
        cmd += ["--start", args.start]

    async def main() -> int:
        server = await forward_tcp_to_unix(args.bind, args.port, ui_sock)
        child = await asyncio.create_subprocess_exec(*cmd)
        loop = asyncio.get_running_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, child.terminate)
        rc = await child.wait()
        server.close()
        return rc

    try:
        return asyncio.run(main())
    finally:
        for s in (ui_sock, ctl_sock):
            with contextlib.suppress(FileNotFoundError):
                s.unlink()


# ------------------------------------------------------------------- inside


def ip_batch(lines: list[str]) -> None:
    if not lines:
        return
    proc = subprocess.run(
        ["ip", "-force", "-batch", "-"], input="\n".join(lines) + "\n", text=True, capture_output=True, check=False
    )
    if proc.returncode and "File exists" not in proc.stderr and "Cannot assign" not in proc.stderr:
        log.debug("ip: %s", proc.stderr.strip())


def sysctl(name: str, value: str) -> None:
    Path("/proc/sys/" + name.replace(".", "/")).write_text(value)


class Addresses:
    """Keeps every present host's addresses on lo (and the MGMT VLAN in the ARP table)."""

    def __init__(self, world: World) -> None:
        self.world = world
        self.current: set[str] = set()

    def wanted(self, snap) -> dict[str, str | None]:
        """ip → MAC for the MGMT ARP table (None outside MGMT)."""
        out: dict[str, str | None] = {}
        for n in self.world.nodes.values():
            if not snap.up(n.name):
                continue
            ips = {n.ip} if n.ip else set()
            for p in n.ports.values():
                ips |= {a.split("/")[0] for a in p.ips}
            for ip in ips:
                if not ip or ip.startswith("127.") or ip == OMINI_IP:
                    continue
                mac = None
                if ip.startswith(MGMT):
                    port_mac = next(
                        (p.mac for p in n.ports.values() if any(a.startswith(ip + "/") for a in p.ips)), None
                    )
                    mac = port_mac or n.mac
                out[ip] = mac
        # The firewalls' CARP addresses (.1 of each routed HQ VLAN) answer from the master.
        if snap.up(snap.master()):
            for (site, _vid), v in self.world.d.vlans.items():
                if site == "hq" and v.gateway:
                    out[v.gateway] = acme.carp_mac(v.id) if v.gateway.startswith(MGMT) else None
        return out

    def sync(self, snap) -> None:
        want = self.wanted(snap)
        lines = []
        for ip in sorted(set(want) - self.current):
            lines.append(f"address add {ip}/32 dev lo")
            if want[ip]:
                lines.append(f"neighbor replace {ip} lladdr {want[ip]} dev eth0 nud permanent")
        for ip in sorted(self.current - set(want)):
            lines.append(f"address del {ip}/32 dev lo")
            if ip.startswith(MGMT):
                lines.append(f"neighbor del {ip} dev eth0")
        ip_batch(lines)
        self.current = set(want)


def setup_namespace(world: World) -> None:
    omini = world.nodes["omini"]
    ip_batch(
        [
            "link set lo up",
            "link add eth0 type dummy",
            f"link set eth0 address {omini.mac}",
            "link set eth0 multicast on",  # like a real NIC: mDNS and SSDP can join their groups
            f"address add {OMINI_IP}/24 dev eth0",
            "link set eth0 up",
            f"route add default via {GATEWAY_VIP} dev eth0",
            *(f"route add unreachable {net}" for net in UNREACHABLE),
        ]
    )
    sysctl("net.ipv4.ping_group_range", "0 0")
    sysctl("net.ipv4.ip_nonlocal_bind", "1")


async def start_services(hub: Hub, ctx: Context) -> None:
    taken: set[tuple[str, int, str]] = set()
    for name, spec in acme.APIS.items():
        svc = service_class(spec["kind"])(ctx, name, spec)
        if spec["kind"] == "snmp":
            await hub.udp(svc, spec["ip"], spec["port"])
            taken.add((spec["ip"], spec["port"], "udp"))
        else:
            await hub.http(svc, spec["ip"], spec["port"], spec["tls"])
            taken.add((spec["ip"], spec["port"], "tcp"))
    try:
        from . import scan
    except ImportError:
        log.warning("devnet: no scan services (emulator/scan.py missing)")
        return
    await scan.start(hub, ctx, taken)


def inner(args: Any) -> int:
    logging.basicConfig(level=os.environ.get("DEVNET_LOG", "WARNING"), format="devnet: %(message)s")
    data = Path(args.data)
    world = World()
    clock = load_clock(data, args.speed, args.start)
    secrets = configs.load(data / "devnet-secrets.json")
    ctx = Context(world, clock, secrets)
    setup_namespace(world)
    addrs = Addresses(world)
    addrs.sync(ctx.snapshot())

    async def main() -> int:
        tls = tls_context(
            ["localhost", "*.acme.local"], [n.ip for n in world.nodes.values() if n.ip] + ["127.0.0.1"], data / "tls"
        )
        hub = Hub(ctx, tls)
        await start_services(hub, ctx)
        from .control import serve_control

        ctl = await serve_control(ctx, "127.0.0.1", 0)
        ctl_port = ctl.sockets[0].getsockname()[1]
        await forward_unix_to_tcp(data / "devnet-control.sock", "127.0.0.1", ctl_port)
        await forward_unix_to_tcp(data / "devnet-ui.sock", "127.0.0.1", args.port)
        env = {k: v for k, v in os.environ.items() if not k.startswith("OMINI_")}
        env.update(
            {
                "OMINI_ADDR": f"127.0.0.1:{args.port}",
                "OMINI_DATA_DIR": str(data),
                "OMINI_AUTOSCAN": "false",
                "OMINI_PLUGIN_INDEX": "off",
                "OMINI_PLUGIN_DIRS": args.plugins,
                "OMINI_POLL_INTERVAL": os.environ.get("DEVNET_POLL", "15s"),
                "UV_OFFLINE": "1",
            }
        )
        omini = await asyncio.create_subprocess_exec(args.omini, env=env, cwd=os.getcwd())
        loop = asyncio.get_running_loop()
        stop = asyncio.Event()
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, stop.set)

        async def bootstrap() -> None:
            here = Path(__file__).resolve().parent.parent
            proc = await asyncio.create_subprocess_exec(
                sys.executable,
                str(here / "bootstrap.py"),
                "--url",
                f"http://127.0.0.1:{args.port}",
                "--data",
                str(data),
                "--lab",
            )
            await proc.wait()
            snap = ctx.snapshot()
            day, hh, mm = snap.local()
            print(
                f"devnet: Acme's network at {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'][day]} {hh:02d}:{mm:02d} "
                f"(simulated, x{clock.speed:g}) on http://{args.bind if hasattr(args, 'bind') else '127.0.0.1'}:"
                f"{args.port}",
                flush=True,
            )

        boot = asyncio.create_task(bootstrap())
        while not stop.is_set() and omini.returncode is None:
            addrs.sync(ctx.snapshot())
            with contextlib.suppress(asyncio.TimeoutError):
                await asyncio.wait_for(stop.wait(), timeout=min(2.0, STEP / max(clock.speed, 1) / 2))
        boot.cancel()
        if omini.returncode is None:
            omini.terminate()
            with contextlib.suppress(asyncio.TimeoutError):
                await asyncio.wait_for(omini.wait(), 10)
        hub.close()
        return omini.returncode or 0

    return asyncio.run(main())
