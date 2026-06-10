"""Elapsed-time tests for subprocess timeout wrapper."""

import time

import pytest

pytestmark = pytest.mark.subprocess

from fireman_market_provider.timeout_util import UpstreamCall, call_with_timeout


def test_timeout_returns_within_deadline() -> None:
    start = time.monotonic()
    with pytest.raises(TimeoutError):
        call_with_timeout(UpstreamCall("time.sleep", (2,)), 1)
    elapsed = time.monotonic() - start
    assert elapsed < 1.5
