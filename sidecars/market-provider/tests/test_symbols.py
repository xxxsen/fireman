"""Tests for symbol normalization."""

from fireman_market_provider.adapters.symbols import (
    cn_exchange_symbol,
    hk_exchange_symbol,
    sina_adjust_policy,
    tx_adjust_policy,
)


def test_hk_adjust_policy() -> None:
    from fireman_market_provider.adapters.symbols import hk_adjust_policy

    assert hk_adjust_policy("qfq") == "qfq"
    assert hk_adjust_policy("hfq") == "hfq"
    assert hk_adjust_policy("none") == ""


def test_hk_exchange_symbol() -> None:
    assert hk_exchange_symbol("700") == "00700"
    assert hk_exchange_symbol("00700") == "00700"
    assert hk_exchange_symbol("HK02800") == "02800"


def test_cn_exchange_symbol_shanghai() -> None:
    assert cn_exchange_symbol("600519") == "sh600519"
    assert cn_exchange_symbol("510300") == "sh510300"


def test_cn_exchange_symbol_shenzhen() -> None:
    assert cn_exchange_symbol("000001") == "sz000001"
    assert cn_exchange_symbol("159915") == "sz159915"


def test_cn_exchange_symbol_passthrough() -> None:
    assert cn_exchange_symbol("sh600519") == "sh600519"
    assert cn_exchange_symbol("SZ000001") == "sz000001"


def test_adjust_policy_mapping() -> None:
    assert tx_adjust_policy("qfq") == "qfq"
    assert tx_adjust_policy("none") == ""
    assert sina_adjust_policy("hfq") == "hfq"
    assert sina_adjust_policy("none") == ""
