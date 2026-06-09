"""Optional live AKShare smoke test — not run in default CI.

Run explicitly: uv run pytest -m live
"""

import os

import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app

pytestmark = pytest.mark.live


@pytest.mark.skipif(os.getenv("FIREMAN_LIVE_AKSHARE") != "1", reason="set FIREMAN_LIVE_AKSHARE=1 to run")
def test_live_cn_etf_smoke() -> None:
    client = TestClient(create_app())
    response = client.post(
        "/v1/instruments/fetch",
        json={
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "510300",
            "start_date": "2024-01-01",
            "end_date": "2024-12-31",
            "adjust_policy": "qfq",
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert len(body["data"]["points"]) > 0
