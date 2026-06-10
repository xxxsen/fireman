"""Lightweight instrument resolve (spot/metadata only, no history fetch)."""

from __future__ import annotations

from dataclasses import dataclass

from ..schemas import ResolveCandidate, ResolveData, ResolveRequest
from ..timeout_util import call_with_timeout, resolve_timeout_seconds
from .names import _load_etf_name_map, _load_lof_name_map, _load_hk_name_map, reset_name_caches
from .symbols import hk_exchange_symbol


@dataclass(frozen=True)
class _Candidate:
    code: str
    provider_symbol: str
    name: str
    exchange: str
    instrument_kind: str


def _bare_digits(code: str) -> str:
    raw = code.strip().lower()
    if raw.startswith(("sh", "sz", "bj")):
        raw = raw[2:]
    digits = "".join(ch for ch in raw if ch.isdigit())
    return digits.zfill(6) if digits else raw


def _has_exchange_prefix(code: str) -> bool:
    raw = code.strip().lower()
    return raw.startswith(("sh", "sz", "bj"))


def _normalize_prefixed(code: str) -> str:
    raw = code.strip().lower()
    if raw.startswith(("sh", "sz", "bj")):
        return raw
    return raw


def _exchange_from_prefix(symbol: str) -> str:
    raw = symbol.strip().lower()
    if raw.startswith("sh"):
        return "SH"
    if raw.startswith("sz"):
        return "SZ"
    if raw.startswith("bj"):
        return "BJ"
    return ""


def _load_stock_name_map() -> dict[str, str]:
    import akshare as ak

    df = call_with_timeout(lambda: ak.stock_zh_a_spot_em(), resolve_timeout_seconds())
    code_col = "代码" if "代码" in df.columns else None
    name_col = "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return {}
    return {
        str(row[code_col]).strip().zfill(6): str(row[name_col]).strip()
        for _, row in df.iterrows()
        if str(row.get(code_col, "")).strip() and str(row.get(name_col, "")).strip()
    }


def _load_us_name(symbol: str) -> str | None:
    import akshare as ak

    try:
        df = call_with_timeout(lambda: ak.stock_us_spot_em(), resolve_timeout_seconds())
    except Exception:  # noqa: BLE001
        return None
    code_col = "symbol" if "symbol" in df.columns else "代码" if "代码" in df.columns else None
    name_col = "name" if "name" in df.columns else "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return None
    target = symbol.strip().upper()
    for _, row in df.iterrows():
        code = str(row.get(code_col, "")).strip().upper()
        if code == target:
            name = str(row.get(name_col, "")).strip()
            return name or target
    return None


def _load_cn_mutual_fund_name(symbol: str) -> str | None:
    import akshare as ak

    bare = _bare_digits(symbol)
    try:
        df = call_with_timeout(lambda: ak.fund_name_em(), resolve_timeout_seconds())
    except Exception:  # noqa: BLE001
        return None
    code_col = "基金代码" if "基金代码" in df.columns else "代码" if "代码" in df.columns else None
    name_col = "基金简称" if "基金简称" in df.columns else "名称" if "名称" in df.columns else None
    if code_col is None or name_col is None:
        return None
    for _, row in df.iterrows():
        code = str(row.get(code_col, "")).strip().zfill(6)
        if code == bare:
            name = str(row.get(name_col, "")).strip()
            return name or bare
    return None


def _dedupe_candidates(items: list[_Candidate]) -> list[_Candidate]:
    seen: set[str] = set()
    out: list[_Candidate] = []
    for item in items:
        if item.provider_symbol in seen:
            continue
        seen.add(item.provider_symbol)
        out.append(item)
    return out


