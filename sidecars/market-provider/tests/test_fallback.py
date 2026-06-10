"""Tests for ordered source fallback."""

import pandas as pd
import pytest

from fireman_market_provider.adapters.fallback import try_sources
from fireman_market_provider.timeout_util import UpstreamCall, clear_test_dispatch, register_test_dispatch


@pytest.fixture(autouse=True)
def _reset_dispatch() -> None:
    clear_test_dispatch()
    yield
    clear_test_dispatch()


def test_try_sources_first_success() -> None:
    df = pd.DataFrame({"date": ["2024-01-01"], "close": [1.0]})
    register_test_dispatch("primary_df", lambda: df)
    register_test_dispatch("secondary_df", lambda: (_ for _ in ()).throw(AssertionError("should not run")))
    result, name = try_sources(
        "demo",
        [
            ("primary", UpstreamCall("primary_df")),
            ("secondary", UpstreamCall("secondary_df")),
        ],
    )
    assert name == "primary"
    assert len(result) == 1


def test_try_sources_skips_empty_and_uses_next() -> None:
    df = pd.DataFrame({"date": ["2024-01-02"], "close": [2.0]})
    register_test_dispatch("empty_df", lambda: pd.DataFrame())
    register_test_dispatch("broken_df", lambda: (_ for _ in ()).throw(RuntimeError("boom")))
    register_test_dispatch("secondary_df", lambda: df)
    result, name = try_sources(
        "demo",
        [
            ("empty", UpstreamCall("empty_df")),
            ("broken", UpstreamCall("broken_df")),
            ("secondary", UpstreamCall("secondary_df")),
        ],
    )
    assert name == "secondary"
    assert float(result["close"].iloc[0]) == 2.0


def test_try_sources_raises_when_all_fail() -> None:
    register_test_dispatch("empty_df", lambda: pd.DataFrame())
    register_test_dispatch("broken_df", lambda: (_ for _ in ()).throw(RuntimeError("boom")))
    with pytest.raises(RuntimeError, match="demo fetch failed|empty; broken"):
        try_sources(
            "demo",
            [
                ("empty", UpstreamCall("empty_df")),
                ("broken", UpstreamCall("broken_df")),
            ],
        )
