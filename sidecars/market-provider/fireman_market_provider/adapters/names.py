"""Instrument display name resolution for AKShare adapters."""

from __future__ import annotations

import json
import os
import threading
import time
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pandas as pd

from ..timeout_util import (
    UpstreamCall,
    call_with_timeout,
    mutual_fund_name_fetch_timeout_seconds,
    resolve_deadline_seconds,
    resolve_timeout_seconds,
)

_ETF_NAME_MAP: dict[str, str] | None = None
_LOF_NAME_MAP: dict[str, str] | None = None
_STOCK_NAME_MAP: dict[str, str] | None = None
_HK_NAME_MAP: dict[str, str] | None = None
_MUTUAL_FUND_NAME_MAP: dict[str, str] | None = None
_ETF_LOADED_AT: float = 0.0
_LOF_LOADED_AT: float = 0.0
_STOCK_LOADED_AT: float = 0.0
_HK_LOADED_AT: float = 0.0
_MUTUAL_FUND_LOADED_AT: float = 0.0
_MUTUAL_FUND_REFRESHED_AT: str | None = None
_MUTUAL_FUND_REFRESH_LOCK = threading.Lock()
_MUTUAL_FUND_REFRESH_EVENT: threading.Event | None = None
_XQ_NAME_CACHE: dict[str, str] = {}
_XQ_NAME_LOADED_AT: dict[str, float] = {}
_XQ_NEGATIVE_AT: dict[str, float] = {}
_INDIVIDUAL_NEGATIVE_AT: dict[str, float] = {}
_DEFAULT_NEGATIVE_NAME_CACHE_TTL = 60.0

_NAME_COLUMNS = (
    "基金简称",
    "名称",
    "基金名称",
    "股票名称",
    "name",
)

_DEFAULT_CACHE_TTL = 300.0
_DEFAULT_MUTUAL_FUND_CACHE_TTL = 86400.0


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
    global _ETF_NAME_MAP, _LOF_NAME_MAP, _STOCK_NAME_MAP, _HK_NAME_MAP, _MUTUAL_FUND_NAME_MAP
    global _ETF_LOADED_AT, _LOF_LOADED_AT, _STOCK_LOADED_AT, _HK_LOADED_AT, _MUTUAL_FUND_LOADED_AT
    global _MUTUAL_FUND_REFRESHED_AT, _MUTUAL_FUND_REFRESH_EVENT, _XQ_NAME_CACHE, _XQ_NAME_LOADED_AT
    global _XQ_NEGATIVE_AT, _INDIVIDUAL_NEGATIVE_AT
    _ETF_NAME_MAP = None
    _LOF_NAME_MAP = None
    _STOCK_NAME_MAP = None
    _HK_NAME_MAP = None
    _MUTUAL_FUND_NAME_MAP = None
    _ETF_LOADED_AT = 0.0
    _LOF_LOADED_AT = 0.0
    _STOCK_LOADED_AT = 0.0
    _HK_LOADED_AT = 0.0
    _MUTUAL_FUND_LOADED_AT = 0.0
    _MUTUAL_FUND_REFRESHED_AT = None
    _XQ_NAME_CACHE = {}
    _XQ_NAME_LOADED_AT = {}
    _XQ_NEGATIVE_AT = {}
    _INDIVIDUAL_NEGATIVE_AT = {}
    with _MUTUAL_FUND_REFRESH_LOCK:
        if _MUTUAL_FUND_REFRESH_EVENT is not None:
            _MUTUAL_FUND_REFRESH_EVENT.set()
        _MUTUAL_FUND_REFRESH_EVENT = None
    _clear_mutual_fund_disk_cache()


def _mutual_fund_cache_path() -> Path:
    raw = os.environ.get("MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH", "").strip()
    if raw:
        return Path(raw)
    return Path("/tmp/fireman/mutual_fund_names.json")


def _clear_mutual_fund_disk_cache() -> None:
    path = _mutual_fund_cache_path()
    try:
        path.unlink(missing_ok=True)
    except OSError:
        pass


def _mutual_fund_cache_ttl() -> float:
    raw = os.environ.get("MARKET_PROVIDER_MUTUAL_FUND_CACHE_TTL", "").strip()
    if not raw:
        return _DEFAULT_MUTUAL_FUND_CACHE_TTL
    try:
        value = float(raw)
    except ValueError:
        return _DEFAULT_MUTUAL_FUND_CACHE_TTL
    return value if value > 0 else _DEFAULT_MUTUAL_FUND_CACHE_TTL


def _parse_refreshed_at(iso: str) -> datetime | None:
    text = iso.strip()
    if not text:
        return None
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=UTC)
    return parsed.astimezone(UTC)


