"""Task executors keyed by worker task type.

Each executor takes the parsed payload dict and returns the standardized
result JSON dict (the resource payload). Failures raise TaskFailure with a
stable error code.
"""

from __future__ import annotations

from typing import Any, Callable

from ..errors import TaskFailure
from .directory import execute_directory_sync
from .fx import execute_fx_sync
from .history import execute_history_sync

_EXECUTORS: dict[str, Callable[[dict[str, Any]], dict[str, Any]]] = {
    "asset_directory_sync": execute_directory_sync,
    "asset_history_sync": execute_history_sync,
    "fx_rate_sync": execute_fx_sync,
}


def execute_task(task_type: str, payload: dict[str, Any]) -> dict[str, Any]:
    executor = _EXECUTORS.get(task_type)
    if executor is None:
        raise TaskFailure("unsupported_task_type", f"unsupported task type {task_type}")
    return executor(payload)
