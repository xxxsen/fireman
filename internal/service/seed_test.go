package service

import (
	"testing"
)

func TestSeedStringRoundTrip(t *testing.T) {
	max := "9223372036854775807"
	v, err := ValidateSeedInput(max)
	if err != nil {
		t.Fatal(err)
	}
	if FormatSeedInt64(v) != max {
		t.Fatalf("round trip failed: %s", FormatSeedInt64(v))
	}
}

func TestFormatSeedInt64PanicsOnNegative(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for negative seed")
		}
	}()
	FormatSeedInt64(-1)
}

func TestSeedStringRejectsInvalid(t *testing.T) {
	cases := []string{"-1", "1.5", "1e10", "9223372036854775808", "abc", " 1"}
	for _, c := range cases {
		if _, err := ValidateSeedInput(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}
