"""TickFlow-first historical kline fetch for resolved exchange-traded instruments.

TickFlow is a priority *fetch-only* source for already-resolved on-exchange
stocks and ETFs, built on the official ``tickflow`` Python SDK. It never
participates in resolve, never serves ``cn_mutual_fund``, and every failure
below degrades silently to the existing AKShare fallback chain — a TickFlow
miss must not surface as a ProviderError.

Free vs paid API: when ``MARKET_PROVIDER_TICKFLOW_API_KEY`` is set the client
targets the paid endpoint (authenticated via the SDK's ``x-api-key`` header),
otherwise it targets the keyless free endpoint. ``MARKET_PROVIDER_TICKFLOW_BASE_URL``
explicitly overrides either default. The API key must never appear in logs,
error strings, docs or test fixtures.
"""

from __future__ import annotations

import os
import threading
from datetime import datetime, timedelta, timezone

import pandas as pd
from tickflow import TickFlow, TickFlowError

from ..logutil import get_logger
from ..schemas import FetchRequest

logger = get_logger(__name__)

TICKFLOW_KLINES_SOURCE = "tickflow.klines:1d"

_DEFAULT_FREE_BASE_URL = "https://free-api.tickflow.org"
_DEFAULT_PAID_BASE_URL = "https://api.tickflow.org"
_DEFAULT_TIMEOUT_SECONDS = 8.0
# The sidecar already has the AKShare fallback chain; the priority source must
# not block that fallback with long internal retry loops.
_DEFAULT_MAX_RETRIES = 0
_DEFAULT_TYPES = "cn_exchange_stock,cn_exchange_fund"

# Only these resolved fund kinds may use TickFlow; LOF stays on AKShare until a
# live reconciliation proves coverage, and unknown kinds must not risk
# same-code ETF/LOF/stock confusion.
_ALLOWED_FUND_KINDS = ("etf", "index_etf")

# Kline timestamps are Beijing midnight expressed in UTC epoch ms; converting
# them naively as UTC would shift every bar to the previous calendar day.
_CN_TZ = timezone(timedelta(hours=8))

# SDK-documented per-request kline cap. A response that fills this cap while
# starting later than the requested range must be treated as possibly
# truncated, never silently accepted as full history.
_KLINES_MAX_COUNT = 10000


def tickflow_enabled() -> bool:
    return os.environ.get("MARKET_PROVIDER_TICKFLOW_ENABLED", "").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )


def tickflow_api_key() -> str:
    """Sidecar-scoped TickFlow API key; empty string means free tier."""
    return os.environ.get("MARKET_PROVIDER_TICKFLOW_API_KEY", "").strip()


def tickflow_free_base_url() -> str:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL", "").strip()
    return raw.rstrip("/") if raw else _DEFAULT_FREE_BASE_URL


def tickflow_paid_base_url() -> str:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL", "").strip()
    return raw.rstrip("/") if raw else _DEFAULT_PAID_BASE_URL


def tickflow_base_url() -> str:
    """Resolve the API endpoint: explicit override > paid (with key) > free."""
    override = os.environ.get("MARKET_PROVIDER_TICKFLOW_BASE_URL", "").strip()
    if override:
        return override.rstrip("/")
    if tickflow_api_key():
        return tickflow_paid_base_url()
    return tickflow_free_base_url()


def tickflow_timeout_seconds() -> float:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_TIMEOUT", "").strip()
    if not raw:
        return _DEFAULT_TIMEOUT_SECONDS
    try:
        value = float(raw.removesuffix("s"))
    except ValueError:
        return _DEFAULT_TIMEOUT_SECONDS
    return value if value > 0 else _DEFAULT_TIMEOUT_SECONDS


def tickflow_max_retries() -> int:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_MAX_RETRIES", "").strip()
    if not raw:
        return _DEFAULT_MAX_RETRIES
    try:
        value = int(raw)
    except ValueError:
        return _DEFAULT_MAX_RETRIES
    return value if value >= 0 else _DEFAULT_MAX_RETRIES


def tickflow_enabled_types() -> set[str]:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_TYPES", "").strip() or _DEFAULT_TYPES
    return {item.strip() for item in raw.split(",") if item.strip()}


def tickflow_require_adjust_none() -> bool:
    raw = os.environ.get("MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE", "").strip().lower()
    if not raw:
        return True
    return raw not in ("0", "false", "no", "off")


# Fireman adjust policies mapped to the SDK's adjust enum. The default
# REQUIRE_ADJUST_NONE gate keeps hfq off TickFlow until reconciliation of
# the adjusted series is done; this mapping keeps the escape hatch coherent
# instead of mislabeling unadjusted data under hfq.
_TICKFLOW_ADJUST_BY_POLICY = {"none": "none", "hfq": "backward"}

_client_lock = threading.Lock()
_client: TickFlow | None = None
_client_config: tuple[str, str, float, int] | None = None


