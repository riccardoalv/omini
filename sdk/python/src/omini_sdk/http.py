"""An HTTP client for plugins, and the login state they keep between runs.

``Client`` wraps ``httpx.Client`` with what every plugin needs: a timeout,
retries when the device refuses a connection or is busy, JSON answers, and
errors that say what went wrong (``Unauthorized``, ``Forbidden`` naming the
privilege, ``NotFound`` naming the resource, ``Unavailable``), all
``PluginError`` so the SDK reports them cleanly::

    from omini_sdk.http import Client, settings

    c = Client(cfg.str("url"), name="OPNsense", auth=(key, secret),
               unauthorized="OPNsense rejected the API key or secret",
               forbidden_hint={"/api/firewall/": "Firewall: Rules"},
               **settings(cfg))
    info = c.get("/api/core/system/status")

A plugin whose device answers errors in its own way subclasses it and
overrides ``check`` (status → error) or ``detail`` (the device's words for an
error). ``Session`` keeps a login (cookies, a token) in the state folder and
``LoginBackoff`` waits after the device refuses the credentials, so a run
neither signs in every time nor locks the account.
"""

from __future__ import annotations

import contextlib
import hashlib
import json
import os
import time
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import TYPE_CHECKING, Any
from urllib.parse import urlsplit, urlunsplit

import httpx

from omini_sdk.errors import Forbidden, HttpError, NotFound, Unauthorized, Unavailable
from omini_sdk.plugin import Config, PluginError, log

__all__ = [
    "Client",
    "Forbidden",
    "HttpError",
    "LoginBackoff",
    "NotFound",
    "Session",
    "Unauthorized",
    "Unavailable",
    "fingerprint",
    "normalize_url",
    "settings",
]

DEFAULT_TIMEOUT = 15.0
DEFAULT_RETRIES = 2

# Methods a timeout or a cut answer may be sent again for: the device may
# have done a POST that timed out, so it is never repeated.
IDEMPOTENT = frozenset({"GET", "HEAD", "OPTIONS", "PUT", "DELETE"})

# Statuses that mean "busy, ask again": sent again for any method (the device
# did not do the request).
BUSY = frozenset({429, 503})

MAX_RETRY_AFTER_S = 10.0

if TYPE_CHECKING:
    ForbiddenHint = Callable[[str, str], str | None] | Mapping[str, str]


def settings(cfg: Config) -> dict[str, Any]:
    """The form's Advanced fields as Client keyword arguments: ``verify_tls``
    (default off) and ``timeout_s`` (default 15 s)."""
    timeout = cfg.int("timeout_s", 0)
    return {
        "verify_tls": cfg.bool("verify_tls", False),
        "timeout": float(timeout) if timeout > 0 else DEFAULT_TIMEOUT,
    }


def normalize_url(
    url: str,
    *,
    scheme: str = "https",
    port: int | Mapping[str, int] | None = None,
    strip: tuple[str, ...] = (),
) -> str:
    """'192.168.1.1' → 'https://192.168.1.1'. ``scheme`` is used when the
    address has none, ``port`` (one, or one per scheme) when it has none,
    and a trailing path in ``strip`` (an API prefix the user pasted, such
    as '/api') is removed. Raises PluginError for an address without a host
    or with another scheme."""
    url = url.strip().rstrip("/")
    if not url:
        raise PluginError("the address is required")
    if "://" not in url:
        url = f"{scheme}://{url}"
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https"):
        raise PluginError("the address must start with http:// or https://")
    if not parts.hostname:
        raise PluginError("the address has no host")
    netloc = parts.netloc
    default = port.get(parts.scheme) if isinstance(port, Mapping) else port
    if parts.port is None and default:
        netloc = f"{netloc}:{default}"
    path = parts.path.rstrip("/")
    for suffix in strip:
        if path.endswith(suffix):
            path = path[: -len(suffix)]
    return urlunsplit((parts.scheme, netloc, path, "", ""))


