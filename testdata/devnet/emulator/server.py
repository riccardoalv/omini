"""A small asyncio HTTP(S) and UDP server for the emulated device APIs.

Vendor emulators subclass `HttpService` (or `UdpService`) and answer one
request at a time from a `Snapshot` of the world: no I/O of their own, no
threads. `Hub` binds every service to its address and runs them together.
"""

from __future__ import annotations

import asyncio
import contextlib
import datetime
import ipaddress
import json
import logging
import ssl
import tempfile
import time
import urllib.parse
from dataclasses import dataclass, field
from http.cookies import SimpleCookie
from pathlib import Path
from typing import Any

from .world import Clock, Snapshot, World

log = logging.getLogger("devnet")

REASONS = {
    200: "OK",
    201: "Created",
    204: "No Content",
    301: "Moved Permanently",
    302: "Found",
    400: "Bad Request",
    401: "Unauthorized",
    403: "Forbidden",
    404: "Not Found",
    405: "Method Not Allowed",
    500: "Internal Server Error",
    424: "Failed Dependency",
    429: "Too Many Requests",
    501: "Not Implemented",
    503: "Service Unavailable",
}


class Drop(Exception):
    """Raised (or returned) to close the connection without answering: the device is unreachable."""


DROP = Drop()


@dataclass
class Request:
    method: str
    path: str  # without the query string, percent-decoded
    raw_path: str  # as sent (with the query string)
    query: dict[str, list[str]]
    headers: dict[str, str]  # lower-case names
    body: bytes
    client: tuple[str, int]
    local: tuple[str, int]
    tls: bool = False

    def json(self) -> Any:
        return json.loads(self.body or b"null")

    def form(self) -> dict[str, str]:
        return {
            k: v[0]
            for k, v in urllib.parse.parse_qs(self.body.decode(errors="replace"), keep_blank_values=True).items()
        }

    def arg(self, name: str, default: str | None = None) -> str | None:
        v = self.query.get(name)
        return v[0] if v else default

    @property
    def cookies(self) -> dict[str, str]:
        c = SimpleCookie()
        try:
            c.load(self.headers.get("cookie", ""))
        except Exception:
            return {}
        return {k: m.value for k, m in c.items()}


@dataclass
class Response:
    status: int = 200
    body: bytes = b""
    headers: list[tuple[str, str]] = field(default_factory=list)

    @classmethod
    def json(cls, obj: Any, status: int = 200, headers: list[tuple[str, str]] | None = None) -> Response:
        return cls(status, json.dumps(obj).encode(), [("Content-Type", "application/json"), *(headers or [])])

    @classmethod
    def text(
        cls,
        text: str,
        status: int = 200,
        ctype: str = "text/html; charset=utf-8",
        headers: list[tuple[str, str]] | None = None,
    ) -> Response:
        return cls(status, text.encode(), [("Content-Type", ctype), *(headers or [])])

    @classmethod
    def html_title(cls, title: str, body: str = "", status: int = 200) -> Response:
        return cls.text(
            f'<!DOCTYPE html><html><head><meta charset="utf-8"><title>{title}</title></head><body>{body}</body></html>',
            status,
        )


class Context:
    """What every service shares: the world, the clock and the generated credentials."""

    def __init__(self, world: World, clock: Clock, secrets: dict[str, Any]) -> None:
        self.world = world
        self.clock = clock
        self.secrets = secrets

    def now(self) -> float:
        return self.clock.now(time.time())

    def snapshot(self) -> Snapshot:
        return self.world.at(self.now(), self.clock.speed)


class Service:
    """One emulated API (an entry of acme.APIS)."""

    def __init__(self, ctx: Context, name: str, spec: dict[str, Any]) -> None:
        self.ctx = ctx
        self.name = name
        self.spec = spec
        self.secrets: dict[str, Any] = ctx.secrets.get(name, {})

    @property
    def node(self) -> str:
        return self.spec["node"]

    def reachable(self, snap: Snapshot) -> bool:
        return snap.reachable(self.name)


class HttpService(Service):
    def handle(self, req: Request, snap: Snapshot) -> Response:
        raise NotImplementedError


class UdpService(Service):
    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        raise NotImplementedError


# ---------------------------------------------------------------------- HTTP


async def _read_request(reader: asyncio.StreamReader) -> tuple[str, str, dict[str, str], bytes] | None:
    line = await reader.readline()
    if not line:
        return None
    try:
        method, target, _version = line.decode("latin-1").rstrip("\r\n").split(" ", 2)
    except ValueError:
        return None
    headers: dict[str, str] = {}
    while True:
        h = await reader.readline()
        if h in (b"\r\n", b"\n", b""):
            break
        k, _, v = h.decode("latin-1").partition(":")
        headers[k.strip().lower()] = v.strip()
    body = b""
    if headers.get("transfer-encoding", "").lower() == "chunked":
        chunks = []
        while True:
            size = int((await reader.readline()).split(b";")[0].strip() or b"0", 16)
            if size == 0:
                await reader.readline()
                break
            chunks.append(await reader.readexactly(size))
            await reader.readline()
        body = b"".join(chunks)
    elif "content-length" in headers:
        body = await reader.readexactly(int(headers["content-length"]))
    return method, target, headers, body


