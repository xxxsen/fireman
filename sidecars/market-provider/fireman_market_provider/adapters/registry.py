"""Instrument-type adapters with ordered AKShare fallback chains.

Adapters return market data only (points, currency, point type, source
semantics). They never emit FIRE asset classes — that classification lives
exclusively in the user's plan holdings. CN on-exchange requests must carry
an explicit sh/sz/bj region prefix sourced from the asset directory; the
adapters only convert that identity into upstream query formats and never
infer an exchange from code prefixes or names.
"""

from __future__ import annotations

import time
from dataclasses import dataclass
from typing import Any, Callable

import pandas as pd

from ..logutil import get_logger
from ..normalize import normalize_dataframe
from ..schemas import FetchData, FetchRequest, PointType
from ..timeout_util import UpstreamCall, call_with_timeout, fetch_timeout_seconds, fetch_upstream_timeout_seconds
from .classification import (
    CnMutualFundSourceKind,
    default_region,
    describe_cn_mutual_fund,
)
from .cn_code import require_explicit_cn_code
from .fallback import try_sources
from .names import lookup_cn_mutual_fund_name_readonly, name_from_dataframe
from .symbols import hk_adjust_policy, hk_exchange_symbol, sina_adjust_policy, tx_adjust_policy
from .tickflow import (
    TICKFLOW_KLINES_SOURCE,
    tickflow_allowed_for_request,
    tickflow_symbol,
    try_tickflow_klines,
)


@dataclass(frozen=True)
class AdapterResult:
    df: pd.DataFrame
    source_name: str
    name: str
    currency: str
    point_type: PointType
    expense_ratio: float | None = None
    expense_ratio_status: str = "unavailable"
    expense_ratio_components: dict[str, Any] | None = None
    region: str = "domestic"
    source_kind: CnMutualFundSourceKind | None = None


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


def _cn_em_adjust(adjust_policy: str) -> str:
    if adjust_policy == "hfq":
        return "hfq"
    if adjust_policy == "none":
        return ""
    raise ValueError(f"unsupported adjust policy {adjust_policy}")


def _cn_stock_em_adjust(adjust_policy: str) -> str:
    return _cn_em_adjust(adjust_policy)


def _exchange_point_type(adjust_policy: str) -> PointType:
    """Keep the stored point type consistent with the requested price semantics."""
    return "close" if adjust_policy == "none" else "adjusted_close"


def _fetch_cn_exchange_stock(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    import akshare as ak

    # Exchange identity must be explicit (directory-provided region prefix);
    # raises AssetIdentityError otherwise instead of guessing from prefixes.
    identity = require_explicit_cn_code(req.source_code)
    canonical = identity.canonical_code
    em_symbol = identity.eastmoney_symbol
    prefixed = identity.prefixed_symbol
    policy = req.adjust_policy if req.adjust_policy in ("hfq", "none") else "hfq"
    em_adjust = _cn_stock_em_adjust(policy)
    tx_adjust = tx_adjust_policy(policy)
    sina_adjust = sina_adjust_policy(policy)

    if tickflow_allowed_for_request(req):
        tf_df = try_tickflow_klines(
            req,
            tickflow_symbol(identity.eastmoney_symbol, identity.exchange),
            start,
            end,
        )
        if tf_df is not None:
            name = (req.resolved_name or "").strip() or canonical
            return AdapterResult(
                df=tf_df,
                source_name=TICKFLOW_KLINES_SOURCE,
                name=name,
                currency="CNY",
                point_type=_exchange_point_type(policy),
                region="domestic",
            )

    sources: list[tuple[str, UpstreamCall]] = [
        (
            "ak.stock_zh_a_hist",
            UpstreamCall(
                "stock_zh_a_hist",
                kwargs=(
                    ("symbol", em_symbol),
                    ("period", "daily"),
                    ("start_date", start),
                    ("end_date", end),
                    ("adjust", em_adjust),
                ),
            ),
        ),
        (
            "ak.stock_zh_a_hist_tx",
            UpstreamCall(
                "stock_zh_a_hist_tx",
                kwargs=(
                    ("symbol", prefixed),
                    ("start_date", start),
                    ("end_date", end),
                    ("adjust", tx_adjust),
                ),
            ),
        ),
        (
            "ak.stock_zh_a_daily",
            UpstreamCall(
                "stock_zh_a_daily",
                kwargs=(
                    ("symbol", prefixed),
                    ("start_date", start),
                    ("end_date", end),
                    ("adjust", sina_adjust),
                ),
            ),
        ),
    ]

    df, source_name = try_sources("cn_exchange_stock", sources, deadline)
    name = canonical
    if "股票名称" in df.columns and not df["股票名称"].empty:
        name = str(df["股票名称"].iloc[0])
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        currency="CNY",
        point_type=_exchange_point_type(policy),
        region="domestic",
    )


