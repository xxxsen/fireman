"""Elapsed-time tests for subprocess timeout wrapper."""

import time
from unittest.mock import patch

import pytest

from fireman_market_provider.timeout_util import UpstreamCall, call_with_timeout

pytestmark = pytest.mark.subprocess


def test_timeout_returns_within_deadline() -> None:
    start = time.monotonic()
    with pytest.raises(TimeoutError):
        call_with_timeout(UpstreamCall("time.sleep", (2,)), 1)
    elapsed = time.monotonic() - start
    assert elapsed < 1.5


def test_timeout_emits_log_timeout_event() -> None:
    with patch("fireman_market_provider.timeout_util.log_timeout_event") as mock_log:
        with pytest.raises(TimeoutError):
            call_with_timeout(UpstreamCall("time.sleep", (2,)), 1)
        mock_log.assert_called()
        kwargs = mock_log.call_args.kwargs
        assert kwargs["operation"] == "time.sleep"
        assert kwargs["layer"] == "sidecar"
        assert kwargs["remaining_ms"] == 0
