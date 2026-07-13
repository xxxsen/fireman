"""Killable process boundary for one claimed worker task."""

from __future__ import annotations

import multiprocessing as mp
import os
import queue
import signal
from collections.abc import Callable
from typing import Any

from .errors import TaskFailure
from .executors import execute_task


class TaskProcessCanceled(Exception):
    pass


class TaskProcessLeaseLost(Exception):
    pass


class TaskProcessShutdown(Exception):
    pass


def _task_entry(
    executor: Callable[[str, dict[str, Any]], dict[str, Any]],
    task_type: str,
    payload: dict[str, Any],
    output: mp.Queue,
    ready: mp.Event,
) -> None:
    if hasattr(os, "setsid"):
        os.setsid()
    ready.set()
    try:
        result = executor(task_type, payload)
        output.put(("ok", result))
    except TaskFailure as exc:
        output.put(
            (
                "task_failure",
                {
                    "error_code": exc.error_code,
                    "message": exc.message,
                    "retryable": exc.retryable,
                },
            )
        )
    except BaseException as exc:  # noqa: BLE001
        output.put(("internal_error", f"{type(exc).__name__}: {exc}"[:500]))


class TaskProcessRunner:
    def __init__(
        self,
        executor: Callable[[str, dict[str, Any]], dict[str, Any]] = execute_task,
        poll_interval_seconds: float = 0.2,
    ) -> None:
        self._executor = executor
        self._poll_interval = poll_interval_seconds

    def run(
        self,
        task_type: str,
        payload: dict[str, Any],
        canceled: Any,
        lost: Any,
        stopped: Any,
    ) -> dict[str, Any]:
        ctx = mp.get_context("spawn")
        output: mp.Queue = ctx.Queue(maxsize=1)
        ready: mp.Event = ctx.Event()
        process = ctx.Process(
            target=_task_entry,
            args=(self._executor, task_type, payload, output, ready),
            daemon=False,
        )
        process.start()
        ready.wait(timeout=2.0)
        try:
            while True:
                if canceled.is_set():
                    self._terminate(process, ready.is_set())
                    raise TaskProcessCanceled()
                if lost.is_set():
                    self._terminate(process, ready.is_set())
                    raise TaskProcessLeaseLost()
                if stopped.is_set():
                    self._terminate(process, ready.is_set())
                    raise TaskProcessShutdown()
                try:
                    kind, value = output.get(timeout=self._poll_interval)
                except queue.Empty:
                    if not process.is_alive():
                        raise RuntimeError(
                            f"task process exited without a result: {process.exitcode}"
                        )
                    continue
                process.join(timeout=1.0)
                if kind == "ok":
                    if not isinstance(value, dict):
                        raise RuntimeError("task executor result must be an object")
                    return value
                if kind == "task_failure":
                    raise TaskFailure(
                        str(value["error_code"]),
                        str(value["message"]),
                        bool(value["retryable"]),
                    )
                raise RuntimeError(str(value))
        finally:
            if process.is_alive():
                self._terminate(process, ready.is_set())
            output.close()
            output.join_thread()

    @staticmethod
    def _terminate(process: mp.Process, process_group_ready: bool) -> None:
        if not process.is_alive():
            process.join(timeout=0.2)
            return
        if process_group_ready and hasattr(os, "killpg"):
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        else:
            process.terminate()
        process.join(timeout=1.0)
        if not process.is_alive():
            return
        if process_group_ready and hasattr(os, "killpg"):
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        else:
            process.kill()
        process.join(timeout=1.0)
