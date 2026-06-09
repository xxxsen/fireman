"""Hard timeout wrapper for upstream AKShare calls."""

from __future__ import annotations

import concurrent.futures
from collections.abc import Callable
from typing import TypeVar

T = TypeVar("T")

DEFAULT_TIMEOUT_SECONDS = 30


def call_with_timeout(fn: Callable[[], T], timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS) -> T:
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(fn)
        return future.result(timeout=timeout_seconds)
