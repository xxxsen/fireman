"""Ordered multi-source fetch with timeout and error collection."""

from __future__ import annotations

import time

import pandas as pd

from ..logutil import get_logger
from ..timeout_util import (
    UpstreamCall,
    call_with_timeout,
    fetch_timeout_seconds,
    fetch_upstream_timeout_seconds,
    log_timeout_event,
)

logger = get_logger(__name__)


def try_sources(
    label: str,
    sources: list[tuple[str, UpstreamCall]],
    deadline: float | None = None,
) -> tuple[pd.DataFrame, str]:
    """Try providers in order until one returns a non-empty DataFrame."""
    if deadline is None:
        deadline = time.monotonic() + fetch_timeout_seconds()
    errors: list[str] = []
    logger.info("fetch %s: trying %d source(s)", label, len(sources))
    for source_name, call in sources:
        remaining = int(deadline - time.monotonic())
        if remaining <= 0:
            log_timeout_event(
                logger,
                operation=f"fetch_{label}",
                symbol=label,
                elapsed_ms=fetch_timeout_seconds() * 1000,
                remaining_ms=0,
                layer="sidecar",
                message="fetch deadline exceeded",
            )
            raise TimeoutError(f"fetch deadline exceeded for {label}")
        timeout = fetch_upstream_timeout_seconds(remaining)
        try:
            df = call_with_timeout(call, timeout)
            if df is not None and not df.empty:
                logger.info(
                    "fetch %s: success via %s (%d rows)",
                    label,
                    source_name,
                    len(df),
                )
                return df, source_name
            msg = f"{source_name}: empty"
            errors.append(msg)
            logger.warning("fetch %s: %s", label, msg)
        except TimeoutError:
            logger.error("fetch %s: %s timed out", label, source_name)
            raise
        except Exception as exc:  # noqa: BLE001 - collect fallback errors
            msg = f"{source_name}: {exc}"
            errors.append(msg)
            logger.warning("fetch %s: %s failed: %s", label, source_name, exc)
    summary = "; ".join(errors) or f"{label} fetch failed"
    logger.error("fetch %s: all sources failed: %s", label, summary)
    raise RuntimeError(summary)
