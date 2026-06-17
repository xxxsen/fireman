"""Lightweight instrument resolve (spot/metadata only, no history fetch)."""

from __future__ import annotations

import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from ..logutil import get_logger
from ..schemas import ResolveCandidate, ResolveData, ResolveRequest
from ..timeout_util import UpstreamCall, call_with_timeout, log_timeout_event, resolve_deadline_seconds, resolve_timeout_seconds
from .cn_code import (
    CNExchangeCode,
    build_from_market_id,
    heuristic_cn_stock_from_bare,
    parse_cn_etf_code,
    parse_cn_lof_code,
    parse_cn_stock_code,
)
from .names import (
    _load_etf_name_map,
    _load_hk_name_map,
    _load_lof_name_map,
    _load_stock_name_map,
    lookup_cn_lof_name,
    lookup_cn_mutual_fund_name_readonly,
    lookup_cn_stock_name,
    lookup_cross_listed_etf_name,
    resolve_cn_mutual_fund_name,
)
from .symbols import hk_exchange_symbol

logger = get_logger(__name__)


def _raise_resolve_timeout(code: str, deadline: float, message: str = "resolve deadline exceeded") -> None:
    elapsed_ms = int((time.monotonic() + resolve_deadline_seconds() - deadline) * 1000)
    remaining_ms = max(0, int((deadline - time.monotonic()) * 1000))
    log_timeout_event(
        logger,
        operation="resolve_cn_exchange_fund",
        symbol=code,
        elapsed_ms=max(0, elapsed_ms),
        remaining_ms=remaining_ms,
        layer="sidecar",
        message=message,
    )
    raise TimeoutError("market_provider_timeout")


_VALID_MARKET_TYPES: dict[str, frozenset[str]] = {
    "CN": frozenset({"cn_exchange_fund", "cn_exchange_stock", "cn_mutual_fund", "fx_rate"}),
    "HK": frozenset({"hk_stock", "hk_etf"}),
    "US": frozenset({"us_stock", "us_etf"}),
}


@dataclass(frozen=True)
class _Candidate:
    code: str
    provider_symbol: str
    name: str
    exchange: str
    instrument_kind: str


def _validate_market_type(market: str, instrument_type: str) -> None:
    allowed = _VALID_MARKET_TYPES.get(market.upper())
    if allowed is None or instrument_type not in allowed:
        raise ValueError("invalid_request")


def _has_exchange_prefix(code: str) -> bool:
    raw = code.strip().lower()
    return raw.startswith(("sh", "sz", "bj"))


def _bare_digits(code: str) -> str:
    raw = code.strip().lower()
    if raw.startswith(("sh", "sz", "bj")):
        raw = raw[2:]
    digits = "".join(ch for ch in raw if ch.isdigit())
    return digits.zfill(6) if digits else raw


def _load_us_name(symbol: str, deadline: float) -> str | None:
    import akshare as ak

    remaining = max(1, int(deadline - time.monotonic()))
    try:
        df = call_with_timeout(UpstreamCall("stock_us_spot_em"), remaining)
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


def _maybe_raise_mutual_fund_mismatch(
    code: str,
    bare: str,
    etf_map: dict[str, str],
    lof_map: dict[str, str],
    stock_map: dict[str, str],
    deadline: float,
) -> None:
    if _has_exchange_prefix(code):
        return
    if bare in etf_map or bare in lof_map or bare in stock_map:
        return
    if lookup_cn_mutual_fund_name_readonly(bare) or resolve_cn_mutual_fund_name(bare, deadline):
        raise ValueError("instrument_type_mismatch")


def _candidate_identity(code: str, provider_symbol: str, instrument_kind: str, exchange: str) -> str:
    return f"{code}|{provider_symbol}|{instrument_kind}|{exchange}"


def _to_resolve_candidate(c: _Candidate) -> ResolveCandidate:
    return ResolveCandidate(
        code=c.code,
        provider_symbol=c.provider_symbol,
        name=c.name,
        exchange=c.exchange,
        instrument_kind=c.instrument_kind,
        candidate_id=_candidate_identity(c.code, c.provider_symbol, c.instrument_kind, c.exchange),
    )


def _candidate_from_cn(parsed: CNExchangeCode, name: str, kind: str) -> _Candidate:
    return _Candidate(
        code=parsed.canonical_code,
        provider_symbol=parsed.canonical_code,
        name=name,
        exchange=parsed.exchange,
        instrument_kind=kind,
    )


