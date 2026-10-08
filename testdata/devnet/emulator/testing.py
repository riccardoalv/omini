"""Helpers for tests: run the emulator in-process on random ports, run real plugins like Omini does.

Plugins live in their own repositories (omini-plugin-<id>), next to this one
by default; OMINI_DEVNET_PLUGINS points to the folder holding them. Each
plugin gets a virtual environment built the way Omini builds it (uv, the
plugin's requirements.txt plus this repository's SDK), cached in
testdata/devnet/.cache/.
"""

from __future__ import annotations

import asyncio
import contextlib
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import threading
from pathlib import Path
from typing import Any

from . import acme, configs
from .server import Context, Hub
from .vendors import service_class
from .world import Clock, World

DEVNET = Path(__file__).resolve().parent.parent
REPO = DEVNET.parent.parent
SDK = REPO / "sdk" / "python"
CACHE = DEVNET / ".cache"


def plugins_root() -> Path | None:
    env = os.environ.get("OMINI_DEVNET_PLUGINS")
    if env:
        return Path(env)
    for p in [REPO, *REPO.parents]:
        if (p / "omini-plugin-opnsense" / "plugin.yaml").exists():
            return p
        if (p.parent / "omini-plugin-opnsense" / "plugin.yaml").exists():
            return p.parent
    return None


def plugin_dir(plugin: str) -> Path | None:
    root = plugins_root()
    if root is None:
        return None
    d = root / f"omini-plugin-{plugin}"
    return d if (d / "plugin.yaml").exists() else None


def plugin_python(plugin: str) -> Path:
    """The plugin's Python, in an environment built like Omini builds it (cached by requirements + SDK)."""
    d = plugin_dir(plugin)
    if d is None:
        raise FileNotFoundError(f"omini-plugin-{plugin} not found (set OMINI_DEVNET_PLUGINS)")
    uv = shutil.which("uv")
    if uv is None:
        raise FileNotFoundError("uv is not installed")
    h = hashlib.sha256((d / "requirements.txt").read_bytes())
    for f in [*sorted(SDK.rglob("*.py")), SDK / "pyproject.toml"]:
        if ".venv" not in f.parts:
            h.update(f.read_bytes())
    venv = CACHE / f"venv-{plugin}"
    marker = venv / ".devnet-env"
    want = h.hexdigest()
    if marker.exists() and marker.read_text() == want and (venv / "bin" / "python").exists():
        return venv / "bin" / "python"
    shutil.rmtree(venv, ignore_errors=True)
    CACHE.mkdir(parents=True, exist_ok=True)
    subprocess.run([uv, "venv", "--quiet", "--python", ">=3.10", str(venv)], check=True)
    with tempfile.TemporaryDirectory() as tmp:  # a copy, like Omini's embedded SDK (no build files left behind)
        sdk = Path(tmp) / "sdk"
        shutil.copytree(SDK, sdk, ignore=shutil.ignore_patterns(".venv", "__pycache__", ".pytest_cache", "tests"))
        subprocess.run(
            [
                uv,
                "pip",
                "install",
                "--quiet",
                "--python",
                str(venv / "bin" / "python"),
                str(sdk),
                "-r",
                str(d / "requirements.txt"),
            ],
            check=True,
        )
    marker.write_text(want)
    return venv / "bin" / "python"


def run_plugin(
    plugin: str, config: dict[str, Any], action: str = "collect", state_dir: Path | None = None, timeout: float = 180
) -> dict[str, Any]:
    """Run a plugin once, the way Omini does: the request on stdin, the answer on stdout."""
    d = plugin_dir(plugin)
    assert d is not None
    py = plugin_python(plugin)
    manifest = (d / "plugin.yaml").read_text()
    entry = next(line.split(":", 1)[1].strip() for line in manifest.splitlines() if line.startswith("entrypoint:"))
    with contextlib.ExitStack() as stack:
        if state_dir is None:
            state_dir = Path(stack.enter_context(tempfile.TemporaryDirectory()))
        req = {"protocol": 1, "action": action, "config": config, "state_dir": str(state_dir)}
        env = {k: v for k, v in os.environ.items() if k in ("PATH", "HOME", "LANG", "TMPDIR") or k.startswith("LC_")}
        proc = subprocess.run(
            [str(py), entry],
            input=json.dumps(req),
            capture_output=True,
            text=True,
            cwd=d,
            timeout=timeout,
            env=env,
            check=False,
        )
    if not proc.stdout.strip():
        raise RuntimeError(f"{plugin}: no answer (exit {proc.returncode}): {proc.stderr[-3000:]}")
    out = json.loads(proc.stdout)
    out["_stderr"] = proc.stderr
    return out


class Emulator:
    """The emulated APIs on 127.0.0.1 (random ports), served from a thread; the clock is frozen."""

    def __init__(
        self,
        at: float,
        apis: list[str] | None = None,
        world: World | None = None,
        secrets: dict[str, Any] | None = None,
    ) -> None:
        self.world = world or World()
        self.clock = Clock(start=at, speed=1.0, frozen=at)
        self.secrets = secrets or configs.generate()
        self.ctx = Context(self.world, self.clock, self.secrets)
        self.apis = apis or list(acme.APIS)
        self.urls: dict[str, str] = {}
        self.addrs: dict[str, tuple[str, int]] = {}
        self._loop = asyncio.new_event_loop()
        self._thread = threading.Thread(target=self._loop.run_forever, daemon=True)
        self.hub: Hub | None = None

    def __enter__(self) -> Emulator:
        from .server import tls_context

        self._thread.start()
        tls = tls_context(["localhost"], ["127.0.0.1"])
        self.hub = Hub(self.ctx, tls)

        async def start() -> None:
            assert self.hub is not None
            for name in self.apis:
                spec = acme.APIS[name]
                svc = service_class(spec["kind"])(self.ctx, name, spec)
                if spec["kind"] == "snmp":
                    self.addrs[name] = await self.hub.udp(svc, "127.0.0.1", 0)
                else:
                    ip, port = await self.hub.http(svc, "127.0.0.1", 0, spec["tls"])
                    self.addrs[name] = (ip, port)
                    self.urls[name] = f"{'https' if spec['tls'] else 'http'}://127.0.0.1:{port}"

        asyncio.run_coroutine_threadsafe(start(), self._loop).result(30)
        return self

    def __exit__(self, *exc: object) -> None:
        if self.hub:
            self._loop.call_soon_threadsafe(self.hub.close)
        self._loop.call_soon_threadsafe(self._loop.stop)
        self._thread.join(5)

    def set_time(self, t: float) -> None:
        self.clock.frozen = t
        self.clock.start = t

    def integrations(self) -> list[tuple[str, str, dict[str, Any]]]:
        return configs.integrations(self.urls, self.secrets)

    def config(self, api: str) -> dict[str, Any]:
        return next(c for n, _, c in self.integrations() if n == api)

    def collect(self, api: str, action: str = "collect", state_dir: Path | None = None) -> dict[str, Any]:
        """Run the plugin of one emulated API against it."""
        plugin = next(p for n, p, _ in self.integrations() if n == api)
        return run_plugin(plugin, self.config(api), action, state_dir)
