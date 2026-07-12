"""API-driven sidecar worker lifecycle."""

from __future__ import annotations

import json
import os
import random
import socket
import threading
import time
import uuid
from dataclasses import dataclass
from typing import Any

from ..logutil import get_logger
from .config import WorkerConfig, worker_enabled
from .errors import TaskFailure
from .executors import execute_task
from .goclient import GoAPIError, GoInternalClient, WorkerTask

logger = get_logger(__name__)

TASK_TYPES = ["asset_directory_sync", "asset_history_sync", "fx_rate_sync"]


@dataclass
class _ActiveAttempt:
    task_id: str
    token: str


class WorkerRunner:
    def __init__(self, config: WorkerConfig) -> None:
        self._config = config
        self._client = GoInternalClient(config.internal_api_url, config.http_timeout_seconds)
        self._worker_id = f"sidecar_worker:{socket.gethostname()}:{os.getpid()}:{uuid.uuid4()}"
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._active_lock = threading.Lock()
        self._active: _ActiveAttempt | None = None

    def start(self) -> None:
        self._thread = threading.Thread(
            target=self._claim_loop, name="worker-claim", daemon=True
        )
        self._thread.start()
        logger.info(
            "worker started worker_id=%s internal_api=%s",
            self._worker_id,
            self._config.internal_api_url,
        )

    def stop(self) -> None:
        self._stop.set()
        with self._active_lock:
            active = self._active
        if active is not None:
            try:
                self._client.release(active.task_id, self._worker_id, active.token)
            except GoAPIError as exc:
                if exc.code != "task_lease_lost":
                    logger.warning("release task %s failed: %s", active.task_id, exc)
        if self._thread is not None:
            self._thread.join(timeout=5.0)
            self._thread = None

    def _claim_loop(self) -> None:
        while not self._stop.is_set():
            try:
                candidates = self._client.list_pending(TASK_TYPES)
            except GoAPIError as exc:
                logger.warning("list pending tasks failed: %s", exc)
                self._wait_for_next_poll()
                continue
            claimed = False
            for candidate in candidates:
                token = uuid.uuid4().hex
                try:
                    task = self._client.claim(candidate.id, self._worker_id, token)
                except GoAPIError as exc:
                    if exc.code == "task_claim_conflict":
                        continue
                    logger.warning("claim task %s failed: %s", candidate.id, exc)
                    continue
                claimed = True
                self._run_task(task, token)
                break
            if not claimed:
                self._wait_for_next_poll()

    def _wait_for_next_poll(self) -> None:
        jittered = self._config.poll_interval_seconds * random.uniform(0.8, 1.2)
        self._stop.wait(jittered)

    def _run_task(self, task: WorkerTask, token: str) -> None:
        logger.info("task %s claimed type=%s version=%d", task.id, task.type, task.version_no)
        lost = threading.Event()
        canceled = threading.Event()
        heartbeat_stop = threading.Event()
        with self._active_lock:
            self._active = _ActiveAttempt(task.id, token)
        heartbeat = threading.Thread(
            target=self._heartbeat_loop,
            args=(task, token, heartbeat_stop, lost, canceled),
            name=f"heartbeat-{task.id[:12]}",
            daemon=True,
        )
        heartbeat.start()
        try:
            result = self._execute(task)
            if lost.is_set():
                logger.warning("task %s result discarded after lease loss", task.id)
                return
            if canceled.is_set() or self._stop.is_set():
                self._report_until_accepted(
                    task, token, lost, "canceled",
                    error_code="canceled_by_user", error_message="task canceled",
                )
                return
            result_key = self._upload_until_accepted(task, token, result, lost)
            if result_key and not lost.is_set():
                self._report_until_accepted(
                    task, token, lost, "success", result_key=result_key
                )
        except TaskFailure as exc:
            self._report_until_accepted(
                task, token, lost, "failed", retryable=exc.retryable,
                error_code=exc.error_code, error_message=exc.message,
            )
        except Exception as exc:  # noqa: BLE001
            logger.exception("task %s crashed", task.id)
            self._report_until_accepted(
                task, token, lost, "failed", retryable=True,
                error_code="worker_internal_error", error_message=str(exc)[:500],
            )
        finally:
            heartbeat_stop.set()
            heartbeat.join(timeout=2.0)
            with self._active_lock:
                if self._active and self._active.task_id == task.id:
                    self._active = None

    def _execute(self, task: WorkerTask) -> dict[str, Any]:
        try:
            payload = json.loads(task.payload_json or "{}")
        except json.JSONDecodeError as exc:
            raise TaskFailure("invalid_task_payload", "task payload is not valid JSON") from exc
        if not isinstance(payload, dict):
            raise TaskFailure("invalid_task_payload", "task payload must be an object")
        return execute_task(task.type, payload)

    def _heartbeat_loop(
        self, task: WorkerTask, token: str, stop: threading.Event,
        lost: threading.Event, canceled: threading.Event,
    ) -> None:
        while not stop.wait(self._config.heartbeat_interval_seconds):
            try:
                current = self._client.heartbeat(
                    task.id, self._worker_id, token,
                    task.progress_current, task.progress_total, task.phase or "fetching",
                )
            except GoAPIError as exc:
                if exc.code in {"task_lease_lost", "task_already_terminal"}:
                    lost.set()
                    return
                logger.warning("heartbeat task %s failed: %s", task.id, exc)
                continue
            if current.cancel_requested:
                canceled.set()
                return

    def _upload_until_accepted(
        self, task: WorkerTask, token: str, result: dict[str, Any], lost: threading.Event
    ) -> str:
        delay = 1.0
        while not lost.is_set() and not self._stop.is_set():
            try:
                return self._client.upload_result(task.id, self._worker_id, token, result)
            except GoAPIError as exc:
                if exc.code in {"task_lease_lost", "task_cancel_requested"}:
                    lost.set()
                    return ""
                logger.warning("resource upload for task %s failed: %s", task.id, exc)
                self._stop.wait(delay)
                delay = min(delay * 2, 30.0)
        return ""

    def _report_until_accepted(
        self, task: WorkerTask, token: str, lost: threading.Event, outcome: str,
        *, result_key: str = "", retryable: bool = False,
        error_code: str = "", error_message: str = "",
    ) -> None:
        delay = 1.0
        while not lost.is_set() and not self._stop.is_set():
            try:
                accepted = self._client.report(
                    task.id, self._worker_id, token, outcome,
                    result_key=result_key, retryable=retryable,
                    error_code=error_code, error_message=error_message,
                )
                logger.info("task %s result accepted status=%s", task.id, accepted.status)
                return
            except GoAPIError as exc:
                if exc.code in {
                    "task_lease_lost", "task_result_conflict", "task_already_terminal"
                }:
                    lost.set()
                    return
                if exc.code == "task_cancel_requested" and outcome != "canceled":
                    outcome, result_key = "canceled", ""
                    retryable = False
                    error_code, error_message = "canceled_by_user", "task canceled"
                    continue
                logger.warning("result report for task %s failed: %s", task.id, exc)
                self._stop.wait(delay)
                delay = min(delay * 2, 30.0)


_runner: WorkerRunner | None = None


def start_worker_from_env() -> WorkerRunner | None:
    global _runner
    if not worker_enabled():
        logger.info("worker disabled via FIREMAN_WORKER_ENABLED")
        return None
    if _runner is None:
        _runner = WorkerRunner(WorkerConfig.from_env())
        _runner.start()
    return _runner


def stop_worker() -> None:
    global _runner
    if _runner is not None:
        _runner.stop()
        _runner = None
