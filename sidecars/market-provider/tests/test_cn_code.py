"""Tests for China on-exchange code formatting (pure conversion, no inference)."""

import pytest

from fireman_market_provider.adapters.cn_code import (
    AssetIdentityError,
    build_cn_exchange_code,
    eastmoney_symbol_from_canonical,
    parse_explicit_cn_code,
    prefixed_symbol_from_canonical,
    require_explicit_cn_code,
)


def test_build_from_directory_region_is_pure_format_conversion() -> None:
    parsed = build_cn_exchange_code("sh", "600036")
    assert parsed.canonical_code == "sh600036"
    assert parsed.eastmoney_symbol == "600036"
    assert parsed.prefixed_symbol == "sh600036"
    assert parsed.exchange == "SH"


def test_build_supports_all_three_exchanges() -> None:
    assert build_cn_exchange_code("sz", "159915").canonical_code == "sz159915"
    assert build_cn_exchange_code("bj", "830799").canonical_code == "bj830799"
    assert build_cn_exchange_code("BJ", "830799").exchange == "BJ"


def test_build_rejects_unknown_region() -> None:
    with pytest.raises(AssetIdentityError):
        build_cn_exchange_code("", "600036")
    with pytest.raises(AssetIdentityError):
        build_cn_exchange_code("hk", "600036")


def test_build_rejects_non_six_digit_symbol() -> None:
    with pytest.raises(AssetIdentityError):
        build_cn_exchange_code("sh", "60003")
    with pytest.raises(AssetIdentityError):
        build_cn_exchange_code("sh", "abcdef")


def test_build_accepts_symbol_with_matching_prefix() -> None:
    assert build_cn_exchange_code("sh", "sh600036").canonical_code == "sh600036"


def test_build_rejects_symbol_with_conflicting_prefix() -> None:
    # A prefixed symbol that contradicts the directory region must fail
    # loudly instead of silently rewriting the identity.
    with pytest.raises(AssetIdentityError):
        build_cn_exchange_code("sz", "sh600036")


def test_doubled_prefix_is_invalid_not_collapsed() -> None:
    assert parse_explicit_cn_code("shsz600036") is None
    with pytest.raises(AssetIdentityError):
        require_explicit_cn_code("shsz600036")


def test_parse_explicit_prefix_only() -> None:
    parsed = parse_explicit_cn_code("sh510300")
    assert parsed is not None
    assert parsed.canonical_code == "sh510300"
    assert parsed.eastmoney_symbol == "510300"
    # Bare codes never resolve to an exchange: identity must come from the
    # directory, not from code-prefix heuristics.
    assert parse_explicit_cn_code("510300") is None
    assert parse_explicit_cn_code("600519") is None


def test_require_explicit_raises_for_bare_code() -> None:
    with pytest.raises(AssetIdentityError):
        require_explicit_cn_code("600519")
    parsed = require_explicit_cn_code("SZ159915")
    assert parsed.canonical_code == "sz159915"


def test_eastmoney_vs_prefixed() -> None:
    assert eastmoney_symbol_from_canonical("sh510300") == "510300"
    assert prefixed_symbol_from_canonical("sh510300") == "sh510300"
