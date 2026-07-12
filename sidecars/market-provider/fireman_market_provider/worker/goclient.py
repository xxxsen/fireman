"""Client for the Go-owned worker task control plane."""

from __future__ import annotations

import gzip
import hashlib
import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any

RESULT_SCHEMA_VERSION = 1
WORKER_TYPE = "sidecar_worker"


class GoAPIError(Exception):
    def __init__(self, message: str, status: int = 0, code: str = "") -> None:
        super().__init__(message)
        self.status = status
        self.code = code


@dataclass(frozen=True)
class WorkerTask:
    id: str
    version_no: int
    type: str
    status: str
    payload_json: str
    progress_current: int = 0
    progress_total: int = 0
    phase: str = ""
    cancel_requested: bool = False

    @staticmethod
    def from_json(value: dict[str, Any]) -> "WorkerTask":
        return WorkerTask(
            id=str(value["id"]),
            version_no=int(value["version_no"]),
            type=str(value["type"]),
            status=str(value["status"]),
            payload_json=str(value.get("payload_json", "{}")),
            progress_current=int(value.get("progress_current", 0)),
            progress_total=int(value.get("progress_total", 0)),
            phase=str(value.get("phase", "")),
            cancel_requested=bool(value.get("cancel_requested", False)),
        )


class GoInternalClient:
    def __init__(self, base_url: str, timeout_seconds: float = 30.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._timeout = timeout_seconds

    def _request(
        self, method: str, path: str, body: bytes | None = None,
        headers: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        req = urllib.request.Request(
            self._base_url + path, data=body, headers=headers or {}, method=method
        )
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as response:
                raw = response.read()
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            code = ""
            try:
                code = str(json.loads(raw).get("code", ""))
            except (json.JSONDecodeError, AttributeError):
                pass
            raise GoAPIError(
                f"{path} returned HTTP {exc.code}: {raw.decode(errors='replace')[:500]}",
                exc.code,
                code,
            ) from exc
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            raise GoAPIError(f"{path} unreachable: {exc}") from exc
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise GoAPIError(f"{path} returned invalid JSON") from exc
        if not isinstance(payload, dict):
            raise GoAPIError(f"{path} returned invalid response envelope")
        return payload

    def _post_json(self, path: str, value: dict[str, Any]) -> dict[str, Any]:
        return self._request(
            "POST", path, json.dumps(value, separators=(",", ":")).encode(),
            {"Content-Type": "application/json"},
        )

    def list_pending(self, task_types: list[str], limit: int = 20) -> list[WorkerTask]:
        query = urllib.parse.urlencode(
            {
                "worker_type": WORKER_TYPE,
                "status": "pending",
                "types": ",".join(task_types),
                "limit": str(limit),
            }
        )
        payload = self._request("GET", f"/internal/worker-tasks?{query}")
        data = payload.get("data", {})
        items = data.get("items", []) if isinstance(data, dict) else []
        return [WorkerTask.from_json(item) for item in items if isinstance(item, dict)]

    def claim(self, task_id: str, worker_id: str, claim_token: str) -> WorkerTask:
        payload = self._post_json(
            f"/internal/worker-tasks/{task_id}/claim",
            {"worker_type": WORKER_TYPE, "worker_id": worker_id, "claim_token": claim_token},
        )
        return WorkerTask.from_json(payload["data"])

    def heartbeat(
        self, task_id: str, worker_id: str, claim_token: str,
        current: int, total: int, phase: str,
    ) -> WorkerTask:
        payload = self._post_json(
            f"/internal/worker-tasks/{task_id}/heartbeat",
            {
                "worker_type": WORKER_TYPE, "worker_id": worker_id,
                "claim_token": claim_token, "progress_current": current,
                "progress_total": total, "phase": phase,
            },
        )
        return WorkerTask.from_json(payload["data"])

    def release(self, task_id: str, worker_id: str, claim_token: str) -> WorkerTask:
        payload = self._post_json(
            f"/internal/worker-tasks/{task_id}/release",
            {"worker_type": WORKER_TYPE, "worker_id": worker_id, "claim_token": claim_token},
        )
        return WorkerTask.from_json(payload["data"])

    def upload_result(
        self, task_id: str, worker_id: str, claim_token: str, result: dict[str, Any]
    ) -> str:
        raw = json.dumps(result, ensure_ascii=False, separators=(",", ":")).encode()
        compressed = gzip.compress(raw, mtime=0)
        digest = hashlib.sha256(compressed).hexdigest()
        payload = self._request(
            "POST", f"/internal/worker-tasks/{task_id}/resources", compressed,
            {
                "Content-Type": "application/octet-stream",
                "X-Fireman-Worker-Type": WORKER_TYPE,
                "X-Fireman-Worker-ID": worker_id,
                "X-Fireman-Claim-Token": claim_token,
                "X-Fireman-Content-Type": "application/json",
                "X-Fireman-Content-Encoding": "gzip",
                "X-Fireman-Schema-Version": str(RESULT_SCHEMA_VERSION),
                "X-Fireman-Content-SHA256": digest,
            },
        )
        data = payload.get("data", {})
        result_key = str(data.get("result_key", "")) if isinstance(data, dict) else ""
        if result_key != f"resource:{digest}":
            raise GoAPIError("resource upload returned an unexpected result key")
        return result_key

    def report(
        self, task_id: str, worker_id: str, claim_token: str, outcome: str,
        *, result_key: str = "", retryable: bool = False,
        error_code: str = "", error_message: str = "",
    ) -> WorkerTask:
        payload = self._post_json(
            f"/internal/worker-tasks/{task_id}/result",
            {
                "worker_type": WORKER_TYPE, "worker_id": worker_id,
                "claim_token": claim_token, "outcome": outcome,
                "result_key": result_key, "retryable": retryable,
                "error_code": error_code, "error_message": error_message,
            },
        )
        return WorkerTask.from_json(payload["data"])