def _is_mutual_fund_cache_fresh(refreshed_at: str | None) -> bool:
    if not refreshed_at:
        return False
    parsed = _parse_refreshed_at(refreshed_at)
    if parsed is None:
        return False
    age = datetime.now(UTC) - parsed
    return age < timedelta(seconds=_mutual_fund_cache_ttl())


def _mutual_fund_cache_expires_at(refreshed_at: str | None) -> str | None:
    parsed = _parse_refreshed_at(refreshed_at or "")
    if parsed is None:
        return None
    return (parsed + timedelta(seconds=_mutual_fund_cache_ttl())).replace(microsecond=0).isoformat()


def _read_mutual_fund_cache_from_disk() -> dict[str, str] | None:
    path = _mutual_fund_cache_path()
    if not path.is_file():
        return None
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if not isinstance(payload, dict):
        return None
    refreshed_at = payload.get("refreshed_at")
    refreshed_text = refreshed_at.strip() if isinstance(refreshed_at, str) else ""
    if not _is_mutual_fund_cache_fresh(refreshed_text):
        return None
    names = payload.get("names")
    if not isinstance(names, dict) or not names:
        return None
    global _MUTUAL_FUND_REFRESHED_AT
    _MUTUAL_FUND_REFRESHED_AT = refreshed_text
    return {
        _normalize_code(str(code)): str(name).strip()
        for code, name in names.items()
        if str(code).strip() and str(name).strip()
    }


def _write_mutual_fund_cache_to_disk(names: dict[str, str]) -> None:
    path = _mutual_fund_cache_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    refreshed_at = datetime.now(UTC).replace(microsecond=0).isoformat()
    global _MUTUAL_FUND_REFRESHED_AT
    _MUTUAL_FUND_REFRESHED_AT = refreshed_at
    payload = {"version": 1, "refreshed_at": refreshed_at, "names": names}
    try:
        path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
    except OSError as exc:
        from ..logutil import get_logger

        get_logger(__name__).warning(
            "mutual fund name cache disk write failed path=%s: %s",
            path,
            exc,
        )


def _fetch_mutual_fund_name_map_from_upstream() -> dict[str, str]:
    timeout = mutual_fund_name_fetch_timeout_seconds()
    df = call_with_timeout(UpstreamCall("fund_name_em"), timeout)
    code_col = "基金代码" if "基金代码" in df.columns else "代码" if "代码" in df.columns else None
    name_col = "基金简称" if "基金简称" in df.columns else "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return {}
    return {
        _normalize_code(str(row[code_col])): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }


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


def _negative_name_cache_ttl() -> float:
    raw = os.environ.get("MARKET_PROVIDER_NEGATIVE_NAME_CACHE_TTL", "").strip()
    if not raw:
        return _DEFAULT_NEGATIVE_NAME_CACHE_TTL
    try:
        value = float(raw)
    except ValueError:
        return _DEFAULT_NEGATIVE_NAME_CACHE_TTL
    return value if value > 0 else _DEFAULT_NEGATIVE_NAME_CACHE_TTL


def _is_negative_cached(cache: dict[str, float], code: str, now: float) -> bool:
    loaded = cache.get(code, 0.0)
    if loaded <= 0:
        return False
    return now - loaded < _negative_name_cache_ttl()


def _mark_negative(cache: dict[str, float], code: str, now: float) -> None:
    cache[code] = now


def _remaining_deadline(deadline: float | None) -> int:
    if deadline is None:
        return resolve_timeout_seconds()
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("resolve deadline exceeded")
    return max(1, min(int(remaining), resolve_timeout_seconds()))


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


def _name_from_xq_basic_info(df: pd.DataFrame) -> str | None:
    if df is None or df.empty:
        return None
    item_col = "item" if "item" in df.columns else None
    value_col = "value" if "value" in df.columns else None
    if item_col is None or value_col is None:
        return None
    for _, row in df.iterrows():
        if str(row.get(item_col, "")).strip() == "基金名称":
            name = str(row.get(value_col, "")).strip()
            return name or None
    return None


def _lookup_cn_exchange_fund_name_xq(bare: str, deadline: float | None = None) -> str | None:
    code = _normalize_code(bare)
    ttl = _cache_ttl()
    now = time.monotonic()
    loaded_at = _XQ_NAME_LOADED_AT.get(code, 0.0)
    if code in _XQ_NAME_CACHE and now - loaded_at < ttl:
        return _XQ_NAME_CACHE[code]
    if _is_negative_cached(_XQ_NEGATIVE_AT, code, now):
        return None

    timeout = _remaining_deadline(deadline) if deadline is not None else resolve_timeout_seconds()
    try:
        df = call_with_timeout(
            UpstreamCall("fund_individual_basic_info_xq", kwargs=(("symbol", code),)),
            timeout,
        )
    except Exception:  # noqa: BLE001 - best-effort name lookup
        _mark_negative(_XQ_NEGATIVE_AT, code, now)
        return None
    name = _name_from_xq_basic_info(df)
    if name:
        _XQ_NAME_CACHE[code] = name
        _XQ_NAME_LOADED_AT[code] = now
    else:
        _mark_negative(_XQ_NEGATIVE_AT, code, now)
    return name


