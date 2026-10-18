import pytest
from pydantic import ValidationError

from omini_sdk import Device, Interface


def test_device_roundtrip():
    device = Device(
        key="aa:bb:cc:00:00:01",
        name="core-switch",
        role="switch",
        macs=["aa:bb:cc:00:00:01"],
        interfaces=[Interface(name="ge1", up=True, speed_mbps=1000, rx_bytes=10)],
    )
    data = device.model_dump(mode="json", exclude_none=True)
    assert Device.model_validate(data) == device


def test_mac_must_be_normalized():
    with pytest.raises(ValidationError):
        Device(key="x", name="x", macs=["AA-BB-CC-00-00-01"])


def test_unknown_fields_are_rejected():
    with pytest.raises(ValidationError):
        Device(key="x", name="x", colour="blue")


def test_radio_and_port_forward_models_are_exported():
    from omini_sdk import PortForward, Radio, RadioChange

    d = Device(
        key="ap",
        name="ap",
        radios=[Radio(name="wifi0", band="2.4ghz", channel=6, width_mhz=20, auto=False)],
        port_forwards=[
            PortForward(
                protocol="tcp", external_port=443, internal_ip="192.168.1.10", internal_port=8443
            )
        ],
    )
    assert d.radios[0].channel == 6 and d.port_forwards[0].external_port == 443
    assert RadioChange(radio="wifi0", channel=11).channel == 11
