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


def cn_exchange_symbol(code: str) -> str:
    """Return sh/sz/bj prefixed symbol for Sina/Tencent fund and stock APIs."""
    raw = code.strip().lower()
    if raw.startswith(("sh", "sz", "bj")):
        return raw
    digits = raw
    if digits.startswith(("5", "6", "9")) or digits.startswith("688"):
        return f"sh{digits}"
    if digits.startswith(("0", "1", "2", "3", "4")):
        return f"sz{digits}"
    return f"sz{digits}"


def tx_adjust_policy(adjust_policy: str) -> str:
    """Map request adjust policy to Tencent API values."""
    if adjust_policy == "hfq":
        return "hfq"
    if adjust_policy == "qfq":
        return "qfq"
    return ""


def sina_adjust_policy(adjust_policy: str) -> str:
    """Map request adjust policy to Sina stock API values."""
    if adjust_policy in ("qfq", "hfq"):
        return adjust_policy
    return ""
