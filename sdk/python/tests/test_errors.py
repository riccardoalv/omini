import logging

import httpx
import pytest

from omini_sdk import (
    Forbidden,
    HttpError,
    NotFound,
    PluginError,
    Unauthorized,
    Unavailable,
    optional,
)
from omini_sdk import errors as errors_module


@pytest.fixture(autouse=True)
def fresh_log():
    errors_module._logged.clear()


def test_messages():
    assert str(Forbidden(privilege="Lobby: Dashboard", resource="/api/x")) == (
        "the account may not use /api/x: give it the privilege Lobby: Dashboard"
    )
    assert str(Forbidden()) == "the account may not use this"
    assert str(NotFound(resource="/api/wol")) == "the device has no /api/wol"
    assert str(NotFound()) == "not found on the device"
    assert str(Unauthorized()) == "the device rejected the credentials"
    assert str(Unavailable(resource="/x")) == "the device could not answer /x"
    assert str(Unavailable()) == "the device is unavailable"
    assert str(HttpError(resource="/x")) == "the device refused /x"
    assert str(HttpError()) == "the device refused the request"
    assert str(Forbidden("custom", privilege="P")) == "custom"
    for e in (Forbidden(), NotFound(), Unauthorized(), Unavailable(), HttpError()):
        assert isinstance(e, PluginError)


def test_optional_returns_the_value():
    assert optional("arp", lambda: [1], []) == [1]


@pytest.mark.parametrize(
    "error",
    [
        NotFound(resource="/api/x"),
        Unavailable("busy"),
        TimeoutError(),
        httpx.ReadTimeout("slow"),
    ],
)
def test_optional_skips_what_is_not_there(error, caplog):
    def fn():
        raise error

    caplog.set_level(logging.INFO, logger="omini.plugin")
    assert optional("the ARP table", fn, []) == []
    assert optional("the ARP table", fn, []) == []
    infos = [r for r in caplog.records if r.levelno == logging.INFO]
    assert len(infos) == 1  # logged once
    assert infos[0].getMessage().startswith("the ARP table: skipped")


def test_optional_collects_missing_privileges(caplog):
    missing: list[str] = []

    def denied():
        raise Forbidden(privilege="Diagnostics: ARP Table")

    def anonymous():
        raise Forbidden()

    caplog.set_level(logging.INFO, logger="omini.plugin")
    assert optional("arp", denied, None, missing=missing) is None
    assert optional("leases", anonymous, None, missing=missing) is None
    assert missing == ["Diagnostics: ARP Table", "leases"]
    assert optional("x", denied, 0) == 0  # without a list


def test_optional_raises_unauthorized():
    def fn():
        raise Unauthorized()

    with pytest.raises(Unauthorized):
        optional("arp", fn, [])


def test_optional_warns_on_a_bug(caplog):
    def fn():
        raise KeyError("ip")

    assert optional("arp", fn, "default") == "default"
    warning = next(r for r in caplog.records if r.levelno == logging.WARNING)
    assert warning.exc_info is not None
