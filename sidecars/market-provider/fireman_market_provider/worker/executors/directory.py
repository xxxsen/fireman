"""asset_directory_sync executor: full listing snapshots per market scope.

Every instrument_type in the payload is a required category: any category
failing upstream fails the whole task (no partial success, matching Go's
post-process contract). Listings are cached in memory for a short TTL so
repeated syncs do not hammer the full-list endpoints; force=true bypasses
the cache.
"""

from __future__ import annotations

import time
from typing import Any

import pandas as pd

from ...adapters.cn_code import heuristic_cn_stock_from_bare
from ...logutil import get_logger
from ...timeout_util import UpstreamCall, call_with_timeout, fetch_timeout_seconds
from ..errors import TaskFailure

logger = get_logger(__name__)

_CACHE_TTL_SECONDS = 600.0
_cache: dict[str, tuple[float, list[dict[str, Any]]]] = {}


def _today() -> str:
    return time.strftime("%Y-%m-%d")


def _call(operation: str, **kwargs: Any) -> pd.DataFrame:
    return call_with_timeout(
        UpstreamCall(operation, kwargs=tuple(kwargs.items())),
        fetch_timeout_seconds(),
    )


def _column(df: pd.DataFrame, *candidates: str) -> str | None:
    for cand in candidates:
        if cand in df.columns:
            return cand
    return None


def _rows(df: pd.DataFrame, code_col: str, name_col: str) -> list[tuple[str, str]]:
    out: list[tuple[str, str]] = []
    for _, row in df.iterrows():
        code = str(row[code_col]).strip()
        name = str(row[name_col]).strip()
        if not code or code.lower() == "nan":
            continue
        if not name or name.lower() == "nan":
            name = code
        out.append((code, name))
    return out


def _require_columns(df: pd.DataFrame, source: str) -> tuple[str, str]:
    if df is None or df.empty:
        raise TaskFailure("directory_data_incomplete", f"{source} returned no rows")
    code_col = _column(df, "代码", "基金代码", "code", "symbol")
    name_col = _column(df, "名称", "基金简称", "name")
    if code_col is None or name_col is None:
        raise TaskFailure(
            "directory_data_incomplete", f"{source} returned unexpected columns"
        )
    return code_col, name_col


def _asset(
    *,
    market: str,
    instrument_type: str,
    region_code: str,
    symbol: str,
    name: str,
    exchange: str,
    kind: str,
    currency: str,
    source_name: str,
    as_of: str,
) -> dict[str, Any]:
    return {
        "market": market,
        "instrument_type": instrument_type,
        "region_code": region_code,
        "symbol": symbol,
        "name": name,
        "exchange": exchange,
        "instrument_kind": kind,
        "currency": currency,
        "source_name": source_name,
        "source_as_of": as_of,
    }


def _list_cn_exchange_stock() -> list[dict[str, Any]]:
    source = "ak.stock_zh_a_spot_em"
    df = _call("stock_zh_a_spot_em")
    code_col, name_col = _require_columns(df, source)
    as_of = _today()
    out: list[dict[str, Any]] = []
    skipped = 0
    for code, name in _rows(df, code_col, name_col):
        parsed = heuristic_cn_stock_from_bare(code)
        if parsed is None:
            skipped += 1
            continue
        out.append(
            _asset(
                market="CN",
                instrument_type="cn_exchange_stock",
                region_code=parsed.canonical_code[:2],
                symbol=parsed.eastmoney_symbol,
                name=name,
                exchange=parsed.exchange,
                kind="stock",
                currency="CNY",
                source_name=source,
                as_of=as_of,
            )
        )
    if skipped:
        logger.info("directory cn stock: skipped %d rows without exchange identity", skipped)
    return out


def _cn_fund_entries(operation: str, source: str, kind: str) -> list[dict[str, Any]]:
    df = _call(operation)
    code_col, name_col = _require_columns(df, source)
    as_of = _today()
    out: list[dict[str, Any]] = []
    for code, name in _rows(df, code_col, name_col):
        parsed = heuristic_cn_stock_from_bare(code)
        if parsed is None:
            continue
        out.append(
            _asset(
                market="CN",
                instrument_type="cn_exchange_fund",
                region_code=parsed.canonical_code[:2],
                symbol=parsed.eastmoney_symbol,
                name=name,
                exchange=parsed.exchange,
                kind=kind,
                currency="CNY",
                source_name=source,
                as_of=as_of,
            )
        )
    return out


