"""Instrument-type adapters with ordered AKShare fallback chains."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date
from typing import Any, Callable

import pandas as pd

from ..normalize import normalize_dataframe
from ..schemas import AssetClass, FetchData, FetchRequest, PointType
from ..timeout_util import call_with_timeout
from .classification import classify_cn_mutual_fund, classify_us_symbol, default_region


@dataclass(frozen=True)
class AdapterResult:
    df: pd.DataFrame
    source_name: str
    name: str
    asset_class: AssetClass
    currency: str
    point_type: PointType
    expense_ratio: float | None = None
    expense_ratio_status: str = "unavailable"
    expense_ratio_components: dict[str, Any] | None = None
    region: str = "domestic"


ProviderFn = Callable[[FetchRequest, str, str], AdapterResult]


def _fmt_date(d: str | None) -> str:
    if d:
        return d.replace("-", "")
    return "19900101"


def _end_date(req: FetchRequest) -> str:
    return req.end_date.replace("-", "")


def _fetch_cn_exchange_stock(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    errors: list[str] = []

    for adjust, source_name in (("qfq", "ak.stock_zh_a_hist"), ("hfq", "ak.stock_zh_a_hist_hfq")):
        if req.adjust_policy not in ("qfq", "hfq", "none") and adjust != req.adjust_policy:
            continue
        if req.adjust_policy == "none" and adjust != "qfq":
            continue
        try:
            df = call_with_timeout(
                lambda: ak.stock_zh_a_hist(
                    symbol=symbol,
                    period="daily",
                    start_date=start,
                    end_date=end,
                    adjust=adjust,
                )
            )
            if df is not None and not df.empty:
                name = str(df.get("股票名称", pd.Series([symbol])).iloc[0]) if "股票名称" in df.columns else symbol
                return AdapterResult(
                    df=df,
                    source_name=source_name,
                    name=name,
                    asset_class="equity",
                    currency="CNY",
                    point_type="adjusted_close",
                    region="domestic",
                )
        except TimeoutError:
            raise
        except Exception as exc:  # noqa: BLE001 - collect fallback errors
            errors.append(f"{source_name}: {exc}")

    raise RuntimeError("; ".join(errors) or "cn_exchange_stock fetch failed")


def _fetch_cn_exchange_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    adjust = req.adjust_policy if req.adjust_policy in ("qfq", "hfq", "none") else "qfq"
    errors: list[str] = []
    for source_name, fn in (
        ("ak.fund_etf_hist_em", lambda: ak.fund_etf_hist_em(symbol=symbol, period="daily", start_date=start, end_date=end, adjust=adjust)),
        ("ak.fund_etf_hist_sina", lambda: ak.fund_etf_hist_sina(symbol=symbol)),
    ):
        try:
            df = call_with_timeout(fn)
            if df is not None and not df.empty:
                return AdapterResult(
                    df=df,
                    source_name=source_name,
                    name=symbol,
                    asset_class="equity",
                    currency="CNY",
                    point_type="adjusted_close",
                    region="domestic",
                )
        except TimeoutError:
            raise
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{source_name}: {exc}")
    raise RuntimeError("; ".join(errors) or "cn_exchange_fund fetch failed")


def _fetch_cn_mutual_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    errors: list[str] = []
    for indicator, point_type, source_name in (
        ("累计净值走势", "total_return_index", "ak.fund_open_fund_info_em:累计净值走势"),
        ("单位净值走势", "nav", "ak.fund_open_fund_info_em:单位净值走势"),
    ):
        try:
            df = call_with_timeout(
                lambda ind=indicator: ak.fund_open_fund_info_em(symbol=symbol, indicator=ind, period="成立来")
            )
            if df is None or df.empty:
                continue
            meta = classify_cn_mutual_fund(df, symbol)
            if meta.asset_class is None:
                raise ValueError("unsupported fund classification")
            if start and end:
                date_col = "净值日期" if "净值日期" in df.columns else df.columns[0]
                df[date_col] = pd.to_datetime(df[date_col], errors="coerce")
                start_dt = pd.to_datetime(start)
                end_dt = pd.to_datetime(end)
                df = df[(df[date_col] >= start_dt) & (df[date_col] <= end_dt)]
            return AdapterResult(
                df=df,
                source_name=source_name,
                name=meta.name,
                asset_class=meta.asset_class,
                currency="CNY",
                point_type=point_type,  # type: ignore[arg-type]
                expense_ratio=meta.expense_ratio,
                expense_ratio_status=meta.expense_ratio_status,
                expense_ratio_components=meta.components,
                region=meta.region,
            )
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{source_name}: {exc}")
    raise RuntimeError("; ".join(errors) or "cn_mutual_fund fetch failed")


def _fetch_us_equity(req: FetchRequest, start: str, end: str, default_type: AssetClass) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    errors: list[str] = []
    for source_name, fn in (
        ("ak.stock_us_daily", lambda: ak.stock_us_daily(symbol=symbol, adjust="qfq")),
        ("ak.stock_us_hist", lambda: ak.stock_us_hist(symbol=symbol, start_date=start, end_date=end, adjust="qfq")),
    ):
        try:
            df = call_with_timeout(fn)
            if df is not None and not df.empty:
                meta = classify_us_symbol(symbol, default_type)
                return AdapterResult(
                    df=df,
                    source_name=source_name,
                    name=meta.name,
                    asset_class=meta.asset_class,
                    currency="USD",
                    point_type="adjusted_close",
                    region="foreign",
                )
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{source_name}: {exc}")
    raise RuntimeError("; ".join(errors) or "us equity fetch failed")


def _fetch_fx_rate(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    code = req.source_code.upper()
    pair_map = {
        "USDCNY": ("美元", "CNY"),
        "HKDCNY": ("港币", "CNY"),
    }
    if code not in pair_map:
        raise ValueError(f"unsupported fx code {code}")
    label, _ = pair_map[code]
    errors: list[str] = []
    for source_name, fn in (
        ("ak.currency_boc_sina", lambda: ak.currency_boc_sina(symbol=label)),
        ("ak.fx_pair_quote", lambda: ak.fx_pair_quote(symbol=code)),
    ):
        try:
            df = call_with_timeout(fn)
            if df is not None and not df.empty:
                return AdapterResult(
                    df=df,
                    source_name=source_name,
                    name=code,
                    asset_class="fx",
                    currency="CNY",
                    point_type="fx_rate",
                    region="domestic",
                )
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{source_name}: {exc}")
    raise RuntimeError("; ".join(errors) or "fx_rate fetch failed")


_REGISTRY: dict[str, ProviderFn] = {
    "cn_exchange_stock": _fetch_cn_exchange_stock,
    "cn_exchange_fund": _fetch_cn_exchange_fund,
    "cn_mutual_fund": _fetch_cn_mutual_fund,
    "us_stock": lambda req, s, e: _fetch_us_equity(req, s, e, "equity"),
    "us_etf": lambda req, s, e: _fetch_us_equity(req, s, e, "equity"),
    "fx_rate": _fetch_fx_rate,
}


def fetch_instrument(req: FetchRequest) -> FetchData:
    provider = _REGISTRY.get(req.instrument_type)
    if provider is None:
        raise ValueError(f"unsupported instrument_type {req.instrument_type}")

    start = _fmt_date(req.start_date)
    end = _end_date(req)
    if req.start_date is None:
        start = "19900101"

    result = provider(req, start, end)
    points = normalize_dataframe(result.df)
    components = dict(result.expense_ratio_components or {})
    components.setdefault("region", result.region or default_region(req.market, req.instrument_type))

    expense_ratio = result.expense_ratio
    expense_status = result.expense_ratio_status
    if expense_ratio is not None:
        if 0 <= expense_ratio <= 0.10:
            expense_status = "provider_verified"
        else:
            expense_status = "unavailable"
            expense_ratio = None

    quality = "full" if points else "empty"
    if points and req.start_date and points[0].date > req.start_date:
        quality = "partial"

    return FetchData(
        provider="akshare",
        provider_symbol=req.source_code,
        name=result.name,
        asset_class=result.asset_class,
        currency=result.currency,
        point_type=result.point_type,
        expense_ratio_status=expense_status,  # type: ignore[arg-type]
        expense_ratio_components={**components, **({"expense_ratio": expense_ratio} if expense_ratio is not None else {})},
        points=points,
        source_name=result.source_name,
        source_quality=quality,
    )
