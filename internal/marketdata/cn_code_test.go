package marketdata

import "testing"

func TestHasCNExchangePrefix(t *testing.T) {
	if !HasCNExchangePrefix("sh510300") {
		t.Fatal("expected sh prefix")
	}
	if HasCNExchangePrefix("510300") {
		t.Fatal("bare code should not have prefix")
	}
}

func TestRequiresCNResolve(t *testing.T) {
	if !RequiresCNResolve("CN", "cn_exchange_fund", "510300") {
		t.Fatal("bare CN fund code requires resolve")
	}
	if RequiresCNResolve("CN", "cn_exchange_fund", "sh510300") {
		t.Fatal("prefixed code should not require resolve")
	}
}
