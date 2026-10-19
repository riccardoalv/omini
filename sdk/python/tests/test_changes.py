import pytest

from omini_sdk import PluginError
from omini_sdk.changes import Failed, Pending, apply_verify
from omini_sdk.http import Forbidden, Unavailable


class Clock:
    def __init__(self):
        self.now = 0.0
        self.sleeps: list[float] = []

    def __call__(self) -> float:
        return self.now

    def sleep(self, s: float) -> None:
        self.sleeps.append(s)
        self.now += s


def run(apply, verify, undo=None, **kw):
    clock = kw.pop("clock", Clock())
    return apply_verify(apply, verify, undo, sleep=clock.sleep, clock=clock, **kw), clock


def test_applied_and_seen():
    seen = iter([False, False, True])
    out, clock = run(lambda: "result", lambda: next(seen))
    assert out == "result"
    assert clock.sleeps == [2.0, 2.0]


def test_not_seen_is_undone():
    undone = []
    with pytest.raises(PluginError) as e:
        run(lambda: None, lambda: False, lambda: undone.append(1), timeout=6)
    assert str(e.value) == "the change did not take effect: it was not seen within 6 s (undone)"
    assert undone == [1]


def test_not_seen_without_undo():
    with pytest.raises(PluginError) as e:
        run(lambda: None, lambda: False, message="could not disable Port 5", timeout=4)
    assert str(e.value) == "could not disable Port 5: it was not seen within 4 s"


def test_runs_verify_at_least_once_with_no_time():
    calls = []
    out, clock = run(lambda: 1, lambda: calls.append(1) or True, timeout=0)
    assert out == 1 and calls == [1] and clock.sleeps == []
    with pytest.raises(PluginError, match="within 0 s"):
        run(lambda: 1, lambda: False, timeout=0)


def test_undo_that_fails():
    def undo():
        raise Forbidden(privilege="Firewall: Rules")

    with pytest.raises(PluginError) as e:
        run(lambda: None, lambda: False, undo, timeout=0, check_in="in UniFi Network")
    assert str(e.value).endswith("(could not be undone: check it in UniFi Network)")
    with pytest.raises(PluginError, match=r"\(could not be undone: check it on the device\)$"):
        run(lambda: None, lambda: False, undo, timeout=0)


def test_pending_reason_is_kept():
    def verify():
        raise Pending("the switch did not report it")

    with pytest.raises(PluginError, match="x: the switch did not report it \\(undone\\)"):
        run(lambda: None, verify, lambda: None, message="x", timeout=4)


def test_failed_stops_at_once():
    calls = []

    def verify():
        calls.append(1)
        raise Failed("the controller did not keep it")

    with pytest.raises(PluginError, match="did not keep it \\(undone\\)"):
        run(lambda: None, verify, lambda: None, timeout=30)
    assert calls == [1]


def test_a_read_that_fails_meanwhile_is_read_again():
    answers = iter([Unavailable("busy"), Forbidden(), True])

    def verify():
        a = next(answers)
        if isinstance(a, Exception):
            raise a
        return a

    out, clock = run(lambda: "ok", verify)
    assert out == "ok" and len(clock.sleeps) == 2

    def down():
        raise Unavailable("cannot connect")

    with pytest.raises(PluginError, match="the device did not answer \\(cannot connect\\)"):
        run(lambda: None, down, timeout=2)


def test_apply_that_fails_is_undone():
    undone = []

    def apply():
        raise Forbidden("may not create the interface")

    with pytest.raises(PluginError) as e:
        run(apply, lambda: True, lambda: undone.append(1))
    assert str(e.value) == "may not create the interface (undone)"
    assert undone == [1]
    # Without undo, the error is raised as it is.
    with pytest.raises(Forbidden):
        run(apply, lambda: True)


def test_a_bug_is_raised_as_it_is_after_the_undo():
    undone = []

    def apply():
        raise KeyError("x")

    with pytest.raises(KeyError):
        run(apply, lambda: True, lambda: undone.append(1))

    def verify():
        raise TypeError("bug")

    with pytest.raises(TypeError):
        run(lambda: None, verify, lambda: undone.append(2))
    assert undone == [1, 2]


def test_ends_by_count_when_time_sleep_is_patched(monkeypatch):
    import time

    calls = []
    monkeypatch.setattr(time, "sleep", lambda _s: calls.append(_s))
    with pytest.raises(PluginError, match="within 30 s"):
        apply_verify(lambda: None, lambda: False)
    assert len(calls) == 15  # 16 checks, 2 s apart


def test_an_all_or_nothing_apply_that_fails_is_not_undone():
    undone = []

    def apply():
        raise Forbidden("the account can only view")

    with pytest.raises(Forbidden, match=r"^the account can only view$"):
        run(apply, lambda: True, lambda: undone.append(1), undo_failed_apply=False)
    assert undone == []


def test_no_interval_waits_by_time_only():
    answers = iter([False, False, True])
    out, clock = run(lambda: 1, lambda: next(answers), interval=0, timeout=5)
    assert out == 1 and clock.sleeps == [0, 0]
