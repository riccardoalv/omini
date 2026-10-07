import io
import json

import pytest

from omini_sdk import Device, Interface, PluginError
from omini_sdk.plugin import Plugin


def request(action="collect", config=None, protocol=1, state_dir="/tmp/state"):
    return json.dumps(
        {"protocol": protocol, "action": action, "config": config or {}, "state_dir": state_dir}
    )


@pytest.fixture
def plugin():
    p = Plugin()

    @p.collect
    def collect(cfg):
        if cfg.str("host") == "down":
            raise PluginError("host unreachable")
        if cfg.str("host") == "bug":
            raise KeyError("oops")
        if cfg.str("host") == "dict":
            return [{"key": "fw", "name": "fw", "role": "firewall"}]
        if cfg.str("host") == "invalid":
            return [{"key": "fw", "name": "fw", "macs": ["AA-BB"]}]
        return [
            Device(
                key="fw",
                name=cfg.str("host"),
                interfaces=[Interface(name="igb0", speed_mbps=1000, media="1000baseT")],
            )
        ]

    @p.test
    def test(cfg):
        assert cfg.state_dir is not None
        return f"Connected to {cfg.str('host')} (tls={cfg.bool('verify_tls', True)})"

    return p


def test_collect(plugin):
    resp, code = plugin.handle(request(config={"host": "fw.lan"}))
    assert code == 0
    assert resp == {
        "devices": [
            {
                "key": "fw",
                "name": "fw.lan",
                "interfaces": [{"name": "igb0", "speed_mbps": 1000, "media": "1000baseT"}],
            }
        ]
    }


def test_dicts_are_validated(plugin):
    resp, code = plugin.handle(request(config={"host": "dict"}))
    assert code == 0 and resp["devices"][0]["role"] == "firewall"
    resp, code = plugin.handle(request(config={"host": "invalid"}))
    assert code == 1 and resp["error"].startswith("the plugin returned invalid data")


def test_test_action(plugin):
    resp, code = plugin.handle(request("test", {"host": "fw.lan", "verify_tls": False}))
    assert (resp, code) == ({"message": "Connected to fw.lan (tls=False)"}, 0)


def test_errors_for_the_user(plugin):
    assert plugin.handle(request(config={"host": "down"})) == ({"error": "host unreachable"}, 1)


def test_unexpected_errors_are_reported(plugin, capsys):
    resp, code = plugin.handle(request(config={"host": "bug"}))
    assert code == 1 and resp["error"] == "unexpected error: KeyError: 'oops'"
    assert "Traceback" in capsys.readouterr().err


def test_bad_requests(plugin):
    assert plugin.handle("not json")[1] == 2
    resp, code = plugin.handle(request(protocol=99))
    assert code == 2 and "protocol 99" in resp["error"]


def test_missing_functions():
    resp, code = Plugin().handle(request())
    assert code == 1 and "no @plugin.collect" in resp["error"]


def test_run_writes_one_json_line(plugin):
    out = io.StringIO()
    with pytest.raises(SystemExit) as e:
        plugin.run(stdin=io.StringIO(request("test", {"host": "x"})), stdout=out)
    assert e.value.code == 0
    assert json.loads(out.getvalue()) == {"message": "Connected to x (tls=True)"}
