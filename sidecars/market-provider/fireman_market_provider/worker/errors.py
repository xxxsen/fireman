"""Worker task failure classification."""

from __future__ import annotations


class TaskFailure(Exception):
    """Marks a task as failed with a stable error code.

    ``error_message`` must stay free of secrets (API keys, upstream URLs with
    credentials); it flows to worker_tasks.error_message and to the frontend.
    """

    def __init__(self, error_code: str, message: str) -> None:
        super().__init__(f"{error_code}: {message}")
        self.error_code = error_code
        self.message = message


class SourceUnavailable(TaskFailure):
    """The pinned data source failed or is not applicable to the asset."""

    def __init__(self, message: str) -> None:
        super().__init__("source_unavailable", message)
