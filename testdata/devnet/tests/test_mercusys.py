"""The Mercusys Halo mesh emulator against the real omini-plugin-mercusys."""

from __future__ import annotations

import base64
import hashlib
import json
import ssl
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any
from urllib.parse import quote_plus

import pytest
from cryptography.hazmat.primitives.asymmetric import padding as rsa_padding
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPublicNumbers

from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.vendors import mercusys
from emulator.world import at_local

UNITS = ("halo-wh-01", "halo-wh-02", "halo-wh-03")
TUE_1030 = at_local(1, 10, 30)
WED_0401 = at_local(2, 4, 1)  # halo-wh-02 reboots every day at 04:00 for 2 minutes

needs_plugin = pytest.mark.skipif(plugin_dir("mercusys") is None, reason="omini-plugin-mercusys not found")


def by_key(out: dict[str, Any]) -> dict[str, dict[str, Any]]:
    assert "error" not in out, out.get("error")
    return {d["key"]: d for d in out["devices"]}


@needs_plugin
def test_mesh_units_and_clients_on_tuesday_morning(tmp_path: Path) -> None:
    with Emulator(at=TUE_1030, apis=["mercusys"]) as emu:
        out = emu.collect("mercusys", state_dir=tmp_path)
        snap = emu.world.at(TUE_1030)
        devices = by_key(out)
        nodes = {u: emu.world.nodes[u] for u in UNITS}
        # Three units, one access point each, named as in the Mercusys app.
        assert set(devices) == {n.mac for n in nodes.values()}
        main, wired, meshed = (devices[nodes[u].mac] for u in UNITS)
        assert [d["name"] for d in (main, wired, meshed)] == ["Galpão Doca", "Galpão Estoque", "Galpão Fundos"]
        for d, u in zip((main, wired, meshed), UNITS, strict=True):
            assert (d["role"], d["vendor"], d["model"]) == ("ap", "Mercusys", "Halo H80X")
            assert d["os_version"] == "1.2.0 Build 20240913 Rel. 47562"
            assert d["host"] == nodes[u].ip and d["ips"] == [nodes[u].ip]
        assert [main["host"], wired["host"], meshed["host"]] == ["10.10.34.10", "10.10.34.11", "10.10.34.12"]
        # Backhaul: the wired satellite is found through the switch; the meshed one hangs from halo-wh-02 on 5 GHz.
        assert "neighbors" not in main and "neighbors" not in wired
        [link] = meshed["neighbors"]
        assert (link["remote_mac"], link["remote_port"], link["protocol"]) == (
            nodes["halo-wh-02"].mac,
            "5 GHz",
            "other",
        )
        units = json.loads((tmp_path / "pages" / "device_list.json").read_text())
        assert [(u["role"], u.get("connection_type"), u["group_status"]) for u in units] == [
            ("master", None, "connected"),
            ("slave", ["wired"], "connected"),
            ("slave", ["band5"], "connected"),
        ]
        assert units[2]["previous"] == nodes["halo-wh-02"].mac.upper().replace(":", "-")

        # Every Wi-Fi client of each unit, as the world has them now.
        for u in UNITS:
            want = {a.client.mac: a for a in snap.wifi_clients(u)}
            got = {w["mac"]: w for w in devices[nodes[u].mac].get("wireless_clients") or []}
            assert set(got) == set(want), u
            for m, w in got.items():
                a = want[m]
                assert w["ssid"] == "Acme-Galpao"
                assert w["band"] == {"2.4": "2.4ghz", "5": "5ghz"}[a.band]
                assert w["interface"] == f"Acme-Galpao · {'2.4' if a.band == '2.4' else '5'} GHz"
                # Current traffic in bits/s; the units report no link rate.
                assert w["rx_bps"] == int(a.tx_rate_now) * 8 and w["tx_bps"] == int(a.rx_rate_now) * 8
                assert "tx_rate_mbps" not in w and "signal_dbm" not in w
        # The warehouse handhelds, two per unit, on 2.4 GHz and busy.
        handhelds = {
            w["mac"]: (d["key"], w)
            for d in devices.values()
            for w in d.get("wireless_clients") or []
            if w["mac"].startswith("4c:cc:34")
        }
        assert len(handhelds) == 6
        for k in range(6):
            n = emu.world.nodes[f"tc52-{k + 1:02d}"]
            unit, w = handhelds[n.mac]
            assert unit == nodes[UNITS[k % 3]].mac
            assert w["band"] == "2.4ghz" and w["rx_bps"] > 0 and w["tx_bps"] > 0
        hosts = {h["mac"]: h for d in devices.values() for h in d.get("hosts") or []}
        tc01 = emu.world.nodes["tc52-01"]
        assert hosts[tc01.mac]["hostnames"] == ["TC52-01"] and hosts[tc01.mac]["ip"] == tc01.ip
        assert all(h["ip"].startswith("10.10.34.") for h in hosts.values())

        # The Wi-Fi passwords travel in the wlan answer: never in the plugin's output or its state.
        secrets_ = [mercusys.WIFI_PASSWORD, mercusys.GUEST_PASSWORD]
        secrets_ += [base64.b64encode(p.encode()).decode() for p in secrets_]
        texts = [json.dumps(out)] + [f.read_text() for f in tmp_path.rglob("*") if f.is_file()]
        assert all(s not in t for s in secrets_ for t in texts)
        assert emu.config("mercusys")["password"] not in "".join(texts)

        # The session is kept in the state folder and reused: no new login.
        again = emu.collect("mercusys", state_dir=tmp_path)
        assert len(by_key(again)) == 3 and "form=login" not in again["_stderr"]


