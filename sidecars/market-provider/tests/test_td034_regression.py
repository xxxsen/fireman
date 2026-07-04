"""Regression tests for historical resolve/fetch fixes."""

from unittest.mock import patch

import pandas as pd
import pytest
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch


def _client() -> TestClient:
    return TestClient(create_app())


def _empty_spot() -> pd.DataFrame:
    return pd.DataFrame({"代码": [], "名称": []})


def _xq_name_frame(name: str) -> pd.DataFrame:
    return pd.DataFrame({"item": ["基金代码", "基金名称"], "value": ["000000", name]})


@pytest.mark.parametrize("code", ["160119", "170001", "180001"])
def test_otc_fund_prefix_rejected_as_exchange_fund_when_spot_fails(code: str) -> None:
    """16/17/18 OTC funds must not become cn_exchange_fund when spot tables fail."""
    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()
    register_test_dispatch(
        "fund_individual_basic_info_xq",
        lambda **_kwargs: _xq_name_frame(f"测试场外基金{code}"),
    )
    with patch("akshare.fund_etf_spot_em", side_effect=TimeoutError("spot slow")), patch(
        "akshare.fund_lof_spot_em", return_value=_empty_spot()
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": code,
            },
        )

    assert response.status_code in (400, 504)
    error_code = response.json()["error_code"]
    assert error_code in ("instrument_type_mismatch", "market_provider_timeout")


def test_resolve_mutual_fund_mismatch_never_calls_fund_name_em() -> None:
    """Resolve path must not sync-refresh fund_name_em during type mismatch checks."""
    reset_name_caches()
    reset_cn_code_caches()
    fund_name_calls = {"count": 0}

    def _blocked_fund_name_em() -> pd.DataFrame:
        fund_name_calls["count"] += 1
        raise TimeoutError("fund_name_em blocked")

    clear_test_dispatch()
    register_test_dispatch(
        "fund_individual_basic_info_xq",
        lambda **_kwargs: _xq_name_frame("测试场外基金160119"),
    )
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_empty_spot()
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_name_em", side_effect=_blocked_fund_name_em
    ):
        response = _client().post(
            "/v1/instruments/resolve",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "code": "160119",
            },
        )

    assert response.status_code == 400
    assert response.json()["error_code"] == "instrument_type_mismatch"
    assert fund_name_calls["count"] == 0


def test_fetch_cn_exchange_fund_uses_resolved_name_without_name_upstream() -> None:
    """Fetch must not call name upstream when resolved_name is provided."""
    reset_name_caches()
    hist = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [4.5]})
    name_calls = {"count": 0}

    def _blocked_spot(*_args, **_kwargs) -> pd.DataFrame:
        name_calls["count"] += 1
        raise RuntimeError("name upstream blocked")

    with patch("akshare.fund_etf_hist_em", return_value=hist), patch(
        "akshare.fund_etf_spot_em", side_effect=_blocked_spot
    ), patch("akshare.stock_hk_spot_em", side_effect=_blocked_spot):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_exchange_fund",
                "source_code": "510300",
                "resolved_name": "沪深300ETF",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            },
        )

    assert response.status_code == 200
    assert response.json()["data"]["name"] == "沪深300ETF"
    assert name_calls["count"] == 0


def test_fetch_mutual_fund_lof_name_uses_open_fund_not_lof_hist() -> None:
    """Offshore fund names containing LOF must stay on open fund fetch path."""
    reset_name_caches()
    nav = pd.DataFrame({"净值日期": ["2024-01-02"], "单位净值": [1.01], "日增长率": [0.1]})
    lof_calls = {"count": 0}

    def _blocked_lof_hist(*_args, **_kwargs) -> pd.DataFrame:
        lof_calls["count"] += 1
        raise RuntimeError("fund_lof_hist_em blocked")

    clear_test_dispatch()
    register_test_dispatch("fund_open_fund_info_em", lambda **_kwargs: nav)

    with patch("akshare.fund_lof_hist_em", side_effect=_blocked_lof_hist):
        response = _client().post(
            "/v1/instruments/fetch",
            json={
                "market": "CN",
                "instrument_type": "cn_mutual_fund",
                "source_code": "270042",
                "resolved_name": "广发纳指100ETF联接（QDII）人民币A",
                "end_date": "2026-06-13",
                "adjust_policy": "none",
            },
        )

    assert response.status_code == 200
    data = response.json()["data"]
    assert data["name"] == "广发纳指100ETF联接（QDII）人民币A"
    assert data["source_kind"] == "open_fund"
    assert lof_calls["count"] == 0
