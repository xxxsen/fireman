"""FastAPI application for the Fireman market data worker sidecar.

The sidecar is a pure task worker: it claims tasks through the Go internal
API, executes market data fetches and uploads results. Go owns task lifecycle
and finalization. The old synchronous
resolve/fetch/metadata HTTP endpoints are gone; HTTP only serves /healthz
for container health checks.
"""

from __future__ import annotations

import os
import threading
from contextlib import asynccontextmanager

from fastapi import FastAPI

from .adapters.names import (
    cn_mutual_fund_name_cache_status,
    warm_cn_mutual_fund_name_cache,
)
from .adapters.tickflow import reset_tickflow_client
from .logutil import configure_logging, get_logger
from .schemas import HealthResponse
from .worker.runner import start_worker_from_env, stop_worker

logger = get_logger(__name__)


def _start_mutual_fund_cache_warm() -> None:
    def _warm() -> None:
        try:
            warm_cn_mutual_fund_name_cache()
            status = cn_mutual_fund_name_cache_status()
            logger.info(
                "mutual fund name cache warm complete entries=%s refreshed_at=%s",
                status.get("entry_count"),
                status.get("refreshed_at"),
            )
        except Exception:  # noqa: BLE001
            logger.exception("mutual fund name cache warm failed")

    threading.Thread(target=_warm, daemon=True, name="mutual-fund-cache-warm").start()


def _startup_warm_enabled() -> bool:
    """Whether the startup name-cache warm thread may hit the real upstream.

    Tests must be able to start the app without triggering any network
    traffic, so both a dedicated kill switch and an explicit toggle exist.
    """
    if os.getenv("FIREMAN_DISABLE_STARTUP_WARM") == "1":
        return False
    raw = os.getenv("MARKET_PROVIDER_STARTUP_WARM_ENABLED", "true")
    return raw.strip().lower() not in {"0", "false", "no"}


def create_app() -> FastAPI:
    configure_logging()

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        # Preheat the mutual fund name cache off the request path; never block
        # startup (and therefore /healthz) on upstream availability.
        if _startup_warm_enabled():
            _start_mutual_fund_cache_warm()
        start_worker_from_env()
        yield
        stop_worker()
        # Release the cached TickFlow SDK client's connection pool on shutdown.
        reset_tickflow_client()

    app = FastAPI(
        title="fireman-market-provider",
        version="0.3.0",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )

    # Async on purpose: a sync `def` endpoint would go through AnyIO's
    # thread pool, which hangs in-process ASGI contract tests in some
    # environments. This zero-I/O health check never needs a thread.
    @app.get("/healthz", response_model=HealthResponse)
    async def healthz() -> HealthResponse:
        return HealthResponse(status="ok")

    return app


app = create_app()
