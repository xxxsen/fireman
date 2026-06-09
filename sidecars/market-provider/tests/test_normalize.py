"""Normalization unit tests (no network)."""

import pandas as pd

from fireman_market_provider.normalize import normalize_dataframe


def test_normalize_sorts_dedupes_and_filters() -> None:
    df = pd.DataFrame(
        {
            "日期": ["2024-01-02", "2024-01-01", "2024-01-02", "bad"],
            "收盘": [10.0, 9.0, 11.0, -1.0],
        }
    )
    points = normalize_dataframe(df)
    assert [p.date for p in points] == ["2024-01-01", "2024-01-02"]
    assert points[-1].value == 11.0


def test_normalize_empty_when_missing_columns() -> None:
    df = pd.DataFrame({"foo": [1, 2]})
    assert normalize_dataframe(df) == []
