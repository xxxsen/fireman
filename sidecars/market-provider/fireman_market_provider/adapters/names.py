"""Instrument display name resolution for AKShare adapters."""

from __future__ import annotations

import time

import pandas as pd

from ..timeout_util import UpstreamCall, call_with_timeout, resolve_deadline_seconds, resolve_timeout_seconds

_ETF_NAME_MAP: dict[str, str] | None = None
_LOF_NAME_MAP: dict[str, str] | None = None
_STOCK_NAME_MAP: dict[str, str] | None = None
_HK_NAME_MAP: dict[str, str] | None = None
_ETF_LOADED_AT: float = 0.0
_LOF_LOADED_AT: float = 0.0
_STOCK_LOADED_AT: float = 0.0
_HK_LOADED_AT: float = 0.0

_NAME_COLUMNS = (
    "基金简称",
    "名称",
    "基金名称",
    "股票名称",
    "name",
)

_DEFAULT_CACHE_TTL = 300.0


def _cache_ttl() -> float:
    raw = __import__("os").environ.get("MARKET_PROVIDER_NAME_CACHE_TTL", "").strip()
    if not raw:
        return _DEFAULT_CACHE_TTL
    try:
        value = float(raw)
    except ValueError:
        return _DEFAULT_CACHE_TTL
    return value if value > 0 else _DEFAULT_CACHE_TTL


def reset_name_caches() -> None:
    """Clear cached spot tables (for tests only)."""
    from .cn_code import reset_cn_code_caches

    global _ETF_NAME_MAP, _LOF_NAME_MAP, _STOCK_NAME_MAP, _HK_NAME_MAP
    global _ETF_LOADED_AT, _LOF_LOADED_AT, _STOCK_LOADED_AT, _HK_LOADED_AT
    _ETF_NAME_MAP = None
    _LOF_NAME_MAP = None
    _STOCK_NAME_MAP = None
    _HK_NAME_MAP = None
    _ETF_LOADED_AT = 0.0
    _LOF_LOADED_AT = 0.0
    _STOCK_LOADED_AT = 0.0
    _HK_LOADED_AT = 0.0
    reset_cn_code_caches()


def _normalize_code(code: str) -> str:
    return str(code).strip().zfill(6)


def name_from_dataframe(df: pd.DataFrame, symbol: str) -> str | None:
    if df is None or df.empty:
        return None
    for col in _NAME_COLUMNS:
        if col not in df.columns:
            continue
        series = df[col].dropna()
        if series.empty:
            continue
        val = str(series.iloc[0]).strip()
        if val and val != symbol:
            return val
    return None


def _remaining_deadline(deadline: float | None) -> int:
    if deadline is None:
        return resolve_timeout_seconds()
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("resolve deadline exceeded")
    return max(1, int(remaining))


def _load_etf_name_map(deadline: float | None = None) -> dict[str, str]:
    global _ETF_NAME_MAP, _ETF_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _ETF_NAME_MAP is not None and now - _ETF_LOADED_AT < ttl:
        return _ETF_NAME_MAP
    import akshare as ak

    timeout = _remaining_deadline(deadline)
    df = call_with_timeout(UpstreamCall("fund_etf_spot_em"), timeout)
    _ETF_NAME_MAP = {
        _normalize_code(str(row["代码"])): str(row["名称"]).strip()
        for _, row in df.iterrows()
        if str(row.get("代码", "")).strip() and str(row.get("名称", "")).strip()
    }
    _ETF_LOADED_AT = now
    return _ETF_NAME_MAP


def _load_lof_name_map(deadline: float | None = None) -> dict[str, str]:
    global _LOF_NAME_MAP, _LOF_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _LOF_NAME_MAP is not None and now - _LOF_LOADED_AT < ttl:
        return _LOF_NAME_MAP
    import akshare as ak

    if not hasattr(ak, "fund_lof_spot_em"):
        _LOF_NAME_MAP = {}
        _LOF_LOADED_AT = now
        return _LOF_NAME_MAP
    timeout = _remaining_deadline(deadline)
    df = call_with_timeout(UpstreamCall("fund_lof_spot_em"), timeout)
    code_col = "代码" if "代码" in df.columns else None
    name_col = "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        _LOF_NAME_MAP = {}
        _LOF_LOADED_AT = now
        return _LOF_NAME_MAP
    _LOF_NAME_MAP = {
        _normalize_code(str(row[code_col])): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }
    _LOF_LOADED_AT = now
    return _LOF_NAME_MAP


def _load_stock_name_map(deadline: float | None = None) -> dict[str, str]:
    global _STOCK_NAME_MAP, _STOCK_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _STOCK_NAME_MAP is not None and now - _STOCK_LOADED_AT < ttl:
        return _STOCK_NAME_MAP
    import akshare as ak

    timeout = _remaining_deadline(deadline)
    df = call_with_timeout(UpstreamCall("stock_zh_a_spot_em"), timeout)
    code_col = "代码" if "代码" in df.columns else None
    name_col = "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        _STOCK_NAME_MAP = {}
        _STOCK_LOADED_AT = now
        return _STOCK_NAME_MAP
    _STOCK_NAME_MAP = {
        _normalize_code(str(row[code_col])): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }
    _STOCK_LOADED_AT = now
    return _STOCK_NAME_MAP


def lookup_cn_exchange_fund_name(symbol: str) -> str | None:
    code = _normalize_code(symbol)
    try:
        name = _load_etf_name_map().get(code)
        if name:
            return name
    except Exception:  # noqa: BLE001 - fallback to LOF lookup
        pass
    try:
        return _load_lof_name_map().get(code)
    except Exception:  # noqa: BLE001 - name lookup is best-effort
        return None


def resolve_cn_exchange_fund_name(symbol: str, df: pd.DataFrame) -> str:
    from_df = name_from_dataframe(df, symbol)
    if from_df:
        return from_df
    looked_up = lookup_cn_exchange_fund_name(symbol)
    if looked_up:
        return looked_up
    return symbol


def _load_hk_name_map(deadline: float | None = None) -> dict[str, str]:
    global _HK_NAME_MAP, _HK_LOADED_AT
    ttl = _cache_ttl()
    now = time.monotonic()
    if _HK_NAME_MAP is not None and now - _HK_LOADED_AT < ttl:
        return _HK_NAME_MAP
    import akshare as ak

    from .symbols import hk_exchange_symbol

    timeout = _remaining_deadline(deadline)
    df = call_with_timeout(UpstreamCall("stock_hk_spot_em"), timeout)
    code_col = "代码" if "代码" in df.columns else None
    name_col = "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        _HK_NAME_MAP = {}
        _HK_LOADED_AT = now
        return _HK_NAME_MAP
    _HK_NAME_MAP = {
        hk_exchange_symbol(str(row[code_col])): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }
    _HK_LOADED_AT = now
    return _HK_NAME_MAP


def resolve_hk_name(symbol: str) -> str:
    from .symbols import hk_exchange_symbol

    normalized = hk_exchange_symbol(symbol)
    try:
        name = _load_hk_name_map().get(normalized)
        if name:
            return name
    except Exception:  # noqa: BLE001 - name lookup is best-effort
        pass
    return normalized
