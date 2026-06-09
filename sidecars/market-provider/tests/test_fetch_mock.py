"""Fetch tests with mocked AKShare (no real network)."""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app


def _client() -> TestClient:
    return TestClient(create_app())


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


def test_fetch_fallback_second_source() -> None:
    df = pd.DataFrame({"日期": ["2024-06-01"], "收盘": [2.5]})

    with patch("akshare.fund_etf_hist_em", side_effect=RuntimeError("primary failed")), patch(
        "akshare.fund_etf_hist_sina", return_value=df
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
        assert response.json()["data"]["source_name"] == "ak.fund_etf_hist_sina"


def test_fetch_timeout_returns_provider_error_envelope() -> None:
    with patch("fireman_market_provider.adapters.registry.call_with_timeout") as mock_timeout:
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
    with patch("akshare.fund_open_fund_info_em", return_value=df):
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