def _list_cn_exchange_fund() -> list[dict[str, Any]]:
    etf = _cn_fund_entries("fund_etf_spot_em", "ak.fund_etf_spot_em", "etf")
    lof = _cn_fund_entries("fund_lof_spot_em", "ak.fund_lof_spot_em", "lof")
    # ETF entries win on symbol collisions (shouldn't happen; defensive).
    seen = {a["symbol"] for a in etf}
    return etf + [a for a in lof if a["symbol"] not in seen]


def _list_cn_mutual_fund() -> list[dict[str, Any]]:
    source = "ak.fund_name_em"
    df = _call("fund_name_em")
    code_col, name_col = _require_columns(df, source)
    kind_col = _column(df, "基金类型")
    as_of = _today()
    out: list[dict[str, Any]] = []
    for idx, (_, row) in enumerate(df.iterrows()):
        del idx
        code = str(row[code_col]).strip()
        if not code or code.lower() == "nan" or not code.isdigit():
            continue
        name = str(row[name_col]).strip() or code
        kind = str(row[kind_col]).strip() if kind_col is not None else ""
        if kind.lower() == "nan":
            kind = ""
        out.append(
            _asset(
                market="CN",
                instrument_type="cn_mutual_fund",
                region_code="",
                symbol=code.zfill(6),
                name=name,
                exchange="",
                kind=kind,
                currency="CNY",
                source_name=source,
                as_of=as_of,
            )
        )
    return out


def _hk_symbol(code: str) -> str:
    digits = "".join(ch for ch in code if ch.isdigit())
    return digits.zfill(5) if digits else ""


# Trust markers inside the Eastmoney HK fund board (fs m:116 t:1): REITs and
# listed trusts share the board with ETFs but are separate directory kinds.
_HK_TRUST_NAME_MARKERS = ("信托", "房托", "房产")
# HKEX allocates ETF/ETP codes from 2800 upward; the fund-board entries below
# that line are REITs/trusts living in the ordinary stock code space.
_HK_ETP_MIN_CODE = 2800


def _is_hk_trust(symbol: str, name: str) -> bool:
    if any(marker in name for marker in _HK_TRUST_NAME_MARKERS):
        return True
    try:
        return int(symbol) < _HK_ETP_MIN_CODE
    except ValueError:
        return True


def _hk_fund_currency(name: str) -> str:
    """HKEX currency counters carry a -U (USD) / -R (RMB) name suffix."""
    if name.endswith("-U"):
        return "USD"
    if name.endswith("-R"):
        return "CNY"
    return "HKD"


def _list_hk_stock() -> list[dict[str, Any]]:
    equity_source = "em.hk_equity_list"
    df = _call("em_hk_equity_list")
    code_col, name_col = _require_columns(df, equity_source)
    as_of = _today()
    out: list[dict[str, Any]] = []
    for code, name in _rows(df, code_col, name_col):
        symbol = _hk_symbol(code)
        if not symbol:
            continue
        out.append(
            _asset(
                market="HK",
                instrument_type="hk_stock",
                region_code="hk",
                symbol=symbol,
                name=name,
                exchange="HK",
                kind="stock",
                currency="HKD",
                source_name=equity_source,
                as_of=as_of,
            )
        )

    # REITs / listed trusts live on the fund board upstream but trade (and
    # import) like stocks, so they stay in hk_stock with kind=reit.
    fund_source = "em.hk_fund_list"
    fund_df = _call("em_hk_fund_list")
    fund_code_col, fund_name_col = _require_columns(fund_df, fund_source)
    seen = {a["symbol"] for a in out}
    for code, name in _rows(fund_df, fund_code_col, fund_name_col):
        symbol = _hk_symbol(code)
        if not symbol or symbol in seen or not _is_hk_trust(symbol, name):
            continue
        out.append(
            _asset(
                market="HK",
                instrument_type="hk_stock",
                region_code="hk",
                symbol=symbol,
                name=name,
                exchange="HK",
                kind="reit",
                currency="HKD",
                source_name=fund_source,
                as_of=as_of,
            )
        )
    return out


