"""China on-exchange symbol formatting: canonical, Eastmoney bare, and prefixed forms.

This module performs *format conversion only*. Exchange identity must come
from the asset directory (``region_code``/``exchange``); any prefix-based
exchange inference has been removed from the production fetch path. When the
explicit exchange information is absent, callers must fail with
``asset_identity_incomplete`` instead of guessing.
"""

from __future__ import annotations

from dataclasses import dataclass

_PREFIXES = ("sh", "sz", "bj")

_EXCHANGE_BY_PREFIX = {"sh": "SH", "sz": "SZ", "bj": "BJ"}


class AssetIdentityError(ValueError):
    """The CN on-exchange identity is missing or inconsistent (non-retryable)."""


@dataclass(frozen=True)
class CNExchangeCode:
    """Parsed China on-exchange identifiers."""

    canonical_code: str
    eastmoney_symbol: str
    prefixed_symbol: str
    exchange: str


def _split_prefixed(code: str) -> tuple[str | None, str]:
    """Split at most one leading exchange prefix; never pads or rewrites digits.

    Stripping is deliberately single-pass so malformed doubled prefixes
    ("shsz600036") surface as invalid instead of silently collapsing.
    """
    raw = code.strip().lower()
    for prefix in _PREFIXES:
        if raw.startswith(prefix):
            return prefix, raw[len(prefix):]
    return None, raw


def build_cn_exchange_code(region_code: str, symbol: str) -> CNExchangeCode:
    """Format directory identity fields into upstream symbol forms.

    ``region_code`` must be an explicit sh/sz/bj value from the asset
    directory; ``symbol`` must be the bare six-digit code. This is a pure
    format conversion (``sh + 600036 -> sh600036``) — no inference.
    """
    region = region_code.strip().lower()
    if region not in _PREFIXES:
        raise AssetIdentityError(f"region_code {region_code!r} is not a CN exchange region")
    prefix, bare = _split_prefixed(symbol)
    if prefix is not None and prefix != region:
        raise AssetIdentityError(
            f"symbol {symbol!r} carries exchange prefix {prefix!r} conflicting with region_code {region!r}"
        )
    if len(bare) != 6 or not bare.isdigit():
        raise AssetIdentityError(f"symbol {symbol!r} is not a six-digit CN exchange code")
    canonical = f"{region}{bare}"
    return CNExchangeCode(
        canonical_code=canonical,
        eastmoney_symbol=bare,
        prefixed_symbol=canonical,
        exchange=_EXCHANGE_BY_PREFIX[region],
    )


def parse_explicit_cn_code(code: str) -> CNExchangeCode | None:
    """Parse a CN code that already carries an explicit sh/sz/bj prefix.

    Returns None when the prefix is absent or the bare code is not a
    six-digit number. Never guesses the exchange from code prefixes.
    """
    prefix, bare = _split_prefixed(code)
    if prefix is None or len(bare) != 6 or not bare.isdigit():
        return None
    return build_cn_exchange_code(prefix, bare)


def require_explicit_cn_code(code: str) -> CNExchangeCode:
    """Parse an explicitly prefixed CN code or fail with AssetIdentityError."""
    parsed = parse_explicit_cn_code(code)
    if parsed is None:
        raise AssetIdentityError(
            f"CN on-exchange code {code!r} lacks an explicit sh/sz/bj exchange prefix; "
            "the asset directory must provide region_code/exchange"
        )
    return parsed


def eastmoney_symbol_from_canonical(canonical: str) -> str:
    """Strip sh/sz/bj prefix for Eastmoney hist APIs."""
    _, bare = _split_prefixed(canonical)
    return bare


def prefixed_symbol_from_canonical(canonical: str) -> str:
    parsed_prefix, bare = _split_prefixed(canonical)
    if parsed_prefix is not None:
        return f"{parsed_prefix}{bare}"
    return canonical.strip().lower()
