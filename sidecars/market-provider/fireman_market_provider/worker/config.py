"""Worker configuration from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass


def _env_float(key: str, default: float) -> float:
    raw = os.environ.get(key, "").strip()
    try:
        value = float(raw) if raw else default
    except ValueError:
        return default
    return value if value > 0 else default


@dataclass(frozen=True)
class WorkerConfig:
    internal_api_url: str
    poll_interval_seconds: float = 2.0
    heartbeat_interval_seconds: float = 10.0
    http_timeout_seconds: float = 30.0
    cancel_poll_interval_seconds: float = 1.0

    @staticmethod
    def from_env() -> "WorkerConfig":
        return WorkerConfig(
            internal_api_url=os.environ.get(
                "FIREMAN_INTERNAL_API_URL", "http://backend:8081"
            ).rstrip("/"),
            poll_interval_seconds=_env_float("FIREMAN_WORKER_POLL_INTERVAL", 2.0),
            heartbeat_interval_seconds=_env_float(
                "FIREMAN_WORKER_HEARTBEAT_INTERVAL", 10.0
            ),
            http_timeout_seconds=_env_float("FIREMAN_WORKER_HTTP_TIMEOUT", 30.0),
            cancel_poll_interval_seconds=_env_float(
                "FIREMAN_WORKER_CANCEL_POLL_INTERVAL", 1.0
            ),
        )


def worker_enabled() -> bool:
    return os.environ.get("FIREMAN_WORKER_ENABLED", "true").strip().lower() not in (
        "0",
        "false",
        "no",
        "off",
    )