def _fetch_display_name(resolved_name: str | None, symbol: str, df: pd.DataFrame) -> str:
    if resolved_name and resolved_name.strip() and resolved_name.strip() != symbol:
        return resolved_name.strip()
    from_df = name_from_dataframe(df, symbol)
    if from_df:
        return from_df
    cached = lookup_cn_mutual_fund_name_readonly(symbol)
    if cached:
        return cached
    return symbol


def _fetch_cn_exchange_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    import akshare as ak

    # Directory identity only: the request must carry an explicit region
    # prefix; the parse below is pure format conversion.
    identity = require_explicit_cn_code(req.source_code)
    em_symbol = identity.eastmoney_symbol
    prefixed = identity.prefixed_symbol
    adjust = req.adjust_policy if req.adjust_policy in ("hfq", "none") else "hfq"
    em_adjust = _cn_em_adjust(adjust)
    tx_adjust = tx_adjust_policy(adjust)

    # TickFlow priority fetch: the central gate limits this to resolved
    # etf/index_etf kinds under the configured adjust policy; LOF and unknown
    # kinds always stay on the AKShare chain below.
    if tickflow_allowed_for_request(req):
        tf_df = try_tickflow_klines(
            req,
            tickflow_symbol(identity.eastmoney_symbol, identity.exchange),
            start,
            end,
        )
        if tf_df is not None:
            return AdapterResult(
                df=tf_df,
                source_name=TICKFLOW_KLINES_SOURCE,
                name=_fetch_display_name(req.resolved_name, em_symbol, tf_df),
                currency="CNY",
                point_type=_exchange_point_type(adjust),
                region="domestic",
            )

    etf_hist = (
        "ak.fund_etf_hist_em",
        UpstreamCall(
            "fund_etf_hist_em",
            kwargs=(
                ("symbol", em_symbol),
                ("period", "daily"),
                ("start_date", start),
                ("end_date", end),
                ("adjust", em_adjust),
            ),
        ),
    )
    # stock_zh_a_hist_tx is keyed by the *prefixed* (exchange-qualified) symbol, so it
    # always returns the same security's quote — identity-safe for both ETF and LOF.
    tx_hist = (
        "ak.stock_zh_a_hist_tx",
        UpstreamCall(
            "stock_zh_a_hist_tx",
            kwargs=(
                ("symbol", prefixed),
                ("start_date", start),
                ("end_date", end),
                ("adjust", tx_adjust),
            ),
        ),
    )
    sina_hist = (
        "ak.fund_etf_hist_sina",
        UpstreamCall("fund_etf_hist_sina", kwargs=(("symbol", prefixed),)),
    )
    lof_hist = (
        "ak.fund_lof_hist_em",
        UpstreamCall(
            "fund_lof_hist_em",
            kwargs=(
                ("symbol", em_symbol),
                ("period", "daily"),
                ("start_date", start),
                ("end_date", end),
                ("adjust", em_adjust),
            ),
        ),
    )
    etf_info = (
        "ak.fund_etf_fund_info_em",
        UpstreamCall(
            "fund_etf_fund_info_em",
            kwargs=(("fund", em_symbol), ("start_date", start), ("end_date", end)),
        ),
    )

    # Select an identity-consistent source set from the directory-resolved kind
    # so a code that collides across ETF/LOF/stock never gets its history
    # silently pulled from the wrong instrument. fund_etf_hist_em (ETF) and
    # fund_lof_hist_em (LOF) are both keyed by the bare 6-digit code, so mixing
    # them is the core data-mixing risk. The kind is only ever the directory's
    # structured field — never guessed from names or codes.
    kind = (req.instrument_kind or "").strip().lower()
    sources: list[tuple[str, UpstreamCall]]
    if kind == "lof":
        sources = [lof_hist, tx_hist]
        if adjust == "none":
            sources.append(sina_hist)
    elif kind in ("etf", "index_etf"):
        sources = [etf_hist, tx_hist]
        if adjust == "none":
            sources.append(sina_hist)
            sources.append(etf_info)
    else:
        # Unknown/absent kind: try every compatible source in a fixed order.
        # A failing source never feeds back into the asset's identity.
        sources = [etf_hist, tx_hist]
        if adjust == "none":
            sources.append(sina_hist)
            sources.append(etf_info)
        sources.append(lof_hist)

    df, source_name = try_sources("cn_exchange_fund", sources, deadline)
    if source_name == "ak.stock_zh_a_hist_tx":
        df = _filter_df_by_date(df, start, end)
    name = _fetch_display_name(req.resolved_name, em_symbol, df)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        currency="CNY",
        point_type=_exchange_point_type(adjust),
        region="domestic",
    )


