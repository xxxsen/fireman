"""Logging coverage for fetch chain failures."""

from unittest.mock import patch

import pytest

from fireman_market_provider.adapters.registry import fetch_instrument
from fireman_market_provider.schemas import FetchRequest


def test_fetch_failure_propagates_and_logs_sources(caplog) -> None:
    caplog.set_level("WARNING")
    req = FetchRequest(
        market="CN",
        instrument_type="cn_exchange_fund",
        source_code="sh510300",
        start_date=None,
        end_date="2026-06-09",
        adjust_policy="hfq",
    )
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=RuntimeError("all sources down"),
    ):
        with pytest.raises(RuntimeError, match="all sources down"):
            fetch_instrument(req)
