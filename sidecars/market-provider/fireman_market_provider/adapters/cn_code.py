"""China on-exchange symbol parsing: canonical, Eastmoney bare, and prefixed forms."""

from __future__ import annotations

from dataclasses import dataclass

_PREFIXES = ("sh", "sz", "bj")


@dataclass(frozen=True)
class CNExchangeCode:
    """Parsed China on-exchange identifiers."""

    canonical_code: str
    eastmoney_symbol: str
    prefixed_symbol: str
    exchange: str


def _bare_digits(code: str) -> str:
    raw = code.strip().lower()
    for prefix in _PREFIXES:
        if raw.startswith(prefix):
            raw = raw[len(prefix):]
    digits = "".join(ch for ch in raw if ch.isdigit())
    if not digits:
        return raw
    return digits.zfill(6)


def _split_prefixed(code: str) -> tuple[str | None, str]:
    raw = code.strip().lower()
    for prefix in _PREFIXES:
        if raw.startswith(prefix):
            return prefix, _bare_digits(raw)
    return None, _bare_digits(raw)


def _market_id_to_prefix(market_id: int) -> str:
    if market_id == 1:
        return "sh"
    if market_id == 0:
        return "sz"
    return "bj"


def _market_id_to_exchange(market_id: int) -> str:
    if market_id == 1:
        return "SH"
    if market_id == 0:
        return "SZ"
    return "BJ"


def etf_market_id(bare: str) -> int:
    from akshare.fund.fund_etf_em import get_market_id

    return get_market_id(bare)


def stock_market_id(bare: str) -> int:
    """Match akshare stock_zh_a_hist_em market_code logic."""
    if bare.startswith("6") or bare.startswith("9"):
        return 1
    if bare.startswith(("4", "8")):
        return 0
    return 0


def lof_market_id(bare: str) -> int | None:
    from akshare.fund.fund_lof_em import _fund_lof_code_id_map_em

    mapping = _fund_lof_code_id_map_em()
    market_id = mapping.get(bare)
    if market_id is None:
        return None
    return int(market_id)


def build_from_market_id(bare: str, market_id: int) -> CNExchangeCode:
    prefix = _market_id_to_prefix(market_id)
    canonical = f"{prefix}{bare}"
    return CNExchangeCode(
        canonical_code=canonical,
        eastmoney_symbol=bare,
        prefixed_symbol=canonical,
        exchange=_market_id_to_exchange(market_id),
    )


def parse_cn_exchange_code(code: str, market_id_fn) -> CNExchangeCode | None:
    """Parse using a market-id resolver; rejects wrong explicit prefixes."""
    prefix, bare = _split_prefixed(code)
    if not bare or len(bare) != 6 or not bare.isdigit():
        return None
    market_id = market_id_fn(bare)
    if market_id is None:
        return None
    parsed = build_from_market_id(bare, market_id)
    if prefix is not None and prefix != parsed.canonical_code[:2]:
        return None
    return parsed


def parse_cn_etf_code(code: str) -> CNExchangeCode | None:
    return parse_cn_exchange_code(code, etf_market_id)


def parse_cn_stock_code(code: str) -> CNExchangeCode | None:
    return parse_cn_exchange_code(code, stock_market_id)


def parse_cn_lof_code(code: str) -> CNExchangeCode | None:
    return parse_cn_exchange_code(code, lof_market_id)


def eastmoney_symbol_from_canonical(canonical: str) -> str:
    """Strip sh/sz/bj prefix for Eastmoney hist APIs."""
    prefix, bare = _split_prefixed(canonical)
    return bare


def prefixed_symbol_from_canonical(canonical: str) -> str:
    parsed_prefix, bare = _split_prefixed(canonical)
    if parsed_prefix is not None:
        return f"{parsed_prefix}{bare}"
    return canonical.strip().lower()
