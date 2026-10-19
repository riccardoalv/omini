import os
import stat

import httpx
import pytest

from omini_sdk import Config, PluginError
from omini_sdk.http import (
    Client,
    Forbidden,
    HttpError,
    LoginBackoff,
    NotFound,
    Session,
    Unauthorized,
    Unavailable,
    fingerprint,
    normalize_url,
    settings,
)
from omini_sdk.plugin import Plugin


def client(handler, **kw):
    pauses: list[float] = []
    kw.setdefault("name", "Example")
    c = Client(
        kw.pop("url", "192.168.1.1"),
        transport=httpx.MockTransport(handler),
        sleep=pauses.append,
        **kw,
    )
    c.pauses = pauses  # type: ignore[attr-defined]
    return c


def test_normalize_url():
    assert normalize_url("192.168.1.1") == "https://192.168.1.1"
    assert normalize_url(" http://fw.lan:8080/ ") == "http://fw.lan:8080"
    assert normalize_url("10.0.0.1", port=8006) == "https://10.0.0.1:8006"
    assert normalize_url("http://nas", port={"https": 5001, "http": 5000}) == "http://nas:5000"
    assert normalize_url("https://pve:443/api2/json", strip=("/api2/json",)) == "https://pve:443"
    assert normalize_url("[fd00::1]", scheme="http") == "http://[fd00::1]"
    with pytest.raises(PluginError, match="required"):
        normalize_url(" ")
    with pytest.raises(PluginError, match="http:// or https://"):
        normalize_url("ftp://x")


def test_settings():
    assert settings(Config({})) == {"verify_tls": False, "timeout": 15.0}
    assert settings(Config({"verify_tls": True, "timeout_s": 40})) == {
        "verify_tls": True,
        "timeout": 40.0,
    }


def test_json_helpers_and_request_details():
    seen = []

    def handler(request):
        seen.append(request)
        if request.url.path == "/empty":
            return httpx.Response(204)
        return httpx.Response(200, json={"path": request.url.path, "q": dict(request.url.params)})

    c = client(handler, auth=("k", "s"), headers={"X-Extra": "1"}, user_agent="omini-plugin-x")
    assert c.get("/a", {"x": "1"}) == {"path": "/a", "q": {"x": "1"}}
    assert c.post("/b", {"v": 1})["path"] == "/b"
    assert c.put("/c")["path"] == "/c"
    assert c.patch("/d", {})["path"] == "/d"
    assert c.delete("/e")["path"] == "/e"
    assert c.get("/empty") is None
    assert seen[1].content == b'{"v":1}' or seen[1].content == b'{"v": 1}'
    assert seen[0].headers["x-extra"] == "1"
    assert seen[0].headers["user-agent"] == "omini-plugin-x"
    assert seen[0].headers["authorization"].startswith("Basic ")
    assert c.host == "192.168.1.1"


def test_api_prefix_and_text():
    def handler(request):
        assert request.url.path.startswith("/api2/json/")
        return httpx.Response(200, text="<config/>")

    with client(handler, api="/api2/json") as c:
        assert c.text("/conf") == "<config/>"


def test_not_json():
    c = client(lambda r: httpx.Response(200, text="<html>login</html>"))
    with pytest.raises(PluginError, match="did not answer with JSON: is it the Example address"):
        c.get("/")


@pytest.mark.parametrize(
    ("status", "error", "message"),
    [
        (401, Unauthorized, "Example rejected the credentials"),
        (403, Forbidden, "the account may not use /api/x"),
        (404, NotFound, "Example has no /api/x"),
        (410, NotFound, "Example has no /api/x"),
        (500, Unavailable, "Example answered 500 for /api/x"),
        (400, HttpError, "Example answered 400 for /api/x"),
    ],
)
def test_statuses(status, error, message):
    c = client(lambda r: httpx.Response(status), retries=0)
    with pytest.raises(error, match=message) as e:
        c.get("/api/x")
    assert isinstance(e.value, PluginError)
    assert e.value.status == status and e.value.resource == "/api/x" and e.value.method == "GET"


def test_errors_are_reported_cleanly_by_the_sdk():
    p = Plugin()
    c = client(lambda r: httpx.Response(403), forbidden_hint={"/api/": "Lobby: Dashboard"})
    p.test(lambda cfg: c.get("/api/system"))
    resp, code = p.handle('{"protocol":1,"action":"test","config":{},"state_dir":"/tmp/s"}')
    assert code == 1
    assert resp == {
        "error": "the account may not use /api/system: give it the privilege Lobby: Dashboard"
    }


def test_forbidden_hint():
    c = client(
        lambda r: httpx.Response(403),
        forbidden_hint={"/api/firewall/": "Firewall: Rules", "/api/firewall/alias/": "Alias"},
    )
    with pytest.raises(Forbidden) as e:
        c.post("/api/firewall/alias/set")
    assert e.value.privilege == "Alias"
    with pytest.raises(Forbidden) as e:
        c.get("/api/other")
    assert e.value.privilege is None

    c = client(lambda r: httpx.Response(403), forbidden_hint=lambda m, p: f"{m} {p}")
    with pytest.raises(Forbidden, match="give it the privilege PUT /x"):
        c.put("/x", {})