def _resolve_cn_exchange_fund(code: str) -> list[_Candidate]:
    bare = _bare_digits(code)
    if _has_exchange_prefix(code):
        sym = _normalize_prefixed(code)
        etf_map = _load_etf_name_map()
        lof_map = _load_lof_name_map()
        name = etf_map.get(bare) or lof_map.get(bare)
        if not name:
            return []
        kind = "lof" if bare in lof_map and bare not in etf_map else "etf"
        return [_Candidate(sym, sym, name, _exchange_from_prefix(sym), kind)]

    etf_map = _load_etf_name_map()
    lof_map = _load_lof_name_map()
    stock_map = _load_stock_name_map()
    out: list[_Candidate] = []

    if bare in etf_map:
        sym = f"sh{bare}"
        out.append(_Candidate(sym, sym, etf_map[bare], "SH", "index_etf"))
    if bare in lof_map:
        for prefix in ("sh", "sz"):
            sym = f"{prefix}{bare}"
            out.append(_Candidate(sym, sym, lof_map[bare], prefix.upper(), "lof"))
    if bare in stock_map:
        sym = f"sz{bare}"
        out.append(_Candidate(sym, sym, stock_map[bare], "SZ", "stock"))

    return _dedupe_candidates(out)


def _resolve_cn_exchange_stock(code: str) -> list[_Candidate]:
    bare = _bare_digits(code)
    stock_map = _load_stock_name_map()
    if bare not in stock_map:
        return []

    if _has_exchange_prefix(code):
        sym = _normalize_prefixed(code)
        return [_Candidate(sym, sym, stock_map[bare], _exchange_from_prefix(sym), "stock")]

    out: list[_Candidate] = []
    for prefix in ("sh", "sz"):
        if prefix == "sh" and not (bare.startswith(("5", "6", "9")) or bare.startswith("688")):
            continue
        if prefix == "sz" and not bare.startswith(("0", "1", "2", "3", "4")):
            continue
        sym = f"{prefix}{bare}"
        out.append(_Candidate(sym, sym, stock_map[bare], prefix.upper(), "stock"))

    if not out:
        sym = f"sz{bare}"
        out.append(_Candidate(sym, sym, stock_map[bare], "SZ", "stock"))
    return _dedupe_candidates(out)


def _resolve_cn_mutual_fund(code: str) -> list[_Candidate]:
    bare = _bare_digits(code)
    name = _load_cn_mutual_fund_name(bare)
    if not name:
        return []
    sym = bare
    return [_Candidate(sym, sym, name, "", "mutual_fund")]


def _resolve_hk(code: str, kind: str) -> list[_Candidate]:
    sym = hk_exchange_symbol(code)
    name = _load_hk_name_map().get(sym)
    if not name:
        return []
    instrument_kind = "etf" if kind == "hk_etf" else "stock"
    return [_Candidate(sym, sym, name, "HK", instrument_kind)]


def _resolve_us(code: str, kind: str) -> list[_Candidate]:
    sym = code.strip().upper()
    name = _load_us_name(sym)
    if not name:
        return []
    instrument_kind = "etf" if kind == "us_etf" else "stock"
    return [_Candidate(sym, sym, name, "US", instrument_kind)]


def resolve_instrument(req: ResolveRequest) -> ResolveData:
    reset_name_caches()
    code = req.code.strip()
    if not code:
        raise ValueError("code is required")

    resolver_map = {
        "cn_exchange_fund": _resolve_cn_exchange_fund,
        "cn_exchange_stock": _resolve_cn_exchange_stock,
        "cn_mutual_fund": _resolve_cn_mutual_fund,
        "hk_stock": lambda c: _resolve_hk(c, "hk_stock"),
        "hk_etf": lambda c: _resolve_hk(c, "hk_etf"),
        "us_stock": lambda c: _resolve_us(c, "us_stock"),
        "us_etf": lambda c: _resolve_us(c, "us_etf"),
    }
    resolver = resolver_map.get(req.instrument_type)
    if resolver is None:
        raise ValueError(f"unsupported instrument_type {req.instrument_type}")

    found = resolver(code)
    if not found:
        raise ValueError("instrument_not_found")

    if len(found) == 1:
        c = found[0]
        return ResolveData(
            ambiguous=False,
            resolved=ResolveCandidate(
                code=c.code,
                provider_symbol=c.provider_symbol,
                name=c.name,
                exchange=c.exchange,
                instrument_kind=c.instrument_kind,
            ),
        )

    return ResolveData(
        ambiguous=True,
        candidates=[
            ResolveCandidate(
                code=c.code,
                provider_symbol=c.provider_symbol,
                name=c.name,
                exchange=c.exchange,
                instrument_kind=c.instrument_kind,
            )
            for c in found
        ],
    )
