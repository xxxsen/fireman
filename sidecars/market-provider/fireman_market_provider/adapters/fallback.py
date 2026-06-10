"""Ordered multi-source fetch with timeout and error collection."""

from __future__ import annotations

import pandas as pd

from ..logutil import get_logger
from ..timeout_util import UpstreamCall, call_with_timeout, fetch_timeout_seconds

logger = get_logger(__name__)


def try_sources(
    label: str,
    sources: list[tuple[str, UpstreamCall]],
) -> tuple[pd.DataFrame, str]:
    """Try providers in order until one returns a non-empty DataFrame."""
    errors: list[str] = []
    logger.info("fetch %s: trying %d source(s)", label, len(sources))
    for source_name, call in sources:
        try:
            df = call_with_timeout(call, fetch_timeout_seconds())
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
