"""Tests for instrument display name resolution."""

import pandas as pd
from unittest.mock import patch

from fireman_market_provider.adapters.names import (
    lookup_cn_exchange_fund_name,
    name_from_dataframe,
    reset_name_caches,
    resolve_cn_exchange_fund_name,
)


def setup_function() -> None:
    reset_name_caches()


def test_name_from_dataframe_prefers_fund_short_name() -> None:
    df = pd.DataFrame({"基金简称": ["沪深300ETF"], "日期": ["2024-01-02"]})
    assert name_from_dataframe(df, "510300") == "沪深300ETF"


def test_resolve_cn_exchange_fund_name_uses_spot_lookup() -> None:
    hist = pd.DataFrame({"日期": ["2024-01-02"], "收盘": [1.0]})
    spot = pd.DataFrame({"代码": ["510300"], "名称": ["沪深300ETF华泰柏瑞"]})
    with patch("akshare.fund_etf_spot_em", return_value=spot):
        assert resolve_cn_exchange_fund_name("510300", hist) == "沪深300ETF华泰柏瑞"
        assert lookup_cn_exchange_fund_name("510300") == "沪深300ETF华泰柏瑞"