def _current_client_config() -> tuple[str, str, float, int]:
    return (
        tickflow_api_key(),
        tickflow_base_url(),
        tickflow_timeout_seconds(),
        tickflow_max_retries(),
    )


def get_tickflow_client() -> TickFlow:
    """Return a cached SDK client, rebuilding it when configuration changes.

    The client wraps a persistent httpx connection pool, so per-fetch
    construction would leak connections; the previous client is closed on
    config change. ``api_key`` is always passed explicitly (empty -> "") so the
    ambient ``TICKFLOW_API_KEY``/``TICKFLOW_BASE_URL`` SDK env vars cannot
    silently alter sidecar behavior.
    """
    global _client, _client_config
    config = _current_client_config()
    with _client_lock:
        if _client is not None and _client_config == config:
            return _client
        if _client is not None:
            try:
                _client.close()
            except Exception:  # noqa: BLE001 - closing a stale client is best-effort
                pass
            _client = None
            _client_config = None
        api_key, base_url, timeout, max_retries = config
        _client = TickFlow(
            api_key=api_key,
            base_url=base_url,
            timeout=timeout,
            max_retries=max_retries,
        )
        _client_config = config
        return _client


def reset_tickflow_client() -> None:
    """Close and drop the cached SDK client (tests and shutdown)."""
    global _client, _client_config
    with _client_lock:
        if _client is not None:
            try:
                _client.close()
            except Exception:  # noqa: BLE001
                pass
        _client = None
        _client_config = None


def tickflow_symbol(bare_code: str, exchange: str) -> str:
    """Build the ``CODE.EXCHANGE`` TickFlow symbol from parsed cn_code parts."""
    return f"{bare_code}.{exchange.upper()}"


def tickflow_allowed_for_request(req: FetchRequest) -> bool:
    """Gate TickFlow by config, instrument type/kind and adjust policy."""
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


def _date_to_epoch_ms(date_yyyymmdd: str, *, end_of_day: bool) -> int:
    """Convert a compact YYYYMMDD date to Beijing-time epoch milliseconds."""
    moment = datetime.strptime(date_yyyymmdd, "%Y%m%d").replace(tzinfo=_CN_TZ)
    if end_of_day:
        moment = moment + timedelta(days=1) - timedelta(milliseconds=1)
    return int(moment.timestamp() * 1000)


def fetch_tickflow_instruments(symbols: list[str]) -> list[dict[str, object]]:
    """Query instrument metadata (reconciliation/live checks only, not the fetch path)."""
    data = get_tickflow_client().instruments.batch(symbols)
    if not isinstance(data, list):
        return []
    return [item for item in data if isinstance(item, dict)]


def _validate_kline_series(data: object, symbol: str) -> dict[str, list[object]] | None:
    """Return the compact kline arrays when structurally valid; None means miss."""
    if not isinstance(data, dict):
        logger.warning("tickflow klines %s: payload is not an object", symbol)
        return None
    echoed = data.get("symbol")
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
    """Fetch daily klines via the TickFlow SDK; any failure returns None (AKShare fallback).

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
    try:
        client = get_tickflow_client()
        data = client.klines.get(
            symbol,
            period="1d",
            count=_KLINES_MAX_COUNT,
            start_time=_date_to_epoch_ms(start, end_of_day=False),
            end_time=_date_to_epoch_ms(end, end_of_day=True),
            adjust=adjust,
            as_dataframe=False,
        )
    except TickFlowError as exc:
        # Covers connection errors, timeouts, API errors and rate limiting.
        # exc carries no credentials, so logging its message is key-safe.
        logger.warning(
            "tickflow klines miss: %s fallback_reason=sdk_error:%s:%s",
            fallback_fields,
            type(exc).__name__,
            exc,
        )
        return None
    except Exception as exc:  # noqa: BLE001 - client misconfig or unexpected SDK failure
        logger.warning(
            "tickflow klines miss: %s fallback_reason=unexpected_error:%s:%s",
            fallback_fields,
            type(exc).__name__,
            exc,
        )
        return None

    series = _validate_kline_series(data, symbol)
    if series is None:
        logger.warning("tickflow klines miss: %s fallback_reason=invalid_or_empty_payload", fallback_fields)
        return None
    frame = _series_to_dataframe(series, symbol)
    if frame is None:
        logger.warning("tickflow klines miss: %s fallback_reason=conversion_failed", fallback_fields)
        return None

    start_date = datetime.strptime(start, "%Y%m%d").date()
    end_date = datetime.strptime(end, "%Y%m%d").date()
    if len(series["timestamp"]) >= _KLINES_MAX_COUNT and frame["日期"].iloc[0] > start_date:
        # The response filled the SDK's per-request cap without reaching the
        # requested range start: older bars may have been cut off server-side.
        # Refusing the hit keeps truncated series out of return computations.
        logger.warning(
            "tickflow klines miss: %s fallback_reason=possibly_truncated rows=%d earliest=%s requested_start=%s",
            fallback_fields,
            len(frame),
            frame["日期"].iloc[0],
            start_date,
        )
        return None
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
