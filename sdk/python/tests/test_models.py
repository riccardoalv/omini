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
