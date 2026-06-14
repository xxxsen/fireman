"""Tests for cn_mutual_fund classification helpers."""

from unittest.mock import patch

import pandas as pd

from fireman_market_provider.adapters.classification import classify_cn_mutual_fund


def test_classify_hybrid_open_fund_as_equity() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [3.45],
            "基金简称": ["华夏成长混合"],
            "基金类型": ["混合型基金"],
        }
    )
    meta = classify_cn_mutual_fund(df, "000001")
    assert meta.asset_class == "equity"
    assert meta.region == "domestic"


def test_classify_open_fund_from_nav_history_and_name_cache() -> None:
    """akshare NAV frames omit 基金简称; classification must use name cache."""
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
        meta = classify_cn_mutual_fund(df, "000001")
    assert meta.name == "华夏成长混合"
    assert meta.asset_class == "equity"
    assert meta.region == "domestic"


def test_classify_qdii_as_foreign_equity() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [1.2],
            "基金简称": ["测试QDII基金"],
            "基金类型": ["QDII"],
        }
    )
    meta = classify_cn_mutual_fund(df, "000002")
    assert meta.asset_class == "equity"
    assert meta.region == "foreign"


def test_classify_fof_as_unsupported() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [1.0],
            "基金简称": ["测试混合FOF"],
            "基金类型": ["混合型FOF"],
        }
    )
    meta = classify_cn_mutual_fund(df, "000003")
    assert meta.asset_class is None


def test_classify_commodity_as_unsupported() -> None:
    df = pd.DataFrame(
        {
            "净值日期": ["2024-01-02"],
            "累计净值": [1.0],
            "基金简称": ["黄金ETF联接"],
            "基金类型": ["商品基金"],
        }
    )
    meta = classify_cn_mutual_fund(df, "000004")
    assert meta.asset_class is None
