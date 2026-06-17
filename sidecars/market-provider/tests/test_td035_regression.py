"""Regression tests for td/035 P1: LOF exchange must come from the authoritative
market-id map. A LOF name hit (fund_lof_spot_em) with a failed/timed-out
fund_lof_code_id_map_em must surface market_provider_timeout, never fabricate an
SZ/SH candidate from the bare code.
"""

from unittest.mock import patch

import pandas as pd
from fastapi.testclient import TestClient

from fireman_market_provider import create_app
from fireman_market_provider.adapters.cn_code import reset_cn_code_caches
from fireman_market_provider.adapters.names import reset_name_caches
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch


def _client() -> TestClient:
    return TestClient(create_app())


def _empty_spot() -> pd.DataFrame:
    return pd.DataFrame({"代码": [], "名称": []})


def _lof_spot(code: str, name: str) -> pd.DataFrame:
    return pd.DataFrame({"代码": [code], "名称": [name]})


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


# A real SH LOF: bare prefix "50" is not covered by stock prefix heuristics, so the
# old build_from_market_id(bare, 0) fallback would have mis-tagged it as sz501302.
SH_LOF = "501302"
SZ_LOF = "166009"


def test_sh_lof_bare_market_id_timeout_returns_timeout_not_sz() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", _blocked_lof_map)
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_lof_spot(SH_LOF, "SH测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _resolve(SH_LOF)

    assert response.status_code == 504
    assert response.json()["detail"] == "upstream timeout"


def test_sh_lof_prefixed_market_id_timeout_returns_timeout() -> None:
    """Even with an explicit prefix, exchange must be confirmed by the authoritative map."""
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", _blocked_lof_map)
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_lof_spot(SH_LOF, "SH测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _resolve(f"sh{SH_LOF}")

    assert response.status_code == 504
    assert response.json()["detail"] == "upstream timeout"


def test_sz_lof_bare_market_id_timeout_returns_timeout_not_fabricated() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", _blocked_lof_map)
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_lof_spot(SZ_LOF, "SZ测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _resolve(SZ_LOF)

    assert response.status_code == 504
    assert response.json()["detail"] == "upstream timeout"


def test_sh_lof_resolves_sh_after_market_id_recovers() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", lambda *_a, **_k: {SH_LOF: 1})
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_lof_spot(SH_LOF, "SH测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _resolve(SH_LOF)

    assert response.status_code == 200
    resolved = response.json()["data"]["resolved"]
    assert resolved["code"] == f"sh{SH_LOF}"
    assert resolved["instrument_kind"] == "lof"
    assert resolved["name"] == "SH测试LOF"


def test_sz_lof_resolves_sz_after_market_id_recovers() -> None:
    _reset()
    register_test_dispatch("fund_lof_code_id_map_em", lambda *_a, **_k: {SZ_LOF: 0})
    with patch("akshare.fund_etf_spot_em", return_value=_empty_spot()), patch(
        "akshare.fund_lof_spot_em", return_value=_lof_spot(SZ_LOF, "SZ测试LOF")
    ), patch("akshare.stock_zh_a_spot_em", return_value=_empty_spot()):
        response = _resolve(SZ_LOF)

    assert response.status_code == 200
    resolved = response.json()["data"]["resolved"]
    assert resolved["code"] == f"sz{SZ_LOF}"
    assert resolved["instrument_kind"] == "lof"
    assert resolved["name"] == "SZ测试LOF"
