"""AKShare adapter registry."""

from .registry import fetch_instrument
from .resolve import resolve_instrument

__all__ = ["fetch_instrument", "resolve_instrument"]