class Client:
    """An HTTP client for one device.

    - ``base_url``: the device's address (normalized with ``normalize_url``;
      ``scheme`` and ``port`` pick the defaults). ``api`` is a path prefix
      every request goes under ('/api2/json').
    - ``name``: the device or software as the user knows it, in messages.
    - ``timeout`` (15 s), ``verify_tls`` (off: most devices have a
      self-signed certificate).
    - ``retries`` (2): a refused connection and a busy answer (429, 503) are
      sent again after a pause (``backoff`` seconds, doubled each time, or
      the device's Retry-After up to 10 s); a timeout or a cut answer is sent
      again once, and only for GET, HEAD, OPTIONS, PUT and DELETE.
    - ``unauthorized``: the message for a 401.
    - ``forbidden_hint``: the privilege a refused (403) request needs, as a
      function of (method, path) or a mapping of path prefixes.
    - ``auth``, ``headers``, ``cookies``, ``follow_redirects``, ``transport``
      (tests: ``httpx.MockTransport``) go to httpx; ``sleep`` too (tests).

    ``http`` is the underlying ``httpx.Client`` for anything else (streams).
    """

    def __init__(
        self,
        base_url: str,
        *,
        name: str = "the device",
        api: str = "",
        scheme: str = "https",
        port: int | Mapping[str, int] | None = None,
        strip: tuple[str, ...] = (),
        verify_tls: bool = False,
        timeout: float = DEFAULT_TIMEOUT,
        retries: int = DEFAULT_RETRIES,
        backoff: float = 1.0,
        auth: Any = None,
        headers: Mapping[str, str] | None = None,
        user_agent: str = "omini-plugin",
        unauthorized: str | None = None,
        forbidden_hint: ForbiddenHint | None = None,
        follow_redirects: bool = False,
        transport: httpx.BaseTransport | None = None,
        sleep: Callable[[float], None] | None = None,
    ):
        self.base = normalize_url(base_url, scheme=scheme, port=port, strip=strip)
        self.name = name
        self.retries = max(0, retries)
        self.backoff = backoff
        self.unauthorized = unauthorized or f"{name} rejected the credentials"
        self.forbidden_hint = forbidden_hint
        self.sleep = sleep
        self.http = httpx.Client(
            base_url=self.base + api,
            auth=auth,
            verify=verify_tls,
            timeout=timeout,
            headers={"Accept": "application/json", "User-Agent": user_agent, **(headers or {})},
            follow_redirects=follow_redirects,
            transport=transport,
        )

    @property
    def host(self) -> str:
        """The device's host name or address, as in the form."""
        return urlsplit(self.base).hostname or ""

    def close(self) -> None:
        self.http.close()

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()

    # --- requests -------------------------------------------------------------

    def get(self, path: str, params: Mapping[str, Any] | None = None, **kw: Any) -> Any:
        """GETs a path and returns its JSON (None for an empty answer)."""
        return self.request("GET", path, params=params, **kw)

    def post(self, path: str, json: Any = None, **kw: Any) -> Any:
        return self.request("POST", path, json=json, **kw)

    def put(self, path: str, json: Any = None, **kw: Any) -> Any:
        return self.request("PUT", path, json=json, **kw)

    def patch(self, path: str, json: Any = None, **kw: Any) -> Any:
        return self.request("PATCH", path, json=json, **kw)

    def delete(self, path: str, **kw: Any) -> Any:
        return self.request("DELETE", path, **kw)

    def text(self, path: str, **kw: Any) -> str:
        """GETs something the device answers as is (a configuration file)."""
        kw.setdefault("headers", {"Accept": "*/*"})
        r = self.send("GET", path, **kw)
        self.check(r, "GET", path)
        return r.text

    def request(self, method: str, path: str, **kw: Any) -> Any:
        """Sends a request (``params``, ``json``, ``data``, ``content``,
        ``headers`` as httpx), raises the error its status means and returns
        the JSON answer (None for an empty one)."""
        method = method.upper()
        r = self.send(method, path, **kw)
        self.check(r, method, path)
        return self.json(r, path)

    def json(self, r: httpx.Response, path: str = "") -> Any:
        """The JSON of an answer; None when it is empty."""
        if not r.content:
            return None
        try:
            return r.json()
        except (json.JSONDecodeError, UnicodeDecodeError) as e:
            raise PluginError(
                f"{self.base} did not answer with JSON: is it the {self.name} address?"
            ) from e

    def send(self, method: str, path: str, **kw: Any) -> httpx.Response:
        """Sends a request with the retries, without looking at the status.
        A connection that fails for good raises Unavailable."""
        method = method.upper()
        timed_out = False
        attempt = 0
        while True:
            try:
                r = self.http.request(method, path, **kw)
            except httpx.ConnectError as e:
                if attempt < self.retries:
                    self._pause(attempt, f"{self.base} refused the connection")
                    attempt += 1
                    continue
                raise Unavailable(
                    f"cannot connect to {self.base}: {e}", method=method, resource=path
                ) from e
            except httpx.TimeoutException as e:
                if method in IDEMPOTENT and not timed_out and attempt < self.retries:
                    timed_out = True
                    self._pause(attempt, f"{self.base} did not answer {path} in time")
                    attempt += 1
                    continue
                raise Unavailable(
                    f"{self.base} did not answer in time", method=method, resource=path
                ) from e
            except (httpx.RemoteProtocolError, httpx.ReadError) as e:
                if method in IDEMPOTENT and attempt < self.retries:
                    self._pause(attempt, f"{self.base} cut its answer to {path} short")
                    attempt += 1
                    continue
                raise Unavailable(
                    f"request to {self.base} failed: {e}", method=method, resource=path
                ) from e
            except httpx.HTTPError as e:
                raise Unavailable(
                    f"request to {self.base} failed: {e}", method=method, resource=path
                ) from e
            if r.status_code in BUSY and attempt < self.retries:
                self._pause(attempt, f"{self.base} is busy ({r.status_code})", r)
                attempt += 1
                continue
            return r

    def _pause(self, attempt: int, why: str, r: httpx.Response | None = None) -> None:
        wait = self.backoff * (2**attempt)
        if r is not None:
            with contextlib.suppress(TypeError, ValueError):
                wait = min(float(r.headers.get("retry-after", "")), MAX_RETRY_AFTER_S)
        log.info("%s, trying again", why)
        (self.sleep or time.sleep)(max(0.0, wait))

    # --- errors ---------------------------------------------------------------

    def privilege(self, method: str, path: str) -> str | None:
        """The privilege a refused request needs, from ``forbidden_hint``."""
        hint = self.forbidden_hint
        if hint is None:
            return None
        if callable(hint):
            return hint(method, path)
        found = [p for p in hint if path.startswith(p)]
        return hint[max(found, key=len)] if found else None

    def check(self, r: httpx.Response, method: str, path: str) -> None:
        """Raises the error a status means: 401 Unauthorized, 403 Forbidden,
        404/410 NotFound, 429/5xx Unavailable, another 4xx HttpError (with
        the device's words for a refused change)."""
        status = r.status_code
        if status < 400:
            return
        where = {"status": status, "method": method, "resource": path}
        if status == 401:
            raise Unauthorized(self.unauthorized, **where)
        if status == 403:
            raise Forbidden(privilege=self.privilege(method, path), **where)
        if status in (404, 410):
            raise NotFound(f"{self.name} has no {path}", **where)
        if status in BUSY or status >= 500:
            raise Unavailable(f"{self.name} answered {status} for {path}", **where)
        if method in ("GET", "HEAD"):
            raise HttpError(f"{self.name} answered {status} for {path}", **where)
        raise HttpError(f"{self.name} refused {method} {path}: {self.detail(r)}", **where)

    def detail(self, r: httpx.Response) -> str:
        """The device's own words for an error answer: a JSON ``message``,
        ``detail`` or ``error``, else a short text, else the status."""
        try:
            body = r.json()
        except (json.JSONDecodeError, UnicodeDecodeError):
            body = None
        if isinstance(body, dict):
            for key in ("message", "detail", "error", "msg"):
                value = body.get(key)
                if isinstance(value, dict):
                    value = value.get("message")
                if isinstance(value, str) and value.strip():
                    return value.strip()
        text = r.text.strip() if body is None else ""
        if text and len(text) <= 200 and "<" not in text:
            return text
        return f"HTTP {r.status_code}"


