"""Identity-consistent fetch source selection."""

from __future__ import annotations

from unittest.mock import patch

import pandas as pd

from fireman_market_provider.adapters.names import reset_name_caches

from .fetch_compat import fetch


def _fetch(kind: str | None):
    payload = {
        "market": "CN",
        "instrument_type": "cn_exchange_fund",
        "source_code": "sh510300",
        "end_date": "2026-06-09",
        "adjust_policy": "qfq",
    }
    if kind is not None:
        payload["instrument_kind"] = kind
    return fetch(payload)


def test_etf_kind_never_falls_back_to_lof_source() -> None:
    """An ETF must not silently receive LOF history for the same bare code."""
    reset_name_caches()
    lof_calls = {"count": 0}

    def _lof(**_kwargs) -> pd.DataFrame:
        lof_calls["count"] += 1
        return pd.DataFrame({"日期": ["2024-06-01"], "收盘": [9.99]})

    empty = pd.DataFrame({"日期": [], "收盘": []})
    with patch("akshare.fund_etf_hist_em", return_value=empty), patch(
        "akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")
    ), patch("akshare.fund_lof_hist_em", side_effect=_lof), patch(
        "akshare.fund_etf_fund_info_em", side_effect=RuntimeError("info blocked")
    ):
        response = _fetch("index_etf")

    # ETF sources exhausted -> fail loudly; LOF data must never be substituted.
    assert response.status_code == 503
    assert response.json()["error_code"] == "market_provider_unavailable"
    assert lof_calls["count"] == 0


def test_lof_kind_uses_lof_source_not_etf() -> None:
    reset_name_caches()
    lof_df = pd.DataFrame({"日期": ["2024-06-01"], "收盘": [1.23]})
    with patch(
        "akshare.fund_etf_hist_em",
        side_effect=AssertionError("ETF source must not run for a LOF"),
    ), patch("akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")), patch(
        "akshare.fund_lof_hist_em", return_value=lof_df
    ):
        response = _fetch("lof")

    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.fund_lof_hist_em"


def test_absent_kind_keeps_legacy_full_chain() -> None:
    """Refresh path (no kind) preserves the legacy ETF->...->LOF fallback chain."""
    reset_name_caches()
    lof_df = pd.DataFrame({"日期": ["2024-06-01"], "收盘": [1.23]})
    empty = pd.DataFrame({"日期": [], "收盘": []})
    with patch("akshare.fund_etf_hist_em", return_value=empty), patch(
        "akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")
    ), patch("akshare.fund_lof_hist_em", return_value=lof_df):
        response = _fetch(None)

    assert response.status_code == 200
    assert response.json()["data"]["source_name"] == "ak.fund_lof_hist_em"
