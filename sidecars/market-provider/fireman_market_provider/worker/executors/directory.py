"""asset_directory_sync executor: full listing snapshots per directory unit.

Each task carries a sync_key identifying one directory sync unit (Go owns
the unit registry and task splitting). Every instrument_type in the payload
is a required category: any category failing upstream fails the whole task
(no partial success, matching Go's post-process contract). Listings are
cached in memory for a short TTL so repeated syncs do not hammer the
full-list endpoints; force=true bypasses the cache.
"""

from __future__ import annotations

import time
from typing import Any

import pandas as pd

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


# Per-exchange spot boards: the exchange is a property of the queried board
# (an upstream structural fact), so no code-prefix inference is needed.
_CN_STOCK_BOARDS = (
    ("stock_sh_a_spot_em", "ak.stock_sh_a_spot_em", "sh", "SH"),
    ("stock_sz_a_spot_em", "ak.stock_sz_a_spot_em", "sz", "SZ"),
    ("stock_bj_a_spot_em", "ak.stock_bj_a_spot_em", "bj", "BJ"),
)

# Eastmoney market ids as returned by the CN fund boards (f13).
_CN_REGION_BY_MARKET_ID = {1: ("sh", "SH"), 0: ("sz", "SZ")}


def _valid_cn_symbol(code: str) -> str | None:
    digits = "".join(ch for ch in code if ch.isdigit())
    if len(digits) != 6:
        return None
    return digits


def _list_cn_exchange_stock() -> list[dict[str, Any]]:
    as_of = _today()
    out: list[dict[str, Any]] = []
    skipped = 0
    for operation, source, region, exchange in _CN_STOCK_BOARDS:
        df = _call(operation)
        code_col, name_col = _require_columns(df, source)
        for code, name in _rows(df, code_col, name_col):
            symbol = _valid_cn_symbol(code)
            if symbol is None:
                skipped += 1
                continue
            out.append(
                _asset(
                    market="CN",
                    instrument_type="cn_exchange_stock",
                    region_code=region,
                    symbol=symbol,
                    name=name,
                    exchange=exchange,
                    kind="stock",
                    currency="CNY",
                    source_name=source,
                    as_of=as_of,
                )
            )
    if skipped:
        logger.info(
            "directory cn stock: skipped %d rows with directory_data_incomplete identity",
            skipped,
        )
    return out


def _cn_fund_entries(operation: str, source: str, kind: str) -> list[dict[str, Any]]:
    """CN fund board entries with the upstream market id as the exchange source.

    Rows whose market id is absent or unknown are skipped and counted as
    directory_data_incomplete — the exchange is never inferred from the code.
    """
    df = _call(operation)
    code_col, name_col = _require_columns(df, source)
    market_col = "市场标识"
    if market_col not in df.columns:
        raise TaskFailure(
            "directory_data_incomplete",
            f"{source} did not return the upstream market id column",
        )
    as_of = _today()
    out: list[dict[str, Any]] = []
    skipped = 0
    for _, row in df.iterrows():
        code = str(row[code_col]).strip()
        symbol = _valid_cn_symbol(code) if code and code.lower() != "nan" else None
        raw_market = row[market_col]
        try:
            market_id = int(raw_market)
        except (TypeError, ValueError):
            market_id = -1
        region_exchange = _CN_REGION_BY_MARKET_ID.get(market_id)
        if symbol is None or region_exchange is None:
            skipped += 1
            continue
        name = str(row[name_col]).strip()
        if not name or name.lower() == "nan":
            name = symbol
        region, exchange = region_exchange
        out.append(
            _asset(
                market="CN",
                instrument_type="cn_exchange_fund",
                region_code=region,
                symbol=symbol,
                name=name,
                exchange=exchange,
                kind=kind,
                currency="CNY",
                source_name=source,
                as_of=as_of,
            )
        )
    if skipped:
        logger.info(
            "directory %s: skipped %d rows with directory_data_incomplete identity",
            source,
            skipped,
        )
    return out


def _list_cn_exchange_fund() -> list[dict[str, Any]]:
    etf = _cn_fund_entries("em_cn_etf_list", "em.cn_etf_list", "etf")
    lof = _cn_fund_entries("em_cn_lof_list", "em.cn_lof_list", "lof")
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


# HKEX List of Securities category/sub-category -> directory identity. This is
# the authoritative upstream classification: no name markers, no code-range
# splits, and the per-counter Trading Currency replaces -U/-R name suffix
# guessing. Categories outside this map (bonds, warrants, CBBCs, unit trusts)
# are intentionally not listed.
_HKEX_SOURCE = "hkex.list_of_securities"
_HKEX_EQUITY_SUB_CATEGORIES = {
    "Equity Securities (Main Board)": "stock",
    "Equity Securities (GEM)": "stock",
}
_HKEX_ETP_SUB_CATEGORIES = {
    "Exchange Traded Funds": "etf",
    "Leveraged and Inverse": "leveraged_inverse",
}


