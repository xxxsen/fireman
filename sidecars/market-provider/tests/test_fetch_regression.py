"""Regression integration tests for historically problematic fund fetch paths.

Resolve-path regressions were removed with the resolve endpoint
(task-worker refactor):
asset identity now comes from the market_assets directory, so cross-listing
ambiguity is handled at directory level, not per-fetch.
"""

from unittest.mock import patch

import pandas as pd

from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch

from .fetch_compat import fetch

# Known anomaly symbols — extend when new production bugs are fixed.
REGRESSION_FUNDS = {
    "510300": {
        "instrument_type": "cn_exchange_fund",
        "name": "沪深300ETF",
        "description": "bare code + adjust none broke primary ETF source",
    },
    "000001": {
        "instrument_type": "cn_mutual_fund",
        "name": "华夏成长混合",
        "description": "NAV-only akshare frame lacks fund name for classification",
    },
    "270042": {
        "instrument_type": "cn_exchange_fund",
        "name": "广发纳指100ETF联接（QDII）人民币A",
        "description": "QDII feeder fund missing from ETF/LOF spot tables; name from XQ basic info",
    },
}


def test_regression_510300_bare_code_none_adjust_hits_primary_etf_source() -> None:
    """510300: fund_etf_hist_em rejects adjust='none'; bare code must resolve to sh510300."""
    df = pd.DataFrame({"日期": ["2024-01-02", "2024-01-03"], "收盘": [4.5, 4.6]})
    captured: dict[str, str] = {}

    def _etf_hist(**kwargs: str) -> pd.DataFrame:
        captured["symbol"] = kwargs["symbol"]
        captured["adjust"] = kwargs["adjust"]
        return df

    with patch("akshare.fund_etf_hist_em", side_effect=_etf_hist):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )

    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert captured["symbol"] == "510300"
    assert captured["adjust"] == ""
    assert body["data"]["provider_symbol"] == "sh510300"
    assert body["data"]["source_name"] == "ak.fund_etf_hist_em"
    assert len(body["data"]["points"]) == 2


def test_regression_000001_open_fund_nav_frame_classifies_via_name_cache() -> None:
    """000001: akshare NAV history omits 基金简称; name cache must drive classification."""
    reset_name_caches()
    nav_df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02", "2024-01-03"],
            "单位净值": [1.0, 1.01],
            "日增长率": [0.0, 1.0],
        }
    )

    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value=REGRESSION_FUNDS["000001"]["name"],
    ), patch("akshare.fund_open_fund_info_em", return_value=nav_df):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "000001",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )

    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["asset_class"] == "equity"
    assert body["data"]["name"] == REGRESSION_FUNDS["000001"]["name"]
    assert body["data"]["source_kind"] == "open_fund"
    assert len(body["data"]["points"]) == 2


def test_regression_270042_fetch_open_fund_with_resolved_name() -> None:
    """270042: mutual fund fetch uses resolved_name when history frame has no name columns."""
    reset_name_caches()
    hist = pd.DataFrame({"净值日期": ["2024-01-02"], "单位净值": [1.23], "日增长率": [0.1]})
    clear_test_dispatch()
    register_test_dispatch(
        "fund_open_fund_info_em",
        lambda **_kwargs: hist,
    )

    response = fetch(
        {
            "market": "CN",
            "instrument_type": "cn_mutual_fund",
            "source_code": "270042",
            "resolved_name": REGRESSION_FUNDS["270042"]["name"],
            "adjust_policy": "none",
            "start_date": "2024-01-01",
            "end_date": "2024-12-31",
        }
    )

    assert response.status_code == 200
    data = response.json()["data"]
    assert data["name"] == REGRESSION_FUNDS["270042"]["name"]
    assert data["source_name"] in (
        "ak.fund_open_fund_info_em:单位净值走势",
        "ak.fund_open_fund_info_em:累计净值走势",
    )
