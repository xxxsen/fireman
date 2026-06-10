package service

import "testing"

func TestResolveCandidateIdentity(t *testing.T) {
	got := resolveCandidateIdentity("sz150001", "sz150001", "lof", "SZ")
	want := "sz150001|sz150001|lof|SZ"
	if got != want {
		t.Fatalf("identity=%q want %q", got, want)
	}
}
