"""Fetch tests with mocked AKShare (no real network)."""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.names import reset_name_caches


def _client() -> TestClient:
    return TestClient(create_app())


def test_fetch_hk_stock_mocked() -> None:
    reset_name_caches()
    df = pd.DataFrame({"日期": ["2024-01-02", "2024-01-03"], "收盘": [300.0, 305.0]})
    spot = pd.DataFrame({"代码": ["00700"], "名称": ["腾讯控股"]})
    with patch("akshare.stock_hk_hist", return_value=df), patch("akshare.stock_hk_spot_em", return_value=spot):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "HK",
                "instrument_type": "hk_stock",
                "source_code": "00700",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["code"] == 0
        assert body["data"]["currency"] == "HKD"
        assert body["data"]["name"] == "腾讯控股"
        assert body["data"]["provider_symbol"] == "00700"
        assert body["data"]["expense_ratio_components"]["region"] == "foreign"


def test_fetch_hk_stock_normalizes_code_and_name() -> None:
    reset_name_caches()
    df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [300.0]})
    spot = pd.DataFrame({"代码": ["00700"], "名称": ["腾讯控股"]})
    with patch("akshare.stock_hk_hist", return_value=df), patch("akshare.stock_hk_spot_em", return_value=spot):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "HK",
                "instrument_type": "hk_stock",
                "source_code": "700",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["data"]["provider_symbol"] == "00700"
        assert body["data"]["name"] == "腾讯控股"


def test_fetch_hk_stock_none_adjust_maps_to_empty_string() -> None:
    reset_name_caches()
    df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [300.0]})
    spot = pd.DataFrame({"代码": ["00700"], "名称": ["腾讯控股"]})

    def _hist(*, symbol: str, period: str, start_date: str, end_date: str, adjust: str) -> pd.DataFrame:
        assert symbol == "00700"
        assert adjust == ""
        return df

    with patch("akshare.stock_hk_hist", side_effect=_hist), patch("akshare.stock_hk_spot_em", return_value=spot):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "HK",
                "instrument_type": "hk_stock",
                "source_code": "700",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
        assert response.status_code == 200
        assert response.json()["data"]["points"]


def test_fetch_hk_etf_mocked() -> None:
    reset_name_caches()
    df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [20.0]})
    spot = pd.DataFrame({"代码": ["02800"], "名称": ["盈富基金"]})
    with patch("akshare.stock_hk_hist", return_value=df), patch("akshare.stock_hk_spot_em", return_value=spot):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "HK",
                "instrument_type": "hk_etf",
                "source_code": "02800",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["code"] == 0
        assert body["data"]["currency"] == "HKD"
        assert body["data"]["name"] == "盈富基金"
        assert body["data"]["provider_symbol"] == "02800"


def test_fetch_cn_exchange_fund_mocked() -> None:
    df = pd.DataFrame({"日期": ["2024-01-02", "2024-01-03"], "收盘": [1.0, 1.1]})
    with patch("akshare.fund_etf_hist_em", return_value=df):
        client = _client()
        payload = {
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "510300",
            "start_date": None,
            "end_date": "2026-06-09",
            "adjust_policy": "qfq",
        }
        response = client.post("/v1/instruments/fetch", json=payload)
        assert response.status_code == 200
        body = response.json()
        assert body["code"] == 0
        assert len(body["data"]["points"]) == 2
        assert body["data"]["source_name"].startswith("ak.")


def test_fetch_cn_exchange_fund_resolves_display_name() -> None:
    df = pd.DataFrame({"日期": ["2024-01-02", "2024-01-03"], "收盘": [1.0, 1.1]})
    spot = pd.DataFrame({"代码": ["510300"], "名称": ["沪深300ETF华泰柏瑞"]})
    with patch("akshare.fund_etf_hist_em", return_value=df), patch(
        "akshare.fund_etf_spot_em", return_value=spot
    ):
        client = _client()
        payload = {
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "510300",
            "start_date": None,
            "end_date": "2026-06-09",
            "adjust_policy": "qfq",
        }
        response = client.post("/v1/instruments/fetch", json=payload)
        assert response.status_code == 200
        body = response.json()
        assert body["data"]["name"] == "沪深300ETF华泰柏瑞"


def test_fetch_fallback_second_source() -> None:
    df = pd.DataFrame({"date": ["2024-06-01"], "close": [2.5]})

    with patch("akshare.fund_etf_hist_em", side_effect=RuntimeError("primary failed")), patch(
        "akshare.stock_zh_a_hist_tx", return_value=df
    ), patch(
        "akshare.fund_etf_hist_sina",
        side_effect=AssertionError("sina should not run when tx succeeds"),
    ):
        client = _client()
        response = client.post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        assert response.json()["data"]["source_name"] == "ak.stock_zh_a_hist_tx"


