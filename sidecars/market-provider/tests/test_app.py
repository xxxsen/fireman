"""HTTP contract tests for the market provider."""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app


def test_healthz_returns_ok() -> None:
    client = TestClient(create_app())
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_fetch_response_shape_matches_design() -> None:
    df = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.0]})
    with patch("akshare.fund_etf_hist_em", return_value=df):
        client = TestClient(create_app())
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
        assert isinstance(body["message"], str)
        data = body["data"]
        assert set(data.keys()) == {
            "provider",
            "provider_symbol",
            "name",
            "asset_class",
            "currency",
            "point_type",
            "expense_ratio_status",
            "expense_ratio_components",
            "points",
            "source_name",
            "source_quality",
        }
        assert data["provider"] == "akshare"
        assert data["provider_symbol"] == "510300"
        assert len(data["points"]) == 1


def test_fetch_rejects_unknown_fields() -> None:
    client = TestClient(create_app())
    payload = {
        "market": "CN",
        "instrument_type": "cn_exchange_fund",
        "source_code": "510300",
        "end_date": "2026-06-09",
        "expense_ratio": 0.5,
    }
    response = client.post("/v1/instruments/fetch", json=payload)
    assert response.status_code == 422