def _lookup_cn_stock_name_individual(bare: str, deadline: float | None = None) -> str | None:
    code = _normalize_code(bare)
    now = time.monotonic()
    if _is_negative_cached(_INDIVIDUAL_NEGATIVE_AT, code, now):
        return None
    timeout = _remaining_deadline(deadline) if deadline is not None else resolve_timeout_seconds()
    try:
        df = call_with_timeout(
            UpstreamCall("stock_individual_info_em", kwargs=(("symbol", bare),)),
            timeout,
        )
    except Exception:  # noqa: BLE001 - best-effort name lookup
        _mark_negative(_INDIVIDUAL_NEGATIVE_AT, code, now)
        return None
    for _, row in df.iterrows():
        if str(row.get("item", "")).strip() == "股票简称":
            name = str(row.get("value", "")).strip()
            if name:
                return name
    _mark_negative(_INDIVIDUAL_NEGATIVE_AT, code, now)
    return None


def lookup_cn_stock_name(symbol: str, deadline: float | None = None) -> str | None:
    code = _normalize_code(symbol)
    try:
        name = _load_stock_name_map(deadline).get(code)
        if name:
            return name
    except Exception:  # noqa: BLE001 - fallback to individual lookup
        pass
    return _lookup_cn_stock_name_individual(code, deadline)


def lookup_cn_exchange_fund_name(symbol: str, deadline: float | None = None) -> str | None:
    """Display-name lookup across the definite fund spot sources, in fixed order.

    Queries the ETF, LOF and XQ sources for every code — never routes by code
    prefix, and never feeds results back into directory identity.
    """
    code = _normalize_code(symbol)
    try:
        name = _load_etf_name_map(deadline).get(code)
        if name:
            return name
    except Exception:  # noqa: BLE001 - fallback to LOF lookup
        pass
    try:
        name = _load_lof_name_map(deadline).get(code)
        if name:
            return name
    except Exception:  # noqa: BLE001 - fallback to XQ lookup
        pass
    return _lookup_cn_exchange_fund_name_xq(code, deadline)


def lookup_cn_lof_name(symbol: str, deadline: float | None = None) -> str | None:
    code = _normalize_code(symbol)
    try:
        return _load_lof_name_map(deadline).get(code)
    except Exception:  # noqa: BLE001 - name lookup is best-effort
        return None


def resolve_cn_mutual_fund_name(symbol: str, deadline: float) -> str | None:
    """Resolve-only CN mutual fund name: cache, disk, then XQ (no batch fund_name_em wait)."""
    global _MUTUAL_FUND_NAME_MAP, _MUTUAL_FUND_LOADED_AT
    code = _normalize_code(symbol)
    if _memory_mutual_fund_cache_fresh() and _MUTUAL_FUND_NAME_MAP is not None:
        if name := _MUTUAL_FUND_NAME_MAP.get(code):
            return name
    cached = _read_mutual_fund_cache_from_disk()
    if cached is not None:
        _MUTUAL_FUND_NAME_MAP = cached
        _MUTUAL_FUND_LOADED_AT = time.monotonic()
        if name := cached.get(code):
            return name
    remaining = max(1, int(deadline - time.monotonic()))
    xq_timeout = min(15, remaining)
    if xq_timeout <= 0:
        return None
    return _lookup_cn_exchange_fund_name_xq(code, deadline=time.monotonic() + xq_timeout)


def resolve_cn_exchange_fund_name(symbol: str, df: pd.DataFrame, deadline: float | None = None) -> str:
    from_df = name_from_dataframe(df, symbol)
    if from_df:
        return from_df
    looked_up = lookup_cn_exchange_fund_name(symbol, deadline=deadline)
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


def _memory_mutual_fund_cache_fresh() -> bool:
    return _MUTUAL_FUND_NAME_MAP is not None and _is_mutual_fund_cache_fresh(_MUTUAL_FUND_REFRESHED_AT)


