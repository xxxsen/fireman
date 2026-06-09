"""Logging coverage for fetch failures."""

from unittest.mock import patch

from fastapi.testclient import TestClient

from fireman_market_provider import create_app


def test_fetch_failure_emits_logs(caplog) -> None:
    caplog.set_level("WARNING")
    with patch(
        "fireman_market_provider.adapters.registry.try_sources",
        side_effect=RuntimeError("all sources down"),
    ):
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
    assert body["code"] == 1
    assert "all sources down" in body["message"]
    assert any("fetch failed" in rec.message for rec in caplog.records)
