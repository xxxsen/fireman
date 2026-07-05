"""cn_mutual_fund candidate sources are fixed and independent of names."""

import pandas as pd
import pytest

from fireman_market_provider.adapters.registry import fetch_instrument
from fireman_market_provider.schemas import FetchRequest
from fireman_market_provider.timeout_util import register_test_dispatch


def _request(resolved_name: str | None) -> FetchRequest:
    return FetchRequest(
        market="CN",
        instrument_type="cn_mutual_fund",
        source_code="007194",
        start_date=None,
        end_date="2024-06-30",
        adjust_policy="none",
        resolved_name=resolved_name,
    )


def _nav_frame() -> pd.DataFrame:
    return pd.DataFrame(
        {
            "净值日期": ["2024-01-02", "2024-01-03"],
            "单位净值": [1.01, 1.02],
        }
    )


@pytest.mark.parametrize("resolved_name", ["长城短债A", "普通基金A", None])
def test_cn_mutual_fund_source_candidates_do_not_depend_on_name_keywords(
    resolved_name: str | None,
) -> None:
    """The same fixed candidate order runs for every fund name (or no name).

    Success is decided by parseable history points, never by name keywords:
    here 累计净值走势 returns an empty frame, so the fetch falls through to
    单位净值走势 regardless of what the fund is called.
    """
    calls: list[tuple[str, str]] = []

    def open_fund(symbol: str, indicator: str, period: str) -> pd.DataFrame:
        del period
        calls.append(("fund_open_fund_info_em", indicator))
        if indicator == "累计净值走势":
            return pd.DataFrame()
        return _nav_frame()

    def unexpected(*args: object, **kwargs: object) -> pd.DataFrame:
        raise AssertionError("later candidates must not run after a success")

    register_test_dispatch("fund_open_fund_info_em", open_fund)
    register_test_dispatch("fund_money_fund_info_em", unexpected)
    register_test_dispatch("fund_financial_fund_info_em", unexpected)

    data = fetch_instrument(_request(resolved_name))

    assert calls == [
        ("fund_open_fund_info_em", "累计净值走势"),
        ("fund_open_fund_info_em", "单位净值走势"),
    ]
    assert data.source_name == "ak.fund_open_fund_info_em:单位净值走势"
    assert data.source_kind == "open_fund"
    assert len(data.points) == 2


def test_money_fund_source_reached_without_name_keyword() -> None:
    """A money fund with no 货币 in its name still reaches money_fund source."""

    def empty(*args: object, **kwargs: object) -> pd.DataFrame:
        return pd.DataFrame()

    def money_fund(symbol: str) -> pd.DataFrame:
        del symbol
        return pd.DataFrame(
            {
                "净值日期": ["2024-01-02", "2024-01-03"],
                "每万份收益": [0.6, 0.61],
                "单位净值": [1.0, 1.0],
            }
        )

    register_test_dispatch("fund_open_fund_info_em", empty)
    register_test_dispatch("fund_money_fund_info_em", money_fund)

    data = fetch_instrument(_request("某某基金B"))
    assert data.source_name == "ak.fund_money_fund_info_em"
    assert data.source_kind == "money_fund"
    assert data.points
