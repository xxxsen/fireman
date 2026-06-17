"""Tests for cn_mutual_fund source kind detection."""

from unittest.mock import patch

from fireman_market_provider.adapters.classification import detect_cn_mutual_fund_source_kind


def test_detect_open_fund_for_hybrid_name() -> None:
    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value="华夏成长混合",
    ):
        assert detect_cn_mutual_fund_source_kind("000001") == "open_fund"


def test_detect_open_fund_from_resolved_name() -> None:
    assert detect_cn_mutual_fund_source_kind("000001", resolved_name="华夏成长混合") == "open_fund"


def test_detect_money_fund_from_name() -> None:
    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value="某某货币基金A",
    ):
        assert detect_cn_mutual_fund_source_kind("000009") == "money_fund"


def test_detect_lof_name_still_uses_open_fund() -> None:
    assert detect_cn_mutual_fund_source_kind("501018", resolved_name="测试LOF") == "open_fund"


def test_detect_lof_from_readonly_cache_uses_open_fund() -> None:
    with patch(
        "fireman_market_provider.adapters.names.lookup_cn_mutual_fund_name_readonly",
        return_value="某某LOF基金",
    ):
        assert detect_cn_mutual_fund_source_kind("501018") == "open_fund"