# --- state between runs ------------------------------------------------------------


def fingerprint(*parts: Any) -> str:
    """A hash of the address and credentials a saved state belongs to: a
    change in the form makes it start over. The parts are never stored."""
    return hashlib.sha256("\0".join(str(p) for p in parts).encode()).hexdigest()


def _read(path: Path | None) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text()) if path and path.exists() else {}
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def _write(path: Path | None, data: dict[str, Any]) -> None:
    """Writes a file only its owner can read."""
    if path is None:
        return
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as out:
            json.dump(data, out)
        os.chmod(path, 0o600)
    except OSError as e:
        log.debug("could not write %s: %s", path.name, e)


def _remove(path: Path | None) -> None:
    if path is not None:
        with contextlib.suppress(OSError):
            path.unlink()


class Session:
    """A login kept in the state folder (``<name>.json``, readable by the
    owner only), tied to a fingerprint of ``key`` (pass the address,
    username and password): other credentials start over. Without a state
    folder nothing is kept.

    Keep a session id, cookies or a token; never the password::

        s = Session(cfg.state_dir, url, user, password)
        saved = s.load()          # {} when there is none for these credentials
        s.save({"sid": sid})
        s.clear()                 # the device forgot it
    """

    def __init__(self, state_dir: Path | str | None, *key: Any, name: str = "session"):
        self.path = Path(state_dir) / f"{name}.json" if state_dir else None
        self.key = fingerprint(*key)

    def load(self) -> dict[str, Any]:
        data = _read(self.path)
        value = data.get("data")
        return value if data.get("for") == self.key and isinstance(value, dict) else {}

    def save(self, data: dict[str, Any]) -> None:
        _write(self.path, {"for": self.key, "data": data})

    def clear(self) -> None:
        _remove(self.path)


