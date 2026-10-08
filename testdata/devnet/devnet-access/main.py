"""Devnet: access switches (devnet: simulated, read-only; see testdata/devnet/README.md)."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "_shared"))

import devnet

from omini_sdk import PluginError, plugin

PLUGIN = "devnet-access"


@plugin.collect
def collect(cfg):
    try:
        return devnet.devices(PLUGIN)
    except devnet.Unreachable as e:
        raise PluginError(str(e)) from e


@plugin.test
def test(cfg):
    try:
        found = devnet.devices(PLUGIN)
    except devnet.Unreachable as e:
        raise PluginError(str(e)) from e
    return f"Simulated: {len(found)} devices"


if __name__ == "__main__":
    plugin.run()
