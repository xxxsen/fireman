"""Tests for ordered source fallback."""

import pandas as pd
import pytest

from fireman_market_provider.adapters.fallback import try_sources


def test_try_sources_first_success() -> None:
    df = pd.DataFrame({"date": ["2024-01-01"], "close": [1.0]})
    result, name = try_sources(
        "demo",
        [
            ("primary", lambda: df),
            ("secondary", lambda: (_ for _ in ()).throw(AssertionError("should not run"))),
        ],
    )
    assert name == "primary"
    assert len(result) == 1


def test_try_sources_skips_empty_and_uses_next() -> None:
    df = pd.DataFrame({"date": ["2024-01-02"], "close": [2.0]})
    result, name = try_sources(
        "demo",
        [
            ("empty", lambda: pd.DataFrame()),
            ("broken", lambda: (_ for _ in ()).throw(RuntimeError("boom"))),
            ("secondary", lambda: df),
        ],
    )
    assert name == "secondary"
    assert float(result["close"].iloc[0]) == 2.0


def test_try_sources_raises_when_all_fail() -> None:
    with pytest.raises(RuntimeError, match="demo fetch failed|empty; broken"):
        try_sources(
            "demo",
            [
                ("empty", lambda: pd.DataFrame()),
                ("broken", lambda: (_ for _ in ()).throw(RuntimeError("boom"))),
            ],
        )
