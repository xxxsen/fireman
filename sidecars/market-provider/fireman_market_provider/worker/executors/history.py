"""asset_history_sync executor with source-pinned semantics.

Two execution modes derived from the task payload:

- required_source_name set (same-source incremental merge): only that
  source's call is constructed and executed. Any failure, timeout or
  inapplicability raises SourceUnavailable — never a fallback. An empty
  result window is reported as no_new_data.
- required_source_name empty (full refresh / switch-source): the existing
  fetch chain (TickFlow priority + AKShare fallback) picks a source and the
  result carries the actually-used source_name.
"""

from __future__ import annotations

import time
from typing import Any, Callable

import pandas as pd

from ...adapters.cn_code import (
    AssetIdentityError,
    CNExchangeCode,
    build_cn_exchange_code,
)
from ...adapters.registry import fetch_instrument
from ...adapters.symbols import (
    hk_adjust_policy,
    hk_exchange_symbol,
    sina_adjust_policy,
    tx_adjust_policy,
)
from ...adapters.tickflow import (
    TICKFLOW_KLINES_SOURCE,
    tickflow_enabled,
    tickflow_symbol,
    try_tickflow_klines,
)
from ...logutil import get_logger
from ...normalize import normalize_dataframe
from ...schemas import FetchRequest
from ...timeout_util import UpstreamCall, call_with_timeout, fetch_timeout_seconds
from ..errors import SourceUnavailable, TaskFailure

logger = get_logger(__name__)


def _today() -> str:
    return time.strftime("%Y-%m-%d")


def _compact(date_iso: str) -> str:
    return date_iso.replace("-", "")


_CN_EXCHANGE_TYPES = ("cn_exchange_stock", "cn_exchange_fund")
_REGION_BY_EXCHANGE = {"SH": "sh", "SZ": "sz", "BJ": "bj"}


def _cn_directory_identity(payload: dict[str, Any]) -> CNExchangeCode:
    """Build the CN on-exchange identity strictly from directory fields.

    The payload's ``region_code``/``exchange``/``symbol`` come from
    ``market_assets``. Only format conversion happens here — a missing
    exchange fails as non-retryable ``asset_identity_incomplete`` and
    conflicting fields fail as ``directory_identity_invalid``; the code
    prefix is never used to infer an exchange.
    """
    region = str(payload.get("region_code", "") or "").strip().lower()
    exchange = str(payload.get("exchange", "") or "").strip().upper()
    symbol = str(payload.get("symbol", "") or "").strip()

    if region and region not in _REGION_BY_EXCHANGE.values():
        raise TaskFailure(
            "directory_identity_invalid",
            f"region_code {region!r} is not a valid CN exchange region",
        )
    if exchange and exchange not in _REGION_BY_EXCHANGE:
        raise TaskFailure(
            "directory_identity_invalid",
            f"exchange {exchange!r} is not a valid CN exchange",
        )
    if region and exchange and _REGION_BY_EXCHANGE[exchange] != region:
        raise TaskFailure(
            "directory_identity_invalid",
            f"directory region_code {region!r} conflicts with exchange {exchange!r}",
        )
    if not region and exchange:
        region = _REGION_BY_EXCHANGE[exchange]
    if not region:
        raise TaskFailure(
            "asset_identity_incomplete",
            "the asset directory does not provide region_code/exchange for this "
            "CN on-exchange asset; fix the directory data instead of guessing",
        )
    try:
        return build_cn_exchange_code(region, symbol)
    except AssetIdentityError as exc:
        raise TaskFailure("directory_identity_invalid", str(exc)) from exc


def _source_code(payload: dict[str, Any]) -> str:
    """Provider-facing code from directory identity fields (no inference)."""
    symbol = str(payload.get("symbol", "") or "")
    if str(payload.get("instrument_type", "") or "") in _CN_EXCHANGE_TYPES:
        return _cn_directory_identity(payload).canonical_code
    return symbol


def _fetch_request(payload: dict[str, Any], start_date: str | None) -> FetchRequest:
    return FetchRequest(
        market=payload.get("market", "CN"),
        instrument_type=payload.get("instrument_type", ""),
        source_code=_source_code(payload),
        start_date=start_date,
        end_date=_today(),
        adjust_policy=payload.get("adjust_policy", "none"),
        resolved_name=None,
        instrument_kind=payload.get("instrument_kind") or None,
    )


# --- source-pinned call builders ---
# Each builder returns an UpstreamCall for (payload, start, end) with compact
# YYYYMMDD dates. CN builders read the validated directory identity and raise
# TaskFailure when it is missing or inconsistent.

CallBuilder = Callable[[dict[str, Any], str, str], UpstreamCall]


def _cn_em_adjust(adjust_policy: str) -> str:
    if adjust_policy == "hfq":
        return "hfq"
    if adjust_policy == "none":
        return ""
    return "qfq"


def _cn_identity(payload: dict[str, Any]) -> CNExchangeCode:
    return _cn_directory_identity(payload)


