"""Errors a plugin raises when a device refuses or lacks something, and
``optional`` for the reads a collection can do without.

They are all ``PluginError``: one that reaches the SDK is reported to the user
with its message, never as an "unexpected error". ``omini_sdk.http.Client``
raises them; a plugin with another transport (SNMP, SSH, a web page) raises
them too, so ``optional`` and ``apply_verify`` treat every plugin alike.
"""

from __future__ import annotations

import logging
import socket
from collections.abc import Callable
from typing import TypeVar

from omini_sdk.plugin import PluginError

log = logging.getLogger("omini.plugin")

T = TypeVar("T")


class HttpError(PluginError):
    """The device answered with an error. ``status`` is the HTTP status (None
    without one), ``method`` and ``resource`` what was asked."""

    def __init__(
        self,
        message: str = "",
        *,
        status: int | None = None,
        method: str | None = None,
        resource: str | None = None,
    ):
        super().__init__(message or self.default_message(resource))
        self.status, self.method, self.resource = status, method, resource

    @staticmethod
    def default_message(resource: str | None) -> str:
        return f"the device refused {resource}" if resource else "the device refused the request"


class Unauthorized(HttpError):
    """The credentials (or the session) were refused (HTTP 401)."""

    @staticmethod
    def default_message(resource: str | None) -> str:
        return "the device rejected the credentials"


class Forbidden(HttpError):
    """The account may not use something (HTTP 403). ``privilege`` names what
    it needs, in the device's own words, when the plugin knows it."""

    def __init__(
        self,
        message: str = "",
        *,
        privilege: str | None = None,
        status: int | None = 403,
        method: str | None = None,
        resource: str | None = None,
    ):
        self.privilege = privilege
        if not message:
            what = resource or "this"
            message = (
                f"the account may not use {what}: give it the privilege {privilege}"
                if privilege
                else f"the account may not use {what}"
            )
        super().__init__(message, status=status, method=method, resource=resource)


class NotFound(HttpError):
    """Something the device does not have (HTTP 404): an endpoint of another
    version, a package that is not installed, an item that does not exist."""

    @staticmethod
    def default_message(resource: str | None) -> str:
        return f"the device has no {resource}" if resource else "not found on the device"


class Unavailable(HttpError):
    """The device could not answer now: refused connection, timeout, busy
    (429/503) or a server error (5xx)."""

    @staticmethod
    def default_message(resource: str | None) -> str:
        return (
            f"the device could not answer {resource}" if resource else "the device is unavailable"
        )


def _timeouts() -> tuple[type[BaseException], ...]:
    out: list[type[BaseException]] = [TimeoutError, socket.timeout]
    try:
        import httpx

        out.append(httpx.TimeoutException)
    except ImportError:  # pragma: no cover - httpx is a dependency of the SDK
        pass
    return tuple(out)


_logged: set[str] = set()


def optional(
    name: str,
    fn: Callable[[], T],
    default: T,
    *,
    missing: list[str] | None = None,
) -> T:
    """Runs an optional read: data the collection can do without.

    - ``Forbidden``, ``NotFound``, ``Unavailable`` and timeouts: logged once
      per ``name`` at info, and ``default`` is returned. A ``Forbidden`` adds
      its privilege (else ``name``) to ``missing``, so a connection test can
      say which privileges the account lacks.
    - ``Unauthorized`` is raised: wrong credentials fail the whole run.
    - Anything else (a parser bug, an answer of an unexpected shape): a
      warning with the traceback, and ``default``.
    """
    try:
        return fn()
    except Unauthorized:
        raise
    except Forbidden as e:
        if missing is not None:
            missing.append(e.privilege or name)
        _once(name, f"{name}: skipped, not allowed ({e})")
    except (NotFound, Unavailable, *_timeouts()) as e:
        _once(name, f"{name}: skipped ({e or type(e).__name__})")
    except Exception:
        log.warning("%s: skipped after an error", name, exc_info=True)
    return default


def _once(name: str, message: str) -> None:
    if name in _logged:
        return
    _logged.add(name)
    log.info("%s", message)
