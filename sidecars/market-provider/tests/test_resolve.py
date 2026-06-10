"""Resolve endpoint tests with mocked AKShare spot tables."""

import time
from unittest.mock import MagicMock, patch

import pandas as pd
import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.adapters.resolve import resolve_instrument
from fireman_market_provider.schemas import ResolveRequest
from fireman_market_provider.timeout_util import dispatch_upstream_call, resolve_timeout_seconds


def _client() -> TestClient:
    return TestClient(create_app())


def test_resolve_cn_etf_159915_sz() -> None:
    reset_name_caches()
    etf = pd.DataFrame({"代码": ["159915"], "名称": ["创业板ETF"]})
    lof = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": [], "名称": []})
    with patch("akshare.fund_etf_spot_em", return_value=etf), patch(
        "akshare.fund_lof_spot_em", return_value=lof
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "159915",
            },
        )
    assert response.status_code == 200
    resolved = response.json()["data"]["resolved"]
    assert resolved["code"] == "sz159915"
    assert resolved["provider_symbol"] == "sz159915"


def test_resolve_cn_exchange_fund_unambiguous() -> None:
    reset_name_caches()
    etf = pd.DataFrame({"代码": ["510300"], "名称": ["沪深300ETF"]})
    lof = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": [], "名称": []})
    with patch("akshare.fund_etf_spot_em", return_value=etf), patch(
        "akshare.fund_lof_spot_em", return_value=lof
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "510300",
            },
        )
    assert response.status_code == 200
    body = response.json()
    assert body["code"] == 0
    assert body["data"]["ambiguous"] is False
    resolved = body["data"]["resolved"]
    assert resolved["code"] == "sh510300"
    assert resolved["provider_symbol"] == "sh510300"
    assert resolved["name"] == "沪深300ETF"


def test_resolve_cn_exchange_fund_ambiguous_510300() -> None:
    reset_name_caches()
    etf = pd.DataFrame({"代码": ["000510"], "名称": ["中证A500"]})
    lof = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": ["000510"], "名称": ["新金路"]})
    with patch("akshare.fund_etf_spot_em", return_value=etf), patch(
        "akshare.fund_lof_spot_em", return_value=lof
    ), patch("akshare.stock_zh_a_spot_em", return_value=stock), patch(
        "akshare.fund.fund_etf_em.get_market_id", return_value=1
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
    body = response.json()
    assert body["data"]["ambiguous"] is True
    codes = {c["code"] for c in body["data"]["candidates"]}
    assert codes == {"sh000510", "sz000510"}


def test_resolve_hk_stock() -> None:
    reset_name_caches()
    spot = pd.DataFrame({"代码": ["00700"], "名称": ["腾讯控股"]})
    with patch("akshare.stock_hk_spot_em", return_value=spot):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "HK",
                "instrument_type": "hk_stock",
                "code": "700",
            },
        )
    assert response.status_code == 200
    resolved = response.json()["data"]["resolved"]
    assert resolved["code"] == "00700"
    assert resolved["provider_symbol"] == "00700"
    assert resolved["name"] == "腾讯控股"
    assert resolved["exchange"] == "HK"


def test_resolve_not_found() -> None:
    reset_name_caches()
    empty = pd.DataFrame({"代码": [], "名称": []})
    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.stock_zh_a_spot_em", return_value=empty):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "999999",
            },
        )
    assert response.status_code == 400
    assert "instrument_not_found" in response.json()["detail"]


def test_resolve_cn_stock_spot_cached_within_ttl() -> None:
    reset_name_caches()
    empty = pd.DataFrame({"代码": [], "名称": []})
    stock = pd.DataFrame({"代码": ["600519"], "名称": ["贵州茅台"]})
    stock_spot = MagicMock(return_value=stock)
    client = _client()
    with patch("akshare.fund_etf_spot_em", return_value=empty), patch(
        "akshare.fund_lof_spot_em", return_value=empty
    ), patch("akshare.stock_zh_a_spot_em", stock_spot):
        for code in ("600519", "sh600519"):
            response = client.post(
                "/v1/instruments/resolve",
                json={
                    "market": "CN",
                    "instrument_type": "cn_exchange_stock",
                    "code": code,
                },
            )
            assert response.status_code == 200
    assert stock_spot.call_count == 1


def test_resolve_cn_exchange_fund_shared_deadline_with_slow_spot_and_lof_map(
    inline_upstream_calls,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """LOF market-id map must not start a fresh resolve timeout after spot loading."""
    monkeypatch.setenv("MARKET_PROVIDER_RESOLVE_DEADLINE", "2")
    reset_name_caches()
    reset_cn_code_caches()

    def slow_spot_maps(deadline: float):
        time.sleep(1.5)
        return (
            {},
            {"166009": "测试LOF"},
            {},
        )

    monkeypatch.setattr(
        "fireman_market_provider.adapters.resolve._load_cn_spot_maps",
        slow_spot_maps,
    )

    lof_timeouts: list[int] = []

    def track_lof_timeout(call, timeout_seconds: int = 30):
        if call.operation == "fund_lof_code_id_map_em":
            lof_timeouts.append(timeout_seconds)
            raise TimeoutError("lof map blocked")
        return dispatch_upstream_call(call)

    monkeypatch.setattr(
        "fireman_market_provider.adapters.cn_code.call_with_timeout",
        track_lof_timeout,
    )

    req = ResolveRequest(market="CN", instrument_type="cn_exchange_fund", code="166009")
    start = time.monotonic()
    with pytest.raises(ValueError, match="instrument_not_found"):
        resolve_instrument(req)
    elapsed = time.monotonic() - start

    assert len(lof_timeouts) == 1
    assert lof_timeouts[0] <= 1
    assert lof_timeouts[0] < resolve_timeout_seconds()
    assert elapsed < 3.0