# Fixed candidate sources for cn_mutual_fund, tried in this order for every
# fund regardless of its name. CSRC mutual fund codes are unique across the
# open/money/financial fund families, so trying every family is identity-safe;
# the successful source decides source_kind. Name keywords must never route
# or gate these attempts.
_CN_MUTUAL_FUND_ATTEMPTS: tuple[tuple[CnMutualFundSourceKind, str, str], ...] = (
    ("open_fund", "total_return_index", "em.fund_open_history:累计净值走势"),
    ("open_fund", "nav", "em.fund_open_history:单位净值走势"),
    ("open_fund", "total_return_index", "ak.fund_open_fund_info_em:累计净值走势"),
    ("open_fund", "nav", "ak.fund_open_fund_info_em:单位净值走势"),
    ("money_fund", "nav", "ak.fund_money_fund_info_em"),
    ("financial_fund", "nav", "ak.fund_financial_fund_info_em"),
)


def _cn_mutual_fund_call(source_name: str, symbol: str) -> UpstreamCall:
    if source_name == "em.fund_open_history:累计净值走势":
        return UpstreamCall(
            "em_fund_open_history",
            kwargs=(("symbol", symbol), ("indicator", "累计净值走势")),
        )
    if source_name == "em.fund_open_history:单位净值走势":
        return UpstreamCall(
            "em_fund_open_history",
            kwargs=(("symbol", symbol), ("indicator", "单位净值走势")),
        )
    if source_name == "ak.fund_open_fund_info_em:累计净值走势":
        return UpstreamCall(
            "fund_open_fund_info_em",
            kwargs=(("symbol", symbol), ("indicator", "累计净值走势"), ("period", "成立来")),
        )
    if source_name == "ak.fund_open_fund_info_em:单位净值走势":
        return UpstreamCall(
            "fund_open_fund_info_em",
            kwargs=(("symbol", symbol), ("indicator", "单位净值走势"), ("period", "成立来")),
        )
    if source_name == "ak.fund_money_fund_info_em":
        return UpstreamCall("fund_money_fund_info_em", kwargs=(("symbol", symbol),))
    return UpstreamCall("fund_financial_fund_info_em", kwargs=(("symbol", symbol),))