class LoginBackoff:
    """Waits after the device refuses the credentials: many lock an account
    (or the address) after repeated failed logins, and Omini runs every
    minute. Kept in the state folder (``login-refused.json``), only for the
    same ``key`` (address and credentials): new ones are tried at once::

        backoff = LoginBackoff(cfg.state_dir, url, user, password, minutes=10)
        backoff.check()                  # raises while waiting
        try:
            login()
        except WrongPassword as e:
            backoff.refused(str(e))      # remember it, then raise
            raise
        backoff.clear()

    Only a refused password should be remembered: a device out of reach or
    busy is asked again on the next run.
    """

    def __init__(
        self,
        state_dir: Path | str | None,
        *key: Any,
        minutes: float = 10,
        name: str = "login-refused",
        clock: Callable[[], float] = time.time,
    ):
        self.path = Path(state_dir) / f"{name}.json" if state_dir else None
        self.key = fingerprint(*key)
        self.seconds = minutes * 60
        self.clock = clock
        self._memory: dict[str, Any] = {}

    def _last(self) -> dict[str, Any]:
        return _read(self.path) if self.path else self._memory

    def waiting(self) -> str | None:
        """The refusal still being waited out (its message), or None."""
        last = self._last()
        if last.get("for") != self.key:
            return None
        if self.clock() - float(last.get("at") or 0) >= self.seconds:
            return None
        return str(last.get("error") or "the device refused the credentials")

    def check(self) -> None:
        """Raises PluginError while a refusal of these credentials is recent."""
        error = self.waiting()
        if error is not None:
            raise PluginError(f"{error} (waiting a few minutes before signing in again)")

    def refused(self, error: str) -> None:
        data = {"for": self.key, "at": self.clock(), "error": error}
        if self.path:
            _write(self.path, data)
        else:
            self._memory = data

    def clear(self) -> None:
        self._memory = {}
        if self.path and self.path.exists():
            _remove(self.path)