@needs_plugin
def test_satellite_reboot_takes_the_meshed_unit_offline(tmp_path: Path) -> None:
    with Emulator(at=WED_0401, apis=["mercusys"]) as emu:
        devices = by_key(emu.collect("mercusys", state_dir=tmp_path))
        assert len(devices) == 3  # offline units are still listed by the main unit
        units = json.loads((tmp_path / "pages" / "device_list.json").read_text())
        status = {u["device_ip"]: (u["group_status"], u["inet_status"]) for u in units}
        assert status == {
            "10.10.34.10": ("connected", "online"),
            "10.10.34.11": ("disconnected", "offline"),
            "10.10.34.12": ("disconnected", "offline"),
        }
        for u in ("halo-wh-02", "halo-wh-03"):
            assert not devices[emu.world.nodes[u].mac].get("wireless_clients")


@needs_plugin
def test_wrong_password_is_refused(tmp_path: Path) -> None:
    with Emulator(at=TUE_1030, apis=["mercusys"]) as emu:
        cfg = dict(emu.config("mercusys"), password="not-the-password")
        out = run_plugin("mercusys", cfg, state_dir=tmp_path)
        assert "wrong password (9 attempts left" in out["error"]
        # The plugin waits before trying again: the unit is not asked a second time.
        out = run_plugin("mercusys", cfg, state_dir=tmp_path)
        assert "waiting a few minutes" in out["error"] and "form=login" not in out["_stderr"]


# --------------------------------------------------------- protocol details


class Raw:
    """A minimal client of the unit's protocol, to check what the plugin never does (lockout, bad requests)."""

    def __init__(self, url: str) -> None:
        self.url = url
        self.ctx = ssl.create_default_context()
        self.ctx.check_hostname = False
        self.ctx.verify_mode = ssl.CERT_NONE

    def post(self, path: str, body: str, ctype: str | None = "application/json") -> tuple[int, Any]:
        req = urllib.request.Request(self.url + path, data=body.encode(), method="POST")
        if ctype:
            req.add_header("Content-Type", ctype)
        try:
            with urllib.request.urlopen(req, context=self.ctx, timeout=10) as r:
                return r.status, json.loads(r.read())
        except urllib.error.HTTPError as e:
            return e.code, None

    @staticmethod
    def rsa(key: list[str], plain: bytes) -> str:
        n, e = (int(x, 16) for x in key)
        pub = RSAPublicNumbers(e, n).public_key()
        step = (n.bit_length() + 7) // 8 - 11
        return "".join(
            pub.encrypt(plain[i : i + step], rsa_padding.PKCS1v15()).hex() for i in range(0, len(plain), step)
        )

    def login(self, password: str) -> dict[str, Any]:
        _, keys = self.post("/cgi-bin/luci/;stok=/login?form=keys", '{"operation":"read"}')
        _, auth = self.post("/cgi-bin/luci/;stok=/login?form=auth", '{"operation":"read"}')
        key, iv = "1234567890123456", "6543210987654321"
        payload = {
            "params": {"password": self.rsa(keys["result"]["password"], password.encode())},
            "operation": "login",
        }
        data = base64.b64encode(mercusys.aes_encrypt(key, iv, json.dumps(payload).encode())).decode()
        h = hashlib.md5(f"admin{password}".encode()).hexdigest()
        sign = self.rsa(auth["result"]["key"], f"k={key}&i={iv}&h={h}&s={auth['result']['seq'] + len(data)}".encode())
        _, answer = self.post("/cgi-bin/luci/;stok=/login?form=login", f"sign={sign}&data={quote_plus(data)}")
        return json.loads(mercusys.aes_decrypt(key, iv, base64.b64decode(answer["data"])))


def test_protocol_title_callbacks_and_lockout() -> None:
    with Emulator(at=TUE_1030, apis=["mercusys"]) as emu:
        raw = Raw(emu.urls["mercusys"])
        with urllib.request.urlopen(raw.url + "/", context=raw.ctx, timeout=10) as r:
            assert "<title>MERCUSYS</title>" in r.read().decode()
        # Without the JSON content type the unit does not find the callback.
        assert raw.post("/cgi-bin/luci/;stok=/login?form=keys", '{"operation":"read"}', None) == (
            200,
            {"error_code": 1, "msg": "no such callback"},
        )
        # A read without a session is refused.
        assert raw.post("/cgi-bin/luci/;stok=deadbeef/admin/device?form=device_list", "sign=00&data=x")[0] == 403
        # Wrong passwords count down, then the login locks (even for the right password).
        good = emu.config("mercusys")["password"]
        left = [raw.login("wrong")["result"]["attemptsAllowed"] for _ in range(mercusys.MAX_ATTEMPTS - 1)]
        assert left == list(range(mercusys.MAX_ATTEMPTS - 1, 0, -1))
        assert raw.login("wrong")["error_code"] == -5003
        assert raw.login(good)["error_code"] == -5003


def test_right_password_signs_in() -> None:
    with Emulator(at=TUE_1030, apis=["mercusys"]) as emu:
        answer = Raw(emu.urls["mercusys"]).login(emu.config("mercusys")["password"])
        assert answer["error_code"] == 0 and len(answer["result"]["stok"]) == 32
