"""Regression tests for historical fetch fixes (td/034).

The resolve-path regressions were removed together with the resolve endpoint
(task-worker refactor); identity/type mismatch handling now happens at
directory level.
"""

from unittest.mock import patch

import pandas as pd

from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch

from .fetch_compat import fetch


def test_fetch_cn_exchange_fund_uses_resolved_name_without_name_upstream() -> None:
    """Fetch must not call name upstream when resolved_name is provided."""
    reset_name_caches()
    hist = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [4.5]})
    name_calls = {"count": 0}

    def _blocked_spot(*_args, **_kwargs) -> pd.DataFrame:
        name_calls["count"] += 1
        raise RuntimeError("name upstream blocked")

    with patch("akshare.fund_etf_hist_em", return_value=hist), patch(
        "akshare.fund_etf_spot_em", side_effect=_blocked_spot
    ), patch("akshare.stock_hk_spot_em", side_effect=_blocked_spot):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "resolved_name": "沪深300ETF",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )

    assert response.status_code == 200
    assert response.json()["data"]["name"] == "沪深300ETF"
    assert name_calls["count"] == 0


def test_fetch_mutual_fund_lof_name_uses_open_fund_not_lof_hist() -> None:
    """Offshore fund names containing LOF must stay on open fund fetch path."""
    reset_name_caches()
    nav = pd.DataFrame({"净值日期": ["2024-01-02"], "单位净值": [1.01], "日增长率": [0.1]})
    lof_calls = {"count": 0}

    def _blocked_lof_hist(*_args, **_kwargs) -> pd.DataFrame:
        lof_calls["count"] += 1
        raise RuntimeError("fund_lof_hist_em blocked")

    clear_test_dispatch()
    register_test_dispatch("fund_open_fund_info_em", lambda **_kwargs: nav)

    with patch("akshare.fund_lof_hist_em", side_effect=_blocked_lof_hist):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "270042",
                "resolved_name": "广发纳指100ETF联接（QDII）人民币A",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )

    assert response.status_code == 200
    data = response.json()["data"]
    assert data["name"] == "广发纳指100ETF联接（QDII）人民币A"
    assert data["source_kind"] == "open_fund"
    assert lof_calls["count"] == 0