def _build_stock_zh_a_hist(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "stock_zh_a_hist",
        kwargs=(
            ("symbol", identity.eastmoney_symbol),
            ("period", "daily"),
            ("start_date", start),
            ("end_date", end),
            ("adjust", _cn_em_adjust(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_stock_zh_a_hist_tx(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "stock_zh_a_hist_tx",
        kwargs=(
            ("symbol", identity.prefixed_symbol),
            ("start_date", start),
            ("end_date", end),
            ("adjust", tx_adjust_policy(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_stock_zh_a_daily(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "stock_zh_a_daily",
        kwargs=(
            ("symbol", identity.prefixed_symbol),
            ("start_date", start),
            ("end_date", end),
            ("adjust", sina_adjust_policy(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_fund_etf_hist_em(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "fund_etf_hist_em",
        kwargs=(
            ("symbol", identity.eastmoney_symbol),
            ("period", "daily"),
            ("start_date", start),
            ("end_date", end),
            ("adjust", _cn_em_adjust(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_fund_lof_hist_em(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "fund_lof_hist_em",
        kwargs=(
            ("symbol", identity.eastmoney_symbol),
            ("period", "daily"),
            ("start_date", start),
            ("end_date", end),
            ("adjust", _cn_em_adjust(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_fund_etf_hist_sina(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    del start, end
    identity = _cn_identity(payload)
    return UpstreamCall("fund_etf_hist_sina", kwargs=(("symbol", identity.prefixed_symbol),))


def _build_fund_etf_fund_info_em(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    identity = _cn_identity(payload)
    return UpstreamCall(
        "fund_etf_fund_info_em",
        kwargs=(("fund", identity.eastmoney_symbol), ("start_date", start), ("end_date", end)),
    )


def _build_open_fund_info(indicator: str) -> CallBuilder:
    def build(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
        del start, end
        return UpstreamCall(
            "fund_open_fund_info_em",
            kwargs=(
                ("symbol", str(payload.get("symbol", ""))),
                ("indicator", indicator),
                ("period", "成立来"),
            ),
        )

    return build


def _build_money_fund_info(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    del start, end
    return UpstreamCall("fund_money_fund_info_em", kwargs=(("symbol", str(payload.get("symbol", ""))),))


def _build_financial_fund_info(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    del start, end
    return UpstreamCall(
        "fund_financial_fund_info_em", kwargs=(("symbol", str(payload.get("symbol", ""))),)
    )


def _build_stock_hk_hist(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    return UpstreamCall(
        "stock_hk_hist",
        kwargs=(
            ("symbol", hk_exchange_symbol(str(payload.get("symbol", "")))),
            ("period", "daily"),
            ("start_date", start),
            ("end_date", end),
            ("adjust", hk_adjust_policy(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_stock_hk_daily(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    del start, end
    return UpstreamCall(
        "stock_hk_daily",
        kwargs=(
            ("symbol", hk_exchange_symbol(str(payload.get("symbol", "")))),
            ("adjust", hk_adjust_policy(payload.get("adjust_policy", "none"))),
        ),
    )


def _build_stock_us_daily(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    del start, end
    return UpstreamCall(
        "stock_us_daily",
        kwargs=(("symbol", str(payload.get("symbol", ""))), ("adjust", "qfq")),
    )


def _build_stock_us_hist(payload: dict[str, Any], start: str, end: str) -> UpstreamCall:
    return UpstreamCall(
        "stock_us_hist",
        kwargs=(
            ("symbol", str(payload.get("symbol", ""))),
            ("start_date", start),
            ("end_date", end),
            ("adjust", "qfq"),
        ),
    )


# Pinned sources allowed per instrument_type. A required_source_name outside
# the asset type's set is source_unavailable by definition.
_PINNED_SOURCES: dict[str, dict[str, CallBuilder]] = {
    "cn_exchange_stock": {
        "ak.stock_zh_a_hist": _build_stock_zh_a_hist,
        "ak.stock_zh_a_hist_tx": _build_stock_zh_a_hist_tx,
        "ak.stock_zh_a_daily": _build_stock_zh_a_daily,
    },
    "cn_exchange_fund": {
        "ak.fund_etf_hist_em": _build_fund_etf_hist_em,
        "ak.fund_lof_hist_em": _build_fund_lof_hist_em,
        "ak.stock_zh_a_hist_tx": _build_stock_zh_a_hist_tx,
        "ak.fund_etf_hist_sina": _build_fund_etf_hist_sina,
        "ak.fund_etf_fund_info_em": _build_fund_etf_fund_info_em,
    },
    "cn_mutual_fund": {
        "ak.fund_open_fund_info_em:累计净值走势": _build_open_fund_info("累计净值走势"),
        "ak.fund_open_fund_info_em:单位净值走势": _build_open_fund_info("单位净值走势"),
        "ak.fund_money_fund_info_em": _build_money_fund_info,
        "ak.fund_financial_fund_info_em": _build_financial_fund_info,
    },
    "hk_stock": {
        "ak.stock_hk_hist": _build_stock_hk_hist,
        "ak.stock_hk_daily": _build_stock_hk_daily,
    },
    "hk_etf": {
        "ak.stock_hk_hist": _build_stock_hk_hist,
        "ak.stock_hk_daily": _build_stock_hk_daily,
    },
    "us_stock": {
        "ak.stock_us_daily": _build_stock_us_daily,
        "ak.stock_us_hist": _build_stock_us_hist,
    },
    "us_etf": {
        "ak.stock_us_daily": _build_stock_us_daily,
        "ak.stock_us_hist": _build_stock_us_hist,
    },
}


def _filter_points(points: list, start_iso: str | None) -> list:
    """Drop points before the requested incremental window."""
    if not start_iso:
        return points
    return [p for p in points if p.date >= start_iso]


def _fetch_pinned_tickflow(payload: dict[str, Any], start: str, end: str) -> pd.DataFrame:
    if not tickflow_enabled():
        raise SourceUnavailable("tickflow source is disabled in this deployment")
    if payload.get("instrument_type") not in _CN_EXCHANGE_TYPES:
        raise SourceUnavailable("tickflow does not serve this instrument type")
    identity = _cn_identity(payload)
    req = _fetch_request(payload, None)
    df = try_tickflow_klines(
        req, tickflow_symbol(identity.eastmoney_symbol, identity.exchange), start, end
    )
    if df is None:
        raise SourceUnavailable("tickflow klines returned no data")
    return df


def _execute_pinned(payload: dict[str, Any], source_name: str) -> list:
    """Run exactly one pinned source; SourceUnavailable on any failure."""
    instrument_type = str(payload.get("instrument_type", ""))
    start_iso = str(payload.get("start_date", "") or "")
    start = _compact(start_iso) if start_iso else "19900101"
    end = _compact(_today())

    if source_name == TICKFLOW_KLINES_SOURCE:
        df = _fetch_pinned_tickflow(payload, start, end)
        return normalize_dataframe(df)

    builders = _PINNED_SOURCES.get(instrument_type, {})
    builder = builders.get(source_name)
    if builder is None:
        raise SourceUnavailable(
            f"source {source_name} is not applicable to instrument type {instrument_type}"
        )
    call = builder(payload, start, end)
    try:
        df = call_with_timeout(call, fetch_timeout_seconds())
    except TimeoutError as exc:
        raise SourceUnavailable(f"source {source_name} timed out") from exc
    except Exception as exc:  # noqa: BLE001
        raise SourceUnavailable(f"source {source_name} failed: {exc}") from exc
    points = normalize_dataframe(df)
    # Sources that ignore start/end kwargs return full history; trim to the
    # requested incremental window.
    return _filter_points(points, start_iso or None)


def _execute_unpinned(payload: dict[str, Any]) -> tuple[list, str]:
    """Full-range fetch via the existing priority/fallback chain."""
    start_iso = str(payload.get("start_date", "") or "") or None
    req = _fetch_request(payload, start_iso)
    try:
        data = fetch_instrument(req)
    except TimeoutError as exc:
        raise TaskFailure("market_provider_timeout", "history fetch timed out") from exc
    except TaskFailure:
        raise
    except AssetIdentityError as exc:
        # Defense in depth: identity is validated up front, but the fetch
        # chain must never degrade an identity failure into a retryable one.
        raise TaskFailure("asset_identity_incomplete", str(exc)) from exc
    except Exception as exc:  # noqa: BLE001
        raise TaskFailure("market_provider_unavailable", f"history fetch failed: {exc}") from exc
    return list(data.points), data.source_name


def execute_history_sync(payload: dict[str, Any]) -> dict[str, Any]:
    asset_key = str(payload.get("asset_key", "")).strip()
    instrument_type = str(payload.get("instrument_type", "")).strip()
    if not asset_key or not instrument_type or not str(payload.get("symbol", "")).strip():
        raise TaskFailure(
            "invalid_task_payload", "history payload needs asset_key, instrument_type and symbol"
        )
    adjust_policy = str(payload.get("adjust_policy", "none") or "none")
    point_type = str(payload.get("point_type", "") or "adjusted_close")
    required_source = str(payload.get("required_source_name", "") or "").strip()
    replacement_mode = str(payload.get("replacement_mode", "full") or "full")

    # Directory identity gate: CN on-exchange assets must carry a definite
    # exchange from market_assets before any upstream call happens. Fails
    # non-retryably (asset_identity_incomplete / directory_identity_invalid).
    if instrument_type in _CN_EXCHANGE_TYPES:
        _cn_directory_identity(payload)

    if required_source:
        points = _execute_pinned(payload, required_source)
        source_name = required_source
    else:
        points, source_name = _execute_unpinned(payload)

    result: dict[str, Any] = {
        "type": "asset_history_sync",
        "asset_key": asset_key,
        "adjust_policy": adjust_policy,
        "point_type": point_type,
        "source_name": source_name,
        "points": [{"date": p.date, "value": p.value} for p in points],
    }
    if replacement_mode == "merge" and not points:
        # Same-source refresh confirmed there is nothing new in the window.
        result["no_new_data"] = True
    logger.info(
        "history sync %s: %d points via %s (pinned=%s mode=%s)",
        asset_key,
        len(points),
        source_name,
        bool(required_source),
        replacement_mode,
    )
    return result