def _http_handler(svc: HttpService, tls: bool):
    async def handle(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        peer = writer.get_extra_info("peername") or ("?", 0)
        local = writer.get_extra_info("sockname") or ("?", 0)
        try:
            while True:
                got = await _read_request(reader)
                if got is None:
                    break
                method, target, headers, body = got
                parsed = urllib.parse.urlsplit(target)
                req = Request(
                    method,
                    urllib.parse.unquote(parsed.path),
                    target,
                    urllib.parse.parse_qs(parsed.query, keep_blank_values=True),
                    headers,
                    body,
                    peer[:2],
                    local[:2],
                    tls,
                )
                snap = svc.ctx.snapshot()
                if not svc.reachable(snap):
                    break
                try:
                    resp = svc.handle(req, snap)
                except Drop:
                    break
                except Exception:
                    log.exception("%s: %s %s failed", svc.name, method, target)
                    resp = Response.text("internal error", 500, "text/plain")
                if resp is DROP:
                    break
                close = headers.get("connection", "").lower() == "close"
                head = [f"HTTP/1.1 {resp.status} {REASONS.get(resp.status, 'OK')}"]
                names = {k.lower() for k, _ in resp.headers}
                hdrs = list(resp.headers)
                if "content-length" not in names:
                    hdrs.append(("Content-Length", str(len(resp.body))))
                if "date" not in names:
                    hdrs.append(("Date", time.strftime("%a, %d %b %Y %H:%M:%S GMT", time.gmtime())))
                hdrs.append(("Connection", "close" if close else "keep-alive"))
                head += [f"{k}: {v}" for k, v in hdrs]
                writer.write(
                    ("\r\n".join(head) + "\r\n\r\n").encode("latin-1") + (b"" if method == "HEAD" else resp.body)
                )
                await writer.drain()
                if close:
                    break
        except (asyncio.IncompleteReadError, ConnectionError, ssl.SSLError, OSError):
            pass
        finally:
            with contextlib.suppress(Exception):
                writer.close()

    return handle


class _Udp(asyncio.DatagramProtocol):
    def __init__(self, svc: UdpService) -> None:
        self.svc = svc
        self.transport: asyncio.DatagramTransport | None = None

    def connection_made(self, transport) -> None:  # type: ignore[override]
        self.transport = transport

    def datagram_received(self, data: bytes, addr) -> None:  # type: ignore[override]
        snap = self.svc.ctx.snapshot()
        if not self.svc.reachable(snap):
            return
        try:
            out = self.svc.datagram(data, addr[:2], snap)
        except Exception:
            log.exception("%s: datagram from %s failed", self.svc.name, addr)
            return
        if out and self.transport:
            self.transport.sendto(out, addr)


# ----------------------------------------------------------------------- TLS


def tls_context(names: list[str], ips: list[str], directory: Path | None = None) -> ssl.SSLContext:
    """A self-signed certificate for every emulated host (verification is off in Omini's integrations)."""
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.x509.oid import NameOID

    key = ec.generate_private_key(ec.SECP256R1())
    subject = x509.Name(
        [
            x509.NameAttribute(NameOID.COMMON_NAME, "devnet.acme.local"),
            x509.NameAttribute(NameOID.ORGANIZATION_NAME, "Acme (Omini devnet)"),
        ]
    )
    san = [x509.DNSName(n) for n in names] + [x509.IPAddress(ipaddress.ip_address(i)) for i in ips]
    now = datetime.datetime.now(datetime.timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(subject)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(days=1))
        .not_valid_after(now + datetime.timedelta(days=825))
        .add_extension(x509.SubjectAlternativeName(san), False)
        .sign(key, hashes.SHA256())
    )
    d = Path(directory or tempfile.mkdtemp(prefix="devnet-tls-"))
    d.mkdir(parents=True, exist_ok=True)
    (d / "devnet.crt").write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    (d / "devnet.key").write_bytes(
        key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption())
    )
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(d / "devnet.crt", d / "devnet.key")
    return ctx


# ----------------------------------------------------------------------- Hub


class Hub:
    """Binds services to addresses and runs them."""

    def __init__(self, ctx: Context, tls: ssl.SSLContext | None) -> None:
        self.ctx = ctx
        self.tls = tls
        self.servers: list[Any] = []
        self.bound: dict[str, tuple[str, int]] = {}  # service name → (ip, port)

    async def http(self, svc: HttpService, ip: str, port: int, tls: bool) -> tuple[str, int]:
        server = await asyncio.start_server(
            _http_handler(svc, tls), ip, port, ssl=self.tls if tls else None, reuse_address=True
        )
        self.servers.append(server)
        addr = server.sockets[0].getsockname()[:2]
        self.bound[svc.name] = addr
        return addr

    async def udp(self, svc: UdpService, ip: str, port: int) -> tuple[str, int]:
        loop = asyncio.get_running_loop()
        transport, _ = await loop.create_datagram_endpoint(lambda: _Udp(svc), local_addr=(ip, port), reuse_port=True)
        self.servers.append(transport)
        addr = transport.get_extra_info("sockname")[:2]
        self.bound[svc.name] = addr
        return addr

    def close(self) -> None:
        for s in self.servers:
            s.close()
