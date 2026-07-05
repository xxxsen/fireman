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
    "sh510300": {
        "instrument_type": "cn_exchange_fund",
        "name": "沪深300ETF",
        "description": "adjust none broke primary ETF source",
    },
    "000001": {
        "instrument_type": "cn_mutual_fund",
        "name": "华夏成长混合",
        "description": "NAV-only akshare frame lacks fund name metadata",
    },
    "007194": {
        "instrument_type": "cn_mutual_fund",
        "name": "长城短债A",
        "description": "short-bond fund was rejected by the provider classification gate",
    },
    "270042": {
        "instrument_type": "cn_exchange_fund",
        "name": "广发纳指100ETF联接（QDII）人民币A",
        "description": "QDII feeder fund missing from ETF/LOF spot tables; name from XQ basic info",
    },
}


def test_regression_510300_directory_code_none_adjust_hits_primary_etf_source() -> None:
    """sh510300: fund_etf_hist_em rejects adjust='none'; directory code drives the symbol."""
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
                "source_code": "sh510300",
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


def test_regression_cn_exchange_bare_code_is_identity_error_not_guess() -> None:
    """A bare CN on-exchange code must fail loudly, never fall back to sh/sz guessing."""
    df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [4.5]})
    with patch("akshare.fund_etf_hist_em", return_value=df):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )
    assert response.json()["code"] == 1


def test_regression_000001_open_fund_nav_frame_names_via_cache() -> None:
    """000001: akshare NAV history omits 基金简称; name cache provides the display name."""
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
    assert body["data"]["name"] == REGRESSION_FUNDS["000001"]["name"]
    assert body["data"]["source_kind"] == "open_fund"
    assert "asset_class" not in body["data"]
    assert len(body["data"]["points"]) == 2


def test_regression_007194_short_bond_fund_fetches_via_open_fund() -> None:
    """007194 长城短债A: history must fetch via the open-fund NAV sources.

    Before td/086, the provider classification gate rejected this frame as
    "unsupported fund classification" because '短债' matched no keyword.
    """
    reset_name_caches()
    nav_df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02", "2024-01-03"],
            "单位净值": [1.05, 1.051],
            "日增长率": [0.0, 0.1],
        }
    )

    with patch("akshare.fund_open_fund_info_em", return_value=nav_df):
        response = fetch(
            {
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "007194",
                "resolved_name": REGRESSION_FUNDS["007194"]["name"],
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            }
        )

    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["name"] == "长城短债A"
    assert body["data"]["source_name"].startswith("ak.fund_open_fund_info_em:")
    assert body["data"]["points"]
    assert "asset_class" not in body["data"]


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
