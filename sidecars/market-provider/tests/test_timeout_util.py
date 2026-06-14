"""Tests for MARKET_PROVIDER_* timeout configuration."""

import pandas as pd
from unittest.mock import patch

from fireman_market_provider.adapters.fallback import try_sources
from fireman_market_provider.timeout_util import UpstreamCall, clear_test_dispatch, register_test_dispatch
from fireman_market_provider.timeout_util import (
    DEFAULT_FETCH_TIMEOUT_SECONDS,
    DEFAULT_FETCH_UPSTREAM_TIMEOUT_SECONDS,
    DEFAULT_RESOLVE_TIMEOUT_SECONDS,
    fetch_timeout_seconds,
    fetch_upstream_timeout_seconds,
    resolve_timeout_seconds,
)


def test_fetch_upstream_timeout_caps_at_180() -> None:
    assert fetch_upstream_timeout_seconds(240) == 180
    assert fetch_upstream_timeout_seconds(30) == 30


def test_fetch_timeout_default(monkeypatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_FETCH_TIMEOUT", raising=False)
    assert fetch_timeout_seconds() == DEFAULT_FETCH_TIMEOUT_SECONDS
    assert fetch_timeout_seconds() == 240


def test_fetch_timeout_from_env(monkeypatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_FETCH_TIMEOUT", "240")
    assert fetch_timeout_seconds() == 240


def test_resolve_timeout_default(monkeypatch) -> None:
    monkeypatch.delenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", raising=False)
    assert resolve_timeout_seconds() == DEFAULT_RESOLVE_TIMEOUT_SECONDS
    assert resolve_timeout_seconds() == 60


def test_resolve_timeout_from_env(monkeypatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", "15")
    assert resolve_timeout_seconds() == 15


def test_fetch_path_uses_fetch_timeout(monkeypatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_FETCH_TIMEOUT", "200")
    df = pd.DataFrame({"date": ["2024-01-01"], "close": [1.0]})
    clear_test_dispatch()
    register_test_dispatch("primary_df", lambda: df)
    with patch("fireman_market_provider.adapters.fallback.call_with_timeout") as mock_timeout:
        mock_timeout.return_value = df
        result, name = try_sources("demo", [("primary", UpstreamCall("primary_df"))])
        assert name == "primary"
        assert len(result) == 1
        mock_timeout.assert_called_once()
        assert mock_timeout.call_args.args[1] == 180
