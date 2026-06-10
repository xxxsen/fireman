"""Tests for Beijing exchange parsing and LOF market-id timeout."""

import time

import pandas as pd
import pytest

from fireman_market_provider.adapters.cn_code import (
    lof_market_id,
    parse_cn_lof_code,
    parse_cn_stock_code,
    reset_cn_code_caches,
)
from fireman_market_provider.timeout_util import UpstreamCall, clear_test_dispatch, register_test_dispatch


@pytest.fixture(autouse=True)
def _reset_caches() -> None:
    from fireman_market_provider.adapters.names import reset_name_caches

    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()
    yield
    reset_name_caches()
    reset_cn_code_caches()
    clear_test_dispatch()


def test_bj_stock_830799(inline_upstream_calls) -> None:
    register_test_dispatch(
        "stock_zh_a_spot_em",
        lambda: pd.DataFrame({"代码": ["830799"], "名称": ["测试北交所"]}),
    )
    parsed = parse_cn_stock_code("830799")
    assert parsed is not None
    assert parsed.canonical_code == "bj830799"
    assert parse_cn_stock_code("sz830799") is None
    assert parse_cn_stock_code("bj830799") is not None


@pytest.mark.subprocess
def test_lof_market_id_uses_cached_timeout_call(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MARKET_PROVIDER_TEST_SLOW_LOF", "1")
    monkeypatch.setenv("MARKET_PROVIDER_RESOLVE_TIMEOUT", "1")
    reset_cn_code_caches()
    start = time.monotonic()
    with pytest.raises(TimeoutError):
        lof_market_id("166009")
    assert time.monotonic() - start < 2.5


def test_lof_market_id_cached_once(inline_upstream_calls) -> None:
    calls = {"n": 0}

    def map_once() -> dict[str, int]:
        calls["n"] += 1
        return {"166009": 0}

    register_test_dispatch("fund_lof_code_id_map_em", map_once)
    assert lof_market_id("166009") == 0
    assert lof_market_id("166009") == 0
    assert calls["n"] == 1


def test_parse_cn_lof_code_rejects_wrong_prefix(inline_upstream_calls) -> None:
    register_test_dispatch("fund_lof_code_id_map_em", lambda: {"166009": 0})
    assert parse_cn_lof_code("166009") is not None
    assert parse_cn_lof_code("sh166009") is None
