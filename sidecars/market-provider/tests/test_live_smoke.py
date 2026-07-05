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
def test_live_cn_a_share_directory_boards_smoke() -> None:
    """CN A-share listings must come through the delayed-quote host chain;
    plausibility floors catch a silently shrunken or misfiltered board."""
    from fireman_market_provider.adapters import em_directory

    sh = em_directory.em_cn_sh_a_list()
    sz = em_directory.em_cn_sz_a_list()
    bj = em_directory.em_cn_bj_a_list()
    assert len(sh) > 1500, f"SH A board too small: {len(sh)}"
    assert len(sz) > 2000, f"SZ A board too small: {len(sz)}"
    assert len(bj) > 100, f"BJ board too small: {len(bj)}"
    for frame in (sh, sz, bj):
        assert list(frame.columns) == ["代码", "名称"]
        assert all("." not in code for code in frame["代码"])


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
