"""Logging helpers for the market provider sidecar."""

from __future__ import annotations

import logging
import os

LOGGER_NAME = "fireman.market_provider"


def get_logger(name: str | None = None) -> logging.Logger:
    return logging.getLogger(name or LOGGER_NAME)


def configure_logging(level: str | None = None) -> None:
    """Configure root logging once for the sidecar process."""
    resolved = (level or os.getenv("FIREMAN_LOG_LEVEL", "INFO")).upper()
    numeric = getattr(logging, resolved, logging.INFO)
    root = logging.getLogger()
    if root.handlers:
        root.setLevel(numeric)
        return
    logging.basicConfig(
        level=numeric,
        format="%(asctime)s %(levelname)s [%(name)s] %(message)s",
    )
