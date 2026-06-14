"""Regression integration tests for historically problematic fund fetch/resolve paths."""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches

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
    "000510": {
        "instrument_type": "cn_exchange_fund",
        "etf_name": "中证A500",
        "stock_name": "新金路",
        "description": "cross-listed ETF/stock must stay ambiguous for user choice",
    },
}


def _client() -> TestClient:
    return TestClient(create_app())


def test_regression_510300_bare_code_none_adjust_hits_primary_etf_source() -> None:
    """510300: fund_etf_hist_em rejects adjust='none'; bare code must resolve to sh510300."""
    df = pd.DataFrame({"日期": ["2024-01-02", "2024-01-03"], "收盘": [4.5, 4.6]})
    captured: dict[str, str] = {}

    def _etf_hist(**kwargs: str) -> pd.DataFrame:
        captured["symbol"] = kwargs["symbol"]
        captured["adjust"] = kwargs["adjust"]
        return df

    with patch("akshare.fund_etf_hist_em", side_effect=_etf_hist):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            },
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
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name",
        return_value=REGRESSION_FUNDS["000001"]["name"],
    ), patch("akshare.fund_open_fund_info_em", return_value=nav_df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "000001",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            },
        )

    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["asset_class"] == "equity"
    assert body["data"]["name"] == REGRESSION_FUNDS["000001"]["name"]
    assert body["data"]["source_kind"] == "open_fund"
    assert len(body["data"]["points"]) == 2


def test_regression_000510_cross_listed_ambiguous_with_full_spot_maps() -> None:
    """000510: 中证A500 (SH ETF) vs 新金路 (stock) must not auto-resolve to a single candidate."""
    reset_name_caches()
    reset_cn_code_caches()
    etf = pd.DataFrame({"代码": ["000510"], "名称": [REGRESSION_FUNDS["000510"]["etf_name"]]})
    lof = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": ["000510"], "名称": [REGRESSION_FUNDS["000510"]["stock_name"]]})
    with patch("akshare.fund_etf_spot_em", return_value=etf), patch(
        "akshare.fund_lof_spot_em", return_value=lof
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "000510",
            },
        )

    assert response.status_code == 200
    body = response.json()["data"]
    assert body["ambiguous"] is True
    assert body.get("resolved") is None
    candidates = body["candidates"]
    by_code = {c["code"]: c for c in candidates}
    assert "sh000510" in by_code
    assert "sz000510" in by_code
    assert by_code["sh000510"]["instrument_kind"] == "index_etf"
    assert by_code["sh000510"]["name"] == REGRESSION_FUNDS["000510"]["etf_name"]
    assert by_code["sz000510"]["instrument_kind"] == "stock"
    assert by_code["sz000510"]["name"] == REGRESSION_FUNDS["000510"]["stock_name"]


def test_regression_000510_etf_spot_timeout_still_ambiguous() -> None:
    """000510: when ETF spot times out, user must still choose ETF vs stock — not a single wrong hit."""
    reset_name_caches()
    reset_cn_code_caches()
    empty = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": ["000510"], "名称": [REGRESSION_FUNDS["000510"]["stock_name"]]})
    with patch("akshare.fund_etf_spot_em", side_effect=TimeoutError("spot slow")), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "000510",
            },
        )

    assert response.status_code == 200
    body = response.json()["data"]
    assert body["ambiguous"] is True
    assert body.get("resolved") is None
    codes = {c["code"] for c in body["candidates"]}
    assert "sh000510" in codes
    assert "sz000510" in codes


def test_regression_000510_all_spot_timeouts_still_ambiguous() -> None:
    """000510: when all spot tables time out, fallback must still return ETF vs stock quickly."""
    from fireman_market_provider.adapters.resolve import _SpotMaps

    reset_name_caches()
    reset_cn_code_caches()
    empty_failed = _SpotMaps(etf={}, lof={}, stock={}, load_failed=True)
    with patch(
        "fireman_market_provider.adapters.resolve._load_cn_exchange_spot_maps",
        return_value=empty_failed,
    ):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "000510",
            },
        )

    assert response.status_code == 200
    body = response.json()["data"]
    assert body["ambiguous"] is True
    codes = {c["code"] for c in body["candidates"]}
    assert "sh000510" in codes
    assert "sz000510" in codes
