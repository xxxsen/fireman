"""FastAPI application for the Fireman market provider sidecar."""

from __future__ import annotations

import threading

from fastapi import FastAPI, HTTPException

from .adapters import fetch_instrument, resolve_instrument
from .adapters.names import (
    cn_mutual_fund_name_cache_status,
    refresh_cn_mutual_fund_names,
    warm_cn_mutual_fund_name_cache,
)
from .logutil import configure_logging, get_logger
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


def create_app() -> FastAPI:
    configure_logging()
    app = FastAPI(
        title="fireman-market-provider",
        version="0.2.0",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )

    @app.on_event("startup")
    def start_mutual_fund_cache_warm() -> None:
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
        except TimeoutError as exc:
            logger.error(
                "resolve timeout code=%s type=%s",
                payload.code,
                payload.instrument_type,
            )
            raise HTTPException(status_code=504, detail="upstream timeout") from exc
        except RuntimeError as exc:
            logger.error(
                "resolve upstream failed code=%s type=%s: %s",
                payload.code,
                payload.instrument_type,
                exc,
            )
            raise HTTPException(status_code=503, detail="upstream unavailable") from exc
        except Exception as exc:  # noqa: BLE001
            logger.exception(
                "resolve failed code=%s type=%s",
                payload.code,
                payload.instrument_type,
            )
            raise HTTPException(status_code=503, detail="upstream unavailable") from exc

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
        except TimeoutError as exc:
            logger.error("metadata refresh timeout target=%s", payload.target)
            raise HTTPException(status_code=504, detail="upstream timeout") from exc
        except RuntimeError as exc:
            logger.error("metadata refresh upstream failed target=%s: %s", payload.target, exc)
            raise HTTPException(status_code=503, detail="upstream unavailable") from exc
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except Exception as exc:  # noqa: BLE001
            logger.exception("metadata refresh failed target=%s", payload.target)
            raise HTTPException(status_code=503, detail="upstream unavailable") from exc

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