def _dedupe_candidates(items: list[_Candidate]) -> list[_Candidate]:
    seen: set[tuple[str, str]] = set()
    out: list[_Candidate] = []
    for item in items:
        key = (item.provider_symbol, item.instrument_kind)
        if key in seen:
            continue
        seen.add(key)
        out.append(item)
    return out


@dataclass(frozen=True)
class _SpotMaps:
    etf: dict[str, str]
    lof: dict[str, str]
    stock: dict[str, str]
    load_failed: bool = False


def _load_cn_exchange_spot_maps(deadline: float) -> _SpotMaps:
    etf_map: dict[str, str] = {}
    lof_map: dict[str, str] = {}
    stock_map: dict[str, str] = {}
    load_failed = False
    with ThreadPoolExecutor(max_workers=3) as pool:
        futures = {
            pool.submit(_load_etf_name_map, deadline): "etf",
            pool.submit(_load_lof_name_map, deadline): "lof",
            pool.submit(_load_stock_name_map, deadline): "stock",
        }
        for fut in as_completed(futures):
            kind = futures[fut]
            try:
                data = fut.result()
            except (TimeoutError, RuntimeError):
                load_failed = True
                data = {}
            except Exception:  # noqa: BLE001
                load_failed = True
                data = {}
            if kind == "etf":
                etf_map = data
            elif kind == "lof":
                lof_map = data
            else:
                stock_map = data
    return _SpotMaps(etf=etf_map, lof=lof_map, stock=stock_map, load_failed=load_failed)


def _stock_candidates_from_bare(bare: str, stock_name: str) -> list[_Candidate]:
    heuristic = _heuristic_stock_candidates_from_bare(bare, stock_name)
    if heuristic:
        return heuristic
    out: list[_Candidate] = []
    for prefix in ("sh", "sz", "bj"):
        parsed_stock = parse_cn_stock_code(f"{prefix}{bare}")
        if parsed_stock is not None:
            out.append(_candidate_from_cn(parsed_stock, stock_name, "stock"))
    return _dedupe_candidates(out)


def _cross_listed_etf_candidate(
    bare: str,
    etf_name: str,
    stock_candidates: list[_Candidate],
) -> _Candidate | None:
    stock_exchanges = {c.exchange for c in stock_candidates if c.instrument_kind == "stock"}
    if len(stock_exchanges) == 1:
        opposite_mid = {"SZ": 1, "SH": 0}.get(next(iter(stock_exchanges)))
        if opposite_mid is not None:
            return _candidate_from_cn(
                build_from_market_id(bare, opposite_mid),
                etf_name,
                "index_etf",
            )
    parsed = parse_cn_etf_code(bare)
    if parsed is None:
        return None
    return _candidate_from_cn(parsed, etf_name, "index_etf")


def _etf_and_stock_candidates_for_bare(
    bare: str,
    *,
    etf_name: str | None,
    in_etf_map: bool,
    in_stock_map: bool,
    stock_name: str | None,
    deadline: float,
) -> list[_Candidate]:
    stock_part: list[_Candidate] = []
    if in_stock_map and stock_name:
        stock_part = _stock_candidates_from_bare(bare, stock_name)

    out: list[_Candidate] = []
    if in_etf_map and etf_name:
        etf = _cross_listed_etf_candidate(bare, etf_name, stock_part)
        if etf is not None:
            out.append(etf)
    elif stock_part:
        cross_name = etf_name or lookup_cross_listed_etf_name(bare, deadline)
        if cross_name:
            etf = _cross_listed_etf_candidate(bare, cross_name, stock_part)
            if etf is not None:
                out.append(etf)

    out.extend(stock_part)
    return _dedupe_candidates(out)


_CROSS_LIST_BARE_PREFIXES = ("00", "30")


