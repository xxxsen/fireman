"""FastAPI application for the Fireman market provider sidecar."""

from __future__ import annotations

from fastapi import FastAPI, HTTPException

from .adapters import fetch_instrument, resolve_instrument
from .logutil import configure_logging, get_logger
from .schemas import (
    FetchData,
    FetchRequest,
    FetchResponse,
    HealthResponse,
    ResolveRequest,
    ResolveResponse,
)

logger = get_logger(__name__)


def create_app() -> FastAPI:
    configure_logging()
    app = FastAPI(
        title="fireman-market-provider",
        version="0.2.0",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )

    @app.get("/healthz", response_model=HealthResponse)
    def healthz() -> HealthResponse:
        return HealthResponse(status="ok")

    @app.post("/v1/instruments/resolve", response_model=ResolveResponse)
    def resolve(payload: ResolveRequest) -> ResolveResponse:
        logger.info(
            "POST /v1/instruments/resolve market=%s type=%s code=%s",
            payload.market,
            payload.instrument_type,
            payload.code,
        )
        try:
            data = resolve_instrument(payload)
            return ResolveResponse(code=0, message="success", data=data)
        except ValueError as exc:
            logger.warning(
                "resolve rejected code=%s type=%s: %s",
                payload.code,
                payload.instrument_type,
                exc,
            )
            raise HTTPException(status_code=400, detail=str(exc)) from exc

    @app.post("/v1/instruments/fetch", response_model=FetchResponse)
    def fetch(payload: FetchRequest) -> FetchResponse:
        logger.info(
            "POST /v1/instruments/fetch market=%s type=%s code=%s",
            payload.market,
            payload.instrument_type,
            payload.source_code,
        )
        try:
            data: FetchData = fetch_instrument(payload)
            logger.info(
                "fetch ok code=%s points=%d source=%s quality=%s",
                payload.source_code,
                len(data.points),
                data.source_name,
                data.source_quality,
            )
            return FetchResponse(code=0, message="success", data=data)
        except ValueError as exc:
            logger.warning(
                "fetch rejected code=%s type=%s: %s",
                payload.source_code,
                payload.instrument_type,
                exc,
            )
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except TimeoutError as exc:
            logger.error(
                "fetch timeout code=%s type=%s",
                payload.source_code,
                payload.instrument_type,
            )
            raise HTTPException(status_code=504, detail="upstream timeout") from exc
        except Exception as exc:  # noqa: BLE001 - mapped to provider error envelope
            logger.exception(
                "fetch failed code=%s type=%s market=%s",
                payload.source_code,
                payload.instrument_type,
                payload.market,
            )
            return FetchResponse(code=1, message=str(exc), data=_empty_data(payload))

    return app


def _empty_data(payload: FetchRequest) -> FetchData:
    return FetchData(
        provider="akshare",
        provider_symbol=payload.source_code,
        name="",
        asset_class="equity",
        currency="CNY",
        point_type="adjusted_close",
        expense_ratio_status="unavailable",
        expense_ratio_components={},
        points=[],
        source_name="error",
        source_quality="empty",
    )


app = create_app()
