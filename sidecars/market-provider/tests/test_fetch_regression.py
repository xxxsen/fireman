"""Regression integration tests for historically problematic fund fetch/resolve paths."""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch

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
    "270042": {
        "instrument_type": "cn_exchange_fund",
        "name": "广发纳指100ETF联接（QDII）人民币A",
        "description": "QDII feeder fund missing from ETF/LOF spot tables; name from XQ basic info",
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
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
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
    index = pd.DataFrame(
        {"index_code": ["000510"], "display_name": [REGRESSION_FUNDS["000510"]["etf_name"]], "publish_date": ["2005-01-04"]}
    )
    with patch("akshare.fund_etf_spot_em", side_effect=TimeoutError("spot slow")), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock), patch(
        "akshare.index_stock_info", return_value=index
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
    assert body.get("resolved") is None
    by_code = {c["code"]: c for c in body["candidates"]}
    assert "sh000510" in by_code
    assert "sz000510" in by_code
    assert by_code["sh000510"]["name"] == REGRESSION_FUNDS["000510"]["etf_name"]
    assert by_code["sz000510"]["name"] == REGRESSION_FUNDS["000510"]["stock_name"]


def _xq_name_frame(name: str) -> pd.DataFrame:
    return pd.DataFrame({"item": ["基金代码", "基金名称"], "value": ["000000", name]})


def test_regression_270042_exchange_fund_type_mismatch() -> None:
    """270042: bare code on cn_exchange_fund must suggest cn_mutual_fund, not LOF."""
    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()
    empty = pd.DataFrame({"代码": [], "名称": []})
    xq = _xq_name_frame(REGRESSION_FUNDS["270042"]["name"])
    register_test_dispatch(
        "fund_individual_basic_info_xq",
        lambda **_kwargs: xq,
    )
    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.stock_zh_a_spot_em", return_value=empty):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "270042",
            },
        )

    assert response.status_code == 400
    assert response.json()["error_code"] == "instrument_type_mismatch"


def test_regression_270042_mutual_fund_resolves_with_name() -> None:
    """270042: cn_mutual_fund resolve returns bare code and real name via XQ."""
    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()
    xq = _xq_name_frame(REGRESSION_FUNDS["270042"]["name"])
    register_test_dispatch(
        "fund_individual_basic_info_xq",
        lambda **_kwargs: xq,
    )
    response = _client().post(
        "/v1/instruments/resolve",
        json={
            "market": "CN",
            "instrument_type": "cn_mutual_fund",
            "code": "270042",
        },
    )

    assert response.status_code == 200
    body = response.json()["data"]
    assert body["ambiguous"] is False
    resolved = body["resolved"]
    assert resolved is not None
    assert resolved["code"] == "270042"
    assert resolved["provider_symbol"] == "270042"
    assert resolved["name"] == REGRESSION_FUNDS["270042"]["name"]
    assert resolved["instrument_kind"] == "mutual_fund"


def test_regression_270042_fetch_open_fund_with_resolved_name() -> None:
    """270042: mutual fund fetch uses resolved_name when history frame has no name columns."""
    reset_name_caches()
    hist = pd.DataFrame({"净值日期": ["2024-01-02"], "单位净值": [1.23], "日增长率": [0.1]})
    clear_test_dispatch()
    register_test_dispatch(
        "fund_open_fund_info_em",
        lambda **_kwargs: hist,
    )

    response = _client().post(
        "/v1/instruments/fetch",
        json={
            "market": "CN",
            "instrument_type": "cn_mutual_fund",
            "source_code": "270042",
            "resolved_name": REGRESSION_FUNDS["270042"]["name"],
            "adjust_policy": "none",
            "start_date": "2024-01-01",
            "end_date": "2024-12-31",
        },
    )

    assert response.status_code == 200
    data = response.json()["data"]
    assert data["name"] == REGRESSION_FUNDS["270042"]["name"]
    assert data["source_name"] in (
        "ak.fund_open_fund_info_em:单位净值走势",
        "ak.fund_open_fund_info_em:累计净值走势",
    )


def test_regression_000510_all_spot_timeouts_still_ambiguous() -> None:
    """000510: when all spot tables time out, cross-list fallback still returns ETF vs stock."""
    from fireman_market_provider.adapters.resolve import _SpotMaps

    reset_name_caches()
    reset_cn_code_caches()
    stock_name = REGRESSION_FUNDS["000510"]["stock_name"]
    empty_failed = _SpotMaps(
        etf={},
        lof={},
        stock={"000510": stock_name},
        load_failed=True,
    )
    with patch(
        "fireman_market_provider.adapters.resolve._load_cn_exchange_spot_maps",
        return_value=empty_failed,
    ), patch(
        "fireman_market_provider.adapters.resolve.lookup_cross_listed_etf_name",
        return_value=REGRESSION_FUNDS["000510"]["etf_name"],
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
    by_code = {c["code"]: c for c in body["candidates"]}
    assert "sh000510" in by_code
    assert "sz000510" in by_code
    assert by_code["sh000510"]["name"] == REGRESSION_FUNDS["000510"]["etf_name"]
    assert by_code["sz000510"]["name"] == REGRESSION_FUNDS["000510"]["stock_name"]