def _refresh_mutual_fund_name_map_sync(deadline: float | None = None) -> dict[str, str]:
    """Synchronously refresh mutual fund names; coalesced via singleflight."""
    global _MUTUAL_FUND_NAME_MAP, _MUTUAL_FUND_LOADED_AT, _MUTUAL_FUND_REFRESH_EVENT

    with _MUTUAL_FUND_REFRESH_LOCK:
        if _MUTUAL_FUND_REFRESH_EVENT is not None:
            event = _MUTUAL_FUND_REFRESH_EVENT
            is_leader = False
        else:
            _MUTUAL_FUND_REFRESH_EVENT = threading.Event()
            event = _MUTUAL_FUND_REFRESH_EVENT
            is_leader = True

    if not is_leader:
        remaining = None
        if deadline is not None:
            remaining = max(0.0, deadline - time.monotonic())
        event.wait(timeout=remaining)
        if _MUTUAL_FUND_NAME_MAP is not None:
            return _MUTUAL_FUND_NAME_MAP
        raise RuntimeError("mutual fund cache refresh failed")

    stale_fallback = _MUTUAL_FUND_NAME_MAP
    stale_refreshed_at = _MUTUAL_FUND_REFRESHED_AT
    try:
        names = _fetch_mutual_fund_name_map_from_upstream()
        _MUTUAL_FUND_NAME_MAP = names
        _MUTUAL_FUND_LOADED_AT = time.monotonic()
        _write_mutual_fund_cache_to_disk(names)
        return names
    except Exception:
        # On upstream failure, serve only still-valid cache; expired cache must not be returned.
        if stale_fallback and _is_mutual_fund_cache_fresh(stale_refreshed_at):
            return stale_fallback
        raise
    finally:
        with _MUTUAL_FUND_REFRESH_LOCK:
            if _MUTUAL_FUND_REFRESH_EVENT is not None:
                _MUTUAL_FUND_REFRESH_EVENT.set()
                _MUTUAL_FUND_REFRESH_EVENT = None


def _load_mutual_fund_name_map(deadline: float | None = None, *, force: bool = False) -> dict[str, str]:
    """Load CN mutual fund names with 1-day TTL and disk persistence."""
    global _MUTUAL_FUND_NAME_MAP, _MUTUAL_FUND_LOADED_AT

    if not force and _memory_mutual_fund_cache_fresh():
        return _MUTUAL_FUND_NAME_MAP  # type: ignore[return-value]

    if not force and _MUTUAL_FUND_NAME_MAP is None:
        cached = _read_mutual_fund_cache_from_disk()
        if cached is not None:
            _MUTUAL_FUND_NAME_MAP = cached
            _MUTUAL_FUND_LOADED_AT = time.monotonic()
            return _MUTUAL_FUND_NAME_MAP

    return _refresh_mutual_fund_name_map_sync(deadline)


def refresh_cn_mutual_fund_names(deadline: float | None = None) -> dict[str, str]:
    """Force refresh mutual fund names from upstream and overwrite disk snapshot."""
    return _refresh_mutual_fund_name_map_sync(deadline)


def cn_mutual_fund_name_cache_status() -> dict[str, int | str | bool | None]:
    loaded = _MUTUAL_FUND_NAME_MAP is not None
    is_fresh = _is_mutual_fund_cache_fresh(_MUTUAL_FUND_REFRESHED_AT)
    return {
        "entry_count": len(_MUTUAL_FUND_NAME_MAP or {}),
        "loaded": loaded,
        "refreshed_at": _MUTUAL_FUND_REFRESHED_AT,
        "cache_path": str(_mutual_fund_cache_path()),
        "ttl_seconds": int(_mutual_fund_cache_ttl()),
        "expires_at": _mutual_fund_cache_expires_at(_MUTUAL_FUND_REFRESHED_AT),
        "is_fresh": is_fresh,
    }


def lookup_cn_mutual_fund_name_readonly(symbol: str) -> str | None:
    """Read-only CN mutual fund name: memory/disk cache only; never triggers fund_name_em."""
    global _MUTUAL_FUND_NAME_MAP, _MUTUAL_FUND_LOADED_AT
    code = _normalize_code(symbol)
    if _memory_mutual_fund_cache_fresh() and _MUTUAL_FUND_NAME_MAP is not None:
        return _MUTUAL_FUND_NAME_MAP.get(code)
    cached = _read_mutual_fund_cache_from_disk()
    if cached is not None:
        _MUTUAL_FUND_NAME_MAP = cached
        _MUTUAL_FUND_LOADED_AT = time.monotonic()
        return cached.get(code)
    return None


def lookup_cn_mutual_fund_name(symbol: str, deadline: float | None = None) -> str | None:
    code = _normalize_code(symbol)
    try:
        return _load_mutual_fund_name_map(deadline).get(code)
    except Exception:  # noqa: BLE001 - best-effort lookup
        return None


def get_cn_mutual_fund_name(symbol: str) -> str | None:
    """Resolve CN mutual fund name; propagates upstream errors (for instrument resolve)."""
    code = _normalize_code(symbol)
    return _load_mutual_fund_name_map().get(code)


def warm_cn_mutual_fund_name_cache() -> None:
    """Load mutual fund names from disk or upstream (for sidecar startup)."""
    _load_mutual_fund_name_map()


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