def test_fetch_cn_stock_fallback_tx() -> None:
    df = pd.DataFrame(
        {
            "date": pd.to_datetime(["2024-06-01"]).date,
            "open": [10.0],
            "close": [10.5],
            "high": [11.0],
            "low": [9.5],
            "amount": [1000.0],
        }
    )
    with patch("akshare.stock_zh_a_hist", side_effect=RuntimeError("em blocked")), patch(
        "akshare.stock_zh_a_hist_tx", return_value=df
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "600519",
                "start_date": "2024-01-01",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["code"] == 0
        assert body["data"]["source_name"] == "ak.stock_zh_a_hist_tx"
        assert len(body["data"]["points"]) == 1


def test_fetch_cn_stock_fallback_sina() -> None:
    df = pd.DataFrame(
        {
            "date": pd.to_datetime(["2024-06-02"]).date,
            "open": [10.0],
            "high": [11.0],
            "low": [9.5],
            "close": [10.8],
            "volume": [100.0],
        }
    )
    with patch("akshare.stock_zh_a_hist", side_effect=RuntimeError("em blocked")), patch(
        "akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")
    ), patch("akshare.stock_zh_a_daily", return_value=df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_stock",
                "source_code": "000001",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["data"]["source_name"] == "ak.stock_zh_a_daily"


def test_fetch_cn_exchange_fund_qfq_skips_sina() -> None:
    df = pd.DataFrame({"日期": ["2024-06-01"], "收盘": [1.2]})
    with patch("akshare.fund_etf_hist_em", side_effect=RuntimeError("em blocked")), patch(
        "akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")
    ), patch(
        "akshare.fund_etf_hist_sina",
        side_effect=AssertionError("sina must be skipped for qfq cn_exchange_fund"),
    ), patch("akshare.fund_lof_hist_em", return_value=df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        assert response.json()["data"]["source_name"] == "ak.fund_lof_hist_em"


def test_fetch_cn_exchange_fund_third_source() -> None:
    df = pd.DataFrame({"日期": ["2024-06-01"], "收盘": [1.2]})
    with patch("akshare.fund_etf_hist_em", side_effect=RuntimeError("em blocked")), patch(
        "akshare.stock_zh_a_hist_tx", side_effect=RuntimeError("tx blocked")
    ), patch("akshare.fund_etf_hist_sina", side_effect=RuntimeError("sina blocked")
    ), patch("akshare.fund_lof_hist_em", return_value=df):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 200
        assert response.json()["data"]["source_name"] == "ak.fund_lof_hist_em"


def test_fetch_mutual_fund_money_fallback() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "每万份收益": [0.45],
            "7日年化收益率": [1.8],
            "申购状态": ["开放申购"],
            "赎回状态": ["开放赎回"],
        }
    )
    with patch("akshare.fund_open_fund_info_em", side_effect=RuntimeError("em blocked")), patch(
        "akshare.fund_money_fund_info_em", return_value=df
    ), patch("akshare.fund_financial_fund_info_em", side_effect=RuntimeError("skip")), patch(
        "akshare.fund_lof_hist_em", side_effect=RuntimeError("skip")
    ):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "000009",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
        assert response.status_code == 200
        body = response.json()
        assert body["code"] == 0
        assert body["data"]["source_name"] == "ak.fund_money_fund_info_em"
        assert body["data"]["asset_class"] == "cash"


def test_fetch_timeout_returns_provider_error_envelope() -> None:
    with patch("fireman_market_provider.adapters.fallback.call_with_timeout") as mock_timeout:
        mock_timeout.side_effect = TimeoutError()
        client = _client()
        response = client.post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "end_date": "2026-06-09",
                "adjust_policy": "qfq",
            },
        )
        assert response.status_code == 504


def test_mutual_fund_unsupported_classification() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [1.0],
            "基金简称": ["测试混合FOF"],
            "基金类型": ["混合型FOF"],
        }
    )
    with patch("akshare.fund_open_fund_info_em", return_value=df), patch(
        "akshare.fund_money_fund_info_em", side_effect=RuntimeError("skip")
    ), patch("akshare.fund_financial_fund_info_em", side_effect=RuntimeError("skip")), patch(
        "akshare.fund_lof_hist_em", side_effect=RuntimeError("skip")
    ):
        client = _client()
        response = client.post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "000001",
                "end_date": "2026-06-09",
                "adjust_policy": "none",
            },
        )
        body = response.json()
        assert body["code"] == 1
