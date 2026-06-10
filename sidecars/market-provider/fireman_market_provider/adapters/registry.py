"""Instrument-type adapters with ordered AKShare fallback chains."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable

import pandas as pd

from ..logutil import get_logger
from ..normalize import normalize_dataframe
from ..schemas import AssetClass, FetchData, FetchRequest, PointType
from ..timeout_util import call_with_timeout, fetch_timeout_seconds
from .classification import FundMeta, classify_cn_mutual_fund, classify_us_symbol, default_region
from .fallback import try_sources
from .names import resolve_cn_exchange_fund_name, resolve_hk_name
from .symbols import cn_exchange_symbol, hk_adjust_policy, hk_exchange_symbol, sina_adjust_policy, tx_adjust_policy


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

logger = get_logger(__name__)


def _fmt_date(d: str | None) -> str:
    if d:
        return d.replace("-", "")
    return "19900101"


def _end_date(req: FetchRequest) -> str:
    return req.end_date.replace("-", "")


def _filter_df_by_date(df: pd.DataFrame, start: str, end: str) -> pd.DataFrame:
    if df is None or df.empty or not start or not end:
        return df
    date_col = _pick_date_column(df)
    if date_col is None:
        return df
    out = df.copy()
    out[date_col] = pd.to_datetime(out[date_col], errors="coerce")
    start_dt = pd.to_datetime(start)
    end_dt = pd.to_datetime(end)
    return out[(out[date_col] >= start_dt) & (out[date_col] <= end_dt)]


def _pick_date_column(df: pd.DataFrame) -> str | None:
    for col in ("净值日期", "日期", "date", "trade_date"):
        if col in df.columns:
            return col
    return str(df.columns[0]) if len(df.columns) else None


def _cn_stock_em_adjust(adjust_policy: str) -> str:
    if adjust_policy == "hfq":
        return "hfq"
    if adjust_policy == "none":
        return ""
    return "qfq"


def _fetch_cn_exchange_stock(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    prefixed = cn_exchange_symbol(symbol)
    policy = req.adjust_policy if req.adjust_policy in ("qfq", "hfq", "none") else "qfq"
    em_adjust = _cn_stock_em_adjust(policy)
    tx_adjust = tx_adjust_policy(policy)
    sina_adjust = sina_adjust_policy(policy)

    sources: list[tuple[str, Callable[[], pd.DataFrame | None]]] = [
        (
            "ak.stock_zh_a_hist",
            lambda: ak.stock_zh_a_hist(
                symbol=symbol,
                period="daily",
                start_date=start,
                end_date=end,
                adjust=em_adjust,
            ),
        ),
        (
            "ak.stock_zh_a_hist_tx",
            lambda: ak.stock_zh_a_hist_tx(
                symbol=prefixed,
                start_date=start,
                end_date=end,
                adjust=tx_adjust,
            ),
        ),
        (
            "ak.stock_zh_a_daily",
            lambda: ak.stock_zh_a_daily(
                symbol=prefixed,
                start_date=start,
                end_date=end,
                adjust=sina_adjust,
            ),
        ),
    ]

    df, source_name = try_sources("cn_exchange_stock", sources)
    name = symbol
    if "股票名称" in df.columns and not df["股票名称"].empty:
        name = str(df["股票名称"].iloc[0])
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        asset_class="equity",
        currency="CNY",
        point_type="adjusted_close",
        region="domestic",
    )


def _fetch_cn_exchange_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    prefixed = cn_exchange_symbol(symbol)
    adjust = req.adjust_policy if req.adjust_policy in ("qfq", "hfq", "none") else "qfq"
    tx_adjust = tx_adjust_policy(adjust)

    sources: list[tuple[str, Callable[[], pd.DataFrame | None]]] = [
        (
            "ak.fund_etf_hist_em",
            lambda: ak.fund_etf_hist_em(
                symbol=symbol,
                period="daily",
                start_date=start,
                end_date=end,
                adjust=adjust,
            ),
        ),
        (
            "ak.stock_zh_a_hist_tx",
            lambda: _filter_df_by_date(
                ak.stock_zh_a_hist_tx(
                    symbol=prefixed,
                    start_date=start,
                    end_date=end,
                    adjust=tx_adjust,
                ),
                start,
                end,
            ),
        ),
    ]
    # fund_etf_hist_sina has no qfq/hfq; skip when adjusted close is required.
    if adjust == "none":
        sources.append(("ak.fund_etf_hist_sina", lambda: ak.fund_etf_hist_sina(symbol=prefixed)))
    sources.extend(
        [
            (
                "ak.fund_lof_hist_em",
                lambda: ak.fund_lof_hist_em(
                    symbol=symbol,
                    period="daily",
                    start_date=start,
                    end_date=end,
                    adjust=adjust if adjust != "none" else "",
                ),
            ),
            (
                "ak.fund_etf_fund_info_em",
                lambda: ak.fund_etf_fund_info_em(fund=symbol, start_date=start, end_date=end),
            ),
        ]
    )

    df, source_name = try_sources("cn_exchange_fund", sources)
    name = resolve_cn_exchange_fund_name(symbol, df)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        asset_class="equity",
        currency="CNY",
        point_type="adjusted_close",
        region="domestic",
    )


def _fetch_open_fund_em(
    symbol: str,
    indicator: str,
    period: str = "成立来",
) -> pd.DataFrame:
    import akshare as ak

    return ak.fund_open_fund_info_em(symbol=symbol, indicator=indicator, period=period)


def _fetch_cn_mutual_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    errors: list[str] = []
    logger.info(
        "fetch cn_mutual_fund %s: date range %s..%s (%d candidate sources)",
        symbol,
        start,
        end,
        5,
    )

    attempts: list[tuple[str, str, PointType, Callable[[], pd.DataFrame | None]]] = [
        (
            "累计净值走势",
            "total_return_index",
            "ak.fund_open_fund_info_em:累计净值走势",
            lambda: _fetch_open_fund_em(symbol, "累计净值走势"),
        ),
        (
            "单位净值走势",
            "nav",
            "ak.fund_open_fund_info_em:单位净值走势",
            lambda: _fetch_open_fund_em(symbol, "单位净值走势"),
        ),
        (
            "money",
            "nav",
            "ak.fund_money_fund_info_em",
            lambda: ak.fund_money_fund_info_em(symbol=symbol),
        ),
        (
            "financial",
            "nav",
            "ak.fund_financial_fund_info_em",
            lambda: ak.fund_financial_fund_info_em(symbol=symbol),
        ),
        (
            "lof",
            "total_return_index",
            "ak.fund_lof_hist_em",
            lambda: ak.fund_lof_hist_em(
                symbol=symbol,
                period="daily",
                start_date=start,
                end_date=end,
                adjust="",
            ),
        ),
    ]

    for _label, point_type, source_name, fetch in attempts:
        try:
            df = call_with_timeout(fetch, fetch_timeout_seconds())
            if df is None or df.empty:
                errors.append(f"{source_name}: empty")
                logger.warning("fetch cn_mutual_fund %s: %s returned empty", symbol, source_name)
                continue
            if source_name == "ak.fund_lof_hist_em":
                meta = FundMeta(
                    name=symbol,
                    asset_class="equity",
                    region="domestic",
                    components={"fund_type": "LOF", "region": "domestic"},
                )
            else:
                meta = classify_cn_mutual_fund(df, symbol)
            if meta.asset_class is None:
                errors.append(f"{source_name}: unsupported fund classification")
                logger.warning(
                    "fetch cn_mutual_fund %s: %s unsupported classification",
                    symbol,
                    source_name,
                )
                continue
            df = _filter_df_by_date(df, start, end)
            if df.empty:
                errors.append(f"{source_name}: empty after date filter")
                logger.warning(
                    "fetch cn_mutual_fund %s: %s empty after date filter",
                    symbol,
                    source_name,
                )
                continue
            logger.info(
                "fetch cn_mutual_fund %s: success via %s (%d rows, point_type=%s)",
                symbol,
                source_name,
                len(df),
                point_type,
            )
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
        except TimeoutError:
            logger.error("fetch cn_mutual_fund %s: %s timed out", symbol, source_name)
            raise
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{source_name}: {exc}")
            logger.warning(
                "fetch cn_mutual_fund %s: %s failed: %s",
                symbol,
                source_name,
                exc,
            )

    summary = "; ".join(errors) or "cn_mutual_fund fetch failed"
    logger.error("fetch cn_mutual_fund %s: all sources failed: %s", symbol, summary)
    raise RuntimeError(summary)


def _fetch_us_equity(req: FetchRequest, start: str, end: str, default_type: AssetClass) -> AdapterResult:
    import akshare as ak

    symbol = req.source_code
    sources: list[tuple[str, Callable[[], pd.DataFrame | None]]] = [
        ("ak.stock_us_daily", lambda: ak.stock_us_daily(symbol=symbol, adjust="qfq")),
        (
            "ak.stock_us_hist",
            lambda: ak.stock_us_hist(
                symbol=symbol,
                start_date=start,
                end_date=end,
                adjust="qfq",
            ),
        ),
    ]
    df, source_name = try_sources("us equity", sources)
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


def _fetch_hk_equity(req: FetchRequest, start: str, end: str, default_type: AssetClass) -> AdapterResult:
    import akshare as ak

    symbol = hk_exchange_symbol(req.source_code)
    adjust = hk_adjust_policy(req.adjust_policy)
    sources: list[tuple[str, Callable[[], pd.DataFrame | None]]] = [
        (
            "ak.stock_hk_hist",
            lambda: ak.stock_hk_hist(
                symbol=symbol,
                period="daily",
                start_date=start,
                end_date=end,
                adjust=adjust,
            ),
        ),
        ("ak.stock_hk_daily", lambda: _filter_df_by_date(ak.stock_hk_daily(symbol=symbol, adjust=adjust), start, end)),
    ]
    df, source_name = try_sources("hk equity", sources)
    name = resolve_hk_name(symbol)
    meta = classify_us_symbol(name, default_type)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=meta.name,
        asset_class=meta.asset_class,
        currency="HKD",
        point_type="adjusted_close",
        region="foreign",
    )


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
    sources: list[tuple[str, Callable[[], pd.DataFrame | None]]] = [
        ("ak.currency_boc_sina", lambda: ak.currency_boc_sina(symbol=label)),
        ("ak.fx_pair_quote", lambda: ak.fx_pair_quote(symbol=code)),
    ]
    df, source_name = try_sources("fx_rate", sources)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=code,
        asset_class="fx",
        currency="CNY",
        point_type="fx_rate",
        region="domestic",
    )


_REGISTRY: dict[str, ProviderFn] = {
    "cn_exchange_stock": _fetch_cn_exchange_stock,
    "cn_exchange_fund": _fetch_cn_exchange_fund,
    "cn_mutual_fund": _fetch_cn_mutual_fund,
    "hk_stock": lambda req, s, e: _fetch_hk_equity(req, s, e, "equity"),
    "hk_etf": lambda req, s, e: _fetch_hk_equity(req, s, e, "equity"),
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

    logger.info(
        "fetch instrument market=%s type=%s code=%s start=%s end=%s adjust=%s",
        req.market,
        req.instrument_type,
        req.source_code,
        start,
        end,
        req.adjust_policy,
    )
    result = provider(req, start, end)
    points = normalize_dataframe(result.df)
    logger.info(
        "fetch instrument %s: normalized %d points via %s",
        req.source_code,
        len(points),
        result.source_name,
    )
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

    provider_symbol = req.source_code
    if req.instrument_type in ("hk_stock", "hk_etf"):
        provider_symbol = hk_exchange_symbol(req.source_code)

    return FetchData(
        provider="akshare",
        provider_symbol=provider_symbol,
        name=result.name,
        asset_class=result.asset_class,
        currency=result.currency,
        point_type=result.point_type,
        expense_ratio_status=expense_status,  # type: ignore[arg-type]
        expense_ratio_components={
            **components,
            **({"expense_ratio": expense_ratio} if expense_ratio is not None else {}),
        },
        points=points,
        source_name=result.source_name,
        source_quality=quality,
    )
