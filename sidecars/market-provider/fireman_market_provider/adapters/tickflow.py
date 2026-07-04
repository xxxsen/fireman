"""TickFlow-first historical kline fetch for resolved exchange-traded instruments.

TickFlow (td/074) is a priority *fetch-only* source for already-resolved
on-exchange stocks and ETFs. It never participates in resolve, never serves
``cn_mutual_fund``, and every failure below degrades silently to the existing
AKShare fallback chain — a TickFlow miss must not surface as a ProviderError.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone

import pandas as pd

from ..logutil import get_logger
from ..schemas import FetchRequest

logger = get_logger(__name__)

TICKFLOW_KLINES_SOURCE = "tickflow.klines:1d"

_DEFAULT_BASE_URL = "https://free-api.tickflow.org"
_DEFAULT_TIMEOUT_SECONDS = 8.0
_DEFAULT_TYPES = "cn_exchange_stock,cn_exchange_fund"

# Only these resolved fund kinds may use TickFlow; LOF stays on AKShare until a
# live reconciliation proves coverage (td/074 §5.2), and unknown kinds must not
# risk same-code ETF/LOF/stock confusion.
_ALLOWED_FUND_KINDS = ("etf", "index_etf")

# Kline timestamps are Beijing midnight expressed in UTC epoch ms; converting
# them naively as UTC would shift every bar to the previous calendar day.
_CN_TZ = timezone(timedelta(hours=8))

# Generous row cap so a full-history request is never truncated server-side
# (the API keeps the most recent N rows; A-share daily history is < 10k rows).
_KLINES_COUNT = 50000

# Fireman adjust policies mapped to TickFlow's adjust enum. The default
# REQUIRE_ADJUST_NONE gate keeps qfq/hfq off TickFlow until reconciliation of
# the adjusted series is done (td/074 §5.4); this mapping keeps the escape
# hatch coherent instead of mislabeling unadjusted data under qfq/hfq.
_TICKFLOW_ADJUST_BY_POLICY = {"none": "none", "qfq": "forward", "hfq": "backward"}


def tickflow_enabled() -> bool:
    return os.environ.get("MARKET_PROVIDER_TICKFLOW_ENABLED", "").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )


def tickflow_base_url() -> str:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_BASE_URL", "").strip()
    return raw.rstrip("/") if raw else _DEFAULT_BASE_URL


def tickflow_timeout_seconds() -> float:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_TIMEOUT", "").strip()
    if not raw:
        return _DEFAULT_TIMEOUT_SECONDS
    try:
        value = float(raw.removesuffix("s"))
    except ValueError:
        return _DEFAULT_TIMEOUT_SECONDS
    return value if value > 0 else _DEFAULT_TIMEOUT_SECONDS


def tickflow_enabled_types() -> set[str]:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_TYPES", "").strip() or _DEFAULT_TYPES
    return {item.strip() for item in raw.split(",") if item.strip()}


def tickflow_require_adjust_none() -> bool:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "").strip().lower()
    if not raw:
        return True
    return raw not in ("0", "false", "no", "off")


def tickflow_symbol(bare_code: str, exchange: str) -> str:
    """Build the ``CODE.EXCHANGE`` TickFlow symbol from parsed cn_code parts."""
    return f"{bare_code}.{exchange.upper()}"


def tickflow_allowed_for_request(req: FetchRequest) -> bool:
    """Gate TickFlow by config, instrument type/kind and adjust policy (td/074 §5.2/5.4)."""
    if not tickflow_enabled():
        return False
    if req.instrument_type not in tickflow_enabled_types():
        return False
    if req.instrument_type not in ("cn_exchange_stock", "cn_exchange_fund"):
        # TickFlow integration currently only covers CN on-exchange types.
        return False
    if req.instrument_type == "cn_exchange_fund":
        kind = (req.instrument_kind or "").strip().lower()
        if kind not in _ALLOWED_FUND_KINDS:
            return False
    if tickflow_require_adjust_none() and req.adjust_policy != "none":
        return False
    return True


def _http_get_json(path: str, params: dict[str, str]) -> object:
    url = f"{tickflow_base_url()}{path}?{urllib.parse.urlencode(params)}"
    # TickFlow's edge rejects the default Python-urllib user agent with 403,
    # so identify ourselves explicitly.
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/json", "User-Agent": "fireman-market-provider/1.0"},
    )
    with urllib.request.urlopen(request, timeout=tickflow_timeout_seconds()) as response:  # noqa: S310
        status = getattr(response, "status", 200)
        if status < 200 or status >= 300:
            raise RuntimeError(f"http status {status}")
        return json.loads(response.read().decode("utf-8"))


def _date_to_epoch_ms(date_yyyymmdd: str, *, end_of_day: bool) -> int:
    """Convert a compact YYYYMMDD date to Beijing-time epoch milliseconds."""
    moment = datetime.strptime(date_yyyymmdd, "%Y%m%d").replace(tzinfo=_CN_TZ)
    if end_of_day:
        moment = moment + timedelta(days=1) - timedelta(milliseconds=1)
    return int(moment.timestamp() * 1000)


def fetch_tickflow_instruments(symbols: list[str]) -> list[dict[str, object]]:
    """Query instrument metadata (reconciliation/live checks only, not the fetch path)."""
    payload = _http_get_json("/v1/instruments", {"symbols": ",".join(symbols)})
    if not isinstance(payload, dict):
        raise RuntimeError("unexpected instruments payload")
    data = payload.get("data")
    if not isinstance(data, list):
        return []
    return [item for item in data if isinstance(item, dict)]


def _validate_kline_payload(payload: object, symbol: str) -> dict[str, list[object]] | None:
    """Return the kline arrays when structurally valid; None means TickFlow miss."""
    if not isinstance(payload, dict):
        logger.warning("tickflow klines %s: payload is not an object", symbol)
        return None
    data = payload.get("data")
    if not isinstance(data, dict):
        logger.warning("tickflow klines %s: missing data object", symbol)
        return None
    echoed = data.get("symbol") or payload.get("symbol")
    if echoed is not None and str(echoed).strip().upper() != symbol.upper():
        logger.warning("tickflow klines %s: symbol mismatch (got %s)", symbol, echoed)
        return None
    timestamps = data.get("timestamp")
    if not isinstance(timestamps, list) or not timestamps:
        logger.info("tickflow klines %s: empty timestamp", symbol)
        return None
    series: dict[str, list[object]] = {"timestamp": timestamps}
    for field in ("open", "high", "low", "close"):
        values = data.get(field)
        if not isinstance(values, list) or len(values) != len(timestamps):
            logger.warning("tickflow klines %s: %s length mismatch", symbol, field)
            return None
        series[field] = values
    for optional in ("volume", "amount"):
        values = data.get(optional)
        if isinstance(values, list) and len(values) == len(timestamps):
            series[optional] = values
    return series


def _series_to_dataframe(series: dict[str, list[object]], symbol: str) -> pd.DataFrame | None:
    dates = pd.to_datetime(
        pd.Series(series["timestamp"]), unit="ms", utc=True, errors="coerce"
    ).dt.tz_convert("Asia/Shanghai")
    if dates.isna().any():
        logger.warning("tickflow klines %s: unparseable timestamp values", symbol)
        return None
    close = pd.to_numeric(pd.Series(series["close"]), errors="coerce")
    frame = pd.DataFrame(
        {
            "日期": dates.dt.date,
            "收盘": close,
            "开盘": pd.to_numeric(pd.Series(series["open"]), errors="coerce"),
            "最高": pd.to_numeric(pd.Series(series["high"]), errors="coerce"),
            "最低": pd.to_numeric(pd.Series(series["low"]), errors="coerce"),
        }
    )
    if "volume" in series:
        frame["成交量"] = pd.to_numeric(pd.Series(series["volume"]), errors="coerce")
    if "amount" in series:
        frame["成交额"] = pd.to_numeric(pd.Series(series["amount"]), errors="coerce")
    frame = frame[frame["收盘"].notna()]
    if frame.empty:
        logger.warning("tickflow klines %s: no numeric close values", symbol)
        return None
    return frame.sort_values("日期").reset_index(drop=True)


def try_tickflow_klines(
    req: FetchRequest,
    symbol: str,
    start: str,
    end: str,
) -> pd.DataFrame | None:
    """Fetch daily klines from TickFlow; any failure returns None (AKShare fallback).

    ``start``/``end`` are compact ``YYYYMMDD`` strings as used by the registry
    fetch chain. The returned DataFrame is date-ascending with 日期/收盘 columns
    feeding the existing ``normalize_dataframe``; OHLC/volume columns ride along
    for debugging only.
    """
    fallback_fields = (
        f"source_code={req.source_code} tickflow_symbol={symbol} "
        f"instrument_type={req.instrument_type} instrument_kind={req.instrument_kind or ''} "
        f"adjust_policy={req.adjust_policy}"
    )
    adjust = _TICKFLOW_ADJUST_BY_POLICY.get(req.adjust_policy)
    if adjust is None:
        logger.warning("tickflow klines miss: %s fallback_reason=unsupported_adjust_policy", fallback_fields)
        return None
    params = {
        "symbol": symbol,
        "period": "1d",
        "adjust": adjust,
        "count": str(_KLINES_COUNT),
        "start_time": str(_date_to_epoch_ms(start, end_of_day=False)),
        "end_time": str(_date_to_epoch_ms(end, end_of_day=True)),
    }
    try:
        payload = _http_get_json("/v1/klines", params)
    except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError, OSError) as exc:
        logger.warning("tickflow klines miss: %s fallback_reason=request_failed:%s", fallback_fields, exc)
        return None
    except (json.JSONDecodeError, ValueError) as exc:
        logger.warning("tickflow klines miss: %s fallback_reason=decode_failed:%s", fallback_fields, exc)
        return None
    except RuntimeError as exc:
        logger.warning("tickflow klines miss: %s fallback_reason=http_error:%s", fallback_fields, exc)
        return None

    series = _validate_kline_payload(payload, symbol)
    if series is None:
        logger.warning("tickflow klines miss: %s fallback_reason=invalid_or_empty_payload", fallback_fields)
        return None
    frame = _series_to_dataframe(series, symbol)
    if frame is None:
        logger.warning("tickflow klines miss: %s fallback_reason=conversion_failed", fallback_fields)
        return None

    start_date = datetime.strptime(start, "%Y%m%d").date()
    end_date = datetime.strptime(end, "%Y%m%d").date()
    frame = frame[(frame["日期"] >= start_date) & (frame["日期"] <= end_date)]
    if frame.empty:
        logger.info("tickflow klines miss: %s fallback_reason=empty_after_range_filter", fallback_fields)
        return None
    logger.info(
        "tickflow klines hit: %s rows=%d first=%s last=%s",
        fallback_fields,
        len(frame),
        frame["日期"].iloc[0],
        frame["日期"].iloc[-1],
    )
    return frame.reset_index(drop=True)
