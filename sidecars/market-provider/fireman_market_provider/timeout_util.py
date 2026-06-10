"""Hard timeout wrapper for upstream AKShare calls."""

from __future__ import annotations

import concurrent.futures
import os
from collections.abc import Callable
from typing import TypeVar

T = TypeVar("T")

DEFAULT_TIMEOUT_SECONDS = 30
DEFAULT_FETCH_TIMEOUT_SECONDS = 180
DEFAULT_RESOLVE_TIMEOUT_SECONDS = 20


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


def call_with_timeout(fn: Callable[[], T], timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS) -> T:
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(fn)
        return future.result(timeout=timeout_seconds)
