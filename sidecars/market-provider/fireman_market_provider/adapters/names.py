"""Instrument display name resolution for AKShare adapters."""

from __future__ import annotations

import pandas as pd

from ..timeout_util import call_with_timeout

_ETF_NAME_MAP: dict[str, str] | None = None
_LOF_NAME_MAP: dict[str, str] | None = None

_NAME_COLUMNS = (
    "基金简称",
    "名称",
    "基金名称",
    "股票名称",
    "name",
)


def reset_name_caches() -> None:
    """Clear cached spot tables (for tests)."""
    global _ETF_NAME_MAP, _LOF_NAME_MAP
    _ETF_NAME_MAP = None
    _LOF_NAME_MAP = None


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


def _load_etf_name_map() -> dict[str, str]:
    global _ETF_NAME_MAP
    if _ETF_NAME_MAP is not None:
        return _ETF_NAME_MAP
    import akshare as ak

    df = call_with_timeout(lambda: ak.fund_etf_spot_em())
    _ETF_NAME_MAP = {
        _normalize_code(str(row["代码"])): str(row["名称"]).strip()
        for _, row in df.iterrows()
        if str(row.get("代码", "")).strip() and str(row.get("名称", "")).strip()
    }
    return _ETF_NAME_MAP


def _load_lof_name_map() -> dict[str, str]:
    global _LOF_NAME_MAP
    if _LOF_NAME_MAP is not None:
        return _LOF_NAME_MAP
    import akshare as ak

    if not hasattr(ak, "fund_lof_spot_em"):
        _LOF_NAME_MAP = {}
        return _LOF_NAME_MAP
    df = call_with_timeout(lambda: ak.fund_lof_spot_em())
    code_col = "代码" if "代码" in df.columns else None
    name_col = "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        _LOF_NAME_MAP = {}
        return _LOF_NAME_MAP
    _LOF_NAME_MAP = {
        _normalize_code(str(row[code_col])): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }
    return _LOF_NAME_MAP


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