def _fetch_cn_mutual_fund(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    symbol = req.source_code
    errors: list[str] = []

    logger.info(
        "fetch cn_mutual_fund %s: date range %s..%s (%d fixed candidate sources)",
        symbol,
        start,
        end,
        len(_CN_MUTUAL_FUND_ATTEMPTS),
    )

    for source_kind, point_type, source_name in _CN_MUTUAL_FUND_ATTEMPTS:
        remaining = int(deadline - time.monotonic())
        if remaining <= 0:
            raise TimeoutError(f"fetch cn_mutual_fund {symbol}: deadline exceeded")
        timeout = fetch_upstream_timeout_seconds(remaining)
        call = _cn_mutual_fund_call(source_name, symbol)
        try:
            df = call_with_timeout(call, timeout)
            if df is None or df.empty:
                errors.append(f"{source_name}: empty")
                logger.warning("fetch cn_mutual_fund %s: %s returned empty", symbol, source_name)
                continue
            meta = describe_cn_mutual_fund(df, symbol, req.resolved_name)
            df = _filter_df_by_date(df, start, end)
            if df.empty:
                errors.append(f"{source_name}: empty after date filter")
                logger.warning(
                    "fetch cn_mutual_fund %s: %s empty after date filter",
                    symbol,
                    source_name,
                )
                continue
            points_probe = normalize_dataframe(df)
            if not points_probe:
                errors.append(f"{source_name}: no parseable points")
                logger.warning(
                    "fetch cn_mutual_fund %s: %s produced no parseable points",
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
                currency="CNY",
                point_type=point_type,  # type: ignore[arg-type]
                expense_ratio=meta.expense_ratio,
                expense_ratio_status=meta.expense_ratio_status,
                expense_ratio_components=meta.components,
                region=meta.region,
                source_kind=source_kind,
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

    summary = "; ".join(errors) or "cn_mutual_fund fetch failed for all candidate sources"
    logger.error("fetch cn_mutual_fund %s: all sources failed: %s", symbol, summary)
    raise RuntimeError(summary)


def _fetch_us_equity(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    import akshare as ak

    symbol = req.source_code
    adjust = "" if req.adjust_policy == "none" else req.adjust_policy
    sources: list[tuple[str, UpstreamCall]] = [
        (
            "ak.stock_us_daily",
            UpstreamCall("stock_us_daily", kwargs=(("symbol", symbol), ("adjust", adjust))),
        ),
        (
            "ak.stock_us_hist",
            UpstreamCall(
                "stock_us_hist",
                kwargs=(
                    ("symbol", symbol),
                    ("start_date", start),
                    ("end_date", end),
                    ("adjust", adjust),
                ),
            ),
        ),
    ]
    df, source_name = try_sources("us equity", sources, deadline)
    name = (req.resolved_name or "").strip() or symbol
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        currency="USD",
        point_type=_exchange_point_type(req.adjust_policy),
        region="foreign",
    )


def _fetch_hk_equity(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    import akshare as ak

    symbol = hk_exchange_symbol(req.source_code)
    adjust = hk_adjust_policy(req.adjust_policy)
    sources: list[tuple[str, UpstreamCall]] = [
        (
            "ak.stock_hk_hist",
            UpstreamCall(
                "stock_hk_hist",
                kwargs=(
                    ("symbol", symbol),
                    ("period", "daily"),
                    ("start_date", start),
                    ("end_date", end),
                    ("adjust", adjust),
                ),
            ),
        ),
        (
            "ak.stock_hk_daily",
            UpstreamCall("stock_hk_daily", kwargs=(("symbol", symbol), ("adjust", adjust))),
        ),
    ]
    df, source_name = try_sources("hk equity", sources, deadline)
    if source_name == "ak.stock_hk_daily":
        df = _filter_df_by_date(df, start, end)
    name = _fetch_display_name(req.resolved_name, symbol, df)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=name,
        currency="HKD",
        point_type=_exchange_point_type(req.adjust_policy),
        region="foreign",
    )


def _fetch_fx_rate(req: FetchRequest, start: str, end: str) -> AdapterResult:
    deadline = time.monotonic() + fetch_timeout_seconds()
    import akshare as ak

    code = req.source_code.upper()
    pair_map = {
        "USDCNY": ("美元", "CNY"),
        "HKDCNY": ("港币", "CNY"),
    }
    if code not in pair_map:
        raise ValueError(f"unsupported fx code {code}")
    label, _ = pair_map[code]
    sources: list[tuple[str, UpstreamCall]] = [
        ("ak.currency_boc_sina", UpstreamCall("currency_boc_sina", kwargs=(("symbol", label),))),
        ("ak.fx_pair_quote", UpstreamCall("fx_pair_quote", kwargs=(("symbol", code),))),
    ]
    df, source_name = try_sources("fx_rate", sources, deadline)
    return AdapterResult(
        df=df,
        source_name=source_name,
        name=code,
        currency="CNY",
        point_type="fx_rate",
        region="domestic",
    )


_REGISTRY: dict[str, ProviderFn] = {
    "cn_exchange_stock": _fetch_cn_exchange_stock,
    "cn_exchange_fund": _fetch_cn_exchange_fund,
    "cn_mutual_fund": _fetch_cn_mutual_fund,
    "hk_stock": _fetch_hk_equity,
    "hk_etf": _fetch_hk_equity,
    "us_stock": _fetch_us_equity,
    "us_etf": _fetch_us_equity,
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

    provider_symbol = req.source_code.strip().lower()
    if req.instrument_type in ("cn_exchange_fund", "cn_exchange_stock"):
        provider_symbol = require_explicit_cn_code(req.source_code).canonical_code
    elif req.instrument_type in ("hk_stock", "hk_etf"):
        provider_symbol = hk_exchange_symbol(req.source_code)

    return FetchData(
        provider="akshare",
        provider_symbol=provider_symbol,
        name=result.name,
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
        source_kind=result.source_kind,
    )
