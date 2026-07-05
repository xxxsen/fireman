"""HTTP contract tests for the worker sidecar app (healthz only)."""

from fastapi.testclient import TestClient

from fireman_market_provider import create_app


def test_healthz_returns_ok(monkeypatch) -> None:
    monkeypatch.setenv("FIREMAN_WORKER_ENABLED", "false")
    with TestClient(create_app()) as client:
        response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_legacy_fetch_endpoints_removed(monkeypatch) -> None:
    """The sidecar is a pure worker; the sync HTTP surface is gone."""
    monkeypatch.setenv("FIREMAN_WORKER_ENABLED", "false")
    with TestClient(create_app()) as client:
        for path in (
            "/v1/instruments/resolve",
            "/v1/instruments/fetch",
            "/v1/metadata/refresh",
        ):
            assert client.post(path, json={}).status_code == 404, path
