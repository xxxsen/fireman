"""Tests for China on-exchange code parsing."""

import pandas as pd
import pytest

from fireman_market_provider.adapters.cn_code import (
    eastmoney_symbol_from_canonical,
    parse_cn_etf_code,
    parse_cn_stock_code,
    prefixed_symbol_from_canonical,
    reset_cn_code_caches,
    resolve_cn_etf_fetch_code,
)
from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch


pytestmark = pytest.mark.usefixtures("inline_upstream_calls")


@pytest.fixture(autouse=True)
def _reset_caches() -> None:
    reset_cn_code_caches()
    clear_test_dispatch()
    yield
    reset_cn_code_caches()
    clear_test_dispatch()


def test_etf_510300_sh() -> None:
    parsed = parse_cn_etf_code("510300")
    assert parsed is not None
    assert parsed.canonical_code == "sh510300"
    assert parsed.eastmoney_symbol == "510300"
    assert parsed.prefixed_symbol == "sh510300"


def test_resolve_cn_etf_fetch_code_normalizes_bare_510300() -> None:
    parsed = resolve_cn_etf_fetch_code("510300")
    assert parsed.canonical_code == "sh510300"
    assert parsed.prefixed_symbol == "sh510300"
    assert prefixed_symbol_from_canonical("510300") == "510300"


def test_etf_159915_sz() -> None:
    parsed = parse_cn_etf_code("159915")
    assert parsed is not None
    assert parsed.canonical_code == "sz159915"
    assert parsed.eastmoney_symbol == "159915"


def test_wrong_prefix_rejected() -> None:
    assert parse_cn_etf_code("sh159915") is None
    assert parse_cn_etf_code("sz510300") is None


def test_eastmoney_vs_prefixed() -> None:
    assert eastmoney_symbol_from_canonical("sh510300") == "510300"
    assert prefixed_symbol_from_canonical("sh510300") == "sh510300"


def test_stock_600519() -> None:
    register_test_dispatch(
        "stock_zh_a_spot_em",
        lambda: pd.DataFrame({"代码": ["600519"], "名称": ["贵州茅台"]}),
    )
    parsed = parse_cn_stock_code("600519")
    assert parsed is not None
    assert parsed.canonical_code == "sh600519"
    assert parsed.eastmoney_symbol == "600519"
