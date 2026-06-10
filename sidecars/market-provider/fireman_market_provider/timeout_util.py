"""Hard timeout wrapper for upstream AKShare calls via terminable child processes."""

from __future__ import annotations

import multiprocessing as mp
import os
import queue
import time
from dataclasses import dataclass
from typing import Any, TypeVar

T = TypeVar("T")

DEFAULT_TIMEOUT_SECONDS = 30
DEFAULT_FETCH_TIMEOUT_SECONDS = 180
DEFAULT_RESOLVE_TIMEOUT_SECONDS = 5
DEFAULT_RESOLVE_DEADLINE_SECONDS = 5

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

    import akshare as ak

    if call.operation == "fund_lof_code_id_map_em":
        if os.environ.get("MARKET_PROVIDER_TEST_SLOW_LOF") == "1":
            time.sleep(10)
            return {}
        from akshare.fund.fund_lof_em import _fund_lof_code_id_map_em

        return _fund_lof_code_id_map_em()

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
    deadline = time.monotonic() + timeout_seconds
    try:
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                proc.terminate()
                proc.join(timeout=1.0)
                if proc.is_alive():
                    proc.kill()
                    proc.join(timeout=1.0)
                raise TimeoutError(f"call timed out after {timeout_seconds}s")
            try:
                kind, payload = out.get(timeout=min(remaining, 0.25))
            except queue.Empty:
                if not proc.is_alive():
                    if proc.exitcode != 0:
                        raise RuntimeError(f"child process exited with code {proc.exitcode}")
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


def resolve_timeout_seconds() -> int:
    return _env_timeout("MARKET_PROVIDER_RESOLVE_TIMEOUT", DEFAULT_RESOLVE_TIMEOUT_SECONDS)


def resolve_deadline_seconds() -> float:
    return float(_env_timeout("MARKET_PROVIDER_RESOLVE_DEADLINE", DEFAULT_RESOLVE_DEADLINE_SECONDS))
