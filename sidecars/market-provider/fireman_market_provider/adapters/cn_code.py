"""China on-exchange symbol parsing: canonical, Eastmoney bare, and prefixed forms."""

from __future__ import annotations

import time
from dataclasses import dataclass

from ..timeout_util import UpstreamCall, call_with_timeout, resolve_timeout_seconds

_PREFIXES = ("sh", "sz", "bj")

_STOCK_CANONICAL_CACHE: dict[str, tuple[str, int]] | None = None
_STOCK_CANONICAL_LOADED_AT: float = 0.0
_LOF_MARKET_ID_CACHE: dict[str, int] | None = None
_LOF_MARKET_ID_LOADED_AT: float = 0.0
_DEFAULT_CACHE_TTL = 300.0


@dataclass(frozen=True)
class CNExchangeCode:
    """Parsed China on-exchange identifiers."""

    canonical_code: str
    eastmoney_symbol: str
    prefixed_symbol: str
    exchange: str


def _cache_ttl() -> float:
    raw = __import__("os").environ.get("MARKET_PROVIDER_NAME_CACHE_TTL", "").strip()
    if not raw:
        return _DEFAULT_CACHE_TTL
    try:
        value = float(raw)
    except ValueError:
        return _DEFAULT_CACHE_TTL
    return value if value > 0 else _DEFAULT_CACHE_TTL


def reset_cn_code_caches() -> None:
    """Clear cached exchange maps (for tests only)."""
    global _STOCK_CANONICAL_CACHE, _STOCK_CANONICAL_LOADED_AT
    global _LOF_MARKET_ID_CACHE, _LOF_MARKET_ID_LOADED_AT
    _STOCK_CANONICAL_CACHE = None
    _STOCK_CANONICAL_LOADED_AT = 0.0
    _LOF_MARKET_ID_CACHE = None
    _LOF_MARKET_ID_LOADED_AT = 0.0


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


def _prefix_to_market_id(prefix: str) -> int:
    if prefix == "sh":
        return 1
    if prefix == "sz":
        return 0
    return 2


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


def _stock_prefix_from_bare(bare: str) -> str | None:
    if bare.startswith(("60", "68", "90", "51", "56", "58")):
        return "sh"
    if bare.startswith(("00", "30", "20", "15", "16", "18")):
        return "sz"
    if bare.startswith(("43", "83", "87", "88", "92")) or bare.startswith(("4", "8")):
        return "bj"
    return None


def _load_stock_canonical_cache(timeout_seconds: int | None = None) -> dict[str, tuple[str, int]]:
    global _STOCK_CANONICAL_CACHE, _STOCK_CANONICAL_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _STOCK_CANONICAL_CACHE is not None and now - _STOCK_CANONICAL_LOADED_AT < ttl:
        return _STOCK_CANONICAL_CACHE

    from .names import _load_stock_name_map

    deadline = None
    if timeout_seconds is not None:
        deadline = time.monotonic() + timeout_seconds
    name_map = _load_stock_name_map(deadline)
    mapping: dict[str, tuple[str, int]] = {}
    for bare in name_map:
        prefix = _stock_prefix_from_bare(bare)
        if prefix is None:
            continue
        mapping[bare] = (prefix, _prefix_to_market_id(prefix))

    _STOCK_CANONICAL_CACHE = mapping
    _STOCK_CANONICAL_LOADED_AT = now
    return mapping


def _load_lof_market_id_cache(timeout_seconds: int | None = None) -> dict[str, int]:
    global _LOF_MARKET_ID_CACHE, _LOF_MARKET_ID_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _LOF_MARKET_ID_CACHE is not None and now - _LOF_MARKET_ID_LOADED_AT < ttl:
        return _LOF_MARKET_ID_CACHE

    timeout = timeout_seconds if timeout_seconds is not None else resolve_timeout_seconds()
    raw_map = call_with_timeout(UpstreamCall("fund_lof_code_id_map_em"), timeout)
    mapping = {str(code).zfill(6): int(market_id) for code, market_id in raw_map.items()}
    _LOF_MARKET_ID_CACHE = mapping
    _LOF_MARKET_ID_LOADED_AT = now
    return mapping


def etf_market_id(bare: str) -> int:
    from akshare.fund.fund_etf_em import get_market_id

    return get_market_id(bare)


def stock_market_id(bare: str) -> int | None:
    """Resolve stock Eastmoney market id from authoritative A-share spot list."""
    entry = _load_stock_canonical_cache().get(bare)
    if entry is None:
        return None
    return entry[1]


def lof_market_id(bare: str) -> int | None:
    market_id = _load_lof_market_id_cache().get(bare)
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
    _, bare = _split_prefixed(canonical)
    return bare


def prefixed_symbol_from_canonical(canonical: str) -> str:
    parsed_prefix, bare = _split_prefixed(canonical)
    if parsed_prefix is not None:
        return f"{parsed_prefix}{bare}"
    return canonical.strip().lower()
