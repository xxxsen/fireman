"""Regression tests for td/036 P1: a bare code that is present in BOTH the ETF and LOF
spot maps must not be silently narrowed to an ETF-only result when the LOF authoritative
``fund_lof_code_id_map_em`` lookup fails/times out. The LOF authoritative failure must win
over any partial ETF candidate and surface ``market_provider_timeout``.
"""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch

# 150001 appears in both the ETF and LOF spot maps (classic dual-code sample).
DUAL = "150001"


def _client() -> TestClient:
    return TestClient(create_app())


def _spot(code: str, name: str) -> pd.DataFrame:
    return pd.DataFrame({"代码": [code], "名称": [name]})


def _empty_spot() -> pd.DataFrame:
    return pd.DataFrame({"代码": [], "名称": []})


def _reset() -> None:
    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()


def _blocked_lof_map(*_args, **_kwargs):
    raise TimeoutError("fund_lof_code_id_map_em blocked")


def _resolve(code: str):
    return _client().post(
        "/v1/instruments/resolve",
        json={"market": "CN", "instrument_type": "cn_exchange_fund", "code": code},
    )


def test_dual_etf_lof_bare_lof_timeout_returns_timeout_not_etf() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", _blocked_lof_map)
    with patch("akshare.fund_etf_spot_em", return_value=_spot(DUAL, "测试ETF")), patch(
        "akshare.fund_lof_spot_em", return_value=_spot(DUAL, "测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund.fund_etf_em.get_market_id", return_value=0
    ):
        response = _resolve(DUAL)

    assert response.status_code == 504
    assert response.json()["detail"] == "upstream timeout"
    assert "data" not in response.json() or response.json().get("data") is None


def test_dual_etf_lof_prefixed_lof_timeout_returns_timeout_not_etf() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", _blocked_lof_map)
    with patch("akshare.fund_etf_spot_em", return_value=_spot(DUAL, "测试ETF")), patch(
        "akshare.fund_lof_spot_em", return_value=_spot(DUAL, "测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund.fund_etf_em.get_market_id", return_value=0
    ):
        response = _resolve(f"sz{DUAL}")

    assert response.status_code == 504
    assert response.json()["detail"] == "upstream timeout"


def test_dual_etf_lof_recovers_to_ambiguous_after_market_id_restored() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", lambda *_a, **_k: {DUAL: 0})
    with patch("akshare.fund_etf_spot_em", return_value=_spot(DUAL, "测试ETF")), patch(
        "akshare.fund_lof_spot_em", return_value=_spot(DUAL, "测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund.fund_etf_em.get_market_id", return_value=0
    ):
        response = _resolve(f"sz{DUAL}")

    assert response.status_code == 200
    body = response.json()["data"]
    assert body["ambiguous"] is True
    kinds = {c["instrument_kind"] for c in body["candidates"]}
    assert kinds == {"etf", "lof"}
    codes = {c["code"] for c in body["candidates"]}
    assert codes == {f"sz{DUAL}"}