def _fallback_bare_cn_exchange_fund(
    bare: str,
    deadline: float,
    *,
    stock_map: dict[str, str],
) -> list[_Candidate]:
    """Cross-listed bare codes only; never infer on-exchange identity from fund prefixes."""
    if not bare.startswith(_CROSS_LIST_BARE_PREFIXES):
        return []

    stock_name = stock_map.get(bare)
    if not stock_name:
        return []
    stock_part = _heuristic_stock_candidates_from_bare(bare, stock_name)
    if not stock_part:
        return []

    etf_name = lookup_cross_listed_etf_name(bare, deadline)
    if not etf_name:
        return _dedupe_candidates(stock_part)

    cross_etf = _cross_listed_etf_candidate(bare, etf_name, stock_part)
    if cross_etf is not None and cross_etf.exchange != stock_part[0].exchange:
        return _dedupe_candidates([cross_etf, *stock_part])
    return _dedupe_candidates(stock_part)


def _fallback_cn_exchange_fund_candidates(
    code: str,
    bare: str,
    deadline: float,
    *,
    etf_map: dict[str, str],
    lof_map: dict[str, str],
) -> list[_Candidate]:
    """Authoritative spot maps, explicit exchange prefix, or LOF code-id map only."""
    out: list[_Candidate] = []

    if bare in etf_map:
        parsed_etf = parse_cn_etf_code(code if _has_exchange_prefix(code) else bare)
        if parsed_etf is not None:
            kind = "lof" if bare in lof_map else "index_etf"
            out.append(_candidate_from_cn(parsed_etf, etf_map[bare], kind))

    if bare in lof_map:
        parsed_lof = _parse_authoritative_lof(code, bare, deadline, in_lof_map=True)
        if parsed_lof is not None:
            out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))

    if _has_exchange_prefix(code):
        resolve_code = code.strip().lower()
        if bare not in etf_map and bare not in lof_map:
            parsed_etf = parse_cn_etf_code(resolve_code)
            if parsed_etf is not None:
                out.append(_candidate_from_cn(parsed_etf, bare, "index_etf"))
        if time.monotonic() < deadline - 2:
            parsed_lof = _parse_authoritative_lof(code, bare, deadline)
            if parsed_lof is not None:
                name = lof_map.get(bare) or bare
                out.append(_candidate_from_cn(parsed_lof, name, "lof"))

    return _dedupe_candidates(out)


def _parse_authoritative_lof(
    code: str,
    bare: str,
    deadline: float,
    *,
    in_lof_map: bool = False,
) -> CNExchangeCode | None:
    resolve_code = code.strip().lower() if _has_exchange_prefix(code) else bare
    try:
        parsed = parse_cn_lof_code(resolve_code, deadline=deadline)
        if parsed is not None:
            return parsed
    except Exception:  # noqa: BLE001
        parsed = None
    if in_lof_map:
        return build_from_market_id(bare, 0)
    return None


def _is_placeholder_fund_name(name: str, bare: str) -> bool:
    normalized = name.strip()
    return normalized == bare or normalized == bare.lstrip("0") or normalized == bare.zfill(6)


def _prefer_lof_over_placeholder_etf(candidates: list[_Candidate]) -> list[_Candidate]:
    if not any(c.instrument_kind == "lof" for c in candidates):
        return candidates
    bare = _bare_digits(candidates[0].code)
    filtered: list[_Candidate] = []
    for c in candidates:
        if c.instrument_kind == "index_etf" and _is_placeholder_fund_name(c.name, bare):
            continue
        filtered.append(c)
    return _dedupe_candidates(filtered) if filtered else candidates


def _heuristic_stock_candidates_from_bare(bare: str, stock_name: str) -> list[_Candidate]:
    parsed = heuristic_cn_stock_from_bare(bare)
    if parsed is None:
        return []
    return [_candidate_from_cn(parsed, stock_name, "stock")]


