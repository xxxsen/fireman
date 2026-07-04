"""Worker configuration from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass


def _env_int(key: str, default: int) -> int:
    raw = os.environ.get(key, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def _env_float(key: str, default: float) -> float:
    raw = os.environ.get(key, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError:
        return default
    return value if value > 0 else default


@dataclass(frozen=True)
class WorkerConfig:
    """All tunables of the sidecar worker loop."""

    db_path: str
    internal_api_url: str
    poll_interval_seconds: float = 2.0
    heartbeat_interval_seconds: float = 10.0
    stale_scan_interval_seconds: float = 30.0
    stale_after_seconds: int = 60
    pre_complete_scan_interval_seconds: float = 2.0
    max_post_process_attempts: int = 10
    max_backoff_seconds: int = 300
    pre_complete_hard_timeout_seconds: int = 3600
    http_timeout_seconds: float = 30.0

    @staticmethod
    def from_env() -> "WorkerConfig":
        return WorkerConfig(
            db_path=os.environ.get("FIREMAN_DB_PATH", "/data/fireman.db"),
            internal_api_url=os.environ.get(
                "FIREMAN_INTERNAL_API_URL", "http://backend:8081"
            ).rstrip("/"),
            poll_interval_seconds=_env_float("FIREMAN_WORKER_POLL_INTERVAL", 2.0),
            heartbeat_interval_seconds=_env_float("FIREMAN_WORKER_HEARTBEAT_INTERVAL", 10.0),
            stale_scan_interval_seconds=_env_float("FIREMAN_WORKER_STALE_SCAN_INTERVAL", 30.0),
            stale_after_seconds=_env_int("FIREMAN_WORKER_STALE_AFTER", 60),
            pre_complete_scan_interval_seconds=_env_float(
                "FIREMAN_WORKER_PRE_COMPLETE_SCAN_INTERVAL", 2.0
            ),
            max_post_process_attempts=_env_int("FIREMAN_WORKER_MAX_POST_PROCESS_ATTEMPTS", 10),
            max_backoff_seconds=_env_int("FIREMAN_WORKER_MAX_BACKOFF", 300),
            pre_complete_hard_timeout_seconds=_env_int(
                "FIREMAN_WORKER_PRE_COMPLETE_HARD_TIMEOUT", 3600
            ),
            http_timeout_seconds=_env_float("FIREMAN_WORKER_HTTP_TIMEOUT", 30.0),
        )


def worker_enabled() -> bool:
    """The worker starts unless explicitly disabled (tests, tooling)."""
    return os.environ.get("FIREMAN_WORKER_ENABLED", "true").strip().lower() not in (
        "0",
        "false",
        "no",
        "off",
    )