def _list_hk_etf() -> list[dict[str, Any]]:
    source = "em.hk_fund_list"
    df = _call("em_hk_fund_list")
    code_col, name_col = _require_columns(df, source)
    as_of = _today()
    out: list[dict[str, Any]] = []
    for code, name in _rows(df, code_col, name_col):
        symbol = _hk_symbol(code)
        if not symbol or _is_hk_trust(symbol, name):
            continue
        out.append(
            _asset(
                market="HK",
                instrument_type="hk_etf",
                region_code="hk",
                symbol=symbol,
                name=name,
                exchange="HK",
                kind="etf",
                currency=_hk_fund_currency(name),
                source_name=source,
                as_of=as_of,
            )
        )
    return out


def _us_entries(operation: str, source: str, instrument_type: str, kind: str) -> list[dict[str, Any]]:
    df = _call(operation)
    code_col, name_col = _require_columns(df, source)
    as_of = _today()
    out: list[dict[str, Any]] = []
    for code, name in _rows(df, code_col, name_col):
        # Eastmoney US codes look like "105.AAPL"; keep the ticker part.
        symbol = code.rsplit(".", 1)[-1].strip().upper()
        if not symbol:
            continue
        out.append(
            _asset(
                market="US",
                instrument_type=instrument_type,
                region_code="us",
                symbol=symbol,
                name=name,
                exchange="US",
                kind=kind,
                currency="USD",
                source_name=source,
                as_of=as_of,
            )
        )
    return out


def _list_us_stock() -> list[dict[str, Any]]:
    return _us_entries("em_us_equity_list", "em.us_equity_list", "us_stock", "stock")


def _list_us_etf() -> list[dict[str, Any]]:
    return _us_entries("em_us_etf_list", "em.us_etf_list", "us_etf", "etf")


_LISTERS = {
    "cn_exchange_stock": _list_cn_exchange_stock,
    "cn_exchange_fund": _list_cn_exchange_fund,
    "cn_mutual_fund": _list_cn_mutual_fund,
    "hk_stock": _list_hk_stock,
    "hk_etf": _list_hk_etf,
    "us_stock": _list_us_stock,
    "us_etf": _list_us_etf,
}


def _list_category(instrument_type: str, force: bool) -> list[dict[str, Any]]:
    now = time.monotonic()
    if not force:
        cached = _cache.get(instrument_type)
        if cached is not None and now - cached[0] < _CACHE_TTL_SECONDS:
            logger.info("directory %s: served from cache (%d assets)", instrument_type, len(cached[1]))
            return cached[1]
    lister = _LISTERS.get(instrument_type)
    if lister is None:
        raise TaskFailure(
            "unsupported_instrument_type",
            f"directory sync does not support instrument_type {instrument_type}",
        )
    try:
        assets = lister()
    except TaskFailure:
        raise
    except TimeoutError as exc:
        raise TaskFailure(
            "market_provider_timeout", f"directory listing for {instrument_type} timed out"
        ) from exc
    except Exception as exc:  # noqa: BLE001
        raise TaskFailure(
            "market_provider_unavailable",
            f"directory listing for {instrument_type} failed: {exc}",
        ) from exc
    if not assets:
        raise TaskFailure(
            "directory_data_incomplete",
            f"directory listing for {instrument_type} returned no usable assets",
        )
    _cache[instrument_type] = (now, assets)
    return assets


def execute_directory_sync(payload: dict[str, Any]) -> dict[str, Any]:
    scope = str(payload.get("scope", "")).strip()
    instrument_types = payload.get("instrument_types") or []
    force = bool(payload.get("force", False))
    if not scope or not instrument_types:
        raise TaskFailure("invalid_task_payload", "directory payload needs scope and instrument_types")

    assets: list[dict[str, Any]] = []
    for instrument_type in instrument_types:
        entries = _list_category(str(instrument_type), force)
        logger.info("directory %s: %d assets", instrument_type, len(entries))
        assets.extend(entries)

    return {"type": "asset_directory_sync", "scope": scope, "assets": assets}
