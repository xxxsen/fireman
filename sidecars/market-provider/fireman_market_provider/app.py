"""FastAPI application for the Fireman market provider sidecar."""

from __future__ import annotations

from fastapi import FastAPI, HTTPException

from .adapters import fetch_instrument
from .schemas import FetchData, FetchRequest, FetchResponse, HealthResponse


def create_app() -> FastAPI:
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

    @app.post("/v1/instruments/fetch", response_model=FetchResponse)
    def fetch(payload: FetchRequest) -> FetchResponse:
        try:
            data: FetchData = fetch_instrument(payload)
            return FetchResponse(code=0, message="success", data=data)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except TimeoutError as exc:
            raise HTTPException(status_code=504, detail="upstream timeout") from exc
        except Exception as exc:  # noqa: BLE001 - mapped to provider error envelope
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
