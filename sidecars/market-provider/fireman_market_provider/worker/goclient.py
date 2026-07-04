"""HTTP client for the Go internal API (resource upload + post-process).

resource_db is owned by the Go layer. The worker gzips its result JSON,
computes the sha256 of the compressed bytes, and uploads it through
POST /internal/resources. The sha256 doubles as the resource key, so retried
uploads are idempotent. The returned envelope is stored verbatim in
worker_tasks.result_data.
"""

from __future__ import annotations

import gzip
import hashlib
import json
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

from ..logutil import get_logger

logger = get_logger(__name__)

RESULT_SCHEMA_VERSION = 1


class GoAPIError(Exception):
    """Transport or protocol failure talking to the Go internal API."""


@dataclass(frozen=True)
class PostProcessOutcome:
    result: str  # success | retryable_error | permanent_error
    error_code: str = ""
    error_message: str = ""


class GoInternalClient:
    def __init__(self, base_url: str, timeout_seconds: float = 30.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._timeout = timeout_seconds

    def _post(self, path: str, body: bytes, headers: dict[str, str]) -> dict[str, Any]:
        req = urllib.request.Request(
            self._base_url + path, data=body, headers=headers, method="POST"
        )
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as exc:
            detail = ""
            try:
                detail = exc.read().decode("utf-8", errors="replace")[:500]
            except Exception:  # noqa: BLE001
                pass
            raise GoAPIError(f"{path} returned HTTP {exc.code}: {detail}") from exc
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            raise GoAPIError(f"{path} unreachable: {exc}") from exc
        try:
            return json.loads(raw)
        except json.JSONDecodeError as exc:
            raise GoAPIError(f"{path} returned invalid JSON") from exc

    def upload_result(self, result: dict[str, Any]) -> dict[str, Any]:
        """Gzip + upload a task result; returns the resource envelope dict."""
        raw = json.dumps(result, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        compressed = gzip.compress(raw, mtime=0)
        digest = hashlib.sha256(compressed).hexdigest()
        body = self._post(
            "/internal/resources",
            compressed,
            {
                "Content-Type": "application/octet-stream",
                "X-Fireman-Content-Type": "application/json",
                "X-Fireman-Content-Encoding": "gzip",
                "X-Fireman-Schema-Version": str(RESULT_SCHEMA_VERSION),
                "X-Fireman-Content-SHA256": digest,
            },
        )
        envelope = body.get("data")
        if not isinstance(envelope, dict) or not envelope.get("resource_key"):
            raise GoAPIError("/internal/resources returned no resource envelope")
        if envelope["resource_key"] != digest:
            raise GoAPIError(
                "resource key mismatch: expected sha256 "
                f"{digest}, got {envelope['resource_key']}"
            )
        return envelope

    def notify_post_process(self, task_id: str, version_no: int) -> PostProcessOutcome:
        body = json.dumps({"task_id": task_id, "version_no": version_no}).encode("utf-8")
        payload = self._post(
            f"/internal/tasks/{task_id}/post-process",
            body,
            {"Content-Type": "application/json"},
        )
        data = payload.get("data")
        if not isinstance(data, dict) or "result" not in data:
            raise GoAPIError("post-process response missing result classification")
        return PostProcessOutcome(
            result=str(data.get("result", "")),
            error_code=str(data.get("error_code", "") or ""),
            error_message=str(data.get("error_message", "") or ""),
        )
