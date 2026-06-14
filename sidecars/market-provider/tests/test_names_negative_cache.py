
def test_xq_name_negative_cache_skips_repeat_upstream(monkeypatch) -> None:
    from fireman_market_provider.adapters.names import (
        _lookup_cn_exchange_fund_name_xq,
        reset_name_caches,
    )
    from fireman_market_provider.timeout_util import clear_test_dispatch, register_test_dispatch

    reset_name_caches()
    clear_test_dispatch()
    calls = {"count": 0}

    def fail_xq(*_args, **_kwargs):
        calls["count"] += 1
        raise TimeoutError("xq timeout")

    register_test_dispatch("fund_individual_basic_info_xq", fail_xq)
    assert _lookup_cn_exchange_fund_name_xq("510300") is None
    assert _lookup_cn_exchange_fund_name_xq("510300") is None
    assert calls["count"] == 1
