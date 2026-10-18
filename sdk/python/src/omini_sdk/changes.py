"""Apply a change, check that the device shows it, undo it when it does not.

Every ``apply`` follows the same steps (docs/plugins.md): send the change,
read the device again until it shows it, and put things back when it does
not. ``apply_verify`` does the waiting and the undoing, so every plugin
waits about as long and says the same thing when a change does not take::

    from omini_sdk.changes import Failed, Pending, apply_verify

    before = read_port(c, port)

    def verify():
        now = read_port(c, port)
        if now is None:
            raise Failed("the switch no longer lists the port")   # stop now
        if now.stored != want:
            raise Pending("the controller did not keep it")         # keep reading
        return now.live == want

    apply_verify(
        lambda: c.put(path, want),
        verify,
        undo=lambda: c.put(path, before),
        message=f"could not disable {port}",
        check_in="in UniFi Network",
    )
"""

from __future__ import annotations

import time
from collections.abc import Callable
from typing import TypeVar

from omini_sdk.plugin import PluginError, log

T = TypeVar("T")

DEFAULT_TIMEOUT_S = 30.0
DEFAULT_INTERVAL_S = 2.0


class Pending(PluginError):
    """Raised by ``verify``: not yet, and why (the reason ends up in the
    message when the time runs out)."""


class Failed(PluginError):
    """Raised by ``verify``: the change did not take and will not (the device
    kept something else); no more reading, the change is undone."""


def apply_verify(
    apply: Callable[[], T],
    verify: Callable[[], bool],
    undo: Callable[[], object] | None = None,
    *,
    timeout: float = DEFAULT_TIMEOUT_S,
    interval: float = DEFAULT_INTERVAL_S,
    message: str = "the change did not take effect",
    check_in: str = "on the device",
    undo_failed_apply: bool = True,
    sleep: Callable[[float], None] | None = None,
    clock: Callable[[], float] | None = None,
) -> T:
    """Runs ``apply``, then ``verify`` every ``interval`` seconds until it
    returns True or ``timeout`` seconds pass (it runs at least once).
    Returns what ``apply`` returned.

    ``verify`` returns False (not yet), raises ``Pending(why)`` (not yet,
    with the reason), ``Failed(why)`` (stop: it will not take), or another
    ``PluginError`` (a read that failed while the device applies it: it is
    read again).

    When ``apply`` raises or the change is not seen, ``undo`` (when given)
    puts things back, and a PluginError says
    ``"<message>: <why> (undone)"`` or
    ``"<message>: <why> (could not be undone: check it <check_in>)"``.
    ``undo`` must be safe to call when ``apply`` changed only part, or
    nothing; with ``undo_failed_apply=False`` (``apply`` is one request,
    all or nothing) an ``apply`` that raises is not undone. Without ``undo``
    the message has no suffix. An exception that is not a PluginError (a
    bug) is raised as it is, after the undo.

    ``sleep`` and ``clock`` default to ``time.sleep`` and ``time.monotonic``,
    looked up at each call: a test that makes ``time.sleep`` a no-op ends
    after the number of checks the timeout allows.
    """
    try:
        result = apply()
    except Exception as e:
        if undo is None or not undo_failed_apply:
            raise
        suffix = _undo(undo, check_in)
        if isinstance(e, PluginError):
            raise PluginError(f"{e} {suffix}") from e
        raise
    deadline = (clock or time.monotonic)() + max(0.0, timeout)
    # Also bounded by count, so a test that makes time.sleep a no-op ends.
    checks = int(max(0.0, timeout) // interval) + 1 if interval > 0 else None
    why = f"it was not seen within {timeout:g} s"
    while True:
        checks = checks - 1 if checks is not None else None
        try:
            if verify():
                return result
            why = f"it was not seen within {timeout:g} s"
        except Failed as e:
            why = str(e)
            break
        except Pending as e:
            why = str(e)
        except PluginError as e:
            why = f"the device did not answer ({e or type(e).__name__})"
        except Exception:
            if undo is not None:
                _undo(undo, check_in)
            raise
        if (checks is not None and checks <= 0) or (clock or time.monotonic)() >= deadline:
            break
        (sleep or time.sleep)(interval)
    text = f"{message}: {why}"
    if undo is None:
        raise PluginError(text)
    raise PluginError(f"{text} {_undo(undo, check_in)}")


def _undo(undo: Callable[[], object], check_in: str) -> str:
    try:
        undo()
    except Exception as e:
        log.warning("could not undo the change: %s", e or type(e).__name__, exc_info=True)
        return f"(could not be undone: check it {check_in})"
    return "(undone)"
