"""Hard timeout wrapper for upstream AKShare calls via terminable child processes."""

from __future__ import annotations

import multiprocessing as mp
import os
import queue
import time
from collections.abc import Callable
from typing import TypeVar

T = TypeVar("T")

DEFAULT_TIMEOUT_SECONDS = 30
DEFAULT_FETCH_TIMEOUT_SECONDS = 180
DEFAULT_RESOLVE_TIMEOUT_SECONDS = 5
DEFAULT_RESOLVE_DEADLINE_SECONDS = 5


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


def _worker_entry(fn: Callable[[], T], out: mp.Queue) -> None:
    try:
        out.put(("ok", fn()))
    except Exception as exc:  # noqa: BLE001
        out.put(("err", exc))


def call_with_timeout(fn: Callable[[], T], timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS) -> T:
    """Run fn in a child process; terminate the child on timeout."""
    if timeout_seconds <= 0:
        return fn()
    if os.environ.get("MARKET_PROVIDER_DISABLE_SUBPROCESS", "").strip() == "1":
        return fn()

    start_method = os.environ.get("MARKET_PROVIDER_SUBPROCESS_START", "fork")
    try:
        ctx = mp.get_context(start_method)
    except ValueError:
        ctx = mp.get_context("fork")
    out: mp.Queue = ctx.Queue(maxsize=1)
    proc = ctx.Process(target=_worker_entry, args=(fn, out), daemon=True)
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
