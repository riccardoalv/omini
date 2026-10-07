"""Plugin runtime: reads the core's request from stdin and answers on stdout.

A plugin registers two functions and calls ``plugin.run()``::

    from omini_sdk import Device, PluginError, plugin

    @plugin.collect
    def collect(cfg):
        return [Device(key="fw", name="fw")]

    @plugin.test
    def test(cfg):
        return "Connected"

    if __name__ == "__main__":
        plugin.run()

Raise ``PluginError`` with a message for the user (wrong key, host
unreachable); any other exception is reported as an unexpected error and its
traceback goes to the log (stderr).
"""

from __future__ import annotations

import json
import logging
import sys
import traceback
from collections.abc import Callable, Iterable
from pathlib import Path
from typing import Any, TextIO

from pydantic import ValidationError

from omini_sdk.models import Device, PluginRequest

PROTOCOLS = (1,)

log = logging.getLogger("omini.plugin")


class PluginError(Exception):
    """An error the user should read, e.g. "invalid API key"."""


class Config(dict[str, Any]):
    """Values of the plugin's form fields, plus a private state directory."""

    def __init__(self, values: dict[str, Any], state_dir: Path | None = None):
        super().__init__(values)
        self.state_dir = state_dir

    def str(self, key: str, default: str = "") -> str:
        value = self.get(key)
        return value if isinstance(value, str) and value != "" else default

    def bool(self, key: str, default: bool = False) -> bool:
        value = self.get(key)
        return value if isinstance(value, bool) else default

    def int(self, key: str, default: int = 0) -> int:
        value = self.get(key)
        return (
            int(value)
            if isinstance(value, (int, float)) and not isinstance(value, bool)
            else default
        )


CollectFn = Callable[[Config], Iterable[Device | dict[str, Any]]]
TestFn = Callable[[Config], str]


class Plugin:
    def __init__(self) -> None:
        self._collect: CollectFn | None = None
        self._test: TestFn | None = None

    def collect(self, fn: CollectFn) -> CollectFn:
        """Registers the function that returns the devices."""
        self._collect = fn
        return fn

    def test(self, fn: TestFn) -> TestFn:
        """Registers the function that checks the connection and returns a short message."""
        self._test = fn
        return fn

    def handle(self, raw: str) -> tuple[dict[str, Any], int]:
        """Answers one request; returns the response and the exit code."""
        try:
            req = PluginRequest.model_validate_json(raw)
        except ValidationError as e:
            return {"error": f"invalid request from Omini: {e.errors()[0]['msg']}"}, 2
        if req.protocol not in PROTOCOLS:
            return {
                "error": f"Omini speaks plugin protocol {req.protocol}, this SDK {list(PROTOCOLS)}"
            }, 2
        cfg = Config(dict(req.config), Path(req.state_dir) if req.state_dir else None)
        action = req.action if isinstance(req.action, str) else req.action.value
        try:
            if action == "collect":
                if self._collect is None:
                    raise PluginError("this plugin has no @plugin.collect function")
                devices = [
                    d if isinstance(d, Device) else Device.model_validate(d)
                    for d in self._collect(cfg)
                ]
                return {
                    "devices": [d.model_dump(mode="json", exclude_none=True) for d in devices]
                }, 0
            if self._test is None:
                raise PluginError("this plugin has no @plugin.test function")
            return {"message": str(self._test(cfg))}, 0
        except PluginError as e:
            return {"error": str(e)}, 1
        except ValidationError as e:
            traceback.print_exc(file=sys.stderr)
            return {"error": f"the plugin returned invalid data: {e.errors()[0]['msg']}"}, 1
        except Exception as e:
            traceback.print_exc(file=sys.stderr)
            return {"error": f"unexpected error: {type(e).__name__}: {e}"}, 1

    def run(self, stdin: TextIO | None = None, stdout: TextIO | None = None) -> None:
        """Reads the request, runs the action, writes the response and exits."""
        logging.basicConfig(
            stream=sys.stderr, level=logging.INFO, format="%(levelname)s %(name)s: %(message)s"
        )
        response, code = self.handle((stdin or sys.stdin).read())
        out = stdout or sys.stdout
        out.write(json.dumps(response, separators=(",", ":")) + "\n")
        out.flush()
        sys.exit(code)


plugin = Plugin()
