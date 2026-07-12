"""Symbol normalization for multi-source AKShare adapters."""

from __future__ import annotations


def hk_exchange_symbol(code: str) -> str:
    """Normalize HK exchange codes to zero-padded 5-digit symbols."""
    raw = code.strip().upper()
    if raw.startswith("HK"):
        raw = raw[2:]
    digits = "".join(ch for ch in raw if ch.isdigit())
    if not digits:
        return raw
    return digits.zfill(5)


def hk_adjust_policy(adjust_policy: str) -> str:
    """Map request adjust policy to HK AKShare API values."""
    if adjust_policy == "hfq":
        return "hfq"
    return ""


def tx_adjust_policy(adjust_policy: str) -> str:
    """Map request adjust policy to Tencent API values."""
    if adjust_policy == "hfq":
        return "hfq"
    return ""


def sina_adjust_policy(adjust_policy: str) -> str:
    """Map request adjust policy to Sina stock API values."""
    if adjust_policy == "hfq":
        return "hfq"
    return ""
