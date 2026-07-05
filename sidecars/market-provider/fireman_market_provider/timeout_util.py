"""Hard timeout wrapper for upstream AKShare calls via terminable child processes."""

from __future__ import annotations

import multiprocessing as mp
import os
import queue
import time
from dataclasses import dataclass
from typing import Any, TypeVar

from .logutil import get_logger

T = TypeVar("T")

_logger = get_logger(__name__)

DEFAULT_TIMEOUT_SECONDS = 30
DEFAULT_FETCH_TIMEOUT_SECONDS = 240
DEFAULT_FETCH_UPSTREAM_TIMEOUT_SECONDS = 180
DEFAULT_RESOLVE_TIMEOUT_SECONDS = 60
DEFAULT_RESOLVE_DEADLINE_SECONDS = 70
DEFAULT_MUTUAL_FUND_NAME_FETCH_TIMEOUT_SECONDS = 60

# Registry of test-only dispatch overrides (keyed by operation name).
_TEST_DISPATCH_OVERRIDES: dict[str, Any] = {}


@dataclass(frozen=True)
class UpstreamCall:
    """Serializable description of an upstream AKShare operation."""

    operation: str
    args: tuple[Any, ...] = ()
    kwargs: tuple[tuple[str, Any], ...] = ()


def register_test_dispatch(operation: str, fn: Any) -> None:
    """Register a test override for a dispatcher operation."""
    _TEST_DISPATCH_OVERRIDES[operation] = fn


def clear_test_dispatch() -> None:
    """Clear all test dispatcher overrides."""
    _TEST_DISPATCH_OVERRIDES.clear()


def _kwargs_dict(call: UpstreamCall) -> dict[str, Any]:
    return dict(call.kwargs)


def dispatch_upstream_call(call: UpstreamCall) -> Any:
    """Execute an upstream call in the current process (used by child workers)."""
    override = _TEST_DISPATCH_OVERRIDES.get(call.operation)
    if override is not None:
        return override(*call.args, **_kwargs_dict(call))

    if call.operation == "time.sleep":
        import time as _time

        _time.sleep(call.args[0])
        return None

    # Custom Eastmoney directory listings (categories AKShare does not expose).
    if call.operation.startswith("em_"):
        from .adapters import em_directory

        fn = getattr(em_directory, call.operation, None)
        if fn is None:
            raise ValueError(f"unsupported upstream operation: {call.operation}")
        return fn(*call.args, **_kwargs_dict(call))

    # HKEX official directory listings (authoritative HK security categories).
    if call.operation.startswith("hkex_"):
        from .adapters import hkex_directory

        fn = getattr(hkex_directory, call.operation, None)
        if fn is None:
            raise ValueError(f"unsupported upstream operation: {call.operation}")
        return fn(*call.args, **_kwargs_dict(call))

    import akshare as ak

    fn = getattr(ak, call.operation, None)
    if fn is None:
        raise ValueError(f"unsupported upstream operation: {call.operation}")
    return fn(*call.args, **_kwargs_dict(call))


def _worker_entry(call: UpstreamCall, out: mp.Queue) -> None:
    try:
        out.put(("ok", dispatch_upstream_call(call)))
    except Exception as exc:  # noqa: BLE001
        out.put(("err", exc))


def call_with_timeout(call: UpstreamCall, timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS) -> Any:
    """Run call in a spawned child process; terminate the child on timeout."""
    if timeout_seconds <= 0:
        return dispatch_upstream_call(call)

    ctx = mp.get_context("spawn")
    out: mp.Queue = ctx.Queue(maxsize=1)
    proc = ctx.Process(target=_worker_entry, args=(call, out), daemon=True)
    proc.start()
    started = time.monotonic()
    deadline = started + timeout_seconds
    try:
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                proc.terminate()
                proc.join(timeout=1.0)
                if proc.is_alive():
                    proc.kill()
                    proc.join(timeout=1.0)
                elapsed_ms = int((time.monotonic() - started) * 1000)
                log_timeout_event(
                    _logger,
                    operation=call.operation,
                    symbol=call.operation,
                    elapsed_ms=elapsed_ms,
                    remaining_ms=0,
                    layer="sidecar",
                    message="upstream call timed out",
                )
                raise TimeoutError(f"call timed out after {timeout_seconds}s")
            try:
                kind, payload = out.get(timeout=min(remaining, 0.25))
            except queue.Empty:
                if not proc.is_alive():
                    if proc.exitcode != 0:
                        raise RuntimeError(f"child process exited with code {proc.exitcode}")
                    elapsed_ms = int((time.monotonic() - started) * 1000)
                    log_timeout_event(
                        _logger,
                        operation=call.operation,
                        symbol=call.operation,
                        elapsed_ms=elapsed_ms,
                        remaining_ms=max(0, int(remaining * 1000)),
                        layer="sidecar",
                        message="upstream call timed out",
                    )
                    raise TimeoutError(f"call timed out after {timeout_seconds}s")
                continue
            if kind == "err":
                raise payload
            return payload
    finally:
        if proc.is_alive():
            proc.terminate()
            proc.join(timeout=0.5)
        if proc.is_alive():
            proc.kill()
            proc.join(timeout=0.5)


def _env_timeout(key: str, default: int) -> int:
    raw = os.environ.get(key, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def fetch_timeout_seconds() -> int:
    return _env_timeout("MARKET_PROVIDER_FETCH_TIMEOUT", DEFAULT_FETCH_TIMEOUT_SECONDS)


def fetch_upstream_timeout_seconds(deadline_remaining: int) -> int:
    """Per historical-source call cap: min(180s, remaining fetch budget)."""
    return max(1, min(DEFAULT_FETCH_UPSTREAM_TIMEOUT_SECONDS, deadline_remaining))


def resolve_timeout_seconds() -> int:
    return _env_timeout("MARKET_PROVIDER_RESOLVE_TIMEOUT", DEFAULT_RESOLVE_TIMEOUT_SECONDS)


def resolve_deadline_seconds() -> float:
    return float(_env_timeout("MARKET_PROVIDER_RESOLVE_DEADLINE", DEFAULT_RESOLVE_DEADLINE_SECONDS))


def mutual_fund_name_fetch_timeout_seconds() -> int:
    return _env_timeout(
        "MARKET_PROVIDER_MUTUAL_FUND_FETCH_TIMEOUT",
        DEFAULT_MUTUAL_FUND_NAME_FETCH_TIMEOUT_SECONDS,
    )


def log_timeout_event(
    logger,
    *,
    operation: str,
    symbol: str,
    elapsed_ms: int,
    remaining_ms: int,
    layer: str,
    message: str = "deadline event",
) -> None:
    """Emit unified timeout/deadline observability fields."""
    logger.info(
        "%s operation=%s symbol=%s elapsed_ms=%d remaining_ms=%d layer=%s",
        message,
        operation,
        symbol,
        elapsed_ms,
        remaining_ms,
        layer,
    )
