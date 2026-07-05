"""Tests for cn_mutual_fund metadata description (no FIRE classification)."""

from unittest.mock import patch

import pandas as pd

from fireman_market_provider.adapters.classification import (
    FundMeta,
    describe_cn_mutual_fund,
)


def test_describe_extracts_name_and_fund_type() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [3.45],
            "基金简称": ["华夏成长混合"],
            "基金类型": ["混合型基金"],
        }
    )
    meta = describe_cn_mutual_fund(df, "000001")
    assert meta.name == "华夏成长混合"
    assert meta.region == "domestic"
    assert meta.components == {"fund_type": "混合型基金", "region": "domestic"}


def test_describe_never_emits_fire_asset_class() -> None:
    # The provider must not decide equity/bond/cash: FundMeta has no such field.
    assert "asset_class" not in FundMeta.__dataclass_fields__


def test_describe_uses_name_cache_for_nav_frames() -> None:
    """akshare NAV frames omit 基金简称; description must use the name cache."""
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "单位净值": [1.0],
            "日增长率": [0.0],
        }
    )
    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value="华夏成长混合",
    ):
        meta = describe_cn_mutual_fund(df, "000001")
    assert meta.name == "华夏成长混合"
    assert meta.region == "domestic"


def test_describe_accepts_unrecognized_fund_types() -> None:
    """FOF/commodity/unknown types are metadata, never rejection reasons."""
    for name, fund_type in (
        ("测试混合FOF", "混合型FOF"),
        ("黄金ETF联接", "商品基金"),
        ("长城短债A", ""),
    ):
        df = pd.DataFrame(
            {
                "净值日期": ["2024-01-02"],
                "累计净值": [1.0],
                "基金简称": [name],
                **({"基金类型": [fund_type]} if fund_type else {}),
            }
        )
        meta = describe_cn_mutual_fund(df, "000003")
        assert meta.name == name


def test_describe_falls_back_to_symbol_without_any_name() -> None:
    df = pd.DataFrame({"净值日期": ["2024-01-02"], "单位净值": [1.0]})
    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value=None,
    ):
        meta = describe_cn_mutual_fund(df, "007194")
    assert meta.name == "007194"