def _hkex_rows() -> pd.DataFrame:
    df = _call("hkex_list_of_securities")
    if df is None or df.empty:
        raise TaskFailure("directory_data_incomplete", f"{_HKEX_SOURCE} returned no rows")
    required = ("symbol", "name_en", "category", "sub_category", "currency")
    missing = [col for col in required if col not in df.columns]
    if missing:
        raise TaskFailure(
            "directory_data_incomplete",
            f"{_HKEX_SOURCE} returned unexpected columns (missing {missing})",
        )
    return df


def _hk_display_names() -> dict[str, str]:
    """Chinese display names from the Eastmoney HK boards, keyed by symbol.

    Display-only enrichment: identity (category/kind/currency) always comes
    from the HKEX list. Both board calls are required so a transient board
    failure never silently downgrades every name to English.
    """
    names: dict[str, str] = {}
    for operation, source in (("em_hk_equity_list", "em.hk_equity_list"), ("em_hk_fund_list", "em.hk_fund_list")):
        df = _call(operation)
        code_col, name_col = _require_columns(df, source)
        for code, name in _rows(df, code_col, name_col):
            symbol = _hk_symbol(code)
            if symbol and name:
                names[symbol] = name
    return names


def _hk_assets_from_hkex(
    hkex: pd.DataFrame,
    display_names: dict[str, str],
    *,
    instrument_type: str,
    kind_by_row,
    as_of: str,
) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    skipped = 0
    seen: set[str] = set()
    for _, row in hkex.iterrows():
        kind = kind_by_row(str(row["category"]), str(row["sub_category"]))
        if kind is None:
            continue
        symbol = _hk_symbol(str(row["symbol"]))
        currency = str(row["currency"]).strip()
        if not symbol or symbol in seen:
            continue
        if not currency:
            # Trading currency is part of the counter identity; skip rather
            # than guess when the upstream field is absent.
            skipped += 1
            continue
        seen.add(symbol)
        name = display_names.get(symbol) or str(row["name_en"]).strip() or symbol
        out.append(
            _asset(
                market="HK",
                instrument_type=instrument_type,
                region_code="hk",
                symbol=symbol,
                name=name,
                exchange="HK",
                kind=kind,
                currency=currency,
                source_name=_HKEX_SOURCE,
                as_of=as_of,
            )
        )
    if skipped:
        logger.info(
            "directory %s: skipped %d rows with directory_data_incomplete identity",
            instrument_type,
            skipped,
        )
    return out


def _list_hk_stock() -> list[dict[str, Any]]:
    hkex = _hkex_rows()
    display_names = _hk_display_names()

    def kind_by_row(category: str, sub_category: str) -> str | None:
        if category == "Equity":
            return _HKEX_EQUITY_SUB_CATEGORIES.get(sub_category)
        if category == "Real Estate Investment Trusts":
            return "reit"
        return None

    return _hk_assets_from_hkex(
        hkex,
        display_names,
        instrument_type="hk_stock",
        kind_by_row=kind_by_row,
        as_of=_today(),
    )


def _list_hk_etf() -> list[dict[str, Any]]:
    hkex = _hkex_rows()
    display_names = _hk_display_names()

    def kind_by_row(category: str, sub_category: str) -> str | None:
        if category == "Exchange Traded Products":
            return _HKEX_ETP_SUB_CATEGORIES.get(sub_category)
        return None

    return _hk_assets_from_hkex(
        hkex,
        display_names,
        instrument_type="hk_etf",
        kind_by_row=kind_by_row,
        as_of=_today(),
    )


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
    sync_key = str(payload.get("sync_key", "")).strip()
    scope = str(payload.get("scope", "")).strip()
    instrument_types = payload.get("instrument_types") or []
    force = bool(payload.get("force", False))
    if not sync_key or not scope or not instrument_types:
        raise TaskFailure(
            "invalid_task_payload",
            "directory payload needs sync_key, scope and instrument_types",
        )

    assets: list[dict[str, Any]] = []
    for instrument_type in instrument_types:
        try:
            entries = _list_category(str(instrument_type), force)
        except TaskFailure as exc:
            # Tag failures with the unit so admin/task detail shows which
            # directory unit broke.
            raise TaskFailure(
                exc.error_code, f"{exc.message} (sync_key={sync_key})"
            ) from exc
        logger.info(
            "directory %s (%s): %d assets", instrument_type, sync_key, len(entries)
        )
        assets.extend(entries)

    return {
        "type": "asset_directory_sync",
        "sync_key": sync_key,
        "scope": scope,
        "assets": assets,
    }