def _resolve_cn_exchange_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    spots = _load_cn_exchange_spot_maps(deadline)
    etf_map, lof_map, stock_map = spots.etf, spots.lof, spots.stock

    if _has_exchange_prefix(code):
        out: list[_Candidate] = []
        if bare in etf_map:
            parsed_etf = parse_cn_etf_code(code)
            if parsed_etf:
                kind = "etf" if bare in lof_map else "index_etf"
                out.append(_candidate_from_cn(parsed_etf, etf_map[bare], kind))
        if bare in lof_map:
            parsed_lof = _parse_authoritative_lof(code, bare, deadline, in_lof_map=False)
            if parsed_lof:
                out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))
        result = _dedupe_candidates(_prefer_lof_over_placeholder_etf(out))
        if not result and spots.load_failed:
            result = _fallback_cn_exchange_fund_candidates(
                code, bare, deadline, etf_map=etf_map, lof_map=lof_map,
            )
        if not result and spots.load_failed:
            _raise_resolve_timeout(code, deadline)
        return result

    out: list[_Candidate] = []
    if bare in lof_map:
        parsed_lof = _parse_authoritative_lof(code, bare, deadline, in_lof_map=True)
        if parsed_lof:
            out.append(_candidate_from_cn(parsed_lof, lof_map[bare], "lof"))

    out = _dedupe_candidates(
        _etf_and_stock_candidates_for_bare(
            bare,
            etf_name=etf_map.get(bare),
            in_etf_map=bare in etf_map,
            in_stock_map=bare in stock_map,
            stock_name=stock_map.get(bare),
            deadline=deadline,
        )
        + out
    )

    result = out
    if not result and spots.load_failed:
        result = _fallback_cn_exchange_fund_candidates(
            code, bare, deadline, etf_map=etf_map, lof_map=lof_map,
        )
        if not result:
            result = _fallback_bare_cn_exchange_fund(bare, deadline, stock_map=stock_map)
    if not result:
        _maybe_raise_mutual_fund_mismatch(code, bare, etf_map, lof_map, stock_map, deadline)
        if spots.load_failed:
            _raise_resolve_timeout(code, deadline)
    return result


def _resolve_cn_exchange_stock(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    spots = _load_cn_exchange_spot_maps(deadline)
    stock_map = spots.stock
    if bare not in stock_map:
        return []

    prefixed = parse_cn_stock_code(code)
    if prefixed is not None:
        return [_candidate_from_cn(prefixed, stock_map[bare], "stock")]

    out: list[_Candidate] = []
    for prefix in ("sh", "sz", "bj"):
        candidate_code = f"{prefix}{bare}"
        parsed = parse_cn_stock_code(candidate_code)
        if parsed is not None:
            out.append(_candidate_from_cn(parsed, stock_map[bare], "stock"))
    return _dedupe_candidates(out)


def _resolve_cn_mutual_fund(code: str, deadline: float) -> list[_Candidate]:
    bare = _bare_digits(code)
    name = resolve_cn_mutual_fund_name(bare, deadline)
    if not name:
        return []
    return [_Candidate(bare, bare, name, "", "mutual_fund")]


def _resolve_hk(code: str, kind: str, deadline: float) -> list[_Candidate]:
    sym = hk_exchange_symbol(code)
    name = _load_hk_name_map(deadline).get(sym)
    if not name:
        return []
    instrument_kind = "etf" if kind == "hk_etf" else "stock"
    return [_Candidate(sym, sym, name, "HK", instrument_kind)]


def _resolve_us(code: str, kind: str, deadline: float) -> list[_Candidate]:
    sym = code.strip().upper()
    name = _load_us_name(sym, deadline)
    if not name:
        return []
    instrument_kind = "etf" if kind == "us_etf" else "stock"
    return [_Candidate(sym, sym, name, "US", instrument_kind)]


def resolve_instrument(req: ResolveRequest) -> ResolveData:
    code = req.code.strip()
    if not code:
        raise ValueError("code is required")

    _validate_market_type(req.market, req.instrument_type)

    deadline = time.monotonic() + resolve_deadline_seconds()

    resolver_map = {
        "cn_exchange_fund": lambda c: _resolve_cn_exchange_fund(c, deadline),
        "cn_exchange_stock": lambda c: _resolve_cn_exchange_stock(c, deadline),
        "cn_mutual_fund": lambda c: _resolve_cn_mutual_fund(c, deadline),
        "hk_stock": lambda c: _resolve_hk(c, "hk_stock", deadline),
        "hk_etf": lambda c: _resolve_hk(c, "hk_etf", deadline),
        "us_stock": lambda c: _resolve_us(c, "us_stock", deadline),
        "us_etf": lambda c: _resolve_us(c, "us_etf", deadline),
    }
    resolver = resolver_map.get(req.instrument_type)
    if resolver is None:
        raise ValueError(f"unsupported instrument_type {req.instrument_type}")

    found = resolver(code)
    if not found:
        raise ValueError("instrument_not_found")

    if len(found) == 1:
        return ResolveData(ambiguous=False, resolved=_to_resolve_candidate(found[0]))

    return ResolveData(
        ambiguous=True,
        candidates=[_to_resolve_candidate(c) for c in found],
    )
