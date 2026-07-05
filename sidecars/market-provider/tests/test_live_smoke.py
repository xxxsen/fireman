"""Optional live AKShare smoke test — not run in default CI.

Run explicitly: FIREMAN_LIVE_AKSHARE=1 uv run pytest -m live

Regression funds (see tests/test_fetch_regression.py):
- sh510300: cn_exchange_fund, adjust none on the primary ETF source
- 000001: cn_mutual_fund, NAV-only open fund frame
"""

import os

import pytest

from .fetch_compat import fetch

pytestmark = pytest.mark.live


@pytest.mark.skipif(os.getenv("FIREMAN_LIVE_AKSHARE") != "1", reason="set FIREMAN_LIVE_AKSHARE=1 to run")
def test_live_cn_etf_smoke() -> None:
    response = fetch(
        {
            "market": "CN",
            "instrument_type": "cn_exchange_fund",
            "source_code": "sh510300",
            "start_date": "2024-01-01",
            "end_date": "2024-12-31",
            "adjust_policy": "none",
        }
    )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["provider_symbol"] == "sh510300"
    assert len(body["data"]["points"]) > 0


@pytest.mark.skipif(os.getenv("FIREMAN_LIVE_AKSHARE") != "1", reason="set FIREMAN_LIVE_AKSHARE=1 to run")
def test_live_cn_mutual_fund_000001_smoke() -> None:
    response = fetch(
        {
            "market": "CN",
            "instrument_type": "cn_mutual_fund",
            "source_code": "000001",
            "start_date": "2024-01-01",
            "end_date": "2024-12-31",
            "adjust_policy": "none",
        }
    )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert "asset_class" not in body["data"]
    assert len(body["data"]["points"]) > 0