def test_refused_change_carries_the_device_words():
    def handler(request):
        if request.url.path == "/json":
            return httpx.Response(400, json={"message": "name is taken"})
        if request.url.path == "/nested":
            return httpx.Response(422, json={"error": {"message": "bad vlan"}})
        if request.url.path == "/html":
            return httpx.Response(400, text="<html>oops</html>")
        return httpx.Response(400, text="invalid IP")

    c = client(handler)
    with pytest.raises(HttpError, match="Example refused POST /json: name is taken"):
        c.post("/json", {})
    with pytest.raises(HttpError, match="bad vlan"):
        c.post("/nested", {})
    with pytest.raises(HttpError, match="refused POST /html: HTTP 400"):
        c.post("/html", {})
    with pytest.raises(HttpError, match="invalid IP"):
        c.put("/text", {})


def test_retries_a_refused_connection_then_gives_up():
    calls = []

    def handler(request):
        calls.append(request.method)
        raise httpx.ConnectError("refused", request=request)

    c = client(handler, retries=2, backoff=0.5)
    with pytest.raises(Unavailable, match=r"cannot connect to https://192\.168\.1\.1"):
        c.post("/login", {})  # nothing reached the device: even a POST
    assert len(calls) == 3
    assert c.pauses == [0.5, 1.0]


def test_a_refused_connection_that_recovers():
    calls = []

    def handler(request):
        calls.append(1)
        if len(calls) == 1:
            raise httpx.ConnectError("refused", request=request)
        return httpx.Response(200, json=[1])

    assert client(handler).get("/x") == [1]


def test_a_timeout_is_retried_once_and_only_for_idempotent_methods():
    calls = []

    def handler(request):
        calls.append(request.method)
        raise httpx.ReadTimeout("slow", request=request)

    c = client(handler, retries=5)
    with pytest.raises(Unavailable, match="did not answer in time"):
        c.get("/x")
    assert calls == ["GET", "GET"]
    calls.clear()
    with pytest.raises(Unavailable, match="did not answer in time"):
        c.post("/x", {})
    assert calls == ["POST"]


def test_a_cut_answer_is_retried_for_reads():
    calls = []

    def handler(request):
        calls.append(1)
        if len(calls) < 3:
            raise httpx.RemoteProtocolError("incomplete chunked read", request=request)
        return httpx.Response(200, json={"ok": True})

    assert client(handler).get("/x") == {"ok": True}
    calls.clear()
    with pytest.raises(Unavailable, match="request to"):
        client(handler).post("/x", {})


def test_other_transport_errors():
    def handler(request):
        raise httpx.UnsupportedProtocol("nope", request=request)

    with pytest.raises(Unavailable, match=r"request to https://192\.168\.1\.1 failed"):
        client(handler).get("/x")


def test_busy_answers_are_retried_with_retry_after():
    answers = [
        httpx.Response(429, headers={"Retry-After": "3"}),
        httpx.Response(503, headers={"Retry-After": "120"}),
        httpx.Response(200, json={"ok": 1}),
    ]
    c = client(lambda r: answers.pop(0))
    assert c.post("/x", {}) == {"ok": 1}
    assert c.pauses == [3.0, 10.0]  # capped


def test_busy_for_good_is_unavailable():
    c = client(lambda r: httpx.Response(503), retries=1)
    with pytest.raises(Unavailable, match="answered 503"):
        c.get("/x")
    assert c.pauses == [1.0]


def test_subclass_overrides_check_and_detail():
    class Mine(Client):
        def check(self, r, method, path):
            if r.status_code == 400:
                raise NotFound(resource=path)
            super().check(r, method, path)

    c = Mine(
        "x",
        transport=httpx.MockTransport(lambda r: httpx.Response(400)),
        unauthorized="RouterOS rejected the username or password",
    )
    with pytest.raises(NotFound, match="the device has no /menu"):
        c.get("/menu")
    assert c.unauthorized.startswith("RouterOS")


def test_fingerprint():
    assert fingerprint("a", "b") == fingerprint("a", "b") != fingerprint("a", "c")
    assert "secret" not in fingerprint("url", "user", "secret")


def test_session(tmp_path):
    s = Session(tmp_path / "state", "https://fw", "omini", "pw")
    assert s.load() == {}
    s.save({"sid": "abc"})
    assert s.load() == {"sid": "abc"}
    f = tmp_path / "state" / "session.json"
    assert stat.S_IMODE(os.stat(f).st_mode) == 0o600
    assert "pw" not in f.read_text()
    # Other credentials: start over.
    assert Session(tmp_path / "state", "https://fw", "omini", "new").load() == {}
    assert Session(tmp_path / "state", "https://fw", "omini", "pw", name="token").load() == {}
    s.clear()
    assert s.load() == {}
    s.clear()  # twice is fine
    f.write_text("not json")
    assert s.load() == {}


def test_session_without_state_dir():
    s = Session(None, "a")
    s.save({"x": 1})
    assert s.load() == {}
    s.clear()


def test_login_backoff(tmp_path):
    now = [1000.0]
    b = LoginBackoff(tmp_path, "https://fw", "omini", "pw", minutes=10, clock=lambda: now[0])
    b.check()
    b.refused("wrong password")
    with pytest.raises(PluginError, match=r"wrong password \(waiting a few minutes"):
        b.check()
    f = tmp_path / "login-refused.json"
    assert stat.S_IMODE(os.stat(f).st_mode) == 0o600
    # Other credentials are tried at once.
    LoginBackoff(tmp_path, "https://fw", "omini", "new", clock=lambda: now[0]).check()
    now[0] += 10 * 60
    assert b.waiting() is None
    b.check()
    b.refused("again")
    b.clear()
    assert not f.exists()
    b.check()


def test_login_backoff_without_state_dir():
    b = LoginBackoff(None, "x")
    b.refused("no")
    with pytest.raises(PluginError):
        b.check()
    b.clear()
    b.check()
