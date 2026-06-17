"""FastAPI application for the Fireman market provider sidecar."""

from __future__ import annotations

import threading
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from .adapters import fetch_instrument, resolve_instrument
from .adapters.names import (
    cn_mutual_fund_name_cache_status,
    refresh_cn_mutual_fund_names,
    warm_cn_mutual_fund_name_cache,
)
from .logutil import configure_logging, get_logger
from .provider_errors import ProviderError, provider_error_from_exception
from .schemas import (
    FetchData,
    FetchRequest,
    FetchResponse,
    HealthResponse,
    MetadataRefreshData,
    MetadataRefreshRequest,
    MetadataRefreshResponse,
    ResolveRequest,
    ResolveResponse,
)

logger = get_logger(__name__)


def _structured_error_response(error_code: str, message: str, status_code: int) -> JSONResponse:
    return JSONResponse(
        status_code=status_code,
        content={
            "code": 1,
            "error_code": error_code,
            "message": message,
            "data": None,
        },
    )


def _provider_error_response(exc: ProviderError) -> JSONResponse:
    return _structured_error_response(exc.error_code, exc.message, exc.http_status)


def _summarize_validation_error(exc: RequestValidationError) -> str:
    parts: list[str] = []
    for err in exc.errors():
        loc = ".".join(str(p) for p in err.get("loc", ()) if p != "body")
        msg = err.get("msg", "invalid value")
        parts.append(f"{loc}: {msg}" if loc else msg)
    return "; ".join(parts) or "invalid request body"


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
        yield

    app = FastAPI(
        title="fireman-market-provider",
        version="0.2.0",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )

    @app.exception_handler(ProviderError)
    def _handle_provider_error(_request: Request, exc: ProviderError) -> JSONResponse:
        return _provider_error_response(exc)

    @app.exception_handler(RequestValidationError)
    def _handle_validation_error(_request: Request, exc: RequestValidationError) -> JSONResponse:
        # Request-body validation failures join the single structured error
        # contract instead of FastAPI's default {"detail": [...]} 422 body.
        return _structured_error_response("invalid_request", _summarize_validation_error(exc), 422)

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
        except ProviderError:
            raise
        except Exception as exc:  # noqa: BLE001 - mapped to structured error envelope
            provider_exc = provider_error_from_exception(exc)
            if provider_exc.error_code in ("invalid_request", "instrument_not_found", "instrument_type_mismatch"):
                logger.warning(
                    "resolve rejected code=%s type=%s error_code=%s: %s",
                    payload.code,
                    payload.instrument_type,
                    provider_exc.error_code,
                    provider_exc.message,
                )
            else:
                logger.error(
                    "resolve failed code=%s type=%s error_code=%s: %s",
                    payload.code,
                    payload.instrument_type,
                    provider_exc.error_code,
                    provider_exc.message,
                )
            raise provider_exc from exc

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
        except ProviderError:
            raise
        except Exception as exc:  # noqa: BLE001 - mapped to structured error envelope
            provider_exc = provider_error_from_exception(exc)
            logger.error(
                "fetch failed code=%s type=%s market=%s error_code=%s: %s",
                payload.source_code,
                payload.instrument_type,
                payload.market,
                provider_exc.error_code,
                provider_exc.message,
            )
            raise provider_exc from exc

    @app.post("/v1/metadata/refresh", response_model=MetadataRefreshResponse)
    def refresh_metadata(payload: MetadataRefreshRequest) -> MetadataRefreshResponse:
        logger.info("POST /v1/metadata/refresh target=%s", payload.target)
        try:
            if payload.target == "cn_mutual_fund_names":
                names = refresh_cn_mutual_fund_names()
                status = cn_mutual_fund_name_cache_status()
                refreshed_at = status.get("refreshed_at")
                if not isinstance(refreshed_at, str) or not refreshed_at:
                    refreshed_at = ""
                cache_path = status.get("cache_path")
                if not isinstance(cache_path, str):
                    cache_path = ""
                return MetadataRefreshResponse(
                    code=0,
                    message="success",
                    data=MetadataRefreshData(
                        target=payload.target,
                        entry_count=len(names),
                        refreshed_at=refreshed_at,
                        cache_path=cache_path,
                    ),
                )
            raise ValueError(f"unsupported refresh target: {payload.target}")
        except ProviderError:
            raise
        except Exception as exc:  # noqa: BLE001 - mapped to structured error envelope
            provider_exc = provider_error_from_exception(exc)
            logger.error(
                "metadata refresh failed target=%s error_code=%s: %s",
                payload.target,
                provider_exc.error_code,
                provider_exc.message,
            )
            raise provider_exc from exc

    return app


app = create_app()
