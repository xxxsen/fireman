"""Tests for symbol normalization."""

from fireman_market_provider.adapters.symbols import (
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


def test_cn_prefix_guessing_removed() -> None:
    """cn_exchange_symbol (code-prefix exchange inference) must stay deleted."""
    import fireman_market_provider.adapters.symbols as symbols

    assert not hasattr(symbols, "cn_exchange_symbol")


def test_adjust_policy_mapping() -> None:
    assert tx_adjust_policy("qfq") == "qfq"
    assert tx_adjust_policy("none") == ""
    assert sina_adjust_policy("hfq") == "hfq"
    assert sina_adjust_policy("none") == ""
