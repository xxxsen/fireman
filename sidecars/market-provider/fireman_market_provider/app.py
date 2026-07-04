"""FastAPI application for the Fireman market data worker sidecar.

Since td/078 the sidecar is a pure task worker: it claims worker_tasks from
the shared SQLite DB, executes market data fetches, uploads results through
the Go internal API and drives task terminal states. The old synchronous
resolve/fetch/metadata HTTP endpoints are gone; HTTP only serves /healthz
for container health checks.
"""

from __future__ import annotations

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


def create_app() -> FastAPI:
    configure_logging()

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        # Preheat the mutual fund name cache off the request path; never block
        # startup (and therefore /healthz) on upstream availability.
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

    @app.get("/healthz", response_model=HealthResponse)
    def healthz() -> HealthResponse:
        return HealthResponse(status="ok")

    return app


app = create_app()
