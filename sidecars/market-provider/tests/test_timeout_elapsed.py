"""Elapsed-time tests for subprocess timeout wrapper."""

import time

import pytest

from fireman_market_provider.timeout_util import call_with_timeout


def _sleep_two_seconds() -> None:
    time.sleep(2)


def test_timeout_returns_within_deadline(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_DISABLE_SUBPROCESS", "0")
    start = time.monotonic()
    with pytest.raises(TimeoutError):
        call_with_timeout(_sleep_two_seconds, 1)
    elapsed = time.monotonic() - start
    assert elapsed < 1.5
