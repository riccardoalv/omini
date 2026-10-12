import io
import json

import pytest

from omini_sdk import ConfigBackup, Device, Interface, PluginError
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
        if cfg.str("host") == "network":
            dhcp = {"enabled": True, "from": "10.0.30.100", "to": "10.0.30.200"}
            return [{"key": "fw", "name": "fw", "networks": [{"vlan": 30, "dhcp": dhcp}]}]
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


def test_fields_keep_their_schema_names(plugin):
    # "from" is a Python keyword: the model calls it from_, the JSON from.
    resp, code = plugin.handle(request(config={"host": "network"}))
    assert code == 0
    assert resp["devices"][0]["networks"][0]["dhcp"] == {
        "enabled": True,
        "from": "10.0.30.100",
        "to": "10.0.30.200",
    }


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


def apply_request(change):
    return json.dumps({"protocol": 1, "action": "apply", "config": {}, "change": change})


def test_apply(plugin):
    seen = []

    @plugin.apply
    def apply(cfg, change):
        seen.append(change)
        return [Device(key="sw", name="sw", interfaces=[Interface(name="Port 5", enabled=False)])]

    change = {"device": "sw", "kind": "port.enabled", "port": "Port 5", "value": False}
    resp, code = plugin.handle(apply_request(change))
    assert code == 0
    assert resp["devices"][0]["interfaces"] == [{"name": "Port 5", "enabled": False}]
    assert seen[0].kind == "port.enabled" and seen[0].port == "Port 5" and seen[0].value is False


def test_apply_needs_a_function_and_a_change(plugin):
    change = {"device": "sw", "kind": "port.enabled", "port": "Port 5", "value": False}
    resp, code = plugin.handle(apply_request(change))
    assert code == 1 and "no @plugin.apply" in resp["error"]
    plugin.apply(lambda cfg, change: [])
    resp, code = plugin.handle(apply_request(None))
    assert code == 1 and resp["error"] == "no change to apply"
    resp, code = plugin.handle(apply_request({**change, "kind": "port.mirror"}))
    assert code == 2


def test_run_writes_one_json_line(plugin):
    out = io.StringIO()
    with pytest.raises(SystemExit) as e:
        plugin.run(stdin=io.StringIO(request("test", {"host": "x"})), stdout=out)
    assert e.value.code == 0
    assert json.loads(out.getvalue()) == {"message": "Connected to x (tls=True)"}


def test_backup(plugin):
    resp, code = plugin.handle(request("backup", {"host": "x"}))
    assert code == 1 and "no @plugin.backup" in resp["error"]

    @plugin.backup
    def backup(cfg):
        return [
            ConfigBackup(device="fw", format="xml", content="<opnsense/>"),
            {"device": "sw", "format": "json", "content": "{}"},
        ]

    resp, code = plugin.handle(request("backup", {"host": "x"}))
    assert code == 0
    assert resp == {
        "backups": [
            {"device": "fw", "format": "xml", "content": "<opnsense/>"},
            {"device": "sw", "format": "json", "content": "{}"},
        ]
    }
