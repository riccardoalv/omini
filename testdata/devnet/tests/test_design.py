"""DESIGN.md and acme.py describe the same network: the tables of the document match the code."""

from __future__ import annotations

from pathlib import Path

from emulator import acme

DESIGN = (Path(__file__).resolve().parent.parent / "DESIGN.md").read_text()


def table(title_cell: str) -> list[list[str]]:
    """Rows of the first markdown table whose header starts with title_cell."""
    lines = DESIGN.splitlines()
    for i, line in enumerate(lines):
        if line.startswith(f"| {title_cell} |"):
            rows = []
            for row in lines[i + 2 :]:
                if not row.startswith("|"):
                    break
                rows.append([c.strip() for c in row.strip("|").split("|")])
            return rows
    raise AssertionError(f"no table starting with {title_cell}")


def test_vlan_table_matches():
    rows = table("VLAN")
    assert len(rows) == len(acme.HQ_VLANS)
    for row, (vid, name, subnet, routed, pool, use) in zip(rows, acme.HQ_VLANS, strict=True):
        assert int(row[0]) == vid
        assert row[1] == name
        assert row[2] == (subnet or "—")
        assert row[3] == ("yes" if routed else "no")
        assert row[4] == ("–".join(pool) if pool else "—")
        assert row[5] == use


def test_api_table_matches():
    rows = {r[0]: r for r in table("API")}
    assert set(rows) == set(acme.APIS)
    for name, spec in acme.APIS.items():
        kind, addr = rows[name][1], rows[name][2]
        assert kind == spec["kind"]
        if kind == "snmp":
            assert addr == f"udp://{spec['ip']}:{spec['port']}"
        else:
            scheme = "https" if spec["tls"] else "http"
            default = 443 if spec["tls"] else 80
            assert addr == f"{scheme}://{spec['ip']}" + ("" if spec["port"] == default else f":{spec['port']}")
